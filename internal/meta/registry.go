package meta

import (
	"context"
	"database/sql"
	"errors"
	"math"
)

// touchInterval throttles touched_at / last_pulled_at writes so pulls stay reads.
const touchInterval = 10 * 60

// Repo is a repository row.
type Repo struct {
	ID        int64
	Name      string
	Public    bool
	CreatedAt int64
}

// RepoByName looks a repository up.
func (db *DB) RepoByName(ctx context.Context, name string) (Repo, error) {
	var r Repo
	err := db.R.QueryRowContext(ctx,
		`SELECT id, name, public, created_at FROM repositories WHERE name = ?`, name).
		Scan(&r.ID, &r.Name, &r.Public, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// EnsureRepo returns the id of the repository, creating it inside tx.
func EnsureRepo(ctx context.Context, tx *sql.Tx, name string) (int64, error) {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO repositories(name, created_at) VALUES(?, ?) ON CONFLICT(name) DO NOTHING`,
		name, Now()); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM repositories WHERE name = ?`, name).Scan(&id)
	return id, err
}

// BlobInRepo returns the blob size if the blob is linked to the repository,
// refreshing the link's touched_at (throttled) so GC keeps it while a push is in
// flight.
func (db *DB) BlobInRepo(ctx context.Context, repo, digest string) (int64, error) {
	var (
		repoID, size, touched int64
	)
	err := db.R.QueryRowContext(ctx, `
		SELECT r.id, b.size, rb.touched_at
		FROM repositories r
		JOIN repo_blobs rb ON rb.repo_id = r.id
		JOIN blobs b ON b.digest = rb.digest
		WHERE r.name = ? AND rb.digest = ?`, repo, digest).Scan(&repoID, &size, &touched)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if now := Now(); now-touched > touchInterval {
		_, _ = db.W.ExecContext(ctx,
			`UPDATE repo_blobs SET touched_at = ? WHERE repo_id = ? AND digest = ?`, now, repoID, digest)
	}
	return size, nil
}

// LinkBlob records a stored blob and links it to the repository. The caller
// holds the storage digest lock and has already placed the file.
func (db *DB) LinkBlob(ctx context.Context, repo, digest string, size int64) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		repoID, err := EnsureRepo(ctx, tx, repo)
		if err != nil {
			return err
		}
		now := Now()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO blobs(digest, size, created_at) VALUES(?, ?, ?) ON CONFLICT(digest) DO NOTHING`,
			digest, size, now); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO repo_blobs(repo_id, digest, touched_at) VALUES(?, ?, ?)
			ON CONFLICT(repo_id, digest) DO UPDATE SET touched_at = excluded.touched_at`,
			repoID, digest, now)
		return err
	})
}

// MountBlob links an existing blob from one repository (any, if from is empty)
// into another. It reports false if the source does not have the blob, in which
// case the client falls back to a regular upload.
func (db *DB) MountBlob(ctx context.Context, from, to, digest string) (int64, bool, error) {
	var size int64
	found := false
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `
			SELECT b.size FROM blobs b
			JOIN repo_blobs rb ON rb.digest = b.digest
			JOIN repositories r ON r.id = rb.repo_id
			WHERE (? = '' OR r.name = ?) AND b.digest = ? LIMIT 1`, from, from, digest).Scan(&size)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		repoID, err := EnsureRepo(ctx, tx, to)
		if err != nil {
			return err
		}
		found = true
		_, err = tx.ExecContext(ctx, `
			INSERT INTO repo_blobs(repo_id, digest, touched_at) VALUES(?, ?, ?)
			ON CONFLICT(repo_id, digest) DO UPDATE SET touched_at = excluded.touched_at`,
			repoID, digest, Now())
		return err
	})
	return size, found, err
}

// BlobExists reports whether any repository has the blob, returning its size.
func (db *DB) BlobExists(ctx context.Context, digest string) (int64, bool) {
	var size int64
	err := db.R.QueryRowContext(ctx, `SELECT size FROM blobs WHERE digest = ?`, digest).Scan(&size)
	return size, err == nil
}

