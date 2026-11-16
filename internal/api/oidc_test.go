package api_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/p-arndt/doreg/internal/api"
	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/gc"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/meta/metatest"
	"github.com/p-arndt/doreg/internal/oidc"
	"github.com/p-arndt/doreg/internal/storage"
)

const oidcTestClientID = "doreg"

// fakeIdP is a minimal OpenID provider: discovery, JWKS and a token endpoint
// that signs an ID token with the nonce and PKCE challenge the test expects.
type fakeIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu        sync.Mutex
	nonce     string
	challenge string
	claims    map[string]any
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{key: key}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]any{
			"issuer":                                f.srv.URL,
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"jwks_uri":                              f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.PostForm.Get("code") != "x" {
			writeTestJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
			writeTestJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "pkce mismatch"})
			return
		}
		now := time.Now()
		claims := map[string]any{}
		for k, v := range f.claims {
			claims[k] = v
		}
		claims["iss"] = f.srv.URL
		claims["aud"] = oidcTestClientID
		claims["nonce"] = f.nonce
		claims["iat"] = now.Unix()
		claims["exp"] = now.Add(time.Hour).Unix()
		payload, err := json.Marshal(claims)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		signer, err := jose.NewSigner(jose.SigningKey{
			Algorithm: jose.RS256,
			Key:       &jose.JSONWebKey{Key: f.key, KeyID: "k1"},
		}, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		obj, err := signer.Sign(payload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		raw, err := obj.CompactSerialize()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeTestJSON(w, map[string]any{
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     raw,
		})
	})
	return f
}

// expect records the nonce and PKCE challenge of the login under test.
func (f *fakeIdP) expect(nonce, challenge string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nonce, f.challenge = nonce, challenge
}

// setUser sets the claims the next ID token will carry.
func (f *fakeIdP) setUser(sub, username, email string, groups ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if groups == nil {
		groups = []string{}
	}
	f.claims = map[string]any{
		"sub":                sub,
		"preferred_username": username,
		"email":              email,
		"email_verified":     true,
		"groups":             groups,
	}
}

// oidcEnv is a doreg API served over HTTP, wired to a fakeIdP.
type oidcEnv struct {
	base   string
	db     *meta.DB
	authn  *auth.Authenticator
	idp    *fakeIdP
	client *http.Client // keeps cookies, does not follow redirects
}

func newOIDCEnv(t *testing.T, adminGroup string) *oidcEnv {
	t.Helper()
	return newOIDCEnvCfg(t, func(c *oidc.Config) { c.AdminRole = adminGroup })
}

