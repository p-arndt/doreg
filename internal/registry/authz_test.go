package registry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/meta"
)

// addUser creates a local account with the test password.
func (e *env) addUser(name string) meta.User {
	e.t.Helper()
	ctx := context.Background()
	if err := e.db.UpsertUser(ctx, name, mustHash(e.t, testPassword), meta.RoleUser); err != nil {
		e.t.Fatal(err)
	}
	u, err := e.db.UserByName(ctx, name)
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

// addSSOUser creates an OIDC account with a CLI secret and returns the secret.
func (e *env) addSSOUser(name string, groups ...string) (meta.User, string) {
	e.t.Helper()
	ctx := context.Background()
	u, err := e.db.CreateOIDCUser(ctx, meta.User{Username: name, Role: meta.RoleUser, OIDCSubject: "sub-" + name, Groups: groups})
	if err != nil {
		e.t.Fatal(err)
	}
	secret := auth.NewSecret()
	if err := e.db.SetCLISecret(ctx, u.ID, mustHash(e.t, secret)); err != nil {
		e.t.Fatal(err)
	}
	return u, secret
}

// addRobot creates a robot for a project and returns its username and secret.
func (e *env) addRobot(project, name string, canPush bool, expiresAt int64, disabled bool) (string, string) {
	e.t.Helper()
	ctx := context.Background()
	p, err := e.db.ProjectByName(ctx, project)
	if err != nil {
		e.t.Fatal(err)
	}
	secret := auth.NewSecret()
	r, err := e.db.CreateRobot(ctx, meta.Robot{
		Name:       meta.RobotName(project, name),
		ProjectID:  p.ID,
		SecretHash: mustHash(e.t, secret),
		CanPush:    canPush,
		ExpiresAt:  expiresAt,
		CreatedBy:  "admin",
	})
	if err != nil {
		e.t.Fatal(err)
	}
	if disabled {
		r.Disabled = true
		if err := e.db.UpdateRobot(ctx, r); err != nil {
			e.t.Fatal(err)
		}
	}
	return r.Name, secret
}

// member grants a user or group a role in a project, creating the project if needed.
func (e *env) member(project string, user meta.User, role string) {
	e.t.Helper()
	ctx := context.Background()
	p, err := e.db.EnsureProject(ctx, project)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.AddMember(ctx, p.ID, user.ID, "", role); err != nil {
		e.t.Fatal(err)
	}
}

// doBasic sends a request with explicit Basic credentials.
func (e *env) doBasic(name, secret, method, path string, body []byte) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	if name != "" {
		req.SetBasicAuth(name, secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// uploadAs starts a monolithic blob upload with the given credentials.
func (e *env) uploadAs(name, secret, repo string, data []byte) *http.Response {
	e.t.Helper()
	d := digest.FromBytes("sha256", data)
	return e.doBasic(name, secret, "POST", "/v2/"+repo+"/blobs/uploads/?digest="+d.String(), data)
}

func mustHash(t *testing.T, s string) string {
	t.Helper()
	h, err := auth.HashPassword(s)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestAdminPushCreatesProject(t *testing.T) {
	e := newEnv(t)
	e.pushImage("newproj/app", "v1", "x")
	if _, err := e.db.ProjectByName(context.Background(), "newproj"); err != nil {
		t.Fatalf("project not created by admin push: %v", err)
	}
	// A plain user cannot create a project by pushing to it.
	e.addUser("plain")
	expect(t, e.doBasic("plain", testPassword, "POST", "/v2/ghost/app/blobs/uploads/", nil), http.StatusForbidden)
	if _, err := e.db.ProjectByName(context.Background(), "ghost"); err == nil {
		t.Fatal("plain user created a project")
	}
}

func TestMembershipRoles(t *testing.T) {
	e := newEnv(t)
	e.pushImage("team/app", "v1", "x")
	reader := e.addUser("dev-read")
	writer := e.addUser("dev-write")
	e.addUser("stranger")
	e.member("team", reader, meta.ProjectRead)
	e.member("team", writer, meta.ProjectWrite)

	get := func(user string) *http.Response {
		return e.doAs(user, "GET", "/v2/team/app/manifests/v1", nil, nil)
	}

	expect(t, get("dev-read"), http.StatusOK)
	expect(t, e.doAs("dev-read", "POST", "/v2/team/app/blobs/uploads/", nil, nil), http.StatusForbidden)
	expect(t, e.doAs("dev-read", "DELETE", "/v2/team/app/manifests/v1", nil, nil), http.StatusForbidden)

	expect(t, get("dev-write"), http.StatusOK)
	expect(t, e.doAs("dev-write", "POST", "/v2/team/app/blobs/uploads/", nil, nil), http.StatusAccepted)
	expect(t, e.doAs("dev-write", "DELETE", "/v2/team/app/manifests/v1", nil, nil), http.StatusAccepted)

	expect(t, get("stranger"), http.StatusForbidden)
}

func TestOIDCGroupMembership(t *testing.T) {
	e := newEnv(t)
	e.pushImage("team/app", "v1", "x")
	ctx := context.Background()
	p, err := e.db.ProjectByName(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddMember(ctx, p.ID, 0, "engineers", meta.ProjectRead); err != nil {
		t.Fatal(err)
	}
	_, secret := e.addSSOUser("sso-dev", "engineers")
	_, otherSecret := e.addSSOUser("sso-other", "marketing")

	expect(t, e.doBasic("sso-dev", secret, "GET", "/v2/team/app/manifests/v1", nil), http.StatusOK)
	expect(t, e.doBasic("sso-dev", secret, "POST", "/v2/team/app/blobs/uploads/", nil), http.StatusForbidden)
	expect(t, e.doBasic("sso-dev", "wrong", "GET", "/v2/team/app/manifests/v1", nil), http.StatusUnauthorized)
	expect(t, e.doBasic("sso-other", otherSecret, "GET", "/v2/team/app/manifests/v1", nil), http.StatusForbidden)
}

func TestPublicProject(t *testing.T) {
	e := newEnv(t)
	e.pushImage("open/app", "v1", "x")
	e.pushImage("closed/app", "v1", "y")
	ctx := context.Background()
	open, err := e.db.ProjectByName(ctx, "open")
	if err != nil {
		t.Fatal(err)
	}
	open.Public = true
	if err := e.db.UpdateProject(ctx, open); err != nil {
		t.Fatal(err)
	}
	e.addUser("stranger")

	expect(t, e.doAs("", "GET", "/v2/open/app/manifests/v1", nil, nil), http.StatusOK)
	expect(t, e.doAs("stranger", "GET", "/v2/open/app/manifests/v1", nil, nil), http.StatusOK)
	expect(t, e.doAs("stranger", "POST", "/v2/open/app/blobs/uploads/", nil, nil), http.StatusForbidden)
	expect(t, e.doAs("", "POST", "/v2/open/app/blobs/uploads/", nil, nil), http.StatusUnauthorized)
	expect(t, e.doAs("", "GET", "/v2/closed/app/manifests/v1", nil, nil), http.StatusUnauthorized)
}

func TestRobotAccess(t *testing.T) {
	t.Run("pull only", func(t *testing.T) {
		e := newEnv(t)
		e.pushImage("team/app", "v1", "x")
		e.pushImage("other/app", "v1", "y")
		name, secret := e.addRobot("team", "ci", false, 0, false)

		expect(t, e.doBasic(name, secret, "GET", "/v2/team/app/manifests/v1", nil), http.StatusOK)
		expect(t, e.doBasic(name, secret, "GET", "/v2/other/app/manifests/v1", nil), http.StatusForbidden)
		expect(t, e.doBasic(name, secret, "POST", "/v2/team/app/blobs/uploads/", nil), http.StatusForbidden)
	})

	t.Run("push with canPush", func(t *testing.T) {
		e := newEnv(t)
		e.pushImage("team/app", "v1", "x")
		name, secret := e.addRobot("team", "ci", true, 0, false)
		data := []byte("robot blob")
		expect(t, e.uploadAs(name, secret, "team/app", data), http.StatusCreated)
	})

	t.Run("expired", func(t *testing.T) {
		e := newEnv(t)
		e.pushImage("team/app", "v1", "x")
		name, secret := e.addRobot("team", "ci", false, meta.Now()-1, false)
		expect(t, e.doBasic(name, secret, "GET", "/v2/team/app/manifests/v1", nil), http.StatusUnauthorized)
	})

	t.Run("disabled", func(t *testing.T) {
		e := newEnv(t)
		e.pushImage("team/app", "v1", "x")
		name, secret := e.addRobot("team", "ci", true, 0, true)
		expect(t, e.doBasic(name, secret, "GET", "/v2/team/app/manifests/v1", nil), http.StatusUnauthorized)
	})
}

func TestProjectQuota(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.db.CreateProject(ctx, meta.Project{Name: "small", QuotaBytes: 100}); err != nil {
		t.Fatal(err)
	}
	dev := e.addUser("dev")
	e.member("small", dev, meta.ProjectWrite)

	first := bytes.Repeat([]byte("a"), 60)
	expect(t, e.doAs("dev", "POST", "/v2/small/app/blobs/uploads/?digest="+digest.FromBytes("sha256", first).String(), first, nil), http.StatusCreated)

	second := bytes.Repeat([]byte("b"), 60)
	resp := e.doAs("dev", "POST", "/v2/small/app/blobs/uploads/?digest="+digest.FromBytes("sha256", second).String(), second, nil)
	expect(t, resp, http.StatusForbidden)
	if b, _ := io.ReadAll(resp.Body); !strings.Contains(string(b), "quota") {
		t.Fatalf("quota error body %s", b)
	}

	// Blobs the project already stores cost nothing.
	expect(t, e.doAs("dev", "POST", "/v2/small/other/blobs/uploads/?digest="+digest.FromBytes("sha256", first).String(), first, nil), http.StatusCreated)
}

func TestCatalogFiltersByProject(t *testing.T) {
	e := newEnv(t)
	e.pushImage("team/api", "v1", "a")
	e.pushImage("other/api", "v1", "b")
	e.pushImage("plain", "v1", "c")
	e.member("team", e.addUser("dev"), meta.ProjectRead)

	catalog := func(user string) []string {
		t.Helper()
		resp := e.doAs(user, "GET", "/v2/_catalog", nil, nil)
		expect(t, resp, http.StatusOK)
		var out struct {
			Repositories []string `json:"repositories"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out.Repositories
	}

	if got := catalog("dev"); !slices.Equal(got, []string{"team/api"}) {
		t.Fatalf("dev catalog = %v", got)
	}
	if got := catalog("admin"); len(got) != 3 {
		t.Fatalf("admin catalog = %v", got)
	}
	expect(t, e.doAs("", "GET", "/v2/_catalog", nil, nil), http.StatusUnauthorized)
}

func TestMountRequiresReadOnSource(t *testing.T) {
	e := newEnv(t)
	desc := e.pushBlob("secret/src", []byte("private layer"))
	e.member("mine", e.addUser("dev"), meta.ProjectWrite)

	url := "/v2/mine/app/blobs/uploads/?mount=" + desc.Digest + "&from=secret/src"
	expect(t, e.doAs("dev", "POST", url, nil, nil), http.StatusAccepted)
	expect(t, e.doAs("dev", "HEAD", "/v2/mine/app/blobs/"+desc.Digest, nil, nil), http.StatusNotFound)

	// With read access to the source the same mount succeeds.
	both := e.addUser("dev-both")
	e.member("secret", both, meta.ProjectRead)
	e.member("mine", both, meta.ProjectWrite)
	expect(t, e.doAs("dev-both", "POST", url, nil, nil), http.StatusCreated)
}
