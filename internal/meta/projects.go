package meta

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

// Project roles, from least to most privileged.
const (
	ProjectRead  = "read"  // pull
	ProjectWrite = "write" // pull, push, delete tags and manifests
	ProjectAdmin = "admin" // also members, robots, webhooks, repository settings
)

// DefaultProject holds repositories whose name has no slash.
const DefaultProject = "library"

var projectNameRe = regexp.MustCompile(`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*$`)

// ValidProjectName reports whether s can be a project (the first path segment
// of a repository name).
func ValidProjectName(s string) bool { return len(s) <= 64 && projectNameRe.MatchString(s) }

// ValidProjectRole reports whether r is a known project role.
func ValidProjectRole(r string) bool {
	return r == ProjectRead || r == ProjectWrite || r == ProjectAdmin
}

// RoleRank orders project roles; 0 means no access.
func RoleRank(r string) int {
	switch r {
	case ProjectRead:
		return 1
	case ProjectWrite:
		return 2
	case ProjectAdmin:
		return 3
	}
	return 0
}

// ProjectOf returns the project a repository belongs to.
func ProjectOf(repo string) string {
	if i := strings.IndexByte(repo, '/'); i > 0 {
		return repo[:i]
	}
	return DefaultProject
}

// projectRepoFilter is a SQL condition selecting repositories (alias r) of the
// project bound to the two placeholders.
const projectRepoFilter = `(CASE WHEN instr(r.name, '/') > 0 THEN substr(r.name, 1, instr(r.name, '/') - 1) ELSE 'library' END) = ?`

// Project is a namespace with members and a quota.
type Project struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Public     bool   `json:"public"`
	QuotaBytes int64  `json:"quotaBytes"`
	// ImmutableTags refuses pushes that move an existing tag to another
	// manifest. DOREG_IMMUTABLE_TAGS turns this on for every project.
	ImmutableTags bool  `json:"immutableTags"`
	CreatedAt     int64 `json:"createdAt"`
}

