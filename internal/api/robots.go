package api

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/meta"
)

var robotNameRe = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

// scopeAllowed checks the permission to manage things (robots, webhooks) of a
// project; project "" means system-wide, which only admins manage.
func (a *API) scopeAllowed(w http.ResponseWriter, r *http.Request, project string) bool {
	if project == "" {
		if !currentUser(r).IsAdmin() {
			writeErr(w, http.StatusForbidden, "only administrators manage system-wide settings")
			return false
		}
		return true
	}
	return a.allow(w, r, project, auth.Manage)
}

// robots lists robots of ?project= (project admins) or all robots (admins).
func (a *API) robots(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" && !currentUser(r).IsAdmin() {
		writeErr(w, http.StatusForbidden, "permission denied")
		return
	}
	if project != "" && !a.allow(w, r, project, auth.Manage) {
		return
	}
	list, err := a.DB.Robots(r.Context(), project)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type robotBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Project     string `json:"project"` // empty: system robot, every project
	CanPush     bool   `json:"canPush"`
	CanDelete   bool   `json:"canDelete"`
	ExpiresAt   int64  `json:"expiresAt"`
	Disabled    bool   `json:"disabled"`
}

func (a *API) createRobot(w http.ResponseWriter, r *http.Request) {
	var b robotBody
	if !decode(w, r, &b) {
		return
	}
	if !robotNameRe.MatchString(b.Name) {
		writeErr(w, http.StatusBadRequest, "robot names use lowercase letters, digits and . _ - separators")
		return
	}
	if !a.scopeAllowed(w, r, b.Project) {
		return
	}
	rb := meta.Robot{Name: meta.RobotName(b.Project, b.Name), Description: b.Description,
		CanPush: b.CanPush, CanDelete: b.CanDelete, ExpiresAt: b.ExpiresAt, CreatedBy: currentUser(r).Username}
	if b.Project != "" {
		p, err := a.DB.ProjectByName(r.Context(), b.Project)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		rb.ProjectID, rb.Project = p.ID, p.Name
	}
	secret := auth.NewSecret()
	hash, err := auth.HashPassword(secret)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	rb.SecretHash = hash
	rb, err = a.DB.CreateRobot(r.Context(), rb)
	if errors.Is(err, meta.ErrExists) {
		writeErr(w, http.StatusConflict, rb.Name+" already exists")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "robot_create", b.Project, rb.Name, permissionSummary(rb))
	writeJSON(w, http.StatusCreated, map[string]any{"robot": rb, "secret": secret})
}

func permissionSummary(rb meta.Robot) string {
	s := "pull"
	if rb.CanPush {
		s += ", push"
	}
	if rb.CanDelete {
		s += ", delete"
	}
	return s
}

func (a *API) loadRobot(w http.ResponseWriter, r *http.Request) (meta.Robot, bool) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return meta.Robot{}, false
	}
	rb, err := a.DB.RobotByID(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return rb, false
	}
	return rb, a.scopeAllowed(w, r, rb.Project)
}

func (a *API) updateRobot(w http.ResponseWriter, r *http.Request) {
	rb, ok := a.loadRobot(w, r)
	if !ok {
		return
	}
	var b robotBody
	if !decode(w, r, &b) {
		return
	}
	rb.Description, rb.CanPush, rb.CanDelete, rb.ExpiresAt, rb.Disabled = b.Description, b.CanPush, b.CanDelete, b.ExpiresAt, b.Disabled
	if err := a.DB.UpdateRobot(r.Context(), rb); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.Invalidate()
	a.record(r, "robot_update", rb.Project, rb.Name, permissionSummary(rb))
	writeJSON(w, http.StatusOK, rb)
}

func (a *API) rotateRobotSecret(w http.ResponseWriter, r *http.Request) {
	rb, ok := a.loadRobot(w, r)
	if !ok {
		return
	}
	secret := auth.NewSecret()
	hash, err := auth.HashPassword(secret)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.DB.SetRobotSecret(r.Context(), rb.ID, hash); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.Invalidate()
	a.record(r, "robot_rotate_secret", rb.Project, rb.Name, "")
	writeJSON(w, http.StatusOK, map[string]any{"robot": rb, "secret": secret})
}

