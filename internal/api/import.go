package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/importer"
	"github.com/p-arndt/doreg/internal/meta"
)

// importSource is what the UI sends to preview or start an import. The
// password is used for this one import and never stored.
type importSource struct {
	URL          string   `json:"url"`
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	Insecure     bool     `json:"insecure"`
	Repositories []string `json:"repositories"`
}

// ImportJob is the state of the current or last import.
type ImportJob struct {
	Source     string            `json:"source"`
	Actor      string            `json:"actor"`
	Status     string            `json:"status"` // running, done, failed, canceled
	StartedAt  int64             `json:"startedAt"`
	FinishedAt int64             `json:"finishedAt,omitempty"`
	Error      string            `json:"error,omitempty"`
	Progress   importer.Progress `json:"progress"`
}

// imports holds the one import that may run at a time. State lives in memory:
// an import is a one-off migration step, and rerunning it skips what is done.
type imports struct {
	mu     sync.Mutex
	job    *ImportJob
	im     *importer.Importer
	cancel context.CancelFunc
}

const importConcurrency = 4

func (a *API) sourceClient(s importSource) (*importer.Client, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = time.Minute
	if s.Insecure {
		// Old registries often run with self-signed certificates; the admin
		// opts in explicitly.
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return importer.NewClient(s.URL, importer.Credentials{Username: s.Username, Password: s.Password}, t, nil)
}

// newImporter connects to the source and checks the login.
func (a *API) newImporter(ctx context.Context, w http.ResponseWriter, r *http.Request, s importSource) (*importer.Importer, bool) {
	if a.Registry == nil {
		writeErr(w, http.StatusServiceUnavailable, "import is not available")
		return nil, false
	}
	if err := importer.ValidatePatterns(s.Repositories); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	src, err := a.sourceClient(s)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	if err := src.Ping(ctx); err != nil {
		writeErr(w, sourceStatus(err), "source registry: "+err.Error())
		return nil, false
	}
	u := currentUser(r)
	dst, err := importer.NewClient("http://doreg.internal", importer.Credentials{},
		a.Registry.Transport(auth.Principal{User: &u}), nil)
	if err != nil {
		a.fail(w, r, err)
		return nil, false
	}
	return &importer.Importer{Src: src, Dst: dst, Concurrency: importConcurrency}, true
}

// sourceStatus maps a failure of the source registry: wrong credentials are the
// admin's input (400), anything else means the source is unreachable (502).
func sourceStatus(err error) int {
	if errors.Is(err, importer.ErrUnauthorized) {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

func (a *API) importPreview(w http.ResponseWriter, r *http.Request) {
	var s importSource
	if !decode(w, r, &s) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	im, ok := a.newImporter(ctx, w, r, s)
	if !ok {
		return
	}
	plan, err := im.Plan(ctx, s.Repositories)
	if err != nil {
		writeErr(w, sourceStatus(err), "source registry: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repositories": plan})
}

func (a *API) importStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"job": a.imports.snapshot()})
}

func (i *imports) snapshot() *ImportJob {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.job == nil {
		return nil
	}
	j := *i.job
	if j.Status == "running" && i.im != nil {
		j.Progress = i.im.Progress()
	}
	return &j
}

func (a *API) importStart(w http.ResponseWriter, r *http.Request) {
	var s importSource
	if !decode(w, r, &s) {
		return
	}
	a.imports.mu.Lock()
	busy := a.imports.job != nil && a.imports.job.Status == "running"
	a.imports.mu.Unlock()
	if busy {
		writeErr(w, http.StatusConflict, "an import is already running")
		return
	}
	checkCtx, cancelCheck := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancelCheck()
	im, ok := a.newImporter(checkCtx, w, r, s)
	if !ok {
		return
	}
	u := currentUser(r)
	job := &ImportJob{Source: im.Src.Host(), Actor: u.Username, Status: "running", StartedAt: meta.Now(),
		Progress: importer.Progress{Errors: []string{}}}
	// Detached from the request: the import outlives the browser tab.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))

	a.imports.mu.Lock()
	if a.imports.job != nil && a.imports.job.Status == "running" {
		a.imports.mu.Unlock()
		cancel()
		writeErr(w, http.StatusConflict, "an import is already running")
		return
	}
	a.imports.job, a.imports.im, a.imports.cancel = job, im, cancel
	a.imports.mu.Unlock()

	a.record(r, "import_started", "", job.Source, "")
	go a.runImport(ctx, cancel, im, job, s.Repositories, u.Username, clientIP(r))
	writeJSON(w, http.StatusAccepted, map[string]any{"job": a.imports.snapshot()})
}

func (a *API) runImport(ctx context.Context, cancel context.CancelFunc, im *importer.Importer, job *ImportJob,
	patterns []string, actor, ip string) {
	defer cancel()
	plan, err := im.Plan(ctx, patterns)
	var progress importer.Progress
	if err == nil {
		progress, err = im.Run(ctx, plan)
	}
	a.imports.mu.Lock()
	job.FinishedAt = meta.Now()
	job.Progress = progress
	if job.Progress.Errors == nil {
		job.Progress.Errors = []string{}
	}
	switch {
	case errors.Is(err, context.Canceled):
		job.Status = "canceled"
	case err != nil:
		job.Status, job.Error = "failed", err.Error()
	default:
		job.Status = "done"
	}
	a.imports.im, a.imports.cancel = nil, nil
	a.imports.mu.Unlock()

	detail := fmt.Sprintf("%s: %d copied, %d up to date, %d failed, %d bytes", job.Status,
		progress.TagsCopied, progress.TagsSkipped, progress.TagsFailed, progress.Bytes)
	if job.Error != "" {
		detail += ": " + job.Error
	}
	a.Events.Audit(ctx, actor, "user", "import_finished", "", job.Source, detail, ip)
	a.Log.Info("import finished", "source", job.Source, "detail", detail)
}

func (a *API) importCancel(w http.ResponseWriter, r *http.Request) {
	a.imports.mu.Lock()
	cancel := a.imports.cancel
	a.imports.mu.Unlock()
	if cancel == nil {
		writeErr(w, http.StatusConflict, "no import is running")
		return
	}
	cancel()
	a.record(r, "import_canceled", "", "", "")
	w.WriteHeader(http.StatusNoContent)
}
