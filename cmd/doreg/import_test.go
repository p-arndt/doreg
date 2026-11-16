package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/meta/metatest"
	"github.com/p-arndt/doreg/internal/oci"
	"github.com/p-arndt/doreg/internal/registry"
	"github.com/p-arndt/doreg/internal/storage"
)

const importTestPassword = "import-password"

// importTestRegistry starts a registry with the admin "admin"; it returns the
// URL and its database.
func importTestRegistry(t *testing.T, hash string) (string, *meta.DB) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := meta.Open(metatest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.UpsertUser(context.Background(), "admin", hash, meta.RoleAdmin)
	srv := httptest.NewServer(&registry.Handler{DB: db, Store: store, Auth: auth.New(db), Log: slog.New(slog.DiscardHandler)})
	t.Cleanup(srv.Close)
	return srv.URL, db
}

func importTestPush(t *testing.T, base, repo, tag string) {
	t.Helper()
	do := func(method, path string, body []byte, contentType string) {
		req, _ := http.NewRequest(method, base+path, bytes.NewReader(body))
		req.SetBasicAuth("admin", importTestPassword)
		req.Header.Set("Content-Type", contentType)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("%s %s: HTTP %d", method, path, resp.StatusCode)
		}
	}
	cfg := []byte(`{"architecture":"amd64","os":"linux","tag":"` + tag + `"}`)
	d := digest.FromBytes("sha256", cfg)
	do("POST", "/v2/"+repo+"/blobs/uploads/?digest="+d.String(), cfg, "application/octet-stream")
	m, _ := json.Marshal(oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest,
		Config: &oci.Descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: d.String(), Size: int64(len(cfg))},
		Layers: []oci.Descriptor{}})
	do("PUT", "/v2/"+repo+"/manifests/"+tag, m, oci.MediaTypeOCIManifest)
}

func TestImportCommand(t *testing.T) {
	hash, err := auth.HashPassword(importTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	from, _ := importTestRegistry(t, hash)
	to, dstDB := importTestRegistry(t, hash)
	importTestPush(t, from, "app", "1.0")
	importTestPush(t, from, "team/api", "2")
	env := map[string]string{"DOREG_FROM_PASSWORD": importTestPassword, "DOREG_TO_PASSWORD": importTestPassword}
	args := []string{"--from", from, "--from-user", "admin", "--to", to, "--to-user", "admin", "--repo", "team/*"}
	ctx := context.Background()

	var out bytes.Buffer
	if err := importCmd(ctx, append(args, "--dry-run"), &out, func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "team/api") || strings.Contains(out.String(), "app ") ||
		!strings.Contains(out.String(), "1 repositories, 1 tags") {
		t.Errorf("dry run output:\n%s", out.String())
	}
	if _, err := dstDB.RepoByName(ctx, "team/api"); err == nil {
		t.Fatal("dry run copied something")
	}

	out.Reset()
	if err := importCmd(ctx, args, &out, func(k string) string { return env[k] }); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "copied   team/api:2") || !strings.Contains(out.String(), "1 copied, 0 up to date, 0 failed") {
		t.Errorf("import output:\n%s", out.String())
	}
	if _, err := dstDB.RepoByName(ctx, "team/api"); err != nil {
		t.Errorf("team/api not imported: %v", err)
	}

	delete(env, "DOREG_TO_PASSWORD")
	if err := importCmd(ctx, args, io.Discard, func(k string) string { return env[k] }); err == nil ||
		!strings.Contains(err.Error(), "doreg:") {
		t.Errorf("missing doreg password: err = %v", err)
	}
}
