package meta

import "testing"

func TestRebind(t *testing.T) {
	cases := map[string]string{
		"SELECT 1":                                          "SELECT 1",
		"SELECT * FROM t WHERE a = ? AND b = ?":             "SELECT * FROM t WHERE a = $1 AND b = $2",
		"SELECT '?', \"a?\" FROM t WHERE a = ?":             "SELECT '?', \"a?\" FROM t WHERE a = $1",
		"SELECT 'it''s ?' WHERE x = ?":                      "SELECT 'it''s ?' WHERE x = $1",
		"-- why?\nSELECT ? /* or ? */, ?":                   "-- why?\nSELECT $1 /* or ? */, $2",
		"INSERT INTO t VALUES(?, ?) ON CONFLICT DO NOTHING": "INSERT INTO t VALUES($1, $2) ON CONFLICT DO NOTHING",
	}
	for in, want := range cases {
		if got := rebind(in); got != want {
			t.Errorf("rebind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsPostgres(t *testing.T) {
	for dsn, want := range map[string]bool{
		"postgres://u@h/db":    true,
		"postgresql://u@h/db":  true,
		"./data/doreg.db":      false,
		"/var/lib/postgres.db": false,
	} {
		if got := IsPostgres(dsn); got != want {
			t.Errorf("IsPostgres(%q) = %v", dsn, got)
		}
	}
}
