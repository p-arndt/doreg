package registry_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oci"
)

func TestChunkedUploadAndPull(t *testing.T) {
	e := newEnv(t)
	data := []byte("hello chunked world")
	d := digest.FromBytes("sha256", data)

	resp := e.do("POST", "/v2/team/app/blobs/uploads/", nil, nil)
	expect(t, resp, http.StatusAccepted)
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/v2/team/app/blobs/uploads/") || resp.Header.Get("Range") != "0-0" {
		t.Fatalf("bad upload start headers: %v", resp.Header)
	}

	resp = e.do("PATCH", loc, data[:5], map[string]string{"Content-Range": "0-4"})
	expect(t, resp, http.StatusAccepted)
	if got := resp.Header.Get("Range"); got != "0-4" {
		t.Fatalf("range after first chunk = %q", got)
	}
	// Out-of-order chunk.
	expect(t, e.do("PATCH", loc, data[5:], map[string]string{"Content-Range": "7-30"}), http.StatusRequestedRangeNotSatisfiable)
	// Docker streams chunks without Content-Range.
	expect(t, e.do("PATCH", loc, data[5:10], nil), http.StatusAccepted)
	expect(t, e.do("GET", loc, nil, nil), http.StatusNoContent)

	resp = e.do("PUT", loc+"?digest="+d.String(), data[10:], nil)
	expect(t, resp, http.StatusCreated)
	if resp.Header.Get("Docker-Content-Digest") != d.String() {
		t.Fatal("missing digest header")
	}

	resp = e.do("GET", "/v2/team/app/blobs/"+d.String(), nil, map[string]string{"Range": "bytes=6-12"})
	expect(t, resp, http.StatusPartialContent)
	if b, _ := io.ReadAll(resp.Body); string(b) != "chunked" {
		t.Fatalf("range body = %q", b)
	}
	// The blob is not linked to another repository.
	expect(t, e.do("HEAD", "/v2/other/blobs/"+d.String(), nil, nil), http.StatusNotFound)
	// Wrong digest is rejected.
	resp = e.do("POST", "/v2/team/app/blobs/uploads/?digest="+digest.FromBytes("sha256", []byte("x")).String(), data, nil)
	expect(t, resp, http.StatusBadRequest)
}

func TestManifestAndTags(t *testing.T) {
	e := newEnv(t)
	var digests []string
	for _, tag := range []string{"b", "a", "c"} {
		digests = append(digests, e.pushImage("app", tag, tag))
	}
	resp := e.do("HEAD", "/v2/app/manifests/a", nil, nil)
	expect(t, resp, http.StatusOK)
	if resp.Header.Get("Docker-Content-Digest") != digests[1] || resp.Header.Get("Content-Type") != oci.MediaTypeOCIManifest ||
		resp.Header.Get("Content-Length") == "" {
		t.Fatalf("bad manifest HEAD headers: %v", resp.Header)
	}

	resp = e.do("GET", "/v2/app/tags/list?n=2", nil, nil)
	expect(t, resp, http.StatusOK)
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"tags":["a","b"]`) || !strings.Contains(resp.Header.Get("Link"), "last=b") {
		t.Fatalf("page 1: %s %v", body, resp.Header.Get("Link"))
	}
	resp = e.do("GET", "/v2/app/tags/list?n=2&last=b", nil, nil)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"tags":["c"]`) {
		t.Fatalf("page 2: %s", body)
	}

	// A manifest referencing an unknown blob is rejected.
	missing := oci.Descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: digest.FromBytes("sha256", []byte("nope")).String(), Size: 4}
	m := `{"schemaVersion":2,"mediaType":"` + oci.MediaTypeOCIManifest + `","config":{"mediaType":"` + missing.MediaType + `","digest":"` + missing.Digest + `","size":4},"layers":[]}`
	resp = e.do("PUT", "/v2/app/manifests/bad", []byte(m), map[string]string{"Content-Type": oci.MediaTypeOCIManifest})
	expect(t, resp, http.StatusBadRequest)
	if b, _ := io.ReadAll(resp.Body); !strings.Contains(string(b), "MANIFEST_BLOB_UNKNOWN") {
		t.Fatalf("error body %s", b)
	}

	// Deleting a tag leaves the manifest reachable by digest.
	expect(t, e.do("DELETE", "/v2/app/manifests/a", nil, nil), http.StatusAccepted)
	expect(t, e.do("GET", "/v2/app/manifests/a", nil, nil), http.StatusNotFound)
	expect(t, e.do("GET", "/v2/app/manifests/"+digests[1], nil, nil), http.StatusOK)
}

