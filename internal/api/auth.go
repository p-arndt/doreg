package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/meta"
)

const oidcStateCookie = "doreg_oidc_state"

func (a *API) authConfig(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"oidc": a.OIDC != nil, "localLogin": true}
	if a.OIDC != nil {
		out["oidcName"] = a.OIDC.Name()
	}
	writeJSON(w, http.StatusOK, out)
}

// safeNext keeps post-login redirects on this site.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}

func (a *API) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		writeErr(w, http.StatusNotFound, "single sign-on is not configured")
		return
	}
	redirect, state := a.OIDC.Start(safeNext(r.URL.Query().Get("next")))
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: state, Path: "/api/auth/oidc", MaxAge: 600,
		HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (a *API) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		writeErr(w, http.StatusNotFound, "single sign-on is not configured")
		return
	}
	loginError := func(msg string) {
		http.Redirect(w, r, "/login?error="+url.QueryEscape(msg), http.StatusFound)
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		loginError(fmt.Sprintf("%s: %s", e, q.Get("error_description")))
		return
	}
	cookieState := ""
	if c, err := r.Cookie(oidcStateCookie); err == nil {
		cookieState = c.Value
	}
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/api/auth/oidc", MaxAge: -1})
	id, next, err := a.OIDC.Finish(r.Context(), q.Get("state"), cookieState, q.Get("code"))
	if err != nil {
		a.Log.Warn("oidc login failed", "err", err)
		loginError(err.Error())
		return
	}
	u, err := a.provisionOIDCUser(r, id.Subject, id.Username, id.Email, id.Groups)
	if err != nil {
		a.Log.Error("oidc provisioning failed", "err", err)
		loginError("could not create your account")
		return
	}
	if err := a.startSession(w, r, u); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, next, http.StatusFound)
}

// provisionOIDCUser finds or creates the account for an SSO identity. An
// existing local account with the same name is never taken over: the SSO user
// gets a suffixed name instead.
func (a *API) provisionOIDCUser(r *http.Request, subject, username, email string, groups []string) (meta.User, error) {
	ctx := r.Context()
	role := meta.RoleUser
	adminGroup := a.OIDC.AdminRole()
	isAdmin := false
	for _, g := range groups {
		if g == adminGroup && adminGroup != "" {
			isAdmin = true
		}
	}
	if isAdmin {
		role = meta.RoleAdmin
	}
	u, err := a.DB.UserByOIDCSubject(ctx, subject)
	if err == nil {
		if adminGroup == "" {
			role = u.Role
		}
		if err := a.DB.UpdateOIDCLogin(ctx, u.ID, email, role, groups); err != nil {
			return u, err
		}
		a.Auth.Invalidate()
		return a.DB.UserByID(ctx, u.ID)
	}
	if !errors.Is(err, meta.ErrNotFound) {
		return u, err
	}
	name := sanitizeUsername(username)
	for i := 2; ; i++ {
		if _, err := a.DB.UserByName(ctx, name); errors.Is(err, meta.ErrNotFound) {
			break
		}
		name = fmt.Sprintf("%s-%d", sanitizeUsername(username), i)
	}
	u, err = a.DB.CreateOIDCUser(ctx, meta.User{Username: name, Role: role, Email: email, OIDCSubject: subject, Groups: groups})
	if err == nil {
		a.Events.Audit(ctx, name, "user", "user_create", "", name, "first sign-in via SSO", clientIP(r))
	}
	return u, err
}

func sanitizeUsername(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == ':' || r == ' ' || r == '$' {
			return '-'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" {
		return "user"
	}
	return s
}

// createCLISecret issues a new secret for docker login. It is shown once.
func (a *API) createCLISecret(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	secret := auth.NewSecret()
	hash, err := auth.HashPassword(secret)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.DB.SetCLISecret(r.Context(), u.ID, hash); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.Invalidate()
	a.record(r, "cli_secret_create", "", u.Username, "")
	writeJSON(w, http.StatusOK, map[string]string{"username": u.Username, "secret": secret})
}

func (a *API) deleteCLISecret(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := a.DB.SetCLISecret(r.Context(), u.ID, ""); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.Invalidate()
	a.record(r, "cli_secret_delete", "", u.Username, "")
	w.WriteHeader(http.StatusNoContent)
}
