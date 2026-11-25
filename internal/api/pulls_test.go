package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/p-arndt/doreg/internal/meta"
)

func TestAPIPullCounts(t *testing.T) {
	e := apitNewEnv(t)
	ctx := context.Background()
	const d = "sha256:ab"
	if err := e.db.PutManifest(ctx, "app", meta.Manifest{Digest: d,
		MediaType: "application/vnd.oci.image.index.v1+json",
		Content:   []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`)}, nil, "v1", false); err != nil {
		t.Fatal(err)
	}
	m, err := e.db.GetManifest(ctx, "app", "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		e.db.MarkPulled(ctx, m)
	}
	if err := e.db.FlushPulls(ctx); err != nil {
		t.Fatal(err)
	}

	var list []struct {
		Pulls int64 `json:"pulls"`
	}
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/repos", nil), &list)
	var repo struct {
		Pulls int64 `json:"pulls"`
		Tags  []struct {
			Manifest struct {
				Pulls int64 `json:"pulls"`
			} `json:"manifest"`
		} `json:"tags"`
	}
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/repo?name=app", nil), &repo)
	var man struct {
		Pulls int64 `json:"pulls"`
	}
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/manifest?repo=app&ref=v1", nil), &man)

	if len(list) != 1 || list[0].Pulls != 3 || repo.Pulls != 3 || len(repo.Tags) != 1 ||
		repo.Tags[0].Manifest.Pulls != 3 || man.Pulls != 3 {
		t.Fatalf("list %+v, repo %+v, manifest %+v; want 3 pulls everywhere", list, repo, man)
	}
}
