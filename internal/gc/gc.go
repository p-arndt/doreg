// Package gc removes unreferenced manifests and blobs while the registry keeps
// serving pushes and pulls.
//
// Safety rests on three rules:
//   - Mark and sweep of metadata runs in one write transaction. SQLite has a
//     single writer, so a manifest PUT (which checks its references in its own
//     write transaction) either commits before the sweep and is marked, or after
//     and sees the sweep's result and fails cleanly.
//   - Blob links (repo_blobs) and untagged manifests younger than the grace
//     period are kept, covering clients between "blob exists" and manifest PUT.
//   - Files are unlinked under the per-digest storage lock, after their row is
//     gone, so a concurrent upload of the same digest cannot lose its file.
package gc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/storage"
)

// Settings configure GC. Stored as JSON under the "gc" settings key.
type Settings struct {
	IntervalHours          int  `json:"intervalHours"`          // 0 disables scheduled runs
	GraceMinutes           int  `json:"graceMinutes"`           // protects in-flight pushes
	DeleteUntagged         bool `json:"deleteUntagged"`         // remove manifests no tag reaches
	KeepPulledUntaggedDays int  `json:"keepPulledUntaggedDays"` // keep untagged manifests pulled within N days (digest-pinned deploys)
	UploadMaxAgeHours      int  `json:"uploadMaxAgeHours"`      // abandon upload sessions after N hours
	ApplyRetention         bool `json:"applyRetention"`         // scheduled runs apply retention policies
	AuditRetentionDays     int  `json:"auditRetentionDays"`     // delete audit entries older than N days (0 keeps them forever)
}

// DefaultSettings are used until an admin changes them.
var DefaultSettings = Settings{
	IntervalHours:          24,
	GraceMinutes:           60,
	DeleteUntagged:         true,
	KeepPulledUntaggedDays: 7,
	UploadMaxAgeHours:      24,
	ApplyRetention:         true,
	AuditRetentionDays:     90,
}

// Validate rejects settings that would make GC unsafe.
func (s Settings) Validate() error {
	if s.GraceMinutes < 10 {
		return fmt.Errorf("grace period must be at least 10 minutes")
	}
	if s.IntervalHours < 0 || s.KeepPulledUntaggedDays < 0 || s.UploadMaxAgeHours < 1 || s.AuditRetentionDays < 0 {
		return fmt.Errorf("invalid values")
	}
	return nil
}

// Options select what a run does.
type Options struct {
	DryRun         bool   `json:"dryRun"`
	ApplyRetention bool   `json:"applyRetention"`
	Trigger        string `json:"-"`
}

// ManifestDeletion is a manifest GC removes.
type ManifestDeletion struct {
	Repo      string `json:"repo"`
	Digest    string `json:"digest"`
	MediaType string `json:"mediaType"`
	CreatedAt int64  `json:"createdAt"`
}

// Report describes what a run removed (or would remove, for dry runs).
type Report struct {
	DeletedTags      []TagDeletion      `json:"deletedTags"`
	DeletedManifests []ManifestDeletion `json:"deletedManifests"`
	DeletedRepos     []string           `json:"deletedRepos"`
	KeptRecent       int                `json:"keptRecent"` // untagged, kept by grace period or a recent pull
	UnlinkedBlobs    int                `json:"unlinkedBlobs"`
	DeletedBlobs     int                `json:"deletedBlobs"`
	FreedBytes       int64              `json:"freedBytes"`
	OrphanFiles      int                `json:"orphanFiles"`
	StaleUploads     int                `json:"staleUploads"`
	// PrunedAuditEntries counts audit rows older than the retention window.
	PrunedAuditEntries int `json:"prunedAuditEntries"`
}

// Run is a gc_runs row.
type Run struct {
	ID               int64   `json:"id"`
	Trigger          string  `json:"trigger"`
	DryRun           bool    `json:"dryRun"`
	StartedAt        int64   `json:"startedAt"`
	FinishedAt       int64   `json:"finishedAt"`
	Status           string  `json:"status"`
	DeletedTags      int     `json:"deletedTags"`
	DeletedManifests int     `json:"deletedManifests"`
	DeletedBlobs     int     `json:"deletedBlobs"`
	FreedBytes       int64   `json:"freedBytes"`
	Error            string  `json:"error,omitempty"`
	Report           *Report `json:"report,omitempty"`
}

