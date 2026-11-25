// Package registry implements the OCI distribution API under /v2/.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/storage"
)

var (
	nameRe = regexp.MustCompile(`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*(/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)*$`)
	tagRe  = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
)

// ValidName reports whether s is a valid repository name.
func ValidName(s string) bool { return len(s) <= 255 && nameRe.MatchString(s) }

// Handler serves /v2/.
type Handler struct {
	DB     *meta.DB
	Store  *storage.FS
	Auth   *auth.Authenticator
	Events *events.Bus // optional
	Log    *slog.Logger
	// ImmutableTags makes tags immutable in every project, regardless of the
	// project setting, except tags matching one of the MutableTags globs.
	ImmutableTags bool
	MutableTags   []string
}

// emit sends an event if a bus is configured, filling actor and host.
func (h *Handler) emit(r *http.Request, e events.Event) {
	if h.Events == nil {
		return
	}
	p := principal(r)
	e.Actor, e.ActorKind, e.IP = p.Name(), p.Kind(), clientIP(r)
	if e.Project == "" && e.Repository != "" {
		e.Project = meta.ProjectOf(e.Repository)
	}
	if e.Repository != "" && e.ResourceURL == "" {
		switch {
		case e.Tag != "":
			e.ResourceURL = r.Host + "/" + e.Repository + ":" + e.Tag
		case e.Digest != "":
			e.ResourceURL = r.Host + "/" + e.Repository + "@" + e.Digest
		}
	}
	h.Events.Emit(r.Context(), e)
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

type route struct {
	kind string // base, catalog, tags, manifest, blob, upload-start, upload, referrers
	name string
	ref  string
}

// parse splits a /v2/ path. Repository names contain slashes, so the path is
// matched from the right.
func parse(path string) (route, bool) {
	p, ok := strings.CutPrefix(path, "/v2/")
	if !ok {
		return route{}, path == "/v2"
	}
	if p == "" {
		return route{kind: "base"}, true
	}
	if p == "_catalog" {
		return route{kind: "catalog"}, true
	}
	if name, ok := strings.CutSuffix(p, "/tags/list"); ok {
		return route{kind: "tags", name: name}, true
	}
	if i := strings.LastIndex(p, "/blobs/uploads/"); i >= 0 {
		id := p[i+len("/blobs/uploads/"):]
		if id == "" {
			return route{kind: "upload-start", name: p[:i]}, true
		}
		return route{kind: "upload", name: p[:i], ref: id}, true
	}
	if name, ok := strings.CutSuffix(p, "/blobs/uploads"); ok {
		return route{kind: "upload-start", name: name}, true
	}
	// The rightmost separator wins: "a/manifests/b/blobs/<digest>" is a blob of
	// repository "a/manifests/b".
	best, bestKind, bestSep := -1, "", ""
	for _, k := range [][2]string{{"/manifests/", "manifest"}, {"/blobs/", "blob"}, {"/referrers/", "referrers"}} {
		if i := strings.LastIndex(p, k[0]); i > best {
			best, bestSep, bestKind = i, k[0], k[1]
		}
	}
	if best >= 0 {
		return route{kind: bestKind, name: p[:best], ref: p[best+len(bestSep):]}, true
	}
	return route{}, false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	if r.URL.Path == tokenPath {
		h.token(w, r)
		return
	}
	rt, ok := parse(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown endpoint")
		return
	}
	if rt.name != "" && !ValidName(rt.name) {
		writeError(w, http.StatusBadRequest, "NAME_INVALID", "invalid repository name")
		return
	}
	r, ok = h.authorize(w, r, rt)
	if !ok {
		return
	}
	switch rt.kind {
	case "base":
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
	case "catalog":
		h.catalog(w, r)
	case "tags":
		h.tags(w, r, rt.name)
	case "manifest":
		h.manifest(w, r, rt.name, rt.ref)
	case "blob":
		h.blob(w, r, rt.name, rt.ref)
	case "upload-start":
		h.startUpload(w, r, rt.name)
	case "upload":
		h.upload(w, r, rt.name, rt.ref)
	case "referrers":
		h.referrers(w, r, rt.name, rt.ref)
	}
}

type principalKey struct{}

func principal(r *http.Request) auth.Principal {
	p, _ := r.Context().Value(principalKey{}).(auth.Principal)
	return p
}

func actionFor(method string) auth.Action {
	switch method {
	case http.MethodGet, http.MethodHead:
		return auth.Pull
	case http.MethodDelete:
		return auth.Delete
	}
	return auth.Push
}

// authorize enforces access and stores the principal in the request context.
// Unauthenticated requests always get 401 with a challenge (never 403):
// clients only send credentials after one. Basic credentials are accepted on
// every request; the challenge itself is Bearer (see tokenPath).
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, rt route) (*http.Request, bool) {
	p := auth.Anonymous
	if tp, ok := r.Context().Value(inProcessKey{}).(auth.Principal); ok {
		p = tp
	} else if name, pw, ok := r.BasicAuth(); ok {
		var status int
		if p, status = h.basic(w, r, name, pw); status == http.StatusUnauthorized {
			return r, challenge(w, r, rt, "invalid_token")
		} else if status != 0 {
			return r, false
		}
	} else if tok, ok := bearerToken(r); ok {
		if p, ok = h.Auth.Bearer(r.Context(), tok); !ok {
			return r, challenge(w, r, rt, "invalid_token")
		}
	}
	ctx := context.WithValue(r.Context(), principalKey{}, p)
	if r.Header.Get(ImportHeader) != "" && p.IsAdmin() {
		ctx = events.WithoutWebhooks(ctx)
	}
	r = r.WithContext(ctx)
	if rt.name == "" {
		// /v2/ and the catalog need an account; the catalog filters per project.
		if p.Kind() == "anonymous" {
			return r, challenge(w, r, rt, "")
		}
		return r, true
	}
	project := meta.ProjectOf(rt.name)
	action := actionFor(r.Method)
	if action == auth.Push && p.IsAdmin() {
		// Admins create projects implicitly by pushing to them.
		if _, err := h.DB.EnsureProject(r.Context(), project); err != nil {
			h.internal(w, r, err)
			return r, false
		}
	}
	ok, err := h.Auth.Can(r.Context(), p, project, action)
	if err != nil {
		h.internal(w, r, err)
		return r, false
	}
	if !ok && action == auth.Pull {
		if repo, err := h.DB.RepoByName(r.Context(), rt.name); err == nil && repo.Public {
			ok = true
		}
	}
	if ok {
		return r, true
	}
	if p.Kind() == "anonymous" {
		return r, challenge(w, r, rt, "insufficient_scope")
	}
	writeError(w, http.StatusForbidden, "DENIED", "requested access to the resource is denied")
	return r, false
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{{"code": code, "message": msg}},
	})
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	h.Log.Error("registry request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "UNKNOWN", "internal error")
}