func scanProject(s scanner) (Project, error) {
	var p Project
	err := s.Scan(&p.ID, &p.Name, &p.Public, &p.QuotaBytes, &p.ImmutableTags, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// ProjectByName looks a project up.
func (db *DB) ProjectByName(ctx context.Context, name string) (Project, error) {
	return scanProject(db.R.QueryRowContext(ctx,
		`SELECT id, name, public, quota_bytes, immutable_tags, created_at FROM projects WHERE name = ?`, name))
}

// Projects lists all projects.
func (db *DB) Projects(ctx context.Context) ([]Project, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT id, name, public, quota_bytes, immutable_tags, created_at FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ErrExists reports a unique name collision.
var ErrExists = errors.New("already exists")

// CreateProject adds a project.
func (db *DB) CreateProject(ctx context.Context, p Project) (Project, error) {
	p.CreatedAt = Now()
	err := db.W.QueryRowContext(ctx,
		`INSERT INTO projects(name, public, quota_bytes, immutable_tags, created_at) VALUES(?, ?, ?, ?, ?) ON CONFLICT(name) DO NOTHING RETURNING id`,
		p.Name, p.Public, p.QuotaBytes, p.ImmutableTags, p.CreatedAt).Scan(&p.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrExists
	}
	return p, err
}

// EnsureProject returns the project, creating it if missing.
func (db *DB) EnsureProject(ctx context.Context, name string) (Project, error) {
	if p, err := db.ProjectByName(ctx, name); err == nil {
		return p, nil
	}
	if _, err := db.CreateProject(ctx, Project{Name: name}); err != nil && !errors.Is(err, ErrExists) {
		return Project{}, err
	}
	return db.ProjectByName(ctx, name)
}

// UpdateProject changes visibility, quota and tag immutability.
func (db *DB) UpdateProject(ctx context.Context, p Project) error {
	res, err := db.W.ExecContext(ctx, `UPDATE projects SET public = ?, quota_bytes = ?, immutable_tags = ? WHERE id = ?`,
		p.Public, p.QuotaBytes, p.ImmutableTags, p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject removes a project and all its repositories' tags and
// manifests. GC frees the bytes.
func (db *DB) DeleteProject(ctx context.Context, name string) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM repositories AS r WHERE `+projectRepoFilter, name); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE name = ?`, name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ProjectUsage returns the bytes stored for a project, counting each blob once
// even when several of its repositories share it.
func (db *DB) ProjectUsage(ctx context.Context, name string) (int64, error) {
	var n int64
	err := db.R.QueryRowContext(ctx, `
		SELECT CAST(coalesce(sum(size), 0) AS BIGINT) FROM blobs WHERE digest IN (
			SELECT rb.digest FROM repo_blobs rb JOIN repositories r ON r.id = rb.repo_id WHERE `+projectRepoFilter+`)`,
		name).Scan(&n)
	return n, err
}

// ProjectHasBlob reports whether any repository of the project already links
// the blob, so linking it again costs no quota.
func (db *DB) ProjectHasBlob(ctx context.Context, name, digest string) bool {
	var one int
	err := db.R.QueryRowContext(ctx, `
		SELECT 1 FROM repo_blobs rb JOIN repositories r ON r.id = rb.repo_id
		WHERE rb.digest = ? AND `+projectRepoFilter+` LIMIT 1`, digest, name).Scan(&one)
	return err == nil
}

// ProjectRepoCount returns the number of repositories in a project.
func (db *DB) ProjectRepoCount(ctx context.Context, name string) (int, error) {
	var n int
	err := db.R.QueryRowContext(ctx, `SELECT count(*) FROM repositories r WHERE `+projectRepoFilter, name).Scan(&n)
	return n, err
}

// Member is a project member: a user or an OIDC group.
type Member struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"userId,omitempty"`
	Username  string `json:"username,omitempty"`
	Group     string `json:"group,omitempty"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"createdAt"`
}

// Members lists a project's members.
func (db *DB) Members(ctx context.Context, projectID int64) ([]Member, error) {
	rows, err := db.R.QueryContext(ctx, `
		SELECT m.id, coalesce(m.user_id, 0), coalesce(u.username, ''), m.group_name, m.role, m.created_at
		FROM project_members m LEFT JOIN users u ON u.id = m.user_id
		WHERE m.project_id = ? ORDER BY m.group_name != '', coalesce(u.username, m.group_name)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.UserID, &m.Username, &m.Group, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMember adds a user (userID > 0) or a group to a project.
func (db *DB) AddMember(ctx context.Context, projectID, userID int64, group, role string) error {
	var uid any
	if userID > 0 {
		uid = userID
		group = ""
	}
	_, err := db.W.ExecContext(ctx, `
		INSERT INTO project_members(project_id, user_id, group_name, role, created_at) VALUES(?, ?, ?, ?, ?)`,
		projectID, uid, group, role, Now())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrExists
	}
	return err
}

// UpdateMember changes a member's role.
func (db *DB) UpdateMember(ctx context.Context, projectID, memberID int64, role string) error {
	res, err := db.W.ExecContext(ctx, `UPDATE project_members SET role = ? WHERE id = ? AND project_id = ?`,
		role, memberID, projectID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RemoveMember deletes a membership.
func (db *DB) RemoveMember(ctx context.Context, projectID, memberID int64) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM project_members WHERE id = ? AND project_id = ?`, memberID, projectID)
	return err
}

// MemberRole returns the strongest role a user holds in a project, directly or
// through one of its groups. Empty means not a member.
func (db *DB) MemberRole(ctx context.Context, project string, userID int64, groups []string) (string, error) {
	q := `SELECT m.role FROM project_members m JOIN projects p ON p.id = m.project_id
		WHERE p.name = ? AND (m.user_id = ?`
	args := []any{project, userID}
	if len(groups) > 0 {
		q += ` OR m.group_name IN (?` + strings.Repeat(`, ?`, len(groups)-1) + `)`
		for _, g := range groups {
			args = append(args, g)
		}
	}
	q += `)`
	rows, err := db.R.QueryContext(ctx, q, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	best := ""
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return "", err
		}
		if RoleRank(r) > RoleRank(best) {
			best = r
		}
	}
	return best, rows.Err()
}
