package registry_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/p-arndt/doreg/internal/oci"
)

func (e *env) pulls(query string, args ...any) int64 {
	e.t.Helper()
	if err := e.db.FlushPulls(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	var n int64
	if err := e.db.R.QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestPullCounts(t *testing.T) {
	e := newEnv(t)
	amd := e.pushImage("app", "", "amd64")
	arm := e.pushImage("app", "", "arm64")
	idx := e.putManifest("app", "v1", oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIIndex, Manifests: []oci.Descriptor{
		{MediaType: oci.MediaTypeOCIManifest, Digest: amd, Size: 1, Platform: &oci.Platform{OS: "linux", Architecture: "amd64"}},
		{MediaType: oci.MediaTypeOCIManifest, Digest: arm, Size: 1, Platform: &oci.Platform{OS: "linux", Architecture: "arm64"}},
	}})
	empty := e.pushBlob("app", []byte("{}"))
	empty.MediaType = oci.MediaTypeOCIEmpty
	sig := e.putManifest("app", "", oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest,
		ArtifactType: "application/vnd.example.sig", Config: &empty, Layers: []oci.Descriptor{empty},
		Subject: &oci.Descriptor{MediaType: oci.MediaTypeOCIIndex, Digest: idx, Size: 1}})
	single := e.pushImage("app", "solo", "solo")

	// Two multi-platform pulls (index, then the platform manifest), one
	// single-platform pull, one signature fetch, and HEADs that must not count.
	for range 2 {
		expect(t, e.do("GET", "/v2/app/manifests/v1", nil, nil), http.StatusOK)
		expect(t, e.do("GET", "/v2/app/manifests/"+amd, nil, nil), http.StatusOK)
	}
	expect(t, e.do("GET", "/v2/app/manifests/"+single, nil, nil), http.StatusOK)
	expect(t, e.do("GET", "/v2/app/manifests/"+sig, nil, nil), http.StatusOK)
	expect(t, e.do("HEAD", "/v2/app/manifests/v1", nil, nil), http.StatusOK)
	expect(t, e.do("HEAD", "/v2/app/manifests/solo", nil, nil), http.StatusOK)

	for d, want := range map[string]int64{idx: 2, amd: 2, arm: 0, single: 1, sig: 1} {
		if got := e.pulls(`SELECT pull_count FROM manifests WHERE digest = ?`, d); got != want {
			t.Errorf("manifest %s: %d pulls, want %d", d, got, want)
		}
	}
	if got := e.pulls(`SELECT pull_count FROM repositories WHERE name = 'app'`); got != 3 {
		t.Errorf("repository: %d pulls, want 3", got)
	}
}