// UnlinkBlob removes a blob from one repository. GC frees the bytes later.
func (db *DB) UnlinkBlob(ctx context.Context, repo, digest string) error {
	res, err := db.W.ExecContext(ctx, `
		DELETE FROM repo_blobs
		WHERE digest = ? AND repo_id = (SELECT id FROM repositories WHERE name = ?)`, digest, repo)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Manifest is a stored manifest.
type Manifest struct {
	ID           int64
	RepoID       int64
	Digest       string
	MediaType    string
	ArtifactType string
	Content      []byte
	Subject      string
	CreatedAt    int64
	LastPulledAt int64
}

// Ref is one descriptor a manifest references.
type Ref struct {
	Kind      string // "blob" or "manifest"
	Digest    string
	Size      int64
	MediaType string
}

// ErrMissingRef reports a manifest that references content the repository lacks.
type ErrMissingRef struct {
	Ref Ref
}

func (e *ErrMissingRef) Error() string {
	return "manifest references unknown " + e.Ref.Kind + " " + e.Ref.Digest
}

// PutManifest stores a manifest and optionally points a tag at it. Reference
// checks run inside the write transaction, so GC (which also takes the single
// writer) can never remove a referenced blob between check and insert.
func (db *DB) PutManifest(ctx context.Context, repo string, m Manifest, refs []Ref, tag string) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		repoID, err := EnsureRepo(ctx, tx, repo)
		if err != nil {
			return err
		}
		for _, r := range refs {
			var q string
			if r.Kind == "manifest" {
				q = `SELECT 1 FROM manifests WHERE repo_id = ? AND digest = ?`
			} else {
				q = `SELECT 1 FROM repo_blobs WHERE repo_id = ? AND digest = ?`
			}
			var one int
			if err := tx.QueryRowContext(ctx, q, repoID, r.Digest).Scan(&one); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return &ErrMissingRef{Ref: r}
				}
				return err
			}
		}
		now := Now()
		var id int64
		err = tx.QueryRowContext(ctx,
			`SELECT id FROM manifests WHERE repo_id = ? AND digest = ?`, repoID, m.Digest).Scan(&id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO manifests(repo_id, digest, media_type, artifact_type, content, subject, created_at)
				VALUES(?, ?, ?, ?, ?, ?, ?) RETURNING id`,
				repoID, m.Digest, m.MediaType, m.ArtifactType, m.Content, m.Subject, now).Scan(&id); err != nil {
				return err
			}
			for _, r := range refs {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO manifest_refs(manifest_id, kind, digest, size, media_type)
					VALUES(?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
					id, r.Kind, r.Digest, r.Size, r.MediaType); err != nil {
					return err
				}
			}
		case err != nil:
			return err
		default:
			// Re-pushing an existing manifest restarts its grace period.
			if _, err := tx.ExecContext(ctx,
				`UPDATE manifests SET created_at = ? WHERE id = ?`, now, id); err != nil {
				return err
			}
		}
		if tag == "" {
			return nil
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO tags(repo_id, name, manifest_id, updated_at) VALUES(?, ?, ?, ?)
			ON CONFLICT(repo_id, name) DO UPDATE SET manifest_id = excluded.manifest_id, updated_at = excluded.updated_at`,
			repoID, tag, id, now)
		return err
	})
}

// GetManifest resolves ref (a tag or digest) in repo.
func (db *DB) GetManifest(ctx context.Context, repo, ref string, isDigest bool) (Manifest, error) {
	q := `SELECT m.id, m.repo_id, m.digest, m.media_type, m.artifact_type, m.content, m.subject, m.created_at, m.last_pulled_at
		FROM manifests m JOIN repositories r ON r.id = m.repo_id `
	if isDigest {
		q += `WHERE r.name = ? AND m.digest = ?`
	} else {
		q += `JOIN tags t ON t.manifest_id = m.id WHERE r.name = ? AND t.name = ?`
	}
	var m Manifest
	err := db.R.QueryRowContext(ctx, q, repo, ref).Scan(&m.ID, &m.RepoID, &m.Digest, &m.MediaType,
		&m.ArtifactType, &m.Content, &m.Subject, &m.CreatedAt, &m.LastPulledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// MarkPulled records a pull (throttled), so GC can keep digest-pinned untagged
// manifests that are still in use.
func (db *DB) MarkPulled(ctx context.Context, m Manifest) {
	if now := Now(); now-m.LastPulledAt > touchInterval {
		_, _ = db.W.ExecContext(ctx, `UPDATE manifests SET last_pulled_at = ? WHERE id = ?`, now, m.ID)
	}
}

// DeleteManifest removes a manifest and every tag pointing at it.
func (db *DB) DeleteManifest(ctx context.Context, repo, digest string) error {
	res, err := db.W.ExecContext(ctx, `
		DELETE FROM manifests
		WHERE digest = ? AND repo_id = (SELECT id FROM repositories WHERE name = ?)`, digest, repo)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTag removes a tag; the manifest stays until GC decides.
func (db *DB) DeleteTag(ctx context.Context, repo, tag string) error {
	res, err := db.W.ExecContext(ctx, `
		DELETE FROM tags
		WHERE name = ? AND repo_id = (SELECT id FROM repositories WHERE name = ?)`, tag, repo)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteRepo removes all tags and manifests of a repository. Blobs are freed by GC.
func (db *DB) DeleteRepo(ctx context.Context, name string) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM repositories WHERE name = ?`, name).Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE id = ?`, id)
		return err
	})
}

