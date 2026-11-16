// Package api is the JSON API behind the web UI.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/buildinfo"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/gc"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oidc"
	"github.com/p-arndt/doreg/internal/registry"
	"github.com/p-arndt/doreg/internal/storage"
)

const (
	sessionCookie = "doreg_session"
	sessionTTL    = 30 * 24 * time.Hour
	// SSO accounts are disabled at the provider, which doreg only notices at
	// the next login; a short session bounds how long a removed user stays in.
	ssoSessionTTL = 24 * time.Hour
)

// API serves /api/.
type API struct {
	DB     *meta.DB
	Store  *storage.FS
	Auth   *auth.Authenticator
	GC     *gc.Collector
	Events *events.Bus
	OIDC   *oidc.Provider // nil when SSO is not configured
	Log    *slog.Logger
	// Registry serves the imports' pushes in-process; nil disables import.
	Registry *registry.Handler
	// PublicBrowse lets visitors without a session list and inspect public
	// repositories. Off by default: unlike anonymous pulls, it reveals which
	// repository names exist.
	PublicBrowse bool

	images  *imageCache
	imports imports
	mux     *http.ServeMux
}

// New wires the routes.
func New(db *meta.DB, store *storage.FS, a *auth.Authenticator, collector *gc.Collector, bus *events.Bus,
	provider *oidc.Provider, log *slog.Logger) *API {
	api := &API{DB: db, Store: store, Auth: a, GC: collector, Events: bus, OIDC: provider, Log: log,
		images: newImageCache(), mux: http.NewServeMux()}
	m := api.mux
	m.HandleFunc("GET /api/auth/config", api.authConfig)
	m.HandleFunc("GET /api/auth/oidc/login", api.oidcLogin)
	m.HandleFunc("GET /api/auth/oidc/callback", api.oidcCallback)
	m.HandleFunc("POST /api/me/cli-secret", api.user(api.createCLISecret))
	m.HandleFunc("DELETE /api/me/cli-secret", api.user(api.deleteCLISecret))
	m.HandleFunc("POST /api/login", api.login)
	m.HandleFunc("POST /api/logout", api.logout)
	m.HandleFunc("GET /api/me", api.user(api.me))

	m.HandleFunc("GET /api/stats", api.user(api.stats))
	m.HandleFunc("GET /api/repos", api.optional(api.repos))
	m.HandleFunc("GET /api/repo", api.optional(api.repo))
	m.HandleFunc("PATCH /api/repo", api.user(api.updateRepo))
	m.HandleFunc("DELETE /api/repo", api.user(api.deleteRepo))
	m.HandleFunc("GET /api/manifest", api.optional(api.manifest))
	m.HandleFunc("DELETE /api/manifest", api.user(api.deleteManifest))
	m.HandleFunc("DELETE /api/tag", api.user(api.deleteTag))

	m.HandleFunc("GET /api/projects", api.user(api.projects))
	m.HandleFunc("POST /api/projects", api.admin(api.createProject))
	m.HandleFunc("GET /api/projects/{name}", api.user(api.project))
	m.HandleFunc("PATCH /api/projects/{name}", api.user(api.updateProject))
	m.HandleFunc("DELETE /api/projects/{name}", api.admin(api.deleteProject))
	m.HandleFunc("POST /api/projects/{name}/members", api.user(api.addMember))
	m.HandleFunc("PUT /api/projects/{name}/members/{id}", api.user(api.updateMember))
	m.HandleFunc("DELETE /api/projects/{name}/members/{id}", api.user(api.removeMember))

	m.HandleFunc("GET /api/robots", api.user(api.robots))
	m.HandleFunc("POST /api/robots", api.user(api.createRobot))
	m.HandleFunc("PUT /api/robots/{id}", api.user(api.updateRobot))
	m.HandleFunc("POST /api/robots/{id}/secret", api.user(api.rotateRobotSecret))
	m.HandleFunc("DELETE /api/robots/{id}", api.user(api.deleteRobot))

	m.HandleFunc("GET /api/audit", api.user(api.audit))

	m.HandleFunc("GET /api/webhooks", api.user(api.webhooks))
	m.HandleFunc("POST /api/webhooks", api.user(api.saveWebhook))
	m.HandleFunc("PUT /api/webhooks/{id}", api.user(api.saveWebhook))
	m.HandleFunc("DELETE /api/webhooks/{id}", api.user(api.deleteWebhook))
	m.HandleFunc("POST /api/webhooks/{id}/test", api.user(api.testWebhook))
	m.HandleFunc("GET /api/webhooks/{id}/deliveries", api.user(api.deliveries))

	m.HandleFunc("GET /api/gc", api.admin(api.gcStatus))
	m.HandleFunc("PUT /api/gc/settings", api.admin(api.gcSaveSettings))
	m.HandleFunc("POST /api/gc/run", api.admin(api.gcRun))
	m.HandleFunc("GET /api/gc/runs/{id}", api.admin(api.gcGetRun))

	m.HandleFunc("GET /api/policies", api.admin(api.policies))
	m.HandleFunc("POST /api/policies", api.admin(api.savePolicy))
	m.HandleFunc("PUT /api/policies/{id}", api.admin(api.savePolicy))
	m.HandleFunc("DELETE /api/policies/{id}", api.admin(api.deletePolicy))
	m.HandleFunc("POST /api/policies/preview", api.admin(api.previewPolicies))

	m.HandleFunc("GET /api/import", api.admin(api.importStatus))
	m.HandleFunc("POST /api/import", api.admin(api.importStart))
	m.HandleFunc("POST /api/import/preview", api.admin(api.importPreview))
	m.HandleFunc("POST /api/import/cancel", api.admin(api.importCancel))

	m.HandleFunc("GET /api/users", api.admin(api.users))
	m.HandleFunc("POST /api/users", api.admin(api.createUser))
	m.HandleFunc("PUT /api/users/{id}", api.admin(api.updateUser))
	m.HandleFunc("DELETE /api/users/{id}", api.admin(api.deleteUser))
	m.HandleFunc("PUT /api/me/password", api.user(api.changePassword))

	m.HandleFunc("POST /api/cli/device", api.cliStart)
	m.HandleFunc("POST /api/cli/token", api.cliPoll)
	m.HandleFunc("GET /api/cli/device/{code}", api.user(api.cliDeviceStatus))
	m.HandleFunc("POST /api/cli/device/{code}/approve", api.user(api.cliApprove))
	m.HandleFunc("POST /api/cli/device/{code}/deny", api.user(api.cliDeny))
	m.HandleFunc("GET /api/me/tokens", api.user(api.cliTokens))
	m.HandleFunc("DELETE /api/me/tokens/{id}", api.user(api.cliRevokeToken))
	return api
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
		writeErr(w, http.StatusForbidden, "cross-origin request rejected")
		return
	}
	a.mux.ServeHTTP(w, r)
}

