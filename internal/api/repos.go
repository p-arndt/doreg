package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oci"
	"github.com/p-arndt/doreg/internal/storage"
)

// imageCache keeps parsed image configs. Configs are content-addressed and
// immutable, so entries never go stale.
type imageCache struct {
	mu sync.Mutex
	m  map[string]*oci.ImageConfig
}

func newImageCache() *imageCache { return &imageCache{m: map[string]*oci.ImageConfig{}} }

const maxConfigSize = 8 << 20

func (a *API) imageConfig(d string) *oci.ImageConfig {
	a.images.mu.Lock()
	cfg, ok := a.images.m[d]
	a.images.mu.Unlock()
	if ok {
		return cfg
	}
	dg, err := digest.Parse(d)
	if err != nil {
		return nil
	}
	f, err := a.Store.Open(dg)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxConfigSize))
	if err != nil {
		return nil
	}
	cfg = &oci.ImageConfig{}
	if json.Unmarshal(data, cfg) != nil {
		cfg = nil
	}
	a.images.mu.Lock()
	if len(a.images.m) > 5000 {
		a.images.m = map[string]*oci.ImageConfig{}
	}
	a.images.m[d] = cfg
	a.images.mu.Unlock()
	return cfg
}

// PlatformInfo is a runnable platform of an image or index.
type PlatformInfo struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
	Digest       string `json:"digest"`
	Size         int64  `json:"size"`
}

// ManifestSummary is what lists show for a manifest.
type ManifestSummary struct {
	Digest       string         `json:"digest"`
	MediaType    string         `json:"mediaType"`
	ArtifactType string         `json:"artifactType,omitempty"`
	Size         int64          `json:"size"` // compressed bytes of config + layers, summed over platforms
	Platforms    []PlatformInfo `json:"platforms"`
	Attestations int            `json:"attestations"`
	Created      string         `json:"created,omitempty"`
	CreatedAt    int64          `json:"createdAt"`
	LastPulledAt int64          `json:"lastPulledAt"`
	IsReferrer   bool           `json:"isReferrer"`
}

type repoManifests map[string]*meta.Manifest

