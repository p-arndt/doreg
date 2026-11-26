// Package meta is the metadata store (SQLite or PostgreSQL): repositories,
// manifests, tags, blob links, users and GC history.
package meta

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql migrations/postgres/*.sql
var migrationFS embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrTagImmutable is returned when a push would move an immutable tag.
var ErrTagImmutable = errors.New("tag is immutable")

// DB wraps one writer connection and a pool of readers. SQLite allows a single
// writer; funnelling writes through one connection with BEGIN IMMEDIATE avoids
// SQLITE_BUSY upgrade deadlocks, and readers never block on WAL.
//
// PostgreSQL keeps the single writer on purpose: GC mark and sweep rely on no
// other write interleaving with their transaction, which SQLite guarantees and
// READ COMMITTED does not. One writer connection gives the same guarantee
// within one doreg process; several processes on one database are unsupported
// anyway because digest locks are in-process.
type DB struct {
	W        *sql.DB
	R        *sql.DB
	Postgres bool

	pulls *pullCounter
}

// Open opens (and migrates) the database: a postgres:// or postgresql:// URL
// selects PostgreSQL, anything else is an SQLite file path.
func Open(dsn string) (*DB, error) {
	if IsPostgres(dsn) {
		return openPG(dsn)
	}
	return openSQLite(dsn)
}

func openPG(dsn string) (*DB, error) {
	w, err := openPostgres(dsn)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := migrate(w, true); err != nil {
		w.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	r, err := openPostgres(dsn)
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	db := &DB{W: w, R: r, Postgres: true}
	db.startPullFlusher()
	return db, nil
}

func openSQLite(path string) (*DB, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	base := "file:" + path + "?" + q.Encode()

	w, err := sql.Open("sqlite", base+"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := migrate(w, false); err != nil {
		w.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	r, err := sql.Open("sqlite", base)
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	db := &DB{W: w, R: r}
	db.startPullFlusher()
	return db, nil
}

// Close writes buffered pull counts and closes both pools.
func (db *DB) Close() error {
	db.stopPullFlusher()
	return errors.Join(db.R.Close(), db.W.Close())
}

// Tx runs fn in a write transaction and commits if fn returns nil.
func (db *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Now is the clock used for all timestamps; tests replace it.
var Now = func() int64 { return time.Now().Unix() }

// Setting reads a settings value, returning def if unset.
func (db *DB) Setting(ctx context.Context, key, def string) string {
	var v string
	if err := db.R.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

// SetSetting writes a settings value.
func (db *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := db.W.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// migrate applies migrations/NNN_*.sql (SQLite) or migrations/postgres/NNN_*.sql
// in order, tracking progress in PRAGMA user_version or the schema_version
// table. Each file runs in its own transaction. Both directories keep the same
// numbering so a schema version means the same thing on either database.
func migrate(w *sql.DB, postgres bool) error {
	dir := "migrations"
	if postgres {
		dir = "migrations/postgres"
	}
	names, err := fs.Glob(migrationFS, dir+"/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	version, err := schemaVersion(w, postgres)
	if err != nil {
		return err
	}
	for i, name := range names {
		if i < version {
			continue
		}
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := w.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(setVersionSQL(postgres, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func schemaVersion(w *sql.DB, postgres bool) (int, error) {
	var v int
	if !postgres {
		err := w.QueryRow(`PRAGMA user_version`).Scan(&v)
		return v, err
	}
	if _, err := w.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version BIGINT NOT NULL)`); err != nil {
		return 0, err
	}
	err := w.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v)
	return v, err
}

func setVersionSQL(postgres bool, v int) string {
	if postgres {
		return fmt.Sprintf(`DELETE FROM schema_version; INSERT INTO schema_version(version) VALUES (%d)`, v)
	}
	return fmt.Sprintf(`PRAGMA user_version = %d`, v)
}
