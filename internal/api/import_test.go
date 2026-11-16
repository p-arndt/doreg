package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/p-arndt/doreg/internal/api"
	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/meta/metatest"
	"github.com/p-arndt/doreg/internal/oci"
	"github.com/p-arndt/doreg/internal/registry"
	"github.com/p-arndt/doreg/internal/storage"
)

// apitSourceRegistry starts a second registry with user "old" and the images
// app:1.0 and team/api:2, standing in for the registry being migrated from.
func apitSourceRegistry(t *testing.T) string {
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
	db.UpsertUser(context.Background(), "old", apitPasswordHash(t), meta.RoleAdmin)
	srv := httptest.NewServer(&registry.Handler{DB: db, Store: store, Auth: auth.New(db), Log: slog.New(slog.DiscardHandler)})
	t.Cleanup(srv.Close)

	do := func(method, path string, body []byte, contentType string) {
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
		req.SetBasicAuth("old", apitPassword)
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
	for _, repo := range []string{"app", "team/api"} {
		cfg := []byte(`{"architecture":"amd64","os":"linux","repo":"` + repo + `"}`)
		d := digest.FromBytes("sha256", cfg)
		do("POST", "/v2/"+repo+"/blobs/uploads/?digest="+d.String(), cfg, "application/octet-stream")
		m, _ := json.Marshal(oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest,
			Config: &oci.Descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: d.String(), Size: int64(len(cfg))},
			Layers: []oci.Descriptor{}})
		tag := map[string]string{"app": "1.0", "team/api": "2"}[repo]
		do("PUT", "/v2/"+repo+"/manifests/"+tag, m, oci.MediaTypeOCIManifest)
	}
	return srv.URL
}

func TestAPIImportPreviewAndRun(t *testing.T) {
	e := apitNewEnv(t)
	src := apitSourceRegistry(t)
	source := map[string]any{"url": src, "username": "old", "password": apitPassword}

	var preview struct {
		Repositories []struct {
			Name string
			Tags []string
		}
	}
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodPost, "/api/import/preview", source), &preview)
	if len(preview.Repositories) != 2 || preview.Repositories[0].Name != "app" || preview.Repositories[1].Tags[0] != "2" {
		t.Fatalf("preview = %+v", preview)
	}

	e.admin.expect(http.StatusAccepted, http.MethodPost, "/api/import", source)
	var status struct{ Job *api.ImportJob }
	deadline := time.Now().Add(10 * time.Second)
	for {
		apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/import", nil), &status)
		if status.Job != nil && status.Job.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("import did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	j := status.Job
	if j.Status != "done" || j.Progress.TagsCopied != 2 || j.Actor != "admin" || j.Source != src {
		t.Fatalf("job = %+v", j)
	}
	if _, err := e.db.RepoByName(context.Background(), "team/api"); err != nil {
		t.Errorf("team/api not imported: %v", err)
	}
	rows, _ := e.db.Audit(context.Background(), meta.AuditQuery{Action: "import_finished", Limit: 5})
	if len(rows) != 1 || rows[0].Resource != src {
		t.Errorf("import_finished audit rows = %+v", rows)
	}
}

func TestAPIImportRejectsWrongSourceLogin(t *testing.T) {
	e := apitNewEnv(t)
	src := apitSourceRegistry(t)
	e.admin.expect(http.StatusBadRequest, http.MethodPost, "/api/import/preview",
		map[string]any{"url": src, "username": "old", "password": "wrong"})
	e.admin.expect(http.StatusBadRequest, http.MethodPost, "/api/import",
		map[string]any{"url": src, "username": "old", "password": "wrong"})
	e.admin.expect(http.StatusBadRequest, http.MethodPost, "/api/import/preview",
		map[string]any{"url": src, "username": "old", "password": apitPassword, "repositories": []string{"["}})
	var status struct{ Job *api.ImportJob }
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/import", nil), &status)
	if status.Job != nil {
		t.Errorf("a rejected import left a job: %+v", status.Job)
	}
}

func TestAPIImportAdminOnly(t *testing.T) {
	e := apitNewEnv(t)
	bob := e.login("bob")
	bob.expect(http.StatusForbidden, http.MethodGet, "/api/import", nil)
	bob.expect(http.StatusForbidden, http.MethodPost, "/api/import", map[string]any{"url": "http://127.0.0.1:1"})
	bob.expect(http.StatusForbidden, http.MethodPost, "/api/import/preview", map[string]any{"url": "http://127.0.0.1:1"})
	bob.expect(http.StatusForbidden, http.MethodPost, "/api/import/cancel", nil)
	e.admin.expect(http.StatusConflict, http.MethodPost, "/api/import/cancel", nil)
}
