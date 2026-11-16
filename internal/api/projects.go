package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/meta"
)

type projectItem struct {
	meta.Project
	Role         string `json:"role"` // the current user's effective role
	Repositories int    `json:"repositories"`
	UsedBytes    int64  `json:"usedBytes"`
}

func (a *API) projectItem(r *http.Request, p meta.Project) (projectItem, error) {
	u := currentUser(r)
	role, err := a.Auth.Role(r.Context(), auth.Principal{User: &u}, p.Name)
	if err != nil {
		return projectItem{}, err
	}
	it := projectItem{Project: p, Role: role}
	if it.Repositories, err = a.DB.ProjectRepoCount(r.Context(), p.Name); err != nil {
		return it, err
	}
	it.UsedBytes, err = a.DB.ProjectUsage(r.Context(), p.Name)
	return it, err
}

// projects lists the projects the user can see: members, public ones, or all
// for admins.
func (a *API) projects(w http.ResponseWriter, r *http.Request) {
	all, err := a.DB.Projects(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := []projectItem{}
	for _, p := range all {
		it, err := a.projectItem(r, p)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if it.Role == "" && !p.Public {
			continue
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, out)
}

type projectBody struct {
	Name       string `json:"name"`
	Public     *bool  `json:"public"`
	QuotaBytes *int64 `json:"quotaBytes"`
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) {
	var b projectBody
	if !decode(w, r, &b) {
		return
	}
	if !meta.ValidProjectName(b.Name) {
		writeErr(w, http.StatusBadRequest, "project names use lowercase letters, digits and . _ - separators")
		return
	}
	p := meta.Project{Name: b.Name}
	if b.Public != nil {
		p.Public = *b.Public
	}
	if b.QuotaBytes != nil && *b.QuotaBytes > 0 {
		p.QuotaBytes = *b.QuotaBytes
	}
	p, err := a.DB.CreateProject(r.Context(), p)
	if errors.Is(err, meta.ErrExists) {
		writeErr(w, http.StatusConflict, "a project with this name exists")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "project_create", p.Name, p.Name, "")
	writeJSON(w, http.StatusCreated, p)
}

func (a *API) loadProject(w http.ResponseWriter, r *http.Request) (meta.Project, bool) {
	p, err := a.DB.ProjectByName(r.Context(), r.PathValue("name"))
	if err != nil {
		a.fail(w, r, err)
		return p, false
	}
	return p, true
}

func (a *API) project(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok || !a.allow(w, r, p.Name, auth.Pull) {
		return
	}
	it, err := a.projectItem(r, p)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := map[string]any{"project": it}
	if ok, _ := a.Auth.Can(r.Context(), auth.Principal{User: ptr(currentUser(r))}, p.Name, auth.Manage); ok {
		members, err := a.DB.Members(r.Context(), p.ID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		out["members"] = members
	}
	writeJSON(w, http.StatusOK, out)
}

func ptr[T any](v T) *T { return &v }

func (a *API) updateProject(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok || !a.allow(w, r, p.Name, auth.Manage) {
		return
	}
	var b projectBody
	if !decode(w, r, &b) {
		return
	}
	if b.Public != nil {
		p.Public = *b.Public
	}
	if b.QuotaBytes != nil {
		// Project admins could otherwise lift their own limit.
		if !currentUser(r).IsAdmin() {
			writeErr(w, http.StatusForbidden, "only administrators change quotas")
			return
		}
		p.QuotaBytes = max(0, *b.QuotaBytes)
	}
	if err := a.DB.UpdateProject(r.Context(), p); err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "project_update", p.Name, p.Name, fmt.Sprintf("public=%t quota=%d", p.Public, p.QuotaBytes))
	writeJSON(w, http.StatusOK, p)
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := a.DB.DeleteProject(r.Context(), name); err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "project_delete", name, name, "")
	w.WriteHeader(http.StatusNoContent)
}

type memberBody struct {
	Username string `json:"username"`
	Group    string `json:"group"`
	Role     string `json:"role"`
}

func (a *API) addMember(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok || !a.allow(w, r, p.Name, auth.Manage) {
		return
	}
	var b memberBody
	if !decode(w, r, &b) {
		return
	}
	if !meta.ValidProjectRole(b.Role) {
		writeErr(w, http.StatusBadRequest, "role must be read, write or admin")
		return
	}
	var userID int64
	subject := b.Group
	switch {
	case b.Username != "":
		u, err := a.DB.UserByName(r.Context(), b.Username)
		if errors.Is(err, meta.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "no user named "+b.Username)
			return
		}
		if err != nil {
			a.fail(w, r, err)
			return
		}
		userID, subject = u.ID, u.Username
	case b.Group == "":
		writeErr(w, http.StatusBadRequest, "give a username or a group")
		return
	}
	if err := a.DB.AddMember(r.Context(), p.ID, userID, b.Group, b.Role); err != nil {
		if errors.Is(err, meta.ErrExists) {
			writeErr(w, http.StatusConflict, subject+" is already a member")
			return
		}
		a.fail(w, r, err)
		return
	}
	a.record(r, "member_add", p.Name, subject, b.Role)
	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateMember(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok || !a.allow(w, r, p.Name, auth.Manage) {
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var b memberBody
	if !decode(w, r, &b) {
		return
	}
	if !meta.ValidProjectRole(b.Role) {
		writeErr(w, http.StatusBadRequest, "role must be read, write or admin")
		return
	}
	if err := a.DB.UpdateMember(r.Context(), p.ID, id, b.Role); err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "member_update", p.Name, fmt.Sprint(id), b.Role)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removeMember(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok || !a.allow(w, r, p.Name, auth.Manage) {
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.DB.RemoveMember(r.Context(), p.ID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "member_remove", p.Name, fmt.Sprint(id), "")
	w.WriteHeader(http.StatusNoContent)
}
