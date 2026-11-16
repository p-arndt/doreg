package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/gc"
	"github.com/p-arndt/doreg/internal/meta"
)

func gcRuns(ctx context.Context, a *API, limit int) ([]gc.Run, error) {
	return gc.Runs(ctx, a.DB, limit)
}

func (a *API) gcStatus(w http.ResponseWriter, r *http.Request) {
	runs, err := gcRuns(r.Context(), a, 50)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var next int64
	if t := a.GC.NextRun(); !t.IsZero() {
		next = t.Unix()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": a.GC.Settings(r.Context()),
		"running":  a.GC.Running(),
		"nextRun":  next,
		"runs":     runs,
	})
}

func (a *API) gcSaveSettings(w http.ResponseWriter, r *http.Request) {
	var s gc.Settings
	if !decode(w, r, &s) {
		return
	}
	if err := a.GC.SaveSettings(r.Context(), s); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *API) gcRun(w http.ResponseWriter, r *http.Request) {
	var opts gc.Options
	if !decode(w, r, &opts) {
		return
	}
	opts.Trigger = "manual"
	if opts.DryRun {
		opts.Trigger = "preview"
	}
	// Detached from the request: closing the browser tab must not abort a sweep
	// halfway through deleting files.
	run, err := a.GC.Run(context.WithoutCancel(r.Context()), opts)
	if errors.Is(err, gc.ErrRunning) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil && run == nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (a *API) gcGetRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	run, err := gc.GetRun(r.Context(), a.DB, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (a *API) policies(w http.ResponseWriter, r *http.Request) {
	ps, err := gc.Policies(r.Context(), a.DB)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (a *API) savePolicy(w http.ResponseWriter, r *http.Request) {
	var p gc.Policy
	if !decode(w, r, &p) {
		return
	}
	p.ID = 0
	if r.PathValue("id") != "" {
		id, ok := pathID(r)
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid id")
			return
		}
		p.ID = id
	}
	id, err := gc.SavePolicy(r.Context(), a.DB, p)
	if errors.Is(err, meta.ErrNotFound) {
		a.fail(w, r, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p.ID = id
	writeJSON(w, http.StatusOK, p)
}

func (a *API) deletePolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := gc.DeletePolicy(r.Context(), a.DB, id); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// previewPolicies evaluates unsaved policies so the editor can show live which
// tags they would delete.
func (a *API) previewPolicies(w http.ResponseWriter, r *http.Request) {
	var ps []gc.Policy
	if !decode(w, r, &ps) {
		return
	}
	for i := range ps {
		if err := ps[i].Validate(); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	dels, err := gc.Evaluate(r.Context(), a.DB.R, ps, meta.Now())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if dels == nil {
		dels = []gc.TagDeletion{}
	}
	writeJSON(w, http.StatusOK, dels)
}

func (a *API) users(w http.ResponseWriter, r *http.Request) {
	us, err := a.DB.Users(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, us)
}

type userBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

const minPasswordLen = 8

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var b userBody
	if !decode(w, r, &b) {
		return
	}
	b.Username = strings.TrimSpace(b.Username)
	if b.Username == "" || strings.ContainsAny(b.Username, ": ") {
		writeErr(w, http.StatusBadRequest, "username must not be empty or contain spaces or colons")
		return
	}
	if !meta.ValidRole(b.Role) {
		writeErr(w, http.StatusBadRequest, "role must be user or admin")
		return
	}
	if len(b.Password) < minPasswordLen {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if _, err := a.DB.UserByName(r.Context(), b.Username); err == nil {
		writeErr(w, http.StatusConflict, "user already exists")
		return
	}
	hash, err := auth.HashPassword(b.Password)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.DB.UpsertUser(r.Context(), b.Username, hash, b.Role); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var b userBody
	if !decode(w, r, &b) {
		return
	}
	if !meta.ValidRole(b.Role) {
		writeErr(w, http.StatusBadRequest, "role must be user or admin")
		return
	}
	if b.Password != "" && len(b.Password) < minPasswordLen {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if !a.keepsAnAdmin(r, id, b.Role) {
		writeErr(w, http.StatusBadRequest, "at least one admin must remain")
		return
	}
	hash := ""
	if b.Password != "" {
		var err error
		if hash, err = auth.HashPassword(b.Password); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	if err := a.DB.UpdateUser(r.Context(), id, hash, b.Role); err != nil {
		a.fail(w, r, err)
		return
	}
	// A new password must lock out whoever knew the old one; the role alone is
	// read from the database on every request and needs no logout.
	if hash != "" {
		keep := ""
		if id == currentUser(r).ID {
			keep = requestSessionToken(r)
		}
		if err := a.DB.DeleteUserSessionsExcept(r.Context(), id, keep); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.Auth.Invalidate()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if id == currentUser(r).ID {
		writeErr(w, http.StatusBadRequest, "you cannot delete yourself")
		return
	}
	if !a.keepsAnAdmin(r, id, "") {
		writeErr(w, http.StatusBadRequest, "at least one admin must remain")
		return
	}
	if err := a.DB.DeleteUser(r.Context(), id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.Invalidate()
	w.WriteHeader(http.StatusNoContent)
}

// keepsAnAdmin reports whether changing user id to newRole ("" = deleting it)
// leaves at least one admin.
func (a *API) keepsAnAdmin(r *http.Request, id int64, newRole string) bool {
	if newRole == meta.RoleAdmin {
		return true
	}
	us, err := a.DB.Users(r.Context())
	if err != nil {
		return false
	}
	for _, u := range us {
		if u.ID != id && u.IsAdmin() {
			return true
		}
	}
	return false
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &b) {
		return
	}
	u := currentUser(r)
	if u.PasswordHash == "" {
		writeErr(w, http.StatusBadRequest, "this account signs in through single sign-on and has no password")
		return
	}
	// CheckPassword, not Check: a CLI secret can be minted with only a session,
	// so accepting it here would let a stolen session take over the account.
	if _, ok := a.Auth.CheckPassword(r.Context(), u.Username, b.Current); !ok {
		writeErr(w, http.StatusBadRequest, "current password is wrong")
		return
	}
	if len(b.New) < minPasswordLen {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	hash, err := auth.HashPassword(b.New)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.DB.UpdateUser(r.Context(), u.ID, hash, u.Role); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.DB.DeleteUserSessionsExcept(r.Context(), u.ID, requestSessionToken(r)); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.Invalidate()
	w.WriteHeader(http.StatusNoContent)
}

// requestSessionToken returns the session token of the request, or "" if it
// has none.
func requestSessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}