func (a *API) deleteRobot(w http.ResponseWriter, r *http.Request) {
	rb, ok := a.loadRobot(w, r)
	if !ok {
		return
	}
	if err := a.DB.DeleteRobot(r.Context(), rb.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.Invalidate()
	a.record(r, "robot_delete", rb.Project, rb.Name, "")
	w.WriteHeader(http.StatusNoContent)
}

// audit lists the audit log: everything for admins, managed projects for others.
func (a *API) audit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	aq := meta.AuditQuery{Project: q.Get("project"), Actor: q.Get("actor"), Action: q.Get("action"), Search: q.Get("q")}
	aq.Before, _ = parseInt(q.Get("before"))
	limit, _ := parseInt(q.Get("limit"))
	aq.Limit = int(limit)
	u := currentUser(r)
	if !u.IsAdmin() {
		projects, err := a.DB.Projects(r.Context())
		if err != nil {
			a.fail(w, r, err)
			return
		}
		aq.Projects = []string{}
		for _, p := range projects {
			if ok, _ := a.Auth.Can(r.Context(), auth.Principal{User: &u}, p.Name, auth.Manage); ok {
				aq.Projects = append(aq.Projects, p.Name)
			}
		}
	}
	entries, err := a.DB.Audit(r.Context(), aq)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (a *API) webhooks(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" && !currentUser(r).IsAdmin() {
		writeErr(w, http.StatusForbidden, "permission denied")
		return
	}
	if project != "" && !a.allow(w, r, project, auth.Manage) {
		return
	}
	list, err := a.DB.Webhooks(r.Context(), project)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": list, "events": events.WebhookEvents})
}

type webhookBody struct {
	Project string   `json:"project"`
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Secret  string   `json:"secret"`
	Events  []string `json:"events"`
	Enabled bool     `json:"enabled"`
}

func (a *API) saveWebhook(w http.ResponseWriter, r *http.Request) {
	var b webhookBody
	if !decode(w, r, &b) {
		return
	}
	hook := meta.Webhook{Name: b.Name, URL: b.URL, Secret: b.Secret, Events: b.Events, Enabled: b.Enabled}
	if r.PathValue("id") != "" {
		existing, ok := a.loadWebhook(w, r)
		if !ok {
			return
		}
		hook.ID, hook.Project = existing.ID, existing.Project
	} else {
		if !a.scopeAllowed(w, r, b.Project) {
			return
		}
		if b.Project != "" {
			p, err := a.DB.ProjectByName(r.Context(), b.Project)
			if err != nil {
				a.fail(w, r, err)
				return
			}
			hook.ProjectID, hook.Project = p.ID, p.Name
		}
	}
	if msg := validateWebhook(hook); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	id, err := a.DB.SaveWebhook(r.Context(), hook)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "webhook_save", hook.Project, hook.Name, hook.URL)
	saved, err := a.DB.WebhookByID(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func validateWebhook(h meta.Webhook) string {
	if h.Name == "" {
		return "name is required"
	}
	if !regexp.MustCompile(`^https?://[^\s]+$`).MatchString(h.URL) {
		return "URL must start with http:// or https://"
	}
	if len(h.Events) == 0 {
		return "pick at least one event"
	}
	known := map[string]bool{}
	for _, e := range events.WebhookEvents {
		known[e] = true
	}
	for _, e := range h.Events {
		if !known[e] {
			return "unknown event " + e
		}
	}
	return ""
}

func (a *API) loadWebhook(w http.ResponseWriter, r *http.Request) (meta.Webhook, bool) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return meta.Webhook{}, false
	}
	h, err := a.DB.WebhookByID(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return h, false
	}
	return h, a.scopeAllowed(w, r, h.Project)
}

func (a *API) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	h, ok := a.loadWebhook(w, r)
	if !ok {
		return
	}
	if err := a.DB.DeleteWebhook(r.Context(), h.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "webhook_delete", h.Project, h.Name, "")
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) testWebhook(w http.ResponseWriter, r *http.Request) {
	h, ok := a.loadWebhook(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, a.Events.Test(r.Context(), h, currentUser(r).Username))
}

func (a *API) deliveries(w http.ResponseWriter, r *http.Request) {
	h, ok := a.loadWebhook(w, r)
	if !ok {
		return
	}
	list, err := a.DB.Deliveries(r.Context(), h.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
