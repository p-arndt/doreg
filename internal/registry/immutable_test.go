package registry_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oci"
)

// imageManifest pushes a config and a layer and returns a manifest for them
// without pushing it.
func (e *env) imageManifest(repo, content string) []byte {
	e.t.Helper()
	cfg := e.pushBlob(repo, []byte(`{"architecture":"amd64","os":"linux","x":"`+content+`"}`))
	cfg.MediaType = "application/vnd.oci.image.config.v1+json"
	layer := e.pushBlob(repo, []byte("layer-"+content))
	layer.MediaType = "application/vnd.oci.image.layer.v1.tar+gzip"
	body, _ := json.Marshal(oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest, Config: &cfg, Layers: []oci.Descriptor{layer}})
	return body
}

func (e *env) putTag(repo, tag string, body []byte) *http.Response {
	e.t.Helper()
	return e.do("PUT", "/v2/"+repo+"/manifests/"+tag, body, map[string]string{"Content-Type": oci.MediaTypeOCIManifest})
}

func TestImmutableTagsProject(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.db.CreateProject(ctx, meta.Project{Name: "locked", ImmutableTags: true}); err != nil {
		t.Fatal(err)
	}
	first := e.pushImage("locked/app", "v1", "a")

	// Re-pushing the same manifest under the same tag is a no-op and allowed.
	e.pushImage("locked/app", "v1", "a")

	// Moving the tag to another manifest is refused, and the tag keeps its target.
	other := e.imageManifest("locked/app", "b")
	resp := e.putTag("locked/app", "v1", other)
	expect(t, resp, http.StatusForbidden)
	if b, _ := io.ReadAll(resp.Body); !strings.Contains(string(b), "immutable") {
		t.Fatalf("error body %s", b)
	}
	m, err := e.db.GetManifest(ctx, "locked/app", "v1", false)
	if err != nil || m.Digest != first {
		t.Fatalf("v1 = %q, %v; want %s", m.Digest, err, first)
	}

	// New tags and digest pushes still work.
	expect(t, e.putTag("locked/app", "v2", other), http.StatusCreated)

	// Other projects are unaffected.
	e.pushImage("open/app", "v1", "a")
	e.pushImage("open/app", "v1", "b")

	// Turning the setting off allows moving the tag again.
	p, _ := e.db.ProjectByName(ctx, "locked")
	p.ImmutableTags = false
	if err := e.db.UpdateProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	expect(t, e.putTag("locked/app", "v1", other), http.StatusCreated)
}

func TestImmutableTagsGlobal(t *testing.T) {
	e := newEnv(t)
	e.h.ImmutableTags = true
	e.pushImage("team/app", "v1", "a")
	e.pushImage("plain", "latest", "a")
	expect(t, e.putTag("team/app", "v1", e.imageManifest("team/app", "b")), http.StatusForbidden)
	expect(t, e.putTag("plain", "latest", e.imageManifest("plain", "b")), http.StatusForbidden)
}
