package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/p-arndt/doreg/internal/api"
	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/gc"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/meta/metatest"
	"github.com/p-arndt/doreg/internal/registry"
	"github.com/p-arndt/doreg/internal/storage"
)

const apitPassword = "apit-password"

var (
	apitHashOnce sync.Once
	apitHash     string
)

// apitPasswordHash hashes the shared test password once; bcrypt is slow.
func apitPasswordHash(t *testing.T) string {
	t.Helper()
	apitHashOnce.Do(func() {
		h, err := auth.HashPassword(apitPassword)
		if err != nil {
			t.Fatal(err)
		}
		apitHash = h
	})
	return apitHash
}

// apitEnv is a running API with users admin, bob, carol and eve (all with the
// same password). The admin client is logged in.
type apitEnv struct {
	t     *testing.T
	srv   *httptest.Server
	db    *meta.DB
	authn *auth.Authenticator
	api   *api.API
	admin *apitClient
}

type apitClient struct {
	t    *testing.T
	base string
	http *http.Client
}

func apitNewEnv(t *testing.T) *apitEnv {
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authn := auth.New(db)
	collector := gc.New(db, store, logger)
	bus := events.New(db, logger)
	bus.AllowPrivate = true // webhook receivers in these tests listen on 127.0.0.1
	handler := api.New(db, store, authn, collector, bus, nil, logger)
	handler.Registry = &registry.Handler{DB: db, Store: store, Auth: authn, Events: bus, Log: logger}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	hash := apitPasswordHash(t)
	for _, u := range []struct{ name, role string }{
		{"admin", meta.RoleAdmin},
		{"bob", meta.RoleUser},
		{"carol", meta.RoleUser},
		{"eve", meta.RoleUser},
	} {
		if err := db.UpsertUser(context.Background(), u.name, hash, u.role); err != nil {
			t.Fatal(err)
		}
	}

	e := &apitEnv{t: t, srv: srv, db: db, authn: authn, api: handler}
	e.admin = e.login("admin")
	return e
}

// login returns a client with its own cookie jar, logged in as user.
func (e *apitEnv) login(user string) *apitClient {
	e.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		e.t.Fatal(err)
	}
	c := &apitClient{t: e.t, base: e.srv.URL, http: &http.Client{Jar: jar}}
	c.expect(http.StatusOK, http.MethodPost, "/api/login", map[string]string{"username": user, "password": apitPassword})
	return c
}

// raw sends a request. Mutating requests carry Origin = server URL unless
// headers override it.
func (c *apitClient) raw(method, path string, body any, headers map[string]string) (int, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Origin", c.base)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp.StatusCode, data
}

// expect sends a request and fails the test unless the status matches.
func (c *apitClient) expect(want int, method, path string, body any) []byte {
	c.t.Helper()
	status, data := c.raw(method, path, body, nil)
	if status != want {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, status, want, data)
	}
	return data
}

func apitDecode(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}

// apitTeamEnv creates projects team (bob admin, carol read), other (private)
// and open (public).
func apitTeamEnv(t *testing.T) *apitEnv {
	t.Helper()
	e := apitNewEnv(t)
	e.admin.expect(http.StatusCreated, http.MethodPost, "/api/projects", map[string]any{"name": "team"})
	e.admin.expect(http.StatusCreated, http.MethodPost, "/api/projects", map[string]any{"name": "other"})
	e.admin.expect(http.StatusCreated, http.MethodPost, "/api/projects", map[string]any{"name": "open", "public": true})
	e.admin.expect(http.StatusCreated, http.MethodPost, "/api/projects/team/members", map[string]any{"username": "bob", "role": "admin"})
	e.admin.expect(http.StatusCreated, http.MethodPost, "/api/projects/team/members", map[string]any{"username": "carol", "role": "read"})
	return e
}

// apitMemberID returns the membership id of username in project.
func apitMemberID(t *testing.T, c *apitClient, project, username string) int64 {
	t.Helper()
	var out struct {
		Members []meta.Member `json:"members"`
	}
	apitDecode(t, c.expect(http.StatusOK, http.MethodGet, "/api/projects/"+project, nil), &out)
	for _, m := range out.Members {
		if m.Username == username {
			return m.ID
		}
	}
	t.Fatalf("%s is not a member of %s", username, project)
	return 0
}

