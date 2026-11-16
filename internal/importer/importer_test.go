package importer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/importer"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/meta/metatest"
	"github.com/p-arndt/doreg/internal/oci"
	"github.com/p-arndt/doreg/internal/registry"
	"github.com/p-arndt/doreg/internal/storage"
)

const password = "secret-password"

var passwordHash = func() string {
	h, err := auth.HashPassword(password)
	if err != nil {
		panic(err)
	}
	return h
}()

type reg struct {
	t   *testing.T
	h   *registry.Handler
	db  *meta.DB
	srv *httptest.Server
}

// newReg starts a doreg registry with the users admin and reader.
func newReg(t *testing.T) *reg {
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
	ctx := context.Background()
	db.UpsertUser(ctx, "admin", passwordHash, meta.RoleAdmin)
	db.UpsertUser(ctx, "reader", passwordHash, meta.RoleUser)
	log := slog.New(slog.DiscardHandler)
	bus := events.New(db, log)
	bus.AllowPrivate = true
	h := &registry.Handler{DB: db, Store: store, Auth: auth.New(db), Events: bus, Log: log}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &reg{t: t, h: h, db: db, srv: srv}
}

func (r *reg) do(method, path string, body []byte, contentType string) *http.Response {
	r.t.Helper()
	req, _ := http.NewRequest(method, r.srv.URL+path, bytes.NewReader(body))
	req.SetBasicAuth("admin", password)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		r.t.Fatalf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return resp
}

func (r *reg) blob(repo string, data []byte, mediaType string) oci.Descriptor {
	d := digest.FromBytes("sha256", data)
	r.do("POST", "/v2/"+repo+"/blobs/uploads/?digest="+d.String(), data, "")
	return oci.Descriptor{MediaType: mediaType, Digest: d.String(), Size: int64(len(data))}
}

func (r *reg) manifest(repo, ref string, m oci.Manifest) oci.Descriptor {
	body, _ := json.Marshal(m)
	d := digest.FromBytes("sha256", body)
	if ref == "" {
		ref = d.String()
	}
	r.do("PUT", "/v2/"+repo+"/manifests/"+ref, body, m.MediaType)
	return oci.Descriptor{MediaType: m.MediaType, Digest: d.String(), Size: int64(len(body))}
}

// image pushes an image with a config and one layer; layer content may repeat
// across images to share blobs.
func (r *reg) image(repo, ref, config, layer string) oci.Descriptor {
	cfg := r.blob(repo, []byte(`{"architecture":"amd64","os":"linux","c":"`+config+`"}`), "application/vnd.oci.image.config.v1+json")
	l := r.blob(repo, []byte(layer), "application/vnd.oci.image.layer.v1.tar+gzip")
	return r.manifest(repo, ref, oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest, Config: &cfg, Layers: []oci.Descriptor{l}})
}

// seed fills a source registry with a single-platform image, a multi-platform
// index and an image in a project namespace.
func seed(src *reg) map[string]string {
	want := map[string]string{}
	want["app:1.0"] = src.image("app", "1.0", "app", "shared-base-layer").Digest
	amd := src.image("multi", "", "amd64", "shared-base-layer")
	arm := src.image("multi", "", "arm64", "arm-layer")
	want["multi:latest"] = src.manifest("multi", "latest", oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIIndex,
		Manifests: []oci.Descriptor{amd, arm}}).Digest
	want["team/api:2"] = src.image("team/api", "2", "api", "api-layer").Digest
	return want
}

