package meta

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// IsPostgres reports whether dsn selects PostgreSQL instead of an SQLite file.
func IsPostgres(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

// openPostgres returns a pool whose connections accept the SQLite flavour the
// queries are written in: "?" placeholders and bool arguments for INTEGER
// columns. Keeping one query text for both databases avoids a second copy of
// every statement.
func openPostgres(dsn string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(pgConnector{stdlib.GetConnector(*cfg)}), nil
}

type pgConnector struct{ base driver.Connector }

func (c pgConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &pgConn{conn.(*stdlib.Conn)}, nil
}

func (c pgConnector) Driver() driver.Driver { return c.base.Driver() }

type pgConn struct{ *stdlib.Conn }

func (c *pgConn) Prepare(query string) (driver.Stmt, error) {
	return c.Conn.Prepare(rebind(query))
}

func (c *pgConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.Conn.PrepareContext(ctx, rebind(query))
}

func (c *pgConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.ExecContext(ctx, rebind(query), args)
}

func (c *pgConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.QueryContext(ctx, rebind(query), args)
}

// CheckNamedValue stores bools as 0/1, matching the INTEGER flag columns of the
// SQLite schema; database/sql scans them back into bool.
func (c *pgConn) CheckNamedValue(nv *driver.NamedValue) error {
	if b, ok := nv.Value.(bool); ok {
		if b {
			nv.Value = int64(1)
		} else {
			nv.Value = int64(0)
		}
		return nil
	}
	return c.Conn.CheckNamedValue(nv)
}

// rebind turns "?" placeholders into $1, $2, ... outside string literals,
// quoted identifiers and comments.
func rebind(q string) string {
	if !strings.Contains(q, "?") {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(q) {
				if q[j] == c {
					if j+1 < len(q) && q[j+1] == c {
						j += 2
						continue
					}
					break
				}
				j++
			}
			end := min(j+1, len(q))
			b.WriteString(q[i:end])
			i = end - 1
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			j := strings.IndexByte(q[i:], '\n')
			if j < 0 {
				j = len(q) - i
			}
			b.WriteString(q[i : i+j])
			i += j - 1
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			j := strings.Index(q[i+2:], "*/")
			end := len(q)
			if j >= 0 {
				end = i + 2 + j + 2
			}
			b.WriteString(q[i:end])
			i = end - 1
		case c == '?':
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