func TestAPIProjectCreate(t *testing.T) {
	e := apitNewEnv(t)

	data := e.admin.expect(http.StatusCreated, http.MethodPost, "/api/projects", map[string]any{"name": "team"})
	var p meta.Project
	apitDecode(t, data, &p)
	if p.Name != "team" {
		t.Fatalf("created project name = %q, want team", p.Name)
	}

	e.admin.expect(http.StatusConflict, http.MethodPost, "/api/projects", map[string]any{"name": "team"})
	e.admin.expect(http.StatusBadRequest, http.MethodPost, "/api/projects", map[string]any{"name": "Bad_Name!"})

	bob := e.login("bob")
	bob.expect(http.StatusForbidden, http.MethodPost, "/api/projects", map[string]any{"name": "bobs"})
	e.admin.expect(http.StatusNotFound, http.MethodGet, "/api/projects/bobs", nil)
}

func TestAPIProjectListVisibility(t *testing.T) {
	e := apitTeamEnv(t)

	type item struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	list := func(c *apitClient) map[string]string {
		var items []item
		apitDecode(t, c.expect(http.StatusOK, http.MethodGet, "/api/projects", nil), &items)
		out := map[string]string{}
		for _, it := range items {
			out[it.Name] = it.Role
		}
		return out
	}

	bob := list(e.login("bob"))
	if len(bob) != 2 || bob["team"] != "admin" || bob["open"] != "" {
		t.Errorf("bob sees %v, want team=admin and open with no role", bob)
	}

	carol := list(e.login("carol"))
	if len(carol) != 2 || carol["team"] != "read" || carol["open"] != "" {
		t.Errorf("carol sees %v, want team=read and open with no role", carol)
	}

	eve := list(e.login("eve"))
	if len(eve) != 1 || eve["open"] != "" {
		t.Errorf("eve sees %v, want only open", eve)
	}

	admin := list(e.admin)
	if len(admin) != 3 || admin["other"] != "admin" {
		t.Errorf("admin sees %v, want all three projects", admin)
	}
}

func TestAPIProjectMembers(t *testing.T) {
	e := apitTeamEnv(t)
	bob := e.login("bob")
	carol := e.login("carol")

	bob.expect(http.StatusCreated, http.MethodPost, "/api/projects/team/members", map[string]any{"username": "eve", "role": "write"})
	carol.expect(http.StatusForbidden, http.MethodPost, "/api/projects/team/members", map[string]any{"username": "eve", "role": "read"})

	// Quota is admin-only; a rejected project admin write must not land.
	bob.expect(http.StatusForbidden, http.MethodPatch, "/api/projects/team", map[string]any{"quotaBytes": 999999})
	e.admin.expect(http.StatusOK, http.MethodPatch, "/api/projects/team", map[string]any{"quotaBytes": 1000})
	var wrapped struct {
		Project struct {
			QuotaBytes int64 `json:"quotaBytes"`
		} `json:"project"`
	}
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/projects/team", nil), &wrapped)
	if wrapped.Project.QuotaBytes != 1000 {
		t.Errorf("quota after admin PATCH = %d, want 1000", wrapped.Project.QuotaBytes)
	}

	bob.expect(http.StatusOK, http.MethodPatch, "/api/projects/team", map[string]any{"public": true})

	// Project admins lock tags; read members cannot.
	carol.expect(http.StatusForbidden, http.MethodPatch, "/api/projects/team", map[string]any{"immutableTags": true})
	bob.expect(http.StatusOK, http.MethodPatch, "/api/projects/team", map[string]any{"immutableTags": true})
	var locked struct {
		Project struct {
			ImmutableTags bool  `json:"immutableTags"`
			QuotaBytes    int64 `json:"quotaBytes"`
		} `json:"project"`
	}
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/projects/team", nil), &locked)
	if !locked.Project.ImmutableTags || locked.Project.QuotaBytes != 1000 {
		t.Errorf("after immutableTags PATCH: %+v, want immutable with quota 1000", locked.Project)
	}
	bob.expect(http.StatusBadRequest, http.MethodPatch, "/api/projects/team", map[string]any{"mutableTags": []string{"[bad"}})
	bob.expect(http.StatusOK, http.MethodPatch, "/api/projects/team", map[string]any{"mutableTags": []string{" latest ", "*-dev", "latest", ""}})
	var exceptions struct {
		Project struct {
			ImmutableTags bool     `json:"immutableTags"`
			MutableTags   []string `json:"mutableTags"`
		} `json:"project"`
	}
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/projects/team", nil), &exceptions)
	if !exceptions.Project.ImmutableTags || strings.Join(exceptions.Project.MutableTags, ",") != "latest,*-dev" {
		t.Errorf("after mutableTags PATCH: %+v", exceptions.Project)
	}
	carol.expect(http.StatusForbidden, http.MethodPatch, "/api/projects/team", map[string]any{"public": false})

	// Read-only members get the project but not the member list.
	var carolView map[string]json.RawMessage
	apitDecode(t, carol.expect(http.StatusOK, http.MethodGet, "/api/projects/team", nil), &carolView)
	if _, ok := carolView["members"]; ok {
		t.Errorf("read member sees the member list")
	}

	eveID := apitMemberID(t, bob, "team", "eve")
	bob.expect(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("/api/projects/team/members/%d", eveID), nil)
	carolID := apitMemberID(t, bob, "team", "carol")
	bob.expect(http.StatusNoContent, http.MethodPut, fmt.Sprintf("/api/projects/team/members/%d", carolID),
		map[string]any{"role": "write"})
	carol.expect(http.StatusForbidden, http.MethodDelete, fmt.Sprintf("/api/projects/team/members/%d", carolID), nil)

	// A membership of another project must not be removable through team.
	e.admin.expect(http.StatusCreated, http.MethodPost, "/api/projects/other/members", map[string]any{"username": "eve", "role": "read"})
	otherID := apitMemberID(t, e.admin, "other", "eve")
	bob.expect(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("/api/projects/team/members/%d", otherID), nil)
	apitMemberID(t, e.admin, "other", "eve")
}