func (r *reg) client(t *testing.T, user string) *importer.Client {
	t.Helper()
	c, err := importer.NewClient(r.srv.URL, importer.Credentials{Username: user, Password: password}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// inProcess is the destination client a server-side import uses.
func (r *reg) inProcess(t *testing.T) *importer.Client {
	t.Helper()
	u, err := r.db.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	c, err := importer.NewClient("http://doreg.internal", importer.Credentials{}, r.h.Transport(auth.Principal{User: &u}), nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestImportCopiesTagsByteForByte(t *testing.T) {
	src, dst := newReg(t), newReg(t)
	want := seed(src)
	ctx := context.Background()

	im := &importer.Importer{Src: src.client(t, "admin"), Dst: dst.inProcess(t), Concurrency: 3}
	plan, err := im.Plan(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 {
		t.Fatalf("plan has %d repositories, want 3: %+v", len(plan), plan)
	}
	p, err := im.Run(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if p.TagsCopied != 3 || p.TagsFailed != 0 || p.Tags != 3 || p.Repositories != 3 {
		t.Fatalf("progress = %+v", p)
	}
	if p.Bytes == 0 || p.Blobs == 0 {
		t.Errorf("no transfer counted: %+v", p)
	}

	check := dst.client(t, "admin")
	for ref, d := range want {
		repo, tag, _ := strings.Cut(ref, ":")
		got, err := check.ManifestDigest(ctx, repo, tag)
		if err != nil || got != d {
			t.Errorf("%s: destination digest %q (%v), want %q", ref, got, err, d)
		}
	}

	// A second run finds everything up to date and transfers nothing.
	again := &importer.Importer{Src: src.client(t, "admin"), Dst: dst.inProcess(t)}
	p, err = again.Run(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if p.TagsSkipped != 3 || p.TagsCopied != 0 || p.Bytes != 0 {
		t.Errorf("second run progress = %+v, want 3 skipped and nothing copied", p)
	}
}

func TestImportRepositoryPatterns(t *testing.T) {
	src, dst := newReg(t), newReg(t)
	seed(src)
	im := &importer.Importer{Src: src.client(t, "admin"), Dst: dst.inProcess(t)}
	ctx := context.Background()

	plan, err := im.Plan(ctx, []string{"team/*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || plan[0].Name != "team/api" || len(plan[0].Tags) != 1 {
		t.Errorf("pattern plan = %+v, want only team/api", plan)
	}
	// Plain names skip the catalog, which hosted registries do not offer.
	plan, err = im.Plan(ctx, []string{"app"})
	if err != nil || len(plan) != 1 || plan[0].Name != "app" {
		t.Errorf("literal plan = %+v (%v), want app", plan, err)
	}
	if _, err := im.Plan(ctx, []string{"["}); err == nil {
		t.Error("malformed pattern accepted")
	}
}

func TestImportSkipsWebhooksButAudits(t *testing.T) {
	src, dst := newReg(t), newReg(t)
	src.image("app", "1.0", "app", "layer")
	var hits atomic.Int32
	rcv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(rcv.Close)
	ctx := context.Background()
	if _, err := dst.db.SaveWebhook(ctx, meta.Webhook{Name: "all", URL: rcv.URL, Events: []string{events.Push}, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	im := &importer.Importer{Src: src.client(t, "admin"), Dst: dst.inProcess(t)}
	plan, _ := im.Plan(ctx, nil)
	if p, err := im.Run(ctx, plan); err != nil || p.TagsCopied != 1 {
		t.Fatalf("run: %+v, %v", p, err)
	}
	rows, err := dst.db.Audit(ctx, meta.AuditQuery{Action: events.Push, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Actor != "admin" || rows[0].Resource != "app:1.0" {
		t.Errorf("push audit rows = %+v, want one by admin for app:1.0", rows)
	}
	// A regular push afterwards is delivered; the imported one was not.
	dst.image("app", "2.0", "app2", "layer2")
	for i := 0; hits.Load() == 0 && i < 500; i++ {
		waitShort()
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("webhook deliveries = %d, want 1 (only the regular push)", n)
	}
}

func TestImportHeaderIgnoredForNonAdmins(t *testing.T) {
	dst := newReg(t)
	var hits atomic.Int32
	rcv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(rcv.Close)
	ctx := context.Background()
	dst.db.SaveWebhook(ctx, meta.Webhook{Name: "all", URL: rcv.URL, Events: []string{events.Push}, Enabled: true})
	proj, err := dst.db.EnsureProject(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	reader, _ := dst.db.UserByName(ctx, "reader")
	if err := dst.db.AddMember(ctx, proj.ID, reader.ID, "", meta.ProjectWrite); err != nil {
		t.Fatal(err)
	}

	c, _ := importer.NewClient(dst.srv.URL, importer.Credentials{Username: "reader", Password: password}, nil,
		http.Header{registry.ImportHeader: {"1"}})
	data := []byte(`{"architecture":"amd64","os":"linux"}`)
	d := digest.FromBytes("sha256", data)
	if _, err := c.PushBlob(ctx, "team/app", d.String(), func() (io.ReadCloser, int64, error) {
		return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg := oci.Descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: d.String(), Size: int64(len(data))}
	body, _ := json.Marshal(oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest, Config: &cfg, Layers: []oci.Descriptor{}})
	if err := c.PutManifest(ctx, "team/app", "1", body, oci.MediaTypeOCIManifest); err != nil {
		t.Fatal(err)
	}
	for i := 0; hits.Load() == 0 && i < 500; i++ {
		waitShort()
	}
	if hits.Load() != 1 {
		t.Error("a non-admin silenced the push webhook with the import header")
	}
}

func TestBasicAuthErrors(t *testing.T) {
	src := newReg(t)
	ctx := context.Background()
	anon, _ := importer.NewClient(src.srv.URL, importer.Credentials{}, nil, nil)
	if err := anon.Ping(ctx); !errors.Is(err, importer.ErrUnauthorized) {
		t.Errorf("anonymous ping: %v, want ErrUnauthorized", err)
	}
	wrong, _ := importer.NewClient(src.srv.URL, importer.Credentials{Username: "admin", Password: "nope"}, nil, nil)
	if err := wrong.Ping(ctx); !errors.Is(err, importer.ErrUnauthorized) {
		t.Errorf("wrong password ping: %v, want ErrUnauthorized", err)
	}
	if err := src.client(t, "admin").Ping(ctx); err != nil {
		t.Errorf("ping with valid login: %v", err)
	}
}

// tokenRegistry puts a registry behind token auth, the way registry:2 with
// "auth: token" or Docker Hub does: the registry answers 401 with a Bearer
// challenge and a token server trades a Basic login for a scoped token.
func tokenRegistry(t *testing.T, backend *reg) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			user, pw, ok := r.BasicAuth()
			if !ok || user != "migrator" || pw != password {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"access_token": "tok:" + r.URL.Query().Get("scope")})
			return
		}
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer tok:") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="test",scope="repository:x:pull,push"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		r.Header.Del("Authorization")
		r.SetBasicAuth("admin", password)
		backend.h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestImportFromTokenAuthRegistry(t *testing.T) {
	backend, dst := newReg(t), newReg(t)
	want := seed(backend)
	srv := tokenRegistry(t, backend)
	ctx := context.Background()

	bad, _ := importer.NewClient(srv.URL, importer.Credentials{Username: "migrator", Password: "wrong"}, nil, nil)
	if err := bad.Ping(ctx); !errors.Is(err, importer.ErrUnauthorized) {
		t.Errorf("wrong token login: %v, want ErrUnauthorized", err)
	}

	src, _ := importer.NewClient(srv.URL, importer.Credentials{Username: "migrator", Password: password}, nil, nil)
	im := &importer.Importer{Src: src, Dst: dst.inProcess(t), Concurrency: 2}
	plan, err := im.Plan(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := im.Run(ctx, plan)
	if err != nil || p.TagsCopied != len(want) {
		t.Fatalf("run: %+v, %v", p, err)
	}
}

func TestImportToRemoteDoreg(t *testing.T) {
	src, dst := newReg(t), newReg(t)
	want := seed(src)
	ctx := context.Background()
	to, _ := importer.NewClient(dst.srv.URL, importer.Credentials{Username: "admin", Password: password}, nil,
		http.Header{registry.ImportHeader: {"1"}})
	im := &importer.Importer{Src: src.client(t, "admin"), Dst: to, Concurrency: 2}
	plan, err := im.Plan(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := im.Run(ctx, plan); err != nil || p.TagsCopied != len(want) {
		t.Fatalf("run: %+v, %v", p, err)
	}
}

func waitShort() { time.Sleep(10 * time.Millisecond) }

func TestParseURL(t *testing.T) {
	for in, want := range map[string]string{
		"old-registry:5000":           "https://old-registry:5000",
		"http://10.0.0.5:5000/":       "http://10.0.0.5:5000",
		"https://reg.example.com/v2/": "https://reg.example.com",
		"https://u:p@reg.example.com": "https://reg.example.com",
	} {
		u, err := importer.ParseURL(in)
		if err != nil || u.String() != want {
			t.Errorf("ParseURL(%q) = %v, %v; want %s", in, u, err, want)
		}
	}
	for _, in := range []string{"", "ftp://x", "https://"} {
		if _, err := importer.ParseURL(in); err == nil {
			t.Errorf("ParseURL(%q) accepted", in)
		}
	}
}