// ErrRunning is returned when a run is already in progress.
var ErrRunning = errors.New("garbage collection is already running")

// Collector runs GC.
type Collector struct {
	DB    *meta.DB
	Store *storage.FS
	Log   *slog.Logger
	// OnFinish, if set, is called after a non-dry run has been recorded.
	OnFinish func(ctx context.Context, run *Run)

	running sync.Mutex
	mu      sync.Mutex
	nextRun time.Time
	wake    chan struct{}
}

// New returns a Collector.
func New(db *meta.DB, store *storage.FS, log *slog.Logger) *Collector {
	return &Collector{DB: db, Store: store, Log: log, wake: make(chan struct{}, 1)}
}

// Settings returns the current settings.
func (c *Collector) Settings(ctx context.Context) Settings {
	s := DefaultSettings
	if raw := c.DB.Setting(ctx, "gc", ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	return s
}

// SaveSettings validates and stores settings, and reschedules the next run.
func (c *Collector) SaveSettings(ctx context.Context, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	raw, _ := json.Marshal(s)
	if err := c.DB.SetSetting(ctx, "gc", string(raw)); err != nil {
		return err
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

// NextRun returns when the scheduler fires next (zero if disabled).
func (c *Collector) NextRun() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nextRun
}

// Running reports whether a run is in progress.
func (c *Collector) Running() bool {
	if c.running.TryLock() {
		c.running.Unlock()
		return false
	}
	return true
}

// Schedule runs GC periodically until ctx is cancelled.
func (c *Collector) Schedule(ctx context.Context) {
	for {
		s := c.Settings(ctx)
		var timer <-chan time.Time
		c.mu.Lock()
		if s.IntervalHours > 0 {
			interval := time.Duration(s.IntervalHours) * time.Hour
			next := time.Now().Add(interval)
			if last := c.lastScheduled(ctx); !last.IsZero() && last.Add(interval).After(time.Now()) {
				next = last.Add(interval)
			}
			c.nextRun = next
			timer = time.After(time.Until(next))
		} else {
			c.nextRun = time.Time{}
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-timer:
			if _, err := c.Run(ctx, Options{ApplyRetention: s.ApplyRetention, Trigger: "schedule"}); err != nil && !errors.Is(err, ErrRunning) {
				c.Log.Error("scheduled gc failed", "err", err)
			}
		}
	}
}

func (c *Collector) lastScheduled(ctx context.Context) time.Time {
	var ts int64
	if err := c.DB.R.QueryRowContext(ctx,
		`SELECT started_at FROM gc_runs WHERE trigger = 'schedule' ORDER BY id DESC LIMIT 1`).Scan(&ts); err != nil {
		return time.Time{}
	}
	return time.Unix(ts, 0)
}

// Run performs one GC pass.
func (c *Collector) Run(ctx context.Context, opts Options) (*Run, error) {
	if !c.running.TryLock() {
		return nil, ErrRunning
	}
	defer c.running.Unlock()
	if opts.Trigger == "" {
		opts.Trigger = "manual"
	}
	run := &Run{Trigger: opts.Trigger, DryRun: opts.DryRun, StartedAt: meta.Now(), Status: "running"}
	if err := c.DB.W.QueryRowContext(ctx,
		`INSERT INTO gc_runs(trigger, dry_run, started_at, status) VALUES(?, ?, ?, 'running') RETURNING id`,
		run.Trigger, run.DryRun, run.StartedAt).Scan(&run.ID); err != nil {
		return nil, err
	}

	report, runErr := c.run(ctx, opts)
	run.Report = report
	run.FinishedAt = meta.Now()
	run.Status = "succeeded"
	if runErr != nil {
		run.Status = "failed"
		run.Error = runErr.Error()
	}
	run.DeletedTags = len(report.DeletedTags)
	run.DeletedManifests = len(report.DeletedManifests)
	run.DeletedBlobs = report.DeletedBlobs
	run.FreedBytes = report.FreedBytes
	raw, _ := json.Marshal(report)
	// Use a fresh context so a cancelled request still records the outcome.
	if _, err := c.DB.W.ExecContext(context.WithoutCancel(ctx), `
		UPDATE gc_runs SET finished_at = ?, status = ?, deleted_tags = ?, deleted_manifests = ?,
			deleted_blobs = ?, freed_bytes = ?, error = ?, report = ? WHERE id = ?`,
		run.FinishedAt, run.Status, run.DeletedTags, run.DeletedManifests, run.DeletedBlobs,
		run.FreedBytes, run.Error, string(raw), run.ID); err != nil {
		return run, err
	}
	if !opts.DryRun && c.OnFinish != nil {
		c.OnFinish(ctx, run)
	}
	c.Log.Info("gc finished", "id", run.ID, "trigger", run.Trigger, "dryRun", run.DryRun, "status", run.Status,
		"tags", run.DeletedTags, "manifests", run.DeletedManifests, "blobs", run.DeletedBlobs, "freedBytes", run.FreedBytes)
	return run, runErr
}

type blobCandidate struct {
	digest string
	size   int64
}

func (c *Collector) run(ctx context.Context, opts Options) (*Report, error) {
	s := c.Settings(ctx)
	now := meta.Now()
	grace := now - int64(s.GraceMinutes)*60
	rep := &Report{DeletedTags: []TagDeletion{}, DeletedManifests: []ManifestDeletion{}, DeletedRepos: []string{}}

	var policies []Policy
	if opts.ApplyRetention {
		var err error
		if policies, err = Policies(ctx, c.DB); err != nil {
			return rep, err
		}
	}

	var candidates []blobCandidate
	tx, err := c.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return rep, err
	}
	err = c.sweep(ctx, tx, s, policies, now, grace, rep, &candidates)
	if err != nil || opts.DryRun {
		// A dry run executes the very same statements and rolls back, so the
		// preview cannot drift from what a real run does.
		tx.Rollback()
		if err != nil {
			return rep, err
		}
		for _, b := range candidates {
			rep.DeletedBlobs++
			rep.FreedBytes += b.size
		}
	} else if err := tx.Commit(); err != nil {
		return rep, err
	} else {
		for _, b := range candidates {
			if ctx.Err() != nil {
				return rep, ctx.Err()
			}
			deleted, err := c.deleteBlob(ctx, digest.Digest(b.digest))
			if err != nil {
				return rep, err
			}
			if deleted {
				rep.DeletedBlobs++
				rep.FreedBytes += b.size
			}
		}
	}

	if err := c.orphanFiles(ctx, grace, opts.DryRun, rep); err != nil {
		return rep, err
	}
	if err := c.staleUploads(ctx, now-int64(s.UploadMaxAgeHours)*3600, opts.DryRun, rep); err != nil {
		return rep, err
	}
	if !opts.DryRun && s.AuditRetentionDays > 0 {
		n, err := c.DB.PruneAudit(ctx, now-int64(s.AuditRetentionDays)*86400)
		if err != nil {
			return rep, fmt.Errorf("prune audit: %w", err)
		}
		rep.PrunedAuditEntries = int(n)
	}
	return rep, nil
}

func (c *Collector) sweep(ctx context.Context, tx *sql.Tx, s Settings, policies []Policy, now, grace int64, rep *Report, candidates *[]blobCandidate) error {
	// 1. Retention: delete tags.
	if len(policies) > 0 {
		dels, err := Evaluate(ctx, tx, policies, now)
		if err != nil {
			return err
		}
		for _, d := range dels {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM tags WHERE name = ? AND repo_id = (SELECT id FROM repositories WHERE name = ?)`,
				d.Tag, d.Repo); err != nil {
				return err
			}
		}
		rep.DeletedTags = append(rep.DeletedTags, dels...)
	}

	// 2. Mark manifests reachable from tags, recent or recently pulled roots,
	// through index children and referrers.
	roots := `SELECT id FROM manifests`
	args := []any{}
	if s.DeleteUntagged {
		roots = `SELECT manifest_id FROM tags
			UNION SELECT id FROM manifests WHERE created_at > ? OR (? > 0 AND last_pulled_at > ?)`
		pulledCutoff := int64(0)
		if s.KeepPulledUntaggedDays > 0 {
			pulledCutoff = now - int64(s.KeepPulledUntaggedDays)*86400
		}
		args = append(args, grace, s.KeepPulledUntaggedDays, pulledCutoff)
	}
	// Edges from a manifest to its index children and its referrers. PostgreSQL
	// allows only one recursive reference in a CTE, so both kinds go into one
	// indexed table that the recursion joins once.
	for _, q := range []string{
		`DROP TABLE IF EXISTS gc_live`,
		`DROP TABLE IF EXISTS gc_edges`,
		`CREATE TEMP TABLE gc_edges AS
			SELECT p.id AS parent, c.id AS child FROM manifests p
				JOIN manifest_refs r ON r.manifest_id = p.id AND r.kind = 'manifest'
				JOIN manifests c ON c.repo_id = p.repo_id AND c.digest = r.digest
			UNION
			SELECT p.id, s.id FROM manifests p
				JOIN manifests s ON s.repo_id = p.repo_id AND s.subject = p.digest`,
		`CREATE INDEX gc_edges_parent ON gc_edges(parent)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("mark: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE gc_live AS
		WITH RECURSIVE live(id) AS (
			`+roots+`
			UNION
			SELECT e.child FROM live JOIN gc_edges e ON e.parent = live.id
		)
		SELECT id FROM live`, args...); err != nil {
		return fmt.Errorf("mark: %w", err)
	}
	defer tx.ExecContext(ctx, `DROP TABLE IF EXISTS gc_edges`)
	defer tx.ExecContext(ctx, `DROP TABLE IF EXISTS gc_live`)

	// Untagged manifests that only the grace period or a recent pull keeps, so
	// the report can explain why they were not collected.
	if s.DeleteUntagged {
		if err := tx.QueryRowContext(ctx, `
			WITH RECURSIVE tagged(id) AS (
				SELECT manifest_id FROM tags
				UNION
				SELECT e.child FROM tagged JOIN gc_edges e ON e.parent = tagged.id
			)
			SELECT count(*) FROM gc_live WHERE id NOT IN (SELECT id FROM tagged)`).Scan(&rep.KeptRecent); err != nil {
			return fmt.Errorf("count kept: %w", err)
		}
	}

	// 3. Sweep unreachable manifests (tags cascade).
	rows, err := tx.QueryContext(ctx, `
		SELECT r.name, m.digest, m.media_type, m.created_at
		FROM manifests m JOIN repositories r ON r.id = m.repo_id
		WHERE m.id NOT IN (SELECT id FROM gc_live)
		ORDER BY r.name, m.created_at DESC`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d ManifestDeletion
		if err := rows.Scan(&d.Repo, &d.Digest, &d.MediaType, &d.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		rep.DeletedManifests = append(rep.DeletedManifests, d)
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM manifests WHERE id NOT IN (SELECT id FROM gc_live)`); err != nil {
		return fmt.Errorf("sweep manifests: %w", err)
	}

	// 4. Unlink blobs no manifest of their repository references any more.
	res, err := tx.ExecContext(ctx, `
		DELETE FROM repo_blobs WHERE touched_at < ? AND NOT EXISTS (
			SELECT 1 FROM manifest_refs mr JOIN manifests m ON m.id = mr.manifest_id
			WHERE m.repo_id = repo_blobs.repo_id AND mr.digest = repo_blobs.digest AND mr.kind = 'blob')`, grace)
	if err != nil {
		return fmt.Errorf("sweep links: %w", err)
	}
	n, _ := res.RowsAffected()
	rep.UnlinkedBlobs = int(n)

	// 5. Blobs no repository links any more. Their rows and files go after
	// commit, one at a time under the digest lock.
	rows, err = tx.QueryContext(ctx, `
		SELECT digest, size FROM blobs b
		WHERE created_at < ? AND NOT EXISTS (SELECT 1 FROM repo_blobs rb WHERE rb.digest = b.digest)`, grace)
	if err != nil {
		return err
	}
	for rows.Next() {
		var b blobCandidate
		if err := rows.Scan(&b.digest, &b.size); err != nil {
			rows.Close()
			return err
		}
		*candidates = append(*candidates, b)
	}
	rows.Close()

	// 6. Empty repositories.
	rows, err = tx.QueryContext(ctx, `
		SELECT name FROM repositories r WHERE created_at < ?
			AND NOT EXISTS (SELECT 1 FROM manifests WHERE repo_id = r.id)
			AND NOT EXISTS (SELECT 1 FROM repo_blobs WHERE repo_id = r.id)
			AND NOT EXISTS (SELECT 1 FROM uploads WHERE repo_id = r.id)
		ORDER BY name`, grace)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		rep.DeletedRepos = append(rep.DeletedRepos, name)
	}
	rows.Close()
	for _, name := range rep.DeletedRepos {
		if _, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE name = ?`, name); err != nil {
			return err
		}
	}
	return nil
}

// deleteBlob removes a blob whose links are gone. The row is re-checked under
// the digest lock because a push may have re-linked it since the sweep.
func (c *Collector) deleteBlob(ctx context.Context, d digest.Digest) (bool, error) {
	unlock := c.Store.Lock(d)
	defer unlock()
	res, err := c.DB.W.ExecContext(ctx, `
		DELETE FROM blobs WHERE digest = ? AND NOT EXISTS (SELECT 1 FROM repo_blobs WHERE digest = ?)`, d, d)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	return true, c.Store.Delete(d)
}

// orphanFiles removes blob files without a database row, left behind by crashes
// or by an earlier run interrupted between row delete and unlink.
func (c *Collector) orphanFiles(ctx context.Context, grace int64, dryRun bool, rep *Report) error {
	known := map[string]bool{}
	rows, err := c.DB.R.QueryContext(ctx, `SELECT digest FROM blobs`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return err
		}
		known[d] = true
	}
	rows.Close()
	return c.Store.WalkBlobs(func(d digest.Digest, size int64, mod time.Time) error {
		if known[d.String()] || mod.Unix() > grace {
			return nil
		}
		if dryRun {
			rep.OrphanFiles++
			rep.FreedBytes += size
			return nil
		}
		unlock := c.Store.Lock(d)
		defer unlock()
		var one int
		err := c.DB.W.QueryRowContext(ctx, `SELECT 1 FROM blobs WHERE digest = ?`, d).Scan(&one)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := c.Store.Delete(d); err != nil {
			return err
		}
		rep.OrphanFiles++
		rep.FreedBytes += size
		return nil
	})
}

func (c *Collector) staleUploads(ctx context.Context, cutoff int64, dryRun bool, rep *Report) error {
	rows, err := c.DB.R.QueryContext(ctx, `SELECT id FROM uploads WHERE updated_at < ?`, cutoff)
	if err != nil {
		return err
	}
	var stale []string
	active := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		stale = append(stale, id)
	}
	rows.Close()
	rows, err = c.DB.R.QueryContext(ctx, `SELECT id FROM uploads WHERE updated_at >= ?`, cutoff)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		active[id] = true
	}
	rows.Close()

	files, err := c.Store.UploadIDs()
	if err != nil {
		return err
	}
	staleSet := map[string]bool{}
	for _, id := range stale {
		staleSet[id] = true
	}
	for _, f := range files {
		if !active[f.ID] && !staleSet[f.ID] && f.ModUnix < cutoff {
			stale = append(stale, f.ID)
		}
	}
	rep.StaleUploads = len(stale)
	if dryRun {
		return nil
	}
	for _, id := range stale {
		if err := c.DB.DeleteUpload(ctx, id); err != nil {
			return err
		}
		if err := c.Store.DeleteUpload(id); err != nil {
			return err
		}
	}
	return nil
}

// Runs lists recent runs without their reports.
func Runs(ctx context.Context, db *meta.DB, limit int) ([]Run, error) {
	rows, err := db.R.QueryContext(ctx, `
		SELECT id, trigger, dry_run, started_at, finished_at, status, deleted_tags, deleted_manifests,
			deleted_blobs, freed_bytes, error
		FROM gc_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.Trigger, &r.DryRun, &r.StartedAt, &r.FinishedAt, &r.Status,
			&r.DeletedTags, &r.DeletedManifests, &r.DeletedBlobs, &r.FreedBytes, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRun returns one run with its report.
func GetRun(ctx context.Context, db *meta.DB, id int64) (*Run, error) {
	var r Run
	var raw string
	err := db.R.QueryRowContext(ctx, `
		SELECT id, trigger, dry_run, started_at, finished_at, status, deleted_tags, deleted_manifests,
			deleted_blobs, freed_bytes, error, report
		FROM gc_runs WHERE id = ?`, id).Scan(&r.ID, &r.Trigger, &r.DryRun, &r.StartedAt, &r.FinishedAt,
		&r.Status, &r.DeletedTags, &r.DeletedManifests, &r.DeletedBlobs, &r.FreedBytes, &r.Error, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, meta.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Report = &Report{}
	_ = json.Unmarshal([]byte(raw), r.Report)
	return &r, nil
}
