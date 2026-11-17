package registry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/p-arndt/doreg/internal/meta"
)

// challengeParams parses a Bearer challenge the way Docker does.
func challengeParams(t *testing.T, h string) map[string]string {
	t.Helper()
	rest, ok := strings.CutPrefix(h, "Bearer ")
	if !ok {
		t.Fatalf("not a Bearer challenge: %q", h)
	}
	params := map[string]string{}
	for rest != "" {
		k, after, _ := strings.Cut(rest, `="`)
		v, after, _ := strings.Cut(after, `"`)
		params[k] = v
		rest = strings.TrimPrefix(after, ",")
	}
	return params
}

// fetchToken requests a token from the challenge realm, with Basic
// credentials unless user is empty.
func (e *env) fetchToken(realm, user, secret string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", realm+"?service=doreg&scope=repository:x:pull", nil)
	if user != "" {
		req.SetBasicAuth(user, secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			e.t.Fatal(err)
		}
		if body.Token == "" || body.Token != body.AccessToken || body.ExpiresIn <= 0 {
			e.t.Fatalf("bad token response %+v", body)
		}
	}
	return resp, body.Token
}

func (e *env) doBearer(tok, method, path string) *http.Response {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// Docker only pulls anonymously after a Bearer challenge; on a Basic one it
// fails with "no basic auth credentials" before requesting the manifest.
func TestAnonymousPullOfPublicRepoViaToken(t *testing.T) {
	e := newEnv(t)
	e.pushImage("app", "v1", "x")
	e.pushImage("private", "v1", "y")
	if err := e.db.SetRepoPublic(t.Context(), "app", true); err != nil {
		t.Fatal(err)
	}

	ping := e.doAs("", "GET", "/v2/", nil, nil)
	expect(t, ping, http.StatusUnauthorized)
	c := challengeParams(t, ping.Header.Get("WWW-Authenticate"))
	if c["realm"] != e.srv.URL+"/v2/token" || c["service"] != "doreg" {
		t.Fatalf("challenge %v", c)
	}

	resp, tok := e.fetchToken(c["realm"], "", "")
	expect(t, resp, http.StatusOK)
	expect(t, e.doBearer(tok, "HEAD", "/v2/app/manifests/v1"), http.StatusOK)
	expect(t, e.doBearer(tok, "GET", "/v2/app/manifests/v1"), http.StatusOK)

	denied := e.doBearer(tok, "HEAD", "/v2/private/manifests/v1")
	expect(t, denied, http.StatusUnauthorized)
	dc := challengeParams(t, denied.Header.Get("WWW-Authenticate"))
	if dc["scope"] != "repository:private:pull" || dc["error"] != "insufficient_scope" {
		t.Fatalf("denied challenge %v", dc)
	}
	expect(t, e.doBearer(tok, "POST", "/v2/app/blobs/uploads/"), http.StatusUnauthorized)
	expect(t, e.doBearer(tok, "GET", "/v2/"), http.StatusUnauthorized)
}

func TestTokenLogin(t *testing.T) {
	e := newEnv(t)
	realm := e.srv.URL + "/v2/token"

	resp, _ := e.fetchToken(realm, "admin", "wrong")
	expect(t, resp, http.StatusUnauthorized)

	resp, tok := e.fetchToken(realm, "admin", testPassword)
	expect(t, resp, http.StatusOK)
	expect(t, e.doBearer(tok, "GET", "/v2/"), http.StatusOK)
	expect(t, e.doBearer(tok, "POST", "/v2/app/blobs/uploads/"), http.StatusAccepted)

	resp, rtok := e.fetchToken(realm, "reader", testPassword)
	expect(t, resp, http.StatusOK)
	expect(t, e.doBearer(rtok, "POST", "/v2/app/blobs/uploads/"), http.StatusForbidden)

	expect(t, e.doBearer("not-a-token", "GET", "/v2/"), http.StatusUnauthorized)
}

func TestTokenPasswordGrant(t *testing.T) {
	e := newEnv(t)
	form := url.Values{"grant_type": {"password"}, "username": {"admin"}, "password": {testPassword}, "service": {"doreg"}}
	resp, err := http.PostForm(e.srv.URL+"/v2/token", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	expect(t, resp, http.StatusOK)
	var body struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	expect(t, e.doBearer(body.AccessToken, "GET", "/v2/"), http.StatusOK)

	form.Set("grant_type", "refresh_token")
	resp2, err := http.PostForm(e.srv.URL+"/v2/token", form)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	expect(t, resp2, http.StatusBadRequest)
}

func TestTokenRealmBehindProxy(t *testing.T) {
	e := newEnv(t)
	resp := e.doAs("", "GET", "/v2/", nil, map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "registry.example.com"})
	expect(t, resp, http.StatusUnauthorized)
	if c := challengeParams(t, resp.Header.Get("WWW-Authenticate")); c["realm"] != "https://registry.example.com/v2/token" {
		t.Fatalf("realm %q", c["realm"])
	}
}

// Deleting a robot must cut off tokens it already holds, not only after they
// expire.
func TestTokenRevokedAfterInvalidate(t *testing.T) {
	e := newEnv(t)
	e.pushImage("app", "v1", "x")
	name, secret := e.addRobot(meta.DefaultProject, "ci", false, 0, false)

	resp, tok := e.fetchToken(e.srv.URL+"/v2/token", name, secret)
	expect(t, resp, http.StatusOK)
	expect(t, e.doBearer(tok, "GET", "/v2/app/manifests/v1"), http.StatusOK)

	r, err := e.db.RobotByName(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.DeleteRobot(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	e.auth.Invalidate()
	expect(t, e.doBearer(tok, "GET", "/v2/app/manifests/v1"), http.StatusUnauthorized)
}