func (a *API) loadManifests(ctx context.Context, repoID int64) (repoManifests, error) {
	rows, err := a.DB.R.QueryContext(ctx, `
		SELECT id, digest, media_type, artifact_type, content, subject, created_at, last_pulled_at
		FROM manifests WHERE repo_id = ?`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := repoManifests{}
	for rows.Next() {
		m := &meta.Manifest{RepoID: repoID}
		if err := rows.Scan(&m.ID, &m.Digest, &m.MediaType, &m.ArtifactType, &m.Content, &m.Subject,
			&m.CreatedAt, &m.LastPulledAt); err != nil {
			return nil, err
		}
		out[m.Digest] = m
	}
	return out, rows.Err()
}

func imageSize(m *oci.Manifest) int64 {
	var n int64
	if m.Config != nil {
		n += m.Config.Size
	}
	for _, l := range m.Layers {
		n += l.Size
	}
	return n
}

func (a *API) summarize(all repoManifests, m *meta.Manifest) ManifestSummary {
	s := ManifestSummary{
		Digest: m.Digest, MediaType: m.MediaType, ArtifactType: m.ArtifactType,
		CreatedAt: m.CreatedAt, LastPulledAt: m.LastPulledAt, IsReferrer: m.Subject != "",
		Platforms: []PlatformInfo{},
	}
	parsed, err := oci.Parse(m.Content, m.MediaType)
	if err != nil {
		return s
	}
	if oci.IsIndex(parsed.MediaType) {
		for _, c := range parsed.Manifests {
			if oci.IsAttestation(c) {
				s.Attestations++
				continue
			}
			p := PlatformInfo{Digest: c.Digest}
			if c.Platform != nil {
				p.OS, p.Architecture, p.Variant = c.Platform.OS, c.Platform.Architecture, c.Platform.Variant
			}
			if child, ok := all[c.Digest]; ok {
				if cm, err := oci.Parse(child.Content, child.MediaType); err == nil {
					p.Size = imageSize(cm)
					if s.Created == "" && cm.Config != nil {
						if cfg := a.imageConfig(cm.Config.Digest); cfg != nil {
							s.Created = cfg.Created
						}
					}
				}
			}
			s.Size += p.Size
			s.Platforms = append(s.Platforms, p)
		}
		return s
	}
	s.Size = imageSize(parsed)
	if parsed.Config != nil && parsed.Config.MediaType != oci.MediaTypeOCIEmpty {
		if cfg := a.imageConfig(parsed.Config.Digest); cfg != nil && cfg.OS != "" {
			s.Created = cfg.Created
			s.Platforms = append(s.Platforms, PlatformInfo{OS: cfg.OS, Architecture: cfg.Architecture,
				Variant: cfg.Variant, Digest: m.Digest, Size: s.Size})
		}
	}
	return s
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var out struct {
		Repositories int64          `json:"repositories"`
		Tags         int64          `json:"tags"`
		Manifests    int64          `json:"manifests"`
		Blobs        int64          `json:"blobs"`
		BlobBytes    int64          `json:"blobBytes"`
		Uploads      int64          `json:"uploads"`
		Disk         *storage.Usage `json:"disk,omitempty"`
		LastGC       any            `json:"lastGc"`
		NextGC       int64          `json:"nextGc"`
		GCRunning    bool           `json:"gcRunning"`
	}
	err := a.DB.R.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM repositories),
		(SELECT count(*) FROM tags),
		(SELECT count(*) FROM manifests),
		(SELECT count(*) FROM blobs),
		(SELECT coalesce(sum(size), 0) FROM blobs),
		(SELECT count(*) FROM uploads)`).Scan(&out.Repositories, &out.Tags, &out.Manifests, &out.Blobs, &out.BlobBytes, &out.Uploads)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if u, err := storage.DiskUsage(a.Store.Root()); err == nil {
		out.Disk = &u
	}
	if runs, err := gcRuns(ctx, a, 1); err == nil && len(runs) > 0 {
		out.LastGC = runs[0]
	}
	if t := a.GC.NextRun(); !t.IsZero() {
		out.NextGC = t.Unix()
	}
	out.GCRunning = a.GC.Running()
	writeJSON(w, http.StatusOK, out)
}

type repoListItem struct {
	Name      string `json:"name"`
	Project   string `json:"project"`
	Public    bool   `json:"public"`
	Tags      int64  `json:"tags"`
	Manifests int64  `json:"manifests"`
	Size      int64  `json:"size"`
	LastPush  int64  `json:"lastPush"`
	CreatedAt int64  `json:"createdAt"`
}

func (a *API) repos(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.R.QueryContext(r.Context(), `
		SELECT r.name, r.public, r.created_at,
			(SELECT count(*) FROM tags t WHERE t.repo_id = r.id),
			(SELECT count(*) FROM manifests m WHERE m.repo_id = r.id),
			(SELECT coalesce(sum(b.size), 0) FROM repo_blobs rb JOIN blobs b ON b.digest = rb.digest WHERE rb.repo_id = r.id),
			coalesce((SELECT max(t.updated_at) FROM tags t WHERE t.repo_id = r.id), r.created_at)
		FROM repositories r ORDER BY r.name`)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer rows.Close()
	p := principal(r)
	project := r.URL.Query().Get("project")
	allowed := map[string]bool{}
	out := []repoListItem{}
	for rows.Next() {
		var it repoListItem
		if err := rows.Scan(&it.Name, &it.Public, &it.CreatedAt, &it.Tags, &it.Manifests, &it.Size, &it.LastPush); err != nil {
			a.fail(w, r, err)
			return
		}
		it.Project = meta.ProjectOf(it.Name)
		visible, seen := allowed[it.Project]
		if !seen {
			visible, _ = a.Auth.Can(r.Context(), p, it.Project, auth.Pull)
			allowed[it.Project] = visible
		}
		if (visible || it.Public) && (project == "" || project == it.Project) {
			out = append(out, it)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type tagItem struct {
	Name      string          `json:"name"`
	UpdatedAt int64           `json:"updatedAt"`
	Manifest  ManifestSummary `json:"manifest"`
}

// readableRepo loads a repository the caller may pull: one in a project they
// can pull from, or a public one. Visitors get 401 for a missing repository
// too, so they cannot probe which private names exist.
func (a *API) readableRepo(w http.ResponseWriter, r *http.Request, name string) (meta.Repo, bool) {
	repo, err := a.DB.RepoByName(r.Context(), name)
	if errors.Is(err, meta.ErrNotFound) && principal(r).Kind() == "anonymous" {
		ok, cerr := a.Auth.Can(r.Context(), auth.Anonymous, meta.ProjectOf(name), auth.Pull)
		if cerr != nil {
			a.fail(w, r, cerr)
			return repo, false
		}
		if !ok {
			denied(w, auth.Anonymous)
			return repo, false
		}
	}
	if err != nil {
		a.fail(w, r, err)
		return repo, false
	}
	if repo.Public {
		return repo, true
	}
	return repo, a.allow(w, r, meta.ProjectOf(repo.Name), auth.Pull)
}

func (a *API) repo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	repo, ok := a.readableRepo(w, r, r.URL.Query().Get("name"))
	if !ok {
		return
	}
	all, err := a.loadManifests(ctx, repo.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	rows, err := a.DB.R.QueryContext(ctx, `
		SELECT t.name, t.updated_at, m.digest FROM tags t JOIN manifests m ON m.id = t.manifest_id
		WHERE t.repo_id = ? ORDER BY t.updated_at DESC, t.name`, repo.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tags := []tagItem{}
	tagged := map[string]bool{}
	summaries := map[string]ManifestSummary{}
	for rows.Next() {
		var t tagItem
		var d string
		if err := rows.Scan(&t.Name, &t.UpdatedAt, &d); err != nil {
			rows.Close()
			a.fail(w, r, err)
			return
		}
		s, ok := summaries[d]
		if !ok {
			s = a.summarize(all, all[d])
			summaries[d] = s
		}
		t.Manifest = s
		tagged[d] = true
		tags = append(tags, t)
	}
	rows.Close()

	// Index children and referrers are reachable without a tag; only list true
	// roots as "untagged".
	reachable := map[string]bool{}
	for d := range tagged {
		markReachable(all, d, reachable)
	}
	untagged := []ManifestSummary{}
	for d, m := range all {
		if reachable[d] {
			continue
		}
		if m.Subject != "" {
			if _, ok := all[m.Subject]; ok {
				continue
			}
		}
		if isChildOfAny(all, d) {
			continue
		}
		untagged = append(untagged, a.summarize(all, m))
	}
	sort.Slice(untagged, func(i, j int) bool { return untagged[i].CreatedAt > untagged[j].CreatedAt })

	var size int64
	_ = a.DB.R.QueryRowContext(ctx, `
		SELECT coalesce(sum(b.size), 0) FROM repo_blobs rb JOIN blobs b ON b.digest = rb.digest WHERE rb.repo_id = ?`,
		repo.ID).Scan(&size)

	writeJSON(w, http.StatusOK, map[string]any{
		"name":      repo.Name,
		"public":    repo.Public,
		"createdAt": repo.CreatedAt,
		"size":      size,
		"tags":      tags,
		"untagged":  untagged,
	})
}

func markReachable(all repoManifests, d string, seen map[string]bool) {
	if seen[d] {
		return
	}
	seen[d] = true
	m, ok := all[d]
	if !ok {
		return
	}
	if parsed, err := oci.Parse(m.Content, m.MediaType); err == nil {
		for _, c := range parsed.Manifests {
			markReachable(all, c.Digest, seen)
		}
	}
	for od, om := range all {
		if om.Subject == d {
			markReachable(all, od, seen)
		}
	}
}

func isChildOfAny(all repoManifests, d string) bool {
	for _, m := range all {
		if !oci.IsIndex(m.MediaType) {
			continue
		}
		if parsed, err := oci.Parse(m.Content, m.MediaType); err == nil {
			for _, c := range parsed.Manifests {
				if c.Digest == d {
					return true
				}
			}
		}
	}
	return false
}

func (a *API) updateRepo(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !a.allow(w, r, meta.ProjectOf(name), auth.Manage) {
		return
	}
	var body struct {
		Public bool `json:"public"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := a.DB.SetRepoPublic(r.Context(), name, body.Public); err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "repository_update", meta.ProjectOf(name), name, fmt.Sprintf("public=%t", body.Public))
	w.WriteHeader(http.StatusNoContent)
}

