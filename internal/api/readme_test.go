package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oci"
)

const (
	readmeConfigMediaType = "application/vnd.oci.image.config.v1+json"
	readmeLayerMediaType  = "application/octet-stream"
)

// readmeRepo is the subset of GET /api/repo that README support adds.
type readmeRepo struct {
	Readme      string `json:"readme"`
	ReadmeHTML  string `json:"readmeHtml"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

func readmeGet(t *testing.T, c *apitClient, name string) readmeRepo {
	t.Helper()
	var out readmeRepo
	apitDecode(t, c.expect(http.StatusOK, http.MethodGet, "/api/repo?name="+name, nil), &out)
	return out
}

func readmeEnsureRepos(t *testing.T, e *apitEnv, names ...string) {
	t.Helper()
	ctx := context.Background()
	err := e.db.Tx(ctx, func(tx *sql.Tx) error {
		for _, name := range names {
			if _, err := meta.EnsureRepo(ctx, tx, name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// readmePushBlob uploads a blob through the registry handler as admin and
// returns its digest.
func readmePushBlob(t *testing.T, base, repo string, data []byte) string {
	t.Helper()
	d := digest.FromBytes("sha256", data).String()
	req, err := http.NewRequest(http.MethodPost, base+"/v2/"+repo+"/blobs/uploads/?digest="+d, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("admin", apitPassword)
	req.Header.Set("Content-Type", readmeLayerMediaType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("blob upload to %s: status %d", repo, resp.StatusCode)
	}
	return d
}

// readmePutManifest pushes a manifest under tag, or by its digest when tag is
// empty, and returns its descriptor.
func readmePutManifest(t *testing.T, base, repo, tag, mediaType string, m oci.Manifest) oci.Descriptor {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	d := digest.FromBytes("sha256", data).String()
	ref := tag
	if ref == "" {
		ref = d
	}
	req, err := http.NewRequest(http.MethodPut, base+"/v2/"+repo+"/manifests/"+ref, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("admin", apitPassword)
	req.Header.Set("Content-Type", mediaType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("manifest push %s:%s: status %d: %s", repo, ref, resp.StatusCode, body)
	}
	return oci.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(data))}
}

// readmePushImage pushes a single-platform image whose config carries labels.
// An empty tag pushes it by digest only.
func readmePushImage(t *testing.T, base, repo, tag string, labels map[string]string) oci.Descriptor {
	t.Helper()
	cfg, err := json.Marshal(map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"config":       map[string]any{"Labels": labels},
		"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	layer := []byte("layer " + repo + " " + tag)
	cfgDigest := readmePushBlob(t, base, repo, cfg)
	layerDigest := readmePushBlob(t, base, repo, layer)
	return readmePutManifest(t, base, repo, tag, oci.MediaTypeOCIManifest, oci.Manifest{
		SchemaVersion: 2,
		MediaType:     oci.MediaTypeOCIManifest,
		Config:        &oci.Descriptor{MediaType: readmeConfigMediaType, Digest: cfgDigest, Size: int64(len(cfg))},
		Layers:        []oci.Descriptor{{MediaType: readmeLayerMediaType, Digest: layerDigest, Size: int64(len(layer))}},
	})
}

func TestReadmeSaveAndRead(t *testing.T) {
	e := apitTeamEnv(t)
	readmeEnsureRepos(t, e, "team/app")
	bob := e.login("bob")

	bob.expect(http.StatusOK, http.MethodPut, "/api/repo/readme?name=team/app",
		map[string]string{"readme": "# Hello\r\n\r\nWorld"})

	want := "# Hello\n\nWorld"
	got := readmeGet(t, bob, "team/app")
	if got.Readme != want {
		t.Errorf("readme = %q, want %q (CRLF should be normalised)", got.Readme, want)
	}
	if !strings.Contains(got.ReadmeHTML, "<h1") {
		t.Errorf("readmeHtml = %q, want an <h1>", got.ReadmeHTML)
	}

	var saved readmeRepo
	apitDecode(t, bob.expect(http.StatusOK, http.MethodPut, "/api/repo/readme?name=team/app",
		map[string]string{"readme": "## Second"}), &saved)
	if saved.Readme != "## Second" || !strings.Contains(saved.ReadmeHTML, "<h2") {
		t.Errorf("save response = %+v, want readme and <h2> html", saved)
	}

	// A repository without a README returns empty fields.
	readmeEnsureRepos(t, e, "team/empty")
	if empty := readmeGet(t, bob, "team/empty"); empty.Readme != "" || empty.ReadmeHTML != "" {
		t.Errorf("empty repository has readme %+v", empty)
	}
}

func TestReadmeSaveRequiresPush(t *testing.T) {
	e := apitTeamEnv(t)
	readmeEnsureRepos(t, e, "team/app", "other/app")
	body := map[string]string{"readme": "# Nope"}

	e.login("carol").expect(http.StatusForbidden, http.MethodPut, "/api/repo/readme?name=team/app", body)
	e.login("bob").expect(http.StatusForbidden, http.MethodPut, "/api/repo/readme?name=other/app", body)
	e.guest().expect(http.StatusUnauthorized, http.MethodPut, "/api/repo/readme?name=team/app", body)

	if got := readmeGet(t, e.admin, "team/app"); got.Readme != "" {
		t.Errorf("rejected save stored readme %q", got.Readme)
	}
}

func TestReadmeSizeLimit(t *testing.T) {
	e := apitTeamEnv(t)
	readmeEnsureRepos(t, e, "team/app")
	bob := e.login("bob")

	exact := strings.Repeat("a", 64<<10)
	bob.expect(http.StatusOK, http.MethodPut, "/api/repo/readme?name=team/app", map[string]string{"readme": exact})

	tooLarge := map[string]string{"readme": exact + "a"}
	var msg struct {
		Error string `json:"error"`
	}
	apitDecode(t, bob.expect(http.StatusBadRequest, http.MethodPut, "/api/repo/readme?name=team/app", tooLarge), &msg)
	if msg.Error != "readme is larger than 64 KiB" {
		t.Errorf("error = %q", msg.Error)
	}
	bob.expect(http.StatusBadRequest, http.MethodPost, "/api/repo/readme/preview", tooLarge)
}

func TestReadmeIsSanitised(t *testing.T) {
	e := apitTeamEnv(t)
	readmeEnsureRepos(t, e, "team/app")
	bob := e.login("bob")

	src := "<script>alert(1)</script>\n\n[x](javascript:alert(1))\n\n<img src=x onerror=alert(1)>"
	bob.expect(http.StatusOK, http.MethodPut, "/api/repo/readme?name=team/app", map[string]string{"readme": src})

	html := readmeGet(t, bob, "team/app").ReadmeHTML
	for _, bad := range []string{"<script", "javascript:", "onerror"} {
		if strings.Contains(strings.ToLower(html), bad) {
			t.Errorf("readmeHtml contains %q: %s", bad, html)
		}
	}
}

func TestReadmePublicBrowse(t *testing.T) {
	e := apitTeamEnv(t)
	readmeEnsureRepos(t, e, "open/app", "team/app")
	e.admin.expect(http.StatusOK, http.MethodPut, "/api/repo/readme?name=open/app", map[string]string{"readme": "# Public"})
	e.admin.expect(http.StatusOK, http.MethodPut, "/api/repo/readme?name=team/app", map[string]string{"readme": "# Private"})
	e.api.PublicBrowse = true
	guest := e.guest()

	got := readmeGet(t, guest, "open/app")
	if got.Readme != "# Public" || !strings.Contains(got.ReadmeHTML, "<h1") {
		t.Errorf("guest sees %+v on open/app", got)
	}
	guest.expect(http.StatusUnauthorized, http.MethodGet, "/api/repo?name=team/app", nil)
	guest.expect(http.StatusUnauthorized, http.MethodPut, "/api/repo/readme?name=open/app", map[string]string{"readme": "# Edit"})
	guest.expect(http.StatusUnauthorized, http.MethodPost, "/api/repo/readme/preview", map[string]string{"readme": "# Edit"})
}

func TestReadmePreview(t *testing.T) {
	e := apitTeamEnv(t)
	bob := e.login("bob")

	var out struct {
		HTML string `json:"html"`
	}
	apitDecode(t, bob.expect(http.StatusOK, http.MethodPost, "/api/repo/readme/preview",
		map[string]string{"readme": "## Draft\n\n<script>x</script>"}), &out)
	if !strings.Contains(out.HTML, "<h2") || strings.Contains(out.HTML, "<script") {
		t.Errorf("preview html = %q", out.HTML)
	}
	// Preview needs a session, but no project permission: carol may preview too.
	e.login("carol").expect(http.StatusOK, http.MethodPost, "/api/repo/readme/preview", map[string]string{"readme": "# Hi"})
	e.guest().expect(http.StatusUnauthorized, http.MethodPost, "/api/repo/readme/preview", map[string]string{"readme": "# Hi"})
}

func TestRepoLabels(t *testing.T) {
	e := apitTeamEnv(t)
	srv := httptest.NewServer(e.api.Registry)
	t.Cleanup(srv.Close)
	base := srv.URL
	labels := map[string]string{
		"org.opencontainers.image.description": "Demo",
		"org.opencontainers.image.source":      "https://github.com/acme/demo",
	}

	// Single image: labels come from the config.
	readmePushImage(t, base, "team/app", "latest", labels)

	// Source that is not http(s) is dropped; the description is kept.
	readmePushImage(t, base, "team/js", "latest", map[string]string{
		"org.opencontainers.image.description": "Scripted",
		"org.opencontainers.image.source":      "javascript:x",
	})

	// Index: labels come from the first platform image, not the attestation.
	// The attestation has no tag, so it cannot win the "newest tag" lookup.
	platform := readmePushImage(t, base, "team/multi", "child", labels)
	platform.Platform = &oci.Platform{OS: "linux", Architecture: "amd64"}
	attestation := readmePushImage(t, base, "team/multi", "", nil)
	attestation.Platform = &oci.Platform{OS: "unknown", Architecture: "unknown"}
	readmePutManifest(t, base, "team/multi", "latest", oci.MediaTypeOCIIndex, oci.Manifest{
		SchemaVersion: 2,
		MediaType:     oci.MediaTypeOCIIndex,
		Manifests:     []oci.Descriptor{attestation, platform},
	})

	bob := e.login("bob")
	app := readmeGet(t, bob, "team/app")
	if app.Description != "Demo" || app.Source != "https://github.com/acme/demo" {
		t.Errorf("team/app labels = %q, %q", app.Description, app.Source)
	}
	js := readmeGet(t, bob, "team/js")
	if js.Description != "Scripted" || js.Source != "" {
		t.Errorf("team/js labels = %q, %q; want description kept and javascript source dropped", js.Description, js.Source)
	}
	multi := readmeGet(t, bob, "team/multi")
	if multi.Description != "Demo" || multi.Source != "https://github.com/acme/demo" {
		t.Errorf("team/multi labels = %q, %q", multi.Description, multi.Source)
	}

	var items []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	apitDecode(t, bob.expect(http.StatusOK, http.MethodGet, "/api/repos", nil), &items)
	descs := map[string]string{}
	for _, it := range items {
		descs[it.Name] = it.Description
	}
	if descs["team/app"] != "Demo" || descs["team/js"] != "Scripted" || descs["team/multi"] != "Demo" {
		t.Errorf("list descriptions = %v", descs)
	}
}

func TestRepoLabelsOnlyForVisibleRepos(t *testing.T) {
	e := apitTeamEnv(t)
	srv := httptest.NewServer(e.api.Registry)
	t.Cleanup(srv.Close)
	readmePushImage(t, srv.URL, "other/app", "latest", map[string]string{
		"org.opencontainers.image.description": "Secret",
	})

	var items []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	apitDecode(t, e.login("bob").expect(http.StatusOK, http.MethodGet, "/api/repos", nil), &items)
	for _, it := range items {
		if it.Name == "other/app" {
			t.Errorf("bob sees other/app with description %q", it.Description)
		}
	}
}
