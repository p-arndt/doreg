// Package meta is the SQLite metadata store: repositories, manifests, tags,
// blob links, users and GC history.
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

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// DB wraps one writer connection and a pool of readers. SQLite allows a single
// writer; funnelling writes through one connection with BEGIN IMMEDIATE avoids
// SQLITE_BUSY upgrade deadlocks, and readers never block on WAL.
type DB struct {
	W *sql.DB
	R *sql.DB
}

// Open opens (and migrates) the database at path.
func Open(path string) (*DB, error) {
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
	if err := migrate(w); err != nil {
		w.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	r, err := sql.Open("sqlite", base)
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	return &DB{W: w, R: r}, nil
}

// Close closes both pools.
func (db *DB) Close() error {
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

// migrate applies migrations/NNN_*.sql in order, tracking progress in
// PRAGMA user_version. Each file runs in its own transaction.
func migrate(w *sql.DB) error {
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	var version int
	if err := w.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
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
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