func TestAPIRobots(t *testing.T) {
	e := apitTeamEnv(t)
	bob := e.login("bob")

	var created struct {
		Robot  meta.Robot `json:"robot"`
		Secret string     `json:"secret"`
	}
	apitDecode(t, bob.expect(http.StatusCreated, http.MethodPost, "/api/robots",
		map[string]any{"name": "ci", "project": "team", "canPush": true}), &created)
	if created.Robot.Name != "robot$team+ci" {
		t.Fatalf("robot name = %q, want robot$team+ci", created.Robot.Name)
	}
	if created.Secret == "" {
		t.Fatal("create returned no secret")
	}
	ctx := context.Background()
	if _, ok := e.authn.Basic(ctx, "robot$team+ci", created.Secret); !ok {
		t.Fatal("new robot secret does not authenticate")
	}

	bob.expect(http.StatusForbidden, http.MethodPost, "/api/robots", map[string]any{"name": "sys", "project": ""})

	var rotated struct {
		Secret string `json:"secret"`
	}
	apitDecode(t, bob.expect(http.StatusOK, http.MethodPost,
		fmt.Sprintf("/api/robots/%d/secret", created.Robot.ID), nil), &rotated)
	if rotated.Secret == "" || rotated.Secret == created.Secret {
		t.Fatalf("rotate returned secret %q, want a new one", rotated.Secret)
	}
	if _, ok := e.authn.Basic(ctx, "robot$team+ci", created.Secret); ok {
		t.Error("old robot secret still authenticates after rotation")
	}
	if _, ok := e.authn.Basic(ctx, "robot$team+ci", rotated.Secret); !ok {
		t.Error("rotated robot secret does not authenticate")
	}
}