// SetRepoPublic toggles anonymous pull for a repository.
func (db *DB) SetRepoPublic(ctx context.Context, name string, public bool) error {
	res, err := db.W.ExecContext(ctx, `UPDATE repositories SET public = ? WHERE name = ?`, public, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTags returns tag names in lexical order after last, at most n (n < 0 = all).
func (db *DB) ListTags(ctx context.Context, repo, last string, n int) ([]string, error) {
	rows, err := db.R.QueryContext(ctx, `
		SELECT t.name FROM tags t JOIN repositories r ON r.id = t.repo_id
		WHERE r.name = ? AND t.name > ? ORDER BY t.name LIMIT ?`, repo, last, noLimit(n))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListRepoNames returns repository names in lexical order after last.
func (db *DB) ListRepoNames(ctx context.Context, last string, n int) ([]string, error) {
	rows, err := db.R.QueryContext(ctx,
		`SELECT name FROM repositories WHERE name > ? ORDER BY name LIMIT ?`, last, noLimit(n))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Referrers returns manifests in repo whose subject is digest.
func (db *DB) Referrers(ctx context.Context, repo, digest string) ([]Manifest, error) {
	rows, err := db.R.QueryContext(ctx, `
		SELECT m.digest, m.media_type, m.artifact_type, m.content
		FROM manifests m JOIN repositories r ON r.id = m.repo_id
		WHERE r.name = ? AND m.subject = ? ORDER BY m.created_at`, repo, digest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Manifest
	for rows.Next() {
		var m Manifest
		if err := rows.Scan(&m.Digest, &m.MediaType, &m.ArtifactType, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateUpload registers an upload session.
func (db *DB) CreateUpload(ctx context.Context, repo, id string) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		repoID, err := EnsureRepo(ctx, tx, repo)
		if err != nil {
			return err
		}
		now := Now()
		_, err = tx.ExecContext(ctx,
			`INSERT INTO uploads(id, repo_id, started_at, updated_at) VALUES(?, ?, ?, ?)`, id, repoID, now, now)
		return err
	})
}

// UploadInRepo reports whether the upload belongs to repo.
func (db *DB) UploadInRepo(ctx context.Context, repo, id string) bool {
	var one int
	err := db.R.QueryRowContext(ctx, `
		SELECT 1 FROM uploads u JOIN repositories r ON r.id = u.repo_id
		WHERE u.id = ? AND r.name = ?`, id, repo).Scan(&one)
	return err == nil
}

// TouchUpload refreshes the upload's activity time.
func (db *DB) TouchUpload(ctx context.Context, id string) {
	_, _ = db.W.ExecContext(ctx, `UPDATE uploads SET updated_at = ? WHERE id = ?`, Now(), id)
}

// DeleteUpload forgets an upload session.
func (db *DB) DeleteUpload(ctx context.Context, id string) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM uploads WHERE id = ?`, id)
	return err
}

// noLimit maps a negative page size ("all") to a LIMIT both databases accept:
// SQLite reads a negative LIMIT as unlimited, PostgreSQL rejects it.
func noLimit(n int) int64 {
	if n < 0 {
		return math.MaxInt64
	}
	return int64(n)
}
