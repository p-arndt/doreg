package registry

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oci"
)

// maxManifestSize matches the limit most registries enforce.
const maxManifestSize = 4 << 20

func (h *Handler) manifest(w http.ResponseWriter, r *http.Request, name, ref string) {
	_, derr := digest.Parse(ref)
	isDigest := derr == nil
	if !isDigest && !tagRe.MatchString(ref) {
		// A lookup of a malformed reference is simply unknown; only writes are invalid.
		if r.Method == http.MethodPut {
			writeError(w, http.StatusBadRequest, "TAG_INVALID", "invalid tag or digest")
		} else {
			writeError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest unknown")
		}
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.getManifest(w, r, name, ref, isDigest)
	case http.MethodPut:
		h.putManifest(w, r, name, ref, isDigest)
	case http.MethodDelete:
		var err error
		ev := events.Event{Repository: name}
		if isDigest {
			err = h.DB.DeleteManifest(r.Context(), name, ref)
			ev.Action, ev.Digest = events.DeleteImage, ref
		} else {
			err = h.DB.DeleteTag(r.Context(), name, ref)
			ev.Action, ev.Tag = events.DeleteTag, ref
		}
		if err == nil {
			h.emit(r, ev)
		}
		if errors.Is(err, meta.ErrNotFound) {
			writeError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest unknown")
			return
		}
		if err != nil {
			h.internal(w, r, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		writeError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "method not allowed")
	}
}

func (h *Handler) getManifest(w http.ResponseWriter, r *http.Request, name, ref string, isDigest bool) {
	m, err := h.DB.GetManifest(r.Context(), name, ref, isDigest)
	if errors.Is(err, meta.ErrNotFound) {
		writeError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest unknown")
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	// Only GET counts as a pull: clients HEAD manifests during pushes to check
	// for existence, which must not keep untagged manifests alive.
	if r.Method == http.MethodGet {
		h.DB.MarkPulled(r.Context(), m)
		ev := events.Event{Action: events.Pull, Repository: name, Digest: m.Digest, MediaType: m.MediaType}
		if !isDigest {
			ev.Tag = ref
		}
		h.emit(r, ev)
	}
	// The stored type is served regardless of Accept: the bytes are what was
	// pushed and the digest depends on them.
	w.Header().Set("Content-Type", m.MediaType)
	w.Header().Set("Docker-Content-Digest", m.Digest)
	w.Header().Set("ETag", `"`+m.Digest+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(m.Content)))
	if r.Method == http.MethodHead {
		return
	}
	w.Write(m.Content)
}

func (h *Handler) putManifest(w http.ResponseWriter, r *http.Request, name, ref string, isDigest bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxManifestSize+1))
	if err != nil {
		h.internal(w, r, err)
		return
	}
	if len(body) > maxManifestSize {
		writeError(w, http.StatusRequestEntityTooLarge, "SIZE_INVALID", "manifest too large")
		return
	}
	alg := "sha256"
	if isDigest {
		alg = digest.Digest(ref).Algorithm()
	}
	d := digest.FromBytes(alg, body)
	if isDigest && d.String() != ref {
		writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "manifest digest does not match reference")
		return
	}
	m, err := oci.Parse(body, r.Header.Get("Content-Type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "MANIFEST_INVALID", err.Error())
		return
	}

	// The subject is deliberately not required to exist: the spec lets
	// referrers be pushed before (or without) the image they describe.
	var refs []meta.Ref
	if m.Config != nil {
		refs = append(refs, meta.Ref{Kind: "blob", Digest: m.Config.Digest, Size: m.Config.Size, MediaType: m.Config.MediaType})
	}
	for _, l := range m.Layers {
		if len(l.URLs) > 0 {
			continue // foreign layer, fetched from elsewhere
		}
		refs = append(refs, meta.Ref{Kind: "blob", Digest: l.Digest, Size: l.Size, MediaType: l.MediaType})
	}
	for _, c := range m.Manifests {
		refs = append(refs, meta.Ref{Kind: "manifest", Digest: c.Digest, Size: c.Size, MediaType: c.MediaType})
	}
	rec := meta.Manifest{
		Digest:       d.String(),
		MediaType:    m.MediaType,
		ArtifactType: m.EffectiveArtifactType(),
		Content:      body,
	}
	if m.Subject != nil {
		rec.Subject = m.Subject.Digest
	}
	tag := ""
	if !isDigest {
		tag = ref
	}
	if err := h.DB.PutManifest(r.Context(), name, rec, refs, tag, tag != "" && h.tagImmutable(r, name, tag)); err != nil {
		if errors.Is(err, meta.ErrTagImmutable) {
			writeError(w, http.StatusForbidden, "DENIED",
				"tag "+tag+" exists and is immutable in project "+meta.ProjectOf(name)+"; push a new tag instead")
			return
		}
		var missing *meta.ErrMissingRef
		if errors.As(err, &missing) {
			code := "MANIFEST_BLOB_UNKNOWN"
			if missing.Ref.Kind == "manifest" {
				code = "MANIFEST_UNKNOWN"
			}
			writeError(w, http.StatusBadRequest, code, missing.Error())
			return
		}
		h.internal(w, r, err)
		return
	}
	h.emit(r, events.Event{Action: events.Push, Repository: name, Tag: tag, Digest: d.String(), MediaType: m.MediaType})
	if m.Subject != nil {
		w.Header().Set("OCI-Subject", m.Subject.Digest)
	}
	w.Header().Set("Location", "/v2/"+name+"/manifests/"+d.String())
	w.Header().Set("Docker-Content-Digest", d.String())
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusCreated)
}

// tagImmutable reports whether the tag, once it exists, may not be moved to
// another manifest. The server-wide setting and the project setting each
// lock it unless the tag matches one of their own exception patterns.
func (h *Handler) tagImmutable(r *http.Request, repo, tag string) bool {
	if h.ImmutableTags && !meta.TagMatches(h.MutableTags, tag) {
		return true
	}
	p, err := h.DB.ProjectByName(r.Context(), meta.ProjectOf(repo))
	return err == nil && p.ImmutableTags && !meta.TagMatches(p.MutableTags, tag)
}

func (h *Handler) referrers(w http.ResponseWriter, r *http.Request, name, ref string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "method not allowed")
		return
	}
	if _, err := digest.Parse(ref); err != nil {
		writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "invalid digest")
		return
	}
	list, err := h.DB.Referrers(r.Context(), name, ref)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	filter := r.URL.Query().Get("artifactType")
	descs := []oci.Descriptor{}
	for _, m := range list {
		if filter != "" && m.ArtifactType != filter {
			continue
		}
		d := oci.Descriptor{MediaType: m.MediaType, Digest: m.Digest, Size: int64(len(m.Content)), ArtifactType: m.ArtifactType}
		var parsed oci.Manifest
		if json.Unmarshal(m.Content, &parsed) == nil {
			d.Annotations = parsed.Annotations
		}
		descs = append(descs, d)
	}
	if filter != "" {
		w.Header().Set("OCI-Filters-Applied", "artifactType")
	}
	w.Header().Set("Content-Type", oci.MediaTypeOCIIndex)
	json.NewEncoder(w).Encode(map[string]any{
		"schemaVersion": 2,
		"mediaType":     oci.MediaTypeOCIIndex,
		"manifests":     descs,
	})
}
