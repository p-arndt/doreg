package registry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/meta/metatest"
	"github.com/p-arndt/doreg/internal/oci"
	"github.com/p-arndt/doreg/internal/registry"
	"github.com/p-arndt/doreg/internal/storage"
)

type env struct {
	t     *testing.T
	srv   *httptest.Server
	db    *meta.DB
	store *storage.FS
}

const testPassword = "secret-password"

func newEnv(t *testing.T) *env {
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
	hash, _ := auth.HashPassword(testPassword)
	ctx := context.Background()
	db.UpsertUser(ctx, "admin", hash, meta.RoleAdmin)
	db.UpsertUser(ctx, "reader", hash, meta.RoleUser)
	h := &registry.Handler{DB: db, Store: store, Auth: auth.New(db), Log: slog.New(slog.DiscardHandler)}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, db: db, store: store}
}

func (e *env) do(method, path string, body []byte, hdr map[string]string) *http.Response {
	e.t.Helper()
	return e.doAs("admin", method, path, body, hdr)
}

func (e *env) doAs(user, method, path string, body []byte, hdr map[string]string) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	if user != "" {
		req.SetBasicAuth(user, testPassword)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func expect(t *testing.T, resp *http.Response, status int) {
	t.Helper()
	if resp.StatusCode != status {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: got %d, want %d: %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, status, b)
	}
}

func (e *env) pushBlob(repo string, data []byte) oci.Descriptor {
	e.t.Helper()
	d := digest.FromBytes("sha256", data)
	expect(e.t, e.do("POST", "/v2/"+repo+"/blobs/uploads/?digest="+d.String(), data, nil), http.StatusCreated)
	return oci.Descriptor{MediaType: "application/octet-stream", Digest: d.String(), Size: int64(len(data))}
}

// pushImage pushes a config and one layer and returns the manifest digest.
func (e *env) pushImage(repo, tag, content string) string {
	e.t.Helper()
	cfg := e.pushBlob(repo, []byte(`{"architecture":"amd64","os":"linux","x":"`+content+`"}`))
	cfg.MediaType = "application/vnd.oci.image.config.v1+json"
	layer := e.pushBlob(repo, []byte("layer-"+content))
	layer.MediaType = "application/vnd.oci.image.layer.v1.tar+gzip"
	return e.putManifest(repo, tag, oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest, Config: &cfg, Layers: []oci.Descriptor{layer}})
}

func (e *env) putManifest(repo, ref string, m oci.Manifest) string {
	e.t.Helper()
	body, _ := json.Marshal(m)
	d := digest.FromBytes("sha256", body)
	if ref == "" {
		ref = d.String()
	}
	resp := e.do("PUT", "/v2/"+repo+"/manifests/"+ref, body, map[string]string{"Content-Type": m.MediaType})
	expect(e.t, resp, http.StatusCreated)
	if got := resp.Header.Get("Docker-Content-Digest"); got != d.String() {
		e.t.Fatalf("digest header %q, want %q", got, d)
	}
	return d.String()
}

func (e *env) count(table string) int {
	e.t.Helper()
	var n int
	if err := e.db.R.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}