// sameOrigin is the CSRF check for state-changing requests, on top of the
// SameSite=Strict session cookie.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") == "" || r.Header.Get("Sec-Fetch-Site") == "same-origin"
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

type ctxKey struct{}

func currentUser(r *http.Request) meta.User { return r.Context().Value(ctxKey{}).(meta.User) }

// principal is the signed-in user, or auth.Anonymous on optional routes.
func principal(r *http.Request) auth.Principal {
	if u, ok := r.Context().Value(ctxKey{}).(meta.User); ok {
		return auth.Principal{User: &u}
	}
	return auth.Anonymous
}

func (a *API) authenticate(r *http.Request) (meta.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return meta.User{}, false
	}
	u, err := a.DB.SessionUser(r.Context(), c.Value)
	return u, err == nil
}

func (a *API) guard(check func(meta.User) bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := a.authenticate(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "not logged in")
			return
		}
		if !check(u) {
			writeErr(w, http.StatusForbidden, "permission denied")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func (a *API) user(next http.HandlerFunc) http.HandlerFunc {
	return a.guard(func(meta.User) bool { return true }, next)
}

// optional admits visitors without a session when PublicBrowse is on; the
// handler then sees auth.Anonymous and Can only grants public content.
func (a *API) optional(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if u, ok := a.authenticate(r); ok {
			next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
			return
		}
		if !a.PublicBrowse {
			writeErr(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next(w, r)
	}
}

func (a *API) admin(next http.HandlerFunc) http.HandlerFunc {
	return a.guard(meta.User.IsAdmin, next)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, meta.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	a.Log.Error("api request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

// maxAuditUsername bounds the attacker-chosen username of failed logins in the
// audit log; real usernames are far shorter.
const maxAuditUsername = 128

func truncateRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password string }
	if !decode(w, r, &body) {
		return
	}
	ip := clientIP(r)
	// Blocked attempts are not audited: logging them would let a client keep
	// filling the audit log once it is throttled.
	if ok, wait := a.Auth.Allowed(ip, body.Username); !ok {
		w.Header().Set("Retry-After", auth.RetryAfter(wait))
		writeErr(w, http.StatusTooManyRequests, "too many failed login attempts, try again later")
		return
	}
	u, ok := a.Auth.CheckPassword(r.Context(), body.Username, body.Password)
	if !ok {
		a.Auth.Failed(ip, body.Username)
		a.Events.Audit(r.Context(), truncateRunes(body.Username, maxAuditUsername), "user", "login_failed", "", "", "", ip)
		writeErr(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	a.Auth.Succeeded(ip, body.Username)
	if err := a.startSession(w, r, u); err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.meBody(r.Context(), u))
}

func (a *API) startSession(w http.ResponseWriter, r *http.Request, u meta.User) error {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	ttl := sessionTTL
	if u.SSO() {
		ttl = ssoSessionTTL
	}
	expires := time.Now().Add(ttl)
	if err := a.DB.CreateSession(r.Context(), token, u.ID, expires.Unix()); err != nil {
		return err
	}
	a.DB.TouchLogin(r.Context(), u.ID)
	a.Events.Audit(r.Context(), u.Username, "user", "login", "", "", "", clientIP(r))
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure(r),
		// Lax, not Strict: the OIDC callback is a cross-site redirect and must
		// carry the cookie it sets on to the UI. Mutations are guarded by sameOrigin.
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func secure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// clientIP prefers the proxy's X-Forwarded-For (doreg runs behind one).
func clientIP(r *http.Request) string {
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		ip, _, _ := strings.Cut(f, ",")
		return strings.TrimSpace(ip)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// audit records an action of the current user.
func (a *API) record(r *http.Request, action, project, resource, detail string) {
	u := currentUser(r)
	a.Events.Audit(r.Context(), u.Username, "user", action, project, resource, detail, clientIP(r))
}

// allow checks a project permission for the current user and writes 403 if denied.
func (a *API) allow(w http.ResponseWriter, r *http.Request, project string, action auth.Action) bool {
	p := principal(r)
	ok, err := a.Auth.Can(r.Context(), p, project, action)
	if err != nil {
		a.fail(w, r, err)
		return false
	}
	if !ok {
		denied(w, p)
	}
	return ok
}

// denied answers 401 to visitors, so the UI offers a login, and 403 to users.
func denied(w http.ResponseWriter, p auth.Principal) {
	if p.Kind() == "anonymous" {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return
	}
	writeErr(w, http.StatusForbidden, "permission denied")
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = a.DB.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) meBody(ctx context.Context, u meta.User) map[string]any {
	// Projects the user can manage decide which admin pages the UI offers.
	managed := []string{}
	if projects, err := a.DB.Projects(ctx); err == nil {
		for _, p := range projects {
			if ok, _ := a.Auth.Can(ctx, auth.Principal{User: &u}, p.Name, auth.Manage); ok {
				managed = append(managed, p.Name)
			}
		}
	}
	return map[string]any{"user": u, "version": buildinfo.Version, "managedProjects": managed}
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.meBody(r.Context(), currentUser(r)))
}

func parseInt(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