// emit publishes a content event caused by the current UI user.
func (a *API) emit(r *http.Request, e events.Event) {
	u := currentUser(r)
	e.Actor, e.ActorKind, e.IP = u.Username, "user", clientIP(r)
	e.Project = meta.ProjectOf(e.Repository)
	a.Events.Emit(r.Context(), e)
}

func (a *API) deleteRepo(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !a.allow(w, r, meta.ProjectOf(name), auth.Delete) {
		return
	}
	if err := a.DB.DeleteRepo(r.Context(), name); err != nil {
		a.fail(w, r, err)
		return
	}
	a.emit(r, events.Event{Action: events.DeleteRepo, Repository: name})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteTag(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !a.allow(w, r, meta.ProjectOf(q.Get("repo")), auth.Delete) {
		return
	}
	if err := a.DB.DeleteTag(r.Context(), q.Get("repo"), q.Get("tag")); err != nil {
		a.fail(w, r, err)
		return
	}
	a.emit(r, events.Event{Action: events.DeleteTag, Repository: q.Get("repo"), Tag: q.Get("tag")})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteManifest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !a.allow(w, r, meta.ProjectOf(q.Get("repo")), auth.Delete) {
		return
	}
	if err := a.DB.DeleteManifest(r.Context(), q.Get("repo"), q.Get("digest")); err != nil {
		a.fail(w, r, err)
		return
	}
	a.emit(r, events.Event{Action: events.DeleteImage, Repository: q.Get("repo"), Digest: q.Get("digest")})
	w.WriteHeader(http.StatusNoContent)
}

type layerInfo struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
	External  bool   `json:"external,omitempty"`
}

