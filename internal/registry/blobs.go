package registry

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/storage"
)

func (h *Handler) blob(w http.ResponseWriter, r *http.Request, name, ref string) {
	d, err := digest.Parse(ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "invalid digest")
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.serveBlob(w, r, name, d)
	case http.MethodDelete:
		if err := h.DB.UnlinkBlob(r.Context(), name, d.String()); err != nil {
			if errors.Is(err, meta.ErrNotFound) {
				writeError(w, http.StatusNotFound, "BLOB_UNKNOWN", "blob unknown to registry")
				return
			}
			h.internal(w, r, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		writeError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "method not allowed")
	}
}

func (h *Handler) serveBlob(w http.ResponseWriter, r *http.Request, name string, d digest.Digest) {
	if _, err := h.DB.BlobInRepo(r.Context(), name, d.String()); err != nil {
		if !errors.Is(err, meta.ErrNotFound) {
			h.internal(w, r, err)
			return
		}
		// Some clients fetch manifests through the blob endpoint.
		m, merr := h.DB.GetManifest(r.Context(), name, d.String(), true)
		if merr != nil {
			writeError(w, http.StatusNotFound, "BLOB_UNKNOWN", "blob unknown to registry")
			return
		}
		w.Header().Set("Docker-Content-Digest", d.String())
		w.Header().Set("Content-Type", m.MediaType)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(m.Content))
		return
	}
	f, err := h.Store.Open(d)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "BLOB_UNKNOWN", "blob unknown to registry")
			return
		}
		h.internal(w, r, err)
		return
	}
	defer f.Close()
	w.Header().Set("Docker-Content-Digest", d.String())
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"`+d.String()+`"`)
	w.Header().Set("Cache-Control", "max-age=31536000")
	http.ServeContent(w, r, "", time.Time{}, f)
}

func newUploadID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func uploadLocation(name, id string) string { return "/v2/" + name + "/blobs/uploads/" + id }

func setUploadHeaders(w http.ResponseWriter, name, id string, size int64) {
	end := size - 1
	if end < 0 {
		end = 0
	}
	w.Header().Set("Location", uploadLocation(name, id))
	w.Header().Set("Range", fmt.Sprintf("0-%d", end))
	w.Header().Set("Docker-Upload-UUID", id)
	w.Header().Set("Content-Length", "0")
}

func (h *Handler) startUpload(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "method not allowed")
		return
	}
	q := r.URL.Query()
	if mount := q.Get("mount"); mount != "" {
		from := q.Get("from")
		allowed := false
		if from == "" {
			// Mounting from "anywhere" would reveal blobs of projects the caller cannot read.
			allowed = principal(r).IsAdmin()
		} else if ValidName(from) {
			allowed, _ = h.Auth.Can(r.Context(), principal(r), meta.ProjectOf(from), auth.Pull)
		}
		if d, err := digest.Parse(mount); err == nil && allowed && h.quotaAllows(r, name, d.String(), -1) {
			// Without "from", any repository may supply the blob; every
			// authenticated user can read every repository anyway.
			_, ok, err := h.DB.MountBlob(r.Context(), from, name, d.String())
			if err != nil {
				h.internal(w, r, err)
				return
			}
			if ok {
				w.Header().Set("Location", "/v2/"+name+"/blobs/"+d.String())
				w.Header().Set("Docker-Content-Digest", d.String())
				w.Header().Set("Content-Length", "0")
				w.WriteHeader(http.StatusCreated)
				return
			}
		}
		// Mount failed: fall through to a normal upload session, as the spec requires.
	}

	id := newUploadID()
	if err := h.Store.CreateUpload(id); err != nil {
		h.internal(w, r, err)
		return
	}
	if err := h.DB.CreateUpload(r.Context(), name, id); err != nil {
		h.Store.DeleteUpload(id)
		h.internal(w, r, err)
		return
	}

	if dgst := q.Get("digest"); dgst != "" {
		// Monolithic upload in a single POST.
		d, err := digest.Parse(dgst)
		if err != nil {
			h.abortUpload(r, id)
			writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "invalid digest")
			return
		}
		if _, err := h.Store.AppendUpload(id, 0, r.Body); err != nil {
			h.abortUpload(r, id)
			h.internal(w, r, err)
			return
		}
		h.finishUpload(w, r, name, id, d)
		return
	}
	setUploadHeaders(w, name, id, 0)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) abortUpload(r *http.Request, id string) {
	h.Store.DeleteUpload(id)
	h.DB.DeleteUpload(r.Context(), id)
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request, name, id string) {
	if !h.DB.UploadInRepo(r.Context(), name, id) {
		writeError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", "upload unknown")
		return
	}
	size, err := h.Store.UploadSize(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", "upload unknown")
		return
	}
	switch r.Method {
	case http.MethodGet:
		setUploadHeaders(w, name, id, size)
		w.WriteHeader(http.StatusNoContent)

	case http.MethodPatch:
		if cr := r.Header.Get("Content-Range"); cr != "" {
			start, ok := parseContentRangeStart(cr)
			if !ok || start != size {
				rejectChunk(w, r, name, id, size)
				return
			}
		}
		newSize, err := h.Store.AppendUpload(id, size, r.Body)
		if err != nil {
			h.internal(w, r, err)
			return
		}
		h.DB.TouchUpload(r.Context(), id)
		setUploadHeaders(w, name, id, newSize)
		w.WriteHeader(http.StatusAccepted)

	case http.MethodPut:
		d, err := digest.Parse(r.URL.Query().Get("digest"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "invalid digest")
			return
		}
		if cr := r.Header.Get("Content-Range"); cr != "" {
			if start, ok := parseContentRangeStart(cr); !ok || start != size {
				rejectChunk(w, r, name, id, size)
				return
			}
		}
		if _, err := h.Store.AppendUpload(id, size, r.Body); err != nil {
			h.internal(w, r, err)
			return
		}
		h.finishUpload(w, r, name, id, d)

	case http.MethodDelete:
		h.abortUpload(r, id)
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "method not allowed")
	}
}

// finishUpload verifies the upload and moves it into the blob store. The digest
// lock makes "file placed + row inserted" atomic with respect to GC.
func (h *Handler) finishUpload(w http.ResponseWriter, r *http.Request, name, id string, d digest.Digest) {
	if size, err := h.Store.UploadSize(id); err == nil && !h.quotaAllows(r, name, d.String(), size) {
		h.abortUpload(r, id)
		writeError(w, http.StatusForbidden, "DENIED", "storage quota of project "+meta.ProjectOf(name)+" exceeded")
		return
	}
	unlock := h.Store.Lock(d)
	size, err := h.Store.CommitUpload(id, d)
	if err == nil {
		err = h.DB.LinkBlob(r.Context(), name, d.String(), size)
	}
	unlock()
	if err != nil {
		if errors.Is(err, digest.ErrMismatch) {
			h.abortUpload(r, id)
			writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "content does not match digest")
			return
		}
		h.internal(w, r, err)
		return
	}
	h.DB.DeleteUpload(r.Context(), id)
	w.Header().Set("Location", "/v2/"+name+"/blobs/"+d.String())
	w.Header().Set("Docker-Content-Digest", d.String())
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusCreated)
}

// parseContentRangeStart reads "<start>-<end>" (optionally prefixed "bytes ").
func parseContentRangeStart(s string) (int64, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "bytes ")
	start, _, ok := strings.Cut(s, "-")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(start, 10, 64)
	return n, err == nil
}

// quotaAllows reports whether linking the blob keeps the project within its
// quota. size < 0 means "look the blob up" (mounts). A blob the project
// already stores costs nothing.
func (h *Handler) quotaAllows(r *http.Request, repo, dgst string, size int64) bool {
	project := meta.ProjectOf(repo)
	p, err := h.DB.ProjectByName(r.Context(), project)
	if err != nil || p.QuotaBytes <= 0 || h.DB.ProjectHasBlob(r.Context(), project, dgst) {
		return true
	}
	if size < 0 {
		size, _ = h.DB.BlobExists(r.Context(), dgst)
	}
	used, err := h.DB.ProjectUsage(r.Context(), project)
	if err != nil {
		return true
	}
	if used+size > p.QuotaBytes {
		h.emit(r, events.Event{Action: events.QuotaExceeded, Repository: repo, Digest: dgst,
			Detail: fmt.Sprintf("%d + %d bytes exceeds quota of %d bytes", used, size, p.QuotaBytes)})
		return false
	}
	return true
}

// rejectChunk answers an out-of-order chunk with 416. The body is drained first
// (bounded): answering before reading it can reset the connection, and the
// client's next request then fails with EOF instead of a clean retry.
func rejectChunk(w http.ResponseWriter, r *http.Request, name, id string, size int64) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 4<<20))
	setUploadHeaders(w, name, id, size)
	writeError(w, http.StatusRequestedRangeNotSatisfiable, "BLOB_UPLOAD_INVALID", "out of order chunk")
}