func TestAPIWebhooks(t *testing.T) {
	e := apitTeamEnv(t)
	bob := e.login("bob")
	eve := e.login("eve")

	hits := make(chan string, 4)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.Header.Get("X-Doreg-Event")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	bob.expect(http.StatusBadRequest, http.MethodPost, "/api/webhooks",
		map[string]any{"project": "team", "name": "bad", "url": "ftp://example.com/hook", "events": []string{"push"}, "enabled": true})
	bob.expect(http.StatusBadRequest, http.MethodPost, "/api/webhooks",
		map[string]any{"project": "team", "name": "bad", "url": receiver.URL, "events": []string{"explode"}, "enabled": true})
	eve.expect(http.StatusForbidden, http.MethodPost, "/api/webhooks",
		map[string]any{"project": "team", "name": "eve", "url": receiver.URL, "events": []string{"push"}, "enabled": true})

	var hook meta.Webhook
	apitDecode(t, bob.expect(http.StatusOK, http.MethodPost, "/api/webhooks",
		map[string]any{"project": "team", "name": "ci-hook", "url": receiver.URL, "events": []string{"push"}, "enabled": true}), &hook)
	if hook.ID == 0 {
		t.Fatal("saved webhook has no id")
	}

	var delivery meta.Delivery
	apitDecode(t, bob.expect(http.StatusOK, http.MethodPost, fmt.Sprintf("/api/webhooks/%d/test", hook.ID), nil), &delivery)
	if delivery.StatusCode != http.StatusOK || delivery.Error != "" {
		t.Errorf("test delivery = status %d error %q, want 200", delivery.StatusCode, delivery.Error)
	}
	select {
	case ev := <-hits:
		if ev != "ping" {
			t.Errorf("receiver got event %q, want ping", ev)
		}
	default:
		t.Error("receiver was not called")
	}

	var list []meta.Delivery
	apitDecode(t, bob.expect(http.StatusOK, http.MethodGet, fmt.Sprintf("/api/webhooks/%d/deliveries", hook.ID), nil), &list)
	if len(list) != 1 || list[0].Event != "ping" || list[0].StatusCode != http.StatusOK {
		t.Errorf("deliveries = %+v, want one ping with status 200", list)
	}

	eve.expect(http.StatusForbidden, http.MethodGet, fmt.Sprintf("/api/webhooks/%d/deliveries", hook.ID), nil)
}

func TestAPIAudit(t *testing.T) {
	e := apitTeamEnv(t)
	bob := e.login("bob")
	bob.expect(http.StatusCreated, http.MethodPost, "/api/robots", map[string]any{"name": "ci", "project": "team"})

	type entry struct {
		Action  string `json:"action"`
		Project string `json:"project"`
	}
	var all []entry
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/audit", nil), &all)
	seen := map[string]bool{}
	for _, en := range all {
		seen[en.Action] = true
	}
	for _, want := range []string{"project_create", "member_add", "robot_create"} {
		if !seen[want] {
			t.Errorf("admin audit has no %s entry", want)
		}
	}

	var bobView []entry
	apitDecode(t, bob.expect(http.StatusOK, http.MethodGet, "/api/audit", nil), &bobView)
	if len(bobView) == 0 {
		t.Fatal("bob sees no audit entries for team")
	}
	bobSeen := map[string]bool{}
	for _, en := range bobView {
		bobSeen[en.Action] = true
		if en.Project != "team" {
			t.Errorf("bob sees %s entry of project %q", en.Action, en.Project)
		}
	}
	if !bobSeen["member_add"] || !bobSeen["robot_create"] {
		t.Errorf("bob audit actions = %v, want member_add and robot_create", bobSeen)
	}

	var adds []entry
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/audit?action=member_add", nil), &adds)
	if len(adds) < 2 {
		t.Errorf("action filter returned %d member_add entries, want at least 2", len(adds))
	}
	for _, en := range adds {
		if en.Action != "member_add" {
			t.Errorf("action filter returned %s", en.Action)
		}
	}

	var bobFiltered []entry
	apitDecode(t, bob.expect(http.StatusOK, http.MethodGet, "/api/audit?action=project_create", nil), &bobFiltered)
	for _, en := range bobFiltered {
		if en.Action != "project_create" || en.Project != "team" {
			t.Errorf("bob filtered audit returned %+v", en)
		}
	}
}

func TestAPIRepoVisibility(t *testing.T) {
	e := apitTeamEnv(t)
	ctx := context.Background()
	err := e.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := meta.EnsureRepo(ctx, tx, "other/app"); err != nil {
			return err
		}
		_, err := meta.EnsureRepo(ctx, tx, "team/app")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	names := func(c *apitClient) map[string]bool {
		var items []struct {
			Name string `json:"name"`
		}
		apitDecode(t, c.expect(http.StatusOK, http.MethodGet, "/api/repos", nil), &items)
		out := map[string]bool{}
		for _, it := range items {
			out[it.Name] = true
		}
		return out
	}

	bob := e.login("bob")
	bobRepos := names(bob)
	if bobRepos["other/app"] {
		t.Error("bob sees repository other/app of a project he is not in")
	}
	if !bobRepos["team/app"] {
		t.Error("bob does not see team/app")
	}
	if !names(e.admin)["other/app"] {
		t.Error("admin does not see other/app")
	}

	bob.expect(http.StatusForbidden, http.MethodDelete, "/api/tag?repo=other/app&tag=latest", nil)
	bob.expect(http.StatusForbidden, http.MethodDelete, "/api/repo?name=other/app", nil)
}