func TestReferrers(t *testing.T) {
	e := newEnv(t)
	subject := e.pushImage("app", "v1", "img")
	empty := e.pushBlob("app", []byte("{}"))
	empty.MediaType = oci.MediaTypeOCIEmpty
	sig := e.putManifest("app", "", oci.Manifest{
		SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest, ArtifactType: "application/vnd.example.sig",
		Config: &empty, Layers: []oci.Descriptor{empty},
		Subject: &oci.Descriptor{MediaType: oci.MediaTypeOCIManifest, Digest: subject, Size: 1},
	})
	resp := e.do("GET", "/v2/app/referrers/"+subject, nil, nil)
	expect(t, resp, http.StatusOK)
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), sig) || !strings.Contains(string(b), "application/vnd.example.sig") {
		t.Fatalf("referrers: %s", b)
	}
	resp = e.do("GET", "/v2/app/referrers/"+subject+"?artifactType=other", nil, nil)
	b, _ = io.ReadAll(resp.Body)
	if strings.Contains(string(b), sig) || resp.Header.Get("OCI-Filters-Applied") != "artifactType" {
		t.Fatalf("filtered referrers: %s", b)
	}
}

func TestAuth(t *testing.T) {
	e := newEnv(t)
	e.pushImage("app", "v1", "x")
	ctx := t.Context()
	lib, err := e.db.EnsureProject(ctx, meta.DefaultProject)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := e.db.UserByName(ctx, "reader")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddMember(ctx, lib.ID, reader.ID, "", meta.ProjectRead); err != nil {
		t.Fatal(err)
	}

	resp := e.doAs("", "GET", "/v2/", nil, nil)
	expect(t, resp, http.StatusUnauthorized)
	if !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic") {
		t.Fatal("missing Basic challenge")
	}
	expect(t, e.doAs("", "GET", "/v2/app/manifests/v1", nil, nil), http.StatusUnauthorized)
	expect(t, e.doAs("reader", "GET", "/v2/app/manifests/v1", nil, nil), http.StatusOK)
	expect(t, e.doAs("reader", "POST", "/v2/app/blobs/uploads/", nil, nil), http.StatusForbidden)

	if err := e.db.SetRepoPublic(t.Context(), "app", true); err != nil {
		t.Fatal(err)
	}
	expect(t, e.doAs("", "GET", "/v2/app/manifests/v1", nil, nil), http.StatusOK)
	// Public pull never means anonymous push.
	expect(t, e.doAs("", "POST", "/v2/app/blobs/uploads/", nil, nil), http.StatusUnauthorized)
}

func TestCrossRepoMount(t *testing.T) {
	e := newEnv(t)
	desc := e.pushBlob("src", []byte("shared layer"))
	resp := e.do("POST", "/v2/dst/blobs/uploads/?mount="+desc.Digest+"&from=src", nil, nil)
	expect(t, resp, http.StatusCreated)
	expect(t, e.do("HEAD", "/v2/dst/blobs/"+desc.Digest, nil, nil), http.StatusOK)
	// Unknown source falls back to a regular upload session.
	missing := digest.FromBytes("sha256", []byte("nope")).String()
	expect(t, e.do("POST", "/v2/dst/blobs/uploads/?mount="+missing+"&from=src", nil, nil), http.StatusAccepted)
}
