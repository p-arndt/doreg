package meta

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// RobotPrefix starts every robot account name, so Basic credentials can be
// told apart from user accounts.
const RobotPrefix = "robot$"

// Robot is a non-human account for CI and deployments.
type Robot struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"` // robot$<project>+<name> or robot$<name> for system robots
	Description string `json:"description"`
	ProjectID   int64  `json:"projectId,omitempty"`
	Project     string `json:"project,omitempty"` // empty: every project
	SecretHash  string `json:"-"`
	CanPush     bool   `json:"canPush"`
	CanDelete   bool   `json:"canDelete"`
	ExpiresAt   int64  `json:"expiresAt"` // 0: never
	Disabled    bool   `json:"disabled"`
	CreatedBy   string `json:"createdBy"`
	CreatedAt   int64  `json:"createdAt"`
	LastUsedAt  int64  `json:"lastUsedAt"`
}

// RobotName builds the account name for a robot.
func RobotName(project, name string) string {
	if project == "" {
		return RobotPrefix + name
	}
	return RobotPrefix + project + "+" + name
}

// IsRobotName reports whether a username belongs to a robot.
func IsRobotName(s string) bool { return strings.HasPrefix(s, RobotPrefix) }

// Active reports whether the robot may authenticate at time now.
func (r Robot) Active(now int64) bool {
	return !r.Disabled && (r.ExpiresAt == 0 || r.ExpiresAt > now)
}

const robotCols = `rb.id, rb.name, rb.description, coalesce(rb.project_id, 0), coalesce(p.name, ''), rb.secret_hash,
	rb.can_push, rb.can_delete, rb.expires_at, rb.disabled, rb.created_by, rb.created_at, rb.last_used_at`

func scanRobot(s scanner) (Robot, error) {
	var r Robot
	err := s.Scan(&r.ID, &r.Name, &r.Description, &r.ProjectID, &r.Project, &r.SecretHash,
		&r.CanPush, &r.CanDelete, &r.ExpiresAt, &r.Disabled, &r.CreatedBy, &r.CreatedAt, &r.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// RobotByName looks a robot up by its full account name.
func (db *DB) RobotByName(ctx context.Context, name string) (Robot, error) {
	return scanRobot(db.R.QueryRowContext(ctx,
		`SELECT `+robotCols+` FROM robots rb LEFT JOIN projects p ON p.id = rb.project_id WHERE rb.name = ?`, name))
}

// RobotByID looks a robot up.
func (db *DB) RobotByID(ctx context.Context, id int64) (Robot, error) {
	return scanRobot(db.R.QueryRowContext(ctx,
		`SELECT `+robotCols+` FROM robots rb LEFT JOIN projects p ON p.id = rb.project_id WHERE rb.id = ?`, id))
}

// Robots lists robots; project "" lists all, "-" only system robots.
func (db *DB) Robots(ctx context.Context, project string) ([]Robot, error) {
	q := `SELECT ` + robotCols + ` FROM robots rb LEFT JOIN projects p ON p.id = rb.project_id`
	var args []any
	switch project {
	case "":
	case "-":
		q += ` WHERE rb.project_id IS NULL`
	default:
		q += ` WHERE p.name = ?`
		args = append(args, project)
	}
	rows, err := db.R.QueryContext(ctx, q+` ORDER BY rb.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Robot{}
	for rows.Next() {
		r, err := scanRobot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateRobot stores a robot.
func (db *DB) CreateRobot(ctx context.Context, r Robot) (Robot, error) {
	var pid any
	if r.ProjectID > 0 {
		pid = r.ProjectID
	}
	r.CreatedAt = Now()
	res, err := db.W.ExecContext(ctx, `
		INSERT INTO robots(name, description, project_id, secret_hash, can_push, can_delete, expires_at, created_by, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(name) DO NOTHING`,
		r.Name, r.Description, pid, r.SecretHash, r.CanPush, r.CanDelete, r.ExpiresAt, r.CreatedBy, r.CreatedAt)
	if err != nil {
		return r, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r, ErrExists
	}
	r.ID, _ = res.LastInsertId()
	return r, nil
}

// UpdateRobot changes permissions, expiry, description and the disabled flag.
func (db *DB) UpdateRobot(ctx context.Context, r Robot) error {
	res, err := db.W.ExecContext(ctx, `
		UPDATE robots SET description = ?, can_push = ?, can_delete = ?, expires_at = ?, disabled = ? WHERE id = ?`,
		r.Description, r.CanPush, r.CanDelete, r.ExpiresAt, r.Disabled, r.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetRobotSecret replaces the secret hash.
func (db *DB) SetRobotSecret(ctx context.Context, id int64, hash string) error {
	_, err := db.W.ExecContext(ctx, `UPDATE robots SET secret_hash = ? WHERE id = ?`, hash, id)
	return err
}

// DeleteRobot removes a robot.
func (db *DB) DeleteRobot(ctx context.Context, id int64) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM robots WHERE id = ?`, id)
	return err
}

// TouchRobot records a use (throttled).
func (db *DB) TouchRobot(ctx context.Context, r Robot) {
	if now := Now(); now-r.LastUsedAt > touchInterval {
		_, _ = db.W.ExecContext(ctx, `UPDATE robots SET last_used_at = ? WHERE id = ?`, now, r.ID)
	}
}