// pagination reads n and last. n < 0 means unlimited.
func pagination(r *http.Request) (n int, last string, err error) {
	n = -1
	if s := r.URL.Query().Get("n"); s != "" {
		n, err = strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0, "", fmt.Errorf("invalid n")
		}
	}
	return n, r.URL.Query().Get("last"), nil
}

func writePage(w http.ResponseWriter, r *http.Request, key string, items []string, extra map[string]any, n int) {
	if n > 0 && len(items) == n {
		q := r.URL.Query()
		q.Set("n", strconv.Itoa(n))
		q.Set("last", items[len(items)-1])
		w.Header().Set("Link", fmt.Sprintf(`<%s?%s>; rel="next"`, r.URL.Path, q.Encode()))
	}
	body := map[string]any{key: items}
	for k, v := range extra {
		body[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	n, last, err := pagination(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "PAGINATION_NUMBER_INVALID", err.Error())
		return
	}
	names := []string{}
	p := principal(r)
	allowed := map[string]bool{}
	// Page through repositories until n visible ones are collected; access is
	// per project, so filtering cannot happen in SQL.
	for cursor := last; n != 0; {
		batch, err := h.DB.ListRepoNames(r.Context(), cursor, 500)
		if err != nil {
			h.internal(w, r, err)
			return
		}
		for _, name := range batch {
			proj := meta.ProjectOf(name)
			ok, seen := allowed[proj]
			if !seen {
				if ok, err = h.Auth.Can(r.Context(), p, proj, auth.Pull); err != nil {
					h.internal(w, r, err)
					return
				}
				allowed[proj] = ok
			}
			if ok {
				names = append(names, name)
				if n > 0 && len(names) == n {
					break
				}
			}
		}
		if len(batch) < 500 || (n > 0 && len(names) == n) {
			break
		}
		cursor = batch[len(batch)-1]
	}
	writePage(w, r, "repositories", names, nil, n)
}

func (h *Handler) tags(w http.ResponseWriter, r *http.Request, name string) {
	if _, err := h.DB.RepoByName(r.Context(), name); err != nil {
		writeError(w, http.StatusNotFound, "NAME_UNKNOWN", "repository not found")
		return
	}
	n, last, err := pagination(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "PAGINATION_NUMBER_INVALID", err.Error())
		return
	}
	tags := []string{}
	if n != 0 {
		if tags, err = h.DB.ListTags(r.Context(), name, last, n); err != nil {
			h.internal(w, r, err)
			return
		}
	}
	writePage(w, r, "tags", tags, map[string]any{"name": name}, n)
}