// newOIDCEnvCfg lets a test adjust the provider configuration.
func newOIDCEnvCfg(t *testing.T, configure func(*oidc.Config)) *oidcEnv {
	t.Helper()
	ctx := t.Context()
	idp := newFakeIdP(t)
	dir := t.TempDir()
	db, err := meta.Open(metatest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authn := auth.New(db)
	collector := gc.New(db, store, logger)
	bus := events.New(db, logger)

	// The listener exists before the API, so the public URL is known when the
	// OIDC provider is configured.
	srv := httptest.NewUnstartedServer(nil)
	base := "http://" + srv.Listener.Addr().String()
	cfg := oidc.Config{
		Issuer:        idp.srv.URL,
		ClientID:      oidcTestClientID,
		ClientSecret:  "s",
		ProviderName:  "Fake",
		RedirectURI:   base + "/api/auth/oidc/callback",
		Scopes:        []string{"openid", "profile", "email", "groups"},
		UsernameClaim: "preferred_username",
	}
	configure(&cfg)
	provider, err := oidc.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = api.New(db, store, authn, collector, bus, provider, logger)
	srv.Start()
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &oidcEnv{
		base:   base,
		db:     db,
		authn:  authn,
		idp:    idp,
		client: noRedirectClient(jar),
	}
}

func noRedirectClient(jar http.CookieJar) *http.Client {
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// startLogin calls /api/auth/oidc/login and returns the state, nonce and PKCE
// challenge from the provider redirect.
func (e *oidcEnv) startLogin(t *testing.T, next string) (state, nonce, challenge string) {
	t.Helper()
	resp, _ := e.do(t, e.client, http.MethodGet, "/api/auth/oidc/login?next="+url.QueryEscape(next), nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: status %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Scheme+"://"+loc.Host != e.idp.srv.URL || loc.Path != "/authorize" {
		t.Fatalf("login: redirect to %q, want the provider authorize endpoint", loc)
	}
	q := loc.Query()
	if q.Get("code_challenge_method") != "S256" {
		t.Fatalf("login: code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	return q.Get("state"), q.Get("nonce"), q.Get("code_challenge")
}

// login runs a full SSO login with the identity set on the fake provider and
// returns the callback response.
func (e *oidcEnv) login(t *testing.T, next string) *http.Response {
	t.Helper()
	state, nonce, challenge := e.startLogin(t, next)
	e.idp.expect(nonce, challenge)
	resp, _ := e.do(t, e.client, http.MethodGet, "/api/auth/oidc/callback?state="+url.QueryEscape(state)+"&code=x", nil)
	return resp
}

// do sends a request and returns the response with its body read and closed.
func (e *oidcEnv) do(t *testing.T, c *http.Client, method, path string, body io.Reader) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, e.base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

// me returns the username, role and groups of the session behind client c.
func (e *oidcEnv) me(t *testing.T, c *http.Client) (username, role string, groups []string) {
	t.Helper()
	resp, body := e.do(t, c, http.MethodGet, "/api/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/me: status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		User struct {
			Username string   `json:"username"`
			Role     string   `json:"role"`
			Groups   []string `json:"groups"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out.User.Username, out.User.Role, out.User.Groups
}

func TestOIDCFirstLoginCreatesUser(t *testing.T) {
	e := newOIDCEnv(t, "")
	// An existing admin keeps the first-SSO-user rule from promoting alice.
	if err := e.db.UpsertUser(t.Context(), "root", "", meta.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	e.idp.setUser("sub-alice", "alice", "alice@example.com", "dev", "ops")

	resp := e.login(t, "/projects/demo")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback: status %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/projects/demo" {
		t.Fatalf("callback redirect = %q, want /projects/demo", loc)
	}
	session := cookieNamed(resp, "doreg_session")
	if session == nil || session.Value == "" {
		t.Fatal("callback did not set doreg_session")
	}
	if !session.HttpOnly {
		t.Error("doreg_session is not HttpOnly")
	}

	u, err := e.db.UserByName(t.Context(), "alice")
	if err != nil {
		t.Fatalf("user not created: %v", err)
	}
	if u.Role != meta.RoleUser {
		t.Errorf("role = %q, want %q", u.Role, meta.RoleUser)
	}
	if want := e.idp.srv.URL + "|sub-alice"; u.OIDCSubject != want {
		t.Errorf("OIDC subject = %q, want %q", u.OIDCSubject, want)
	}

	username, role, groups := e.me(t, e.client)
	if username != "alice" {
		t.Errorf("/api/me username = %q, want alice", username)
	}
	if role != meta.RoleUser {
		t.Errorf("/api/me role = %q, want %q", role, meta.RoleUser)
	}
	if strings.Join(groups, ",") != "dev,ops" {
		t.Errorf("/api/me groups = %v, want [dev ops]", groups)
	}
}

func TestOIDCSessionIsShortLived(t *testing.T) {
	e := newOIDCEnv(t, "")
	e.idp.setUser("sub-1", "alice", "alice@example.com")
	session := cookieNamed(e.login(t, "/"), "doreg_session")
	if session == nil {
		t.Fatal("callback did not set doreg_session")
	}
	const want = 24 * time.Hour
	if left := time.Until(session.Expires); left > want || left < want-time.Minute {
		t.Errorf("SSO session expires in %s, want about %s", left, want)
	}
}

func TestOIDCAdminGroupTogglesRole(t *testing.T) {
	e := newOIDCEnv(t, "admins")
	e.idp.setUser("sub-bob", "bob", "bob@example.com", "admins")
	if resp := e.login(t, "/"); resp.StatusCode != http.StatusFound {
		t.Fatalf("first login: status %d, want 302", resp.StatusCode)
	}
	if _, role, _ := e.me(t, e.client); role != meta.RoleAdmin {
		t.Fatalf("member of admin group: role = %q, want %q", role, meta.RoleAdmin)
	}

	e.idp.setUser("sub-bob", "bob", "bob@example.com", "dev")
	if resp := e.login(t, "/"); resp.StatusCode != http.StatusFound {
		t.Fatalf("second login: status %d, want 302", resp.StatusCode)
	}
	if _, role, _ := e.me(t, e.client); role != meta.RoleUser {
		t.Fatalf("after leaving admin group: role = %q, want %q", role, meta.RoleUser)
	}
}

func TestOIDCLocalAccountNotTakenOver(t *testing.T) {
	e := newOIDCEnv(t, "")
	ctx := t.Context()
	hash, err := auth.HashPassword("local-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.UpsertUser(ctx, "alice", hash, meta.RoleUser); err != nil {
		t.Fatal(err)
	}
	e.idp.setUser("sub-alice", "alice", "alice@example.com")

	if resp := e.login(t, "/"); resp.StatusCode != http.StatusFound {
		t.Fatalf("login: status %d, want 302", resp.StatusCode)
	}
	if username, _, _ := e.me(t, e.client); username != "alice-2" {
		t.Errorf("SSO session username = %q, want alice-2", username)
	}

	local, err := e.db.UserByName(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if local.OIDCSubject != "" || local.PasswordHash != hash {
		t.Errorf("local account alice was modified: subject=%q", local.OIDCSubject)
	}
	sso, err := e.db.UserByOIDCSubject(ctx, e.idp.srv.URL+"|sub-alice")
	if err != nil {
		t.Fatal(err)
	}
	if sso.Username != "alice-2" {
		t.Errorf("SSO account username = %q, want alice-2", sso.Username)
	}
}

func TestOIDCStateMismatchRejected(t *testing.T) {
	e := newOIDCEnv(t, "")
	e.idp.setUser("sub-dave", "dave", "dave@example.com")

	cases := []struct {
		name        string
		callbackQS  func(state string) string
		cookieValue string
	}{
		{
			name:        "wrong cookie",
			callbackQS:  func(state string) string { return "state=" + url.QueryEscape(state) + "&code=x" },
			cookieValue: "not-the-state",
		},
		{
			name:        "wrong state parameter",
			callbackQS:  func(string) string { return "state=forged&code=x" },
			cookieValue: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh client without a cookie jar, so the cookie is set explicitly.
			c := noRedirectClient(nil)
			state, _, _ := e.startLogin(t, "/")

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
				e.base+"/api/auth/oidc/callback?"+tc.callbackQS(state), nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.cookieValue != "" {
				req.AddCookie(&http.Cookie{Name: "doreg_oidc_state", Value: tc.cookieValue})
			}
			resp, err := c.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusFound {
				t.Fatalf("status %d, want 302", resp.StatusCode)
			}
			loc := resp.Header.Get("Location")
			if !strings.HasPrefix(loc, "/login?error=") {
				t.Fatalf("redirect = %q, want /login?error=...", loc)
			}
			if !strings.Contains(loc, "state+mismatch") && !strings.Contains(loc, "state%20mismatch") {
				t.Errorf("redirect = %q, want a state mismatch message", loc)
			}
			if c := cookieNamed(resp, "doreg_session"); c != nil && c.Value != "" {
				t.Error("state mismatch set a session cookie")
			}
			if _, body := e.do(t, c, http.MethodGet, "/api/me", nil); !strings.Contains(string(body), "not logged in") {
				t.Errorf("/api/me after rejected login: %s", body)
			}
		})
	}
}

func TestOIDCCLISecretLifecycle(t *testing.T) {
	e := newOIDCEnv(t, "")
	ctx := t.Context()
	e.idp.setUser("sub-carol", "carol", "carol@example.com")
	if resp := e.login(t, "/"); resp.StatusCode != http.StatusFound {
		t.Fatalf("login: status %d, want 302", resp.StatusCode)
	}

	resp, body := e.do(t, e.client, http.MethodPost, "/api/me/cli-secret", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST cli-secret: status %d: %s", resp.StatusCode, body)
	}
	var created struct {
		Username string `json:"username"`
		Secret   string `json:"secret"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if created.Username != "carol" || created.Secret == "" {
		t.Fatalf("POST cli-secret returned username=%q secret-empty=%v", created.Username, created.Secret == "")
	}

	p, ok := e.authn.Basic(ctx, created.Username, created.Secret)
	if !ok || p.User == nil || p.User.Username != "carol" {
		t.Fatal("CLI secret does not authenticate after creation")
	}

	resp, body = e.do(t, e.client, http.MethodDelete, "/api/me/cli-secret", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE cli-secret: status %d: %s", resp.StatusCode, body)
	}
	if _, ok := e.authn.Basic(ctx, created.Username, created.Secret); ok {
		t.Error("CLI secret still authenticates after DELETE")
	}
}

func TestOIDCAuthConfigReportsProvider(t *testing.T) {
	e := newOIDCEnv(t, "")
	resp, body := e.do(t, e.client, http.MethodGet, "/api/auth/config", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["oidc"] != true {
		t.Errorf("oidc = %v, want true", cfg["oidc"])
	}
	if cfg["oidcName"] != "Fake" {
		t.Errorf("oidcName = %v, want Fake", cfg["oidcName"])
	}
}

func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func writeTestJSON(w http.ResponseWriter, v any) {
	writeTestJSONStatus(w, http.StatusOK, v)
}

func writeTestJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
