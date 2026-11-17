package meta_test

import (
	"context"
	"testing"

	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/meta/metatest"
)

func TestPullCountsSurviveClose(t *testing.T) {
	ctx := context.Background()
	dsn := metatest.DSN(t)
	db, err := meta.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	m := meta.Manifest{Digest: "sha256:aa", MediaType: "application/vnd.oci.image.manifest.v1+json", Content: []byte("{}")}
	if err := db.PutManifest(ctx, "app", m, nil, "v1"); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetManifest(ctx, "app", "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	db.MarkPulled(ctx, got)
	db.MarkPulled(ctx, got)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = meta.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err = db.GetManifest(ctx, "app", "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	var repo int64
	if err := db.R.QueryRow(`SELECT pull_count FROM repositories WHERE name = 'app'`).Scan(&repo); err != nil {
		t.Fatal(err)
	}
	if got.PullCount != 2 || repo != 2 {
		t.Fatalf("manifest %d, repository %d pulls; want 2 each", got.PullCount, repo)
	}
}