// guest returns a client without a session.
func (e *apitEnv) guest() *apitClient {
	return &apitClient{t: e.t, base: e.srv.URL, http: &http.Client{}}
}

func TestAPIPublicBrowse(t *testing.T) {
	e := apitTeamEnv(t)
	ctx := context.Background()
	err := e.db.Tx(ctx, func(tx *sql.Tx) error {
		for _, name := range []string{"open/app", "other/app", "other/pub", "team/app"} {
			if _, err := meta.EnsureRepo(ctx, tx, name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	e.admin.expect(http.StatusNoContent, http.MethodPatch, "/api/repo?name=other/pub", map[string]any{"public": true})
	guest := e.guest()

	var cfg struct {
		PublicBrowse bool `json:"publicBrowse"`
	}
	apitDecode(t, guest.expect(http.StatusOK, http.MethodGet, "/api/auth/config", nil), &cfg)
	if cfg.PublicBrowse {
		t.Error("publicBrowse is on by default")
	}
	guest.expect(http.StatusUnauthorized, http.MethodGet, "/api/repos", nil)
	guest.expect(http.StatusUnauthorized, http.MethodGet, "/api/repo?name=open/app", nil)

	e.api.PublicBrowse = true
	apitDecode(t, guest.expect(http.StatusOK, http.MethodGet, "/api/auth/config", nil), &cfg)
	if !cfg.PublicBrowse {
		t.Error("publicBrowse is off after enabling it")
	}

	var items []struct {
		Name string `json:"name"`
	}
	apitDecode(t, guest.expect(http.StatusOK, http.MethodGet, "/api/repos", nil), &items)
	got := map[string]bool{}
	for _, it := range items {
		got[it.Name] = true
	}
	want := map[string]bool{"open/app": true, "other/pub": true}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("guest sees %v, want %v", got, want)
	}

	guest.expect(http.StatusOK, http.MethodGet, "/api/repo?name=open/app", nil)
	guest.expect(http.StatusOK, http.MethodGet, "/api/repo?name=other/pub", nil)
	guest.expect(http.StatusUnauthorized, http.MethodGet, "/api/repo?name=other/app", nil)
	guest.expect(http.StatusUnauthorized, http.MethodGet, "/api/repo?name=team/app", nil)
	// A missing name in a private project looks like a private repository.
	guest.expect(http.StatusUnauthorized, http.MethodGet, "/api/repo?name=other/missing", nil)
	guest.expect(http.StatusNotFound, http.MethodGet, "/api/repo?name=open/missing", nil)
	guest.expect(http.StatusUnauthorized, http.MethodGet, "/api/manifest?repo=other/app&ref=latest", nil)

	for _, req := range []struct{ method, path string }{
		{http.MethodGet, "/api/me"},
		{http.MethodGet, "/api/stats"},
		{http.MethodGet, "/api/projects"},
		{http.MethodGet, "/api/projects/open"},
		{http.MethodPatch, "/api/repo?name=open/app"},
		{http.MethodDelete, "/api/repo?name=open/app"},
		{http.MethodDelete, "/api/tag?repo=open/app&tag=latest"},
		{http.MethodDelete, "/api/manifest?repo=open/app&digest=sha256:00"},
	} {
		guest.expect(http.StatusUnauthorized, req.method, req.path, map[string]any{"public": false})
	}

	// Signed-in users still see what their projects allow.
	bob := e.login("bob")
	bob.expect(http.StatusOK, http.MethodGet, "/api/repo?name=team/app", nil)
	bob.expect(http.StatusForbidden, http.MethodGet, "/api/repo?name=other/app", nil)
}

func TestAPICrossOriginRejected(t *testing.T) {
	e := apitNewEnv(t)

	status, _ := e.admin.raw(http.MethodPost, "/api/projects", map[string]any{"name": "evilproj"},
		map[string]string{"Origin": "https://evil.example"})
	if status != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want 403", status)
	}
	e.admin.expect(http.StatusNotFound, http.MethodGet, "/api/projects/evilproj", nil)

	status, _ = e.admin.raw(http.MethodPost, "/api/projects", map[string]any{"name": "sameorigin"}, nil)
	if status != http.StatusCreated {
		t.Fatalf("same-origin POST status = %d, want 201", status)
	}
}
