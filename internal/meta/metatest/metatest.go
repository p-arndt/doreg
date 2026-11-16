// Package metatest opens a fresh metadata database for tests: SQLite in a temp
// directory, or a throwaway PostgreSQL schema when DOREG_TEST_DATABASE_URL is set.
package metatest

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/p-arndt/doreg/internal/meta"
)

// EnvURL names the PostgreSQL server the tests run against.
const EnvURL = "DOREG_TEST_DATABASE_URL"

// DSN returns a database for one test and drops it on cleanup.
func DSN(t testing.TB) string {
	t.Helper()
	base := os.Getenv(EnvURL)
	if base == "" {
		return filepath.Join(t.TempDir(), "doreg.db")
	}
	b := make([]byte, 8)
	rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		admin.Close()
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// Open opens and migrates a fresh database, closed on cleanup.
func Open(t testing.TB) *meta.DB {
	t.Helper()
	db, err := meta.Open(DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