type childInfo struct {
	Digest      string        `json:"digest"`
	MediaType   string        `json:"mediaType"`
	Size        int64         `json:"size"`
	Platform    *oci.Platform `json:"platform,omitempty"`
	Attestation bool          `json:"attestation"`
	ImageSize   int64         `json:"imageSize"`
}

func (a *API) manifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	repo, ok := a.readableRepo(w, r, q.Get("repo"))
	if !ok {
		return
	}
	ref := q.Get("ref")
	_, derr := digest.Parse(ref)
	m, err := a.DB.GetManifest(ctx, repo.Name, ref, derr == nil)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	all, err := a.loadManifests(ctx, repo.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	parsed, err := oci.Parse(m.Content, m.MediaType)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	out := map[string]any{
		"repo":         repo.Name,
		"digest":       m.Digest,
		"mediaType":    m.MediaType,
		"artifactType": m.ArtifactType,
		"manifestSize": len(m.Content),
		"raw":          json.RawMessage(m.Content),
		"createdAt":    m.CreatedAt,
		"lastPulledAt": m.LastPulledAt,
		"subject":      m.Subject,
		"annotations":  parsed.Annotations,
		"summary":      a.summarize(all, &m),
	}

	tags := []string{}
	if rows, err := a.DB.R.QueryContext(ctx, `SELECT name FROM tags WHERE manifest_id = ? ORDER BY name`, m.ID); err == nil {
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil {
				tags = append(tags, t)
			}
		}
		rows.Close()
	}
	out["tags"] = tags

	if oci.IsIndex(parsed.MediaType) {
		children := []childInfo{}
		for _, c := range parsed.Manifests {
			ci := childInfo{Digest: c.Digest, MediaType: c.MediaType, Size: c.Size, Platform: c.Platform, Attestation: oci.IsAttestation(c)}
			if child, ok := all[c.Digest]; ok {
				if cm, err := oci.Parse(child.Content, child.MediaType); err == nil {
					ci.ImageSize = imageSize(cm)
				}
			}
			children = append(children, ci)
		}
		out["children"] = children
	} else {
		layers := []layerInfo{}
		for _, l := range parsed.Layers {
			layers = append(layers, layerInfo{Digest: l.Digest, Size: l.Size, MediaType: l.MediaType, External: len(l.URLs) > 0})
		}
		out["layers"] = layers
		if parsed.Config != nil {
			out["configDescriptor"] = parsed.Config
			if parsed.Config.MediaType != oci.MediaTypeOCIEmpty {
				out["config"] = a.imageConfig(parsed.Config.Digest)
			}
		}
	}

	referrers := []ManifestSummary{}
	for _, om := range all {
		if om.Subject == m.Digest {
			referrers = append(referrers, a.summarize(all, om))
		}
	}
	sort.Slice(referrers, func(i, j int) bool { return referrers[i].CreatedAt > referrers[j].CreatedAt })
	out["referrers"] = referrers

	parents := []string{}
	for od, om := range all {
		if !oci.IsIndex(om.MediaType) {
			continue
		}
		if p, err := oci.Parse(om.Content, om.MediaType); err == nil {
			for _, c := range p.Manifests {
				if c.Digest == m.Digest {
					parents = append(parents, od)
				}
			}
		}
	}
	out["parents"] = parents
	writeJSON(w, http.StatusOK, out)
}
