package meta

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// System roles. What a user may do inside a project comes from its project
// membership (see ProjectRole), not from these.
const (
	RoleUser  = "user"
	RoleAdmin = "admin" // system administrator: every project, users, GC, settings
)

// ValidRole reports whether r is a known system role.
func ValidRole(r string) bool { return r == RoleUser || r == RoleAdmin }

// User is an account.
type User struct {
	ID           int64    `json:"id"`
	Username     string   `json:"username"`
	PasswordHash string   `json:"-"`
	Role         string   `json:"role"`
	Email        string   `json:"email"`
	OIDCSubject  string   `json:"-"`
	Groups       []string `json:"groups"`
	CLISecret    string   `json:"-"` // bcrypt hash
	CreatedAt    int64    `json:"createdAt"`
	LastLoginAt  int64    `json:"lastLoginAt"`
}

// IsAdmin reports system admin permission.
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// SSO reports whether the account is managed by the OIDC provider.
func (u User) SSO() bool { return u.OIDCSubject != "" }

// MarshalJSON adds derived fields for the UI.
func (u User) MarshalJSON() ([]byte, error) {
	type plain User
	return json.Marshal(struct {
		plain
		SSO          bool `json:"sso"`
		HasCLISecret bool `json:"hasCliSecret"`
		HasPassword  bool `json:"hasPassword"`
	}{plain(u), u.SSO(), u.CLISecret != "", u.PasswordHash != ""})
}

const userCols = `id, username, password_hash, role, email, oidc_subject, groups, cli_secret_hash, created_at, last_login_at`

type scanner interface{ Scan(dest ...any) error }

func scanUser(s scanner) (User, error) {
	var u User
	var groups string
	err := s.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Email, &u.OIDCSubject, &groups,
		&u.CLISecret, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	_ = json.Unmarshal([]byte(groups), &u.Groups)
	if u.Groups == nil {
		u.Groups = []string{}
	}
	return u, err
}

// UserByName looks a user up.
func (db *DB) UserByName(ctx context.Context, name string) (User, error) {
	return scanUser(db.R.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, name))
}

// UserByID looks a user up.
func (db *DB) UserByID(ctx context.Context, id int64) (User, error) {
	return scanUser(db.R.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

// UserByOIDCSubject finds the account linked to an OIDC identity.
func (db *DB) UserByOIDCSubject(ctx context.Context, subject string) (User, error) {
	return scanUser(db.R.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE oidc_subject = ?`, subject))
}

// Users lists all users.
func (db *DB) Users(ctx context.Context) ([]User, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpsertUser creates a local user or replaces its password hash and role.
func (db *DB) UpsertUser(ctx context.Context, name, hash, role string) error {
	_, err := db.W.ExecContext(ctx, `
		INSERT INTO users(username, password_hash, role, created_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET password_hash = excluded.password_hash, role = excluded.role`,
		name, hash, role, Now())
	return err
}

// CreateOIDCUser creates an account for a first-time SSO login.
func (db *DB) CreateOIDCUser(ctx context.Context, u User) (User, error) {
	groups, _ := json.Marshal(u.Groups)
	now := Now()
	res, err := db.W.ExecContext(ctx, `
		INSERT INTO users(username, password_hash, role, email, oidc_subject, groups, created_at, last_login_at)
		VALUES(?, '', ?, ?, ?, ?, ?, ?)`, u.Username, u.Role, u.Email, u.OIDCSubject, string(groups), now, now)
	if err != nil {
		return u, err
	}
	u.ID, _ = res.LastInsertId()
	u.CreatedAt, u.LastLoginAt = now, now
	return u, nil
}

// UpdateOIDCLogin refreshes claims that may change at the provider.
func (db *DB) UpdateOIDCLogin(ctx context.Context, id int64, email, role string, groups []string) error {
	g, _ := json.Marshal(groups)
	_, err := db.W.ExecContext(ctx,
		`UPDATE users SET email = ?, role = ?, groups = ?, last_login_at = ? WHERE id = ?`,
		email, role, string(g), Now(), id)
	return err
}

// TouchLogin records a successful local login.
func (db *DB) TouchLogin(ctx context.Context, id int64) {
	_, _ = db.W.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, Now(), id)
}

// UpdateUser changes role and, if hash is non-empty, the password.
func (db *DB) UpdateUser(ctx context.Context, id int64, hash, role string) error {
	var res sql.Result
	var err error
	if hash != "" {
		res, err = db.W.ExecContext(ctx, `UPDATE users SET role = ?, password_hash = ? WHERE id = ?`, role, hash, id)
	} else {
		res, err = db.W.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, id)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetCLISecret stores (or with an empty hash, clears) the user's CLI secret.
func (db *DB) SetCLISecret(ctx context.Context, id int64, hash string) error {
	_, err := db.W.ExecContext(ctx, `UPDATE users SET cli_secret_hash = ? WHERE id = ?`, hash, id)
	return err
}

// DeleteUser removes a user, its sessions and memberships.
func (db *DB) DeleteUser(ctx context.Context, id int64) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession stores a session token (hashed) for the user.
func (db *DB) CreateSession(ctx context.Context, token string, userID, expiresAt int64) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, Now())
	if err != nil {
		return err
	}
	_, err = db.W.ExecContext(ctx,
		`INSERT INTO sessions(token_hash, user_id, expires_at) VALUES(?, ?, ?)`, hashToken(token), userID, expiresAt)
	return err
}

// SessionUser resolves a session token.
func (db *DB) SessionUser(ctx context.Context, token string) (User, error) {
	return scanUser(db.R.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.password_hash, u.role, u.email, u.oidc_subject, u.groups, u.cli_secret_hash,
			u.created_at, u.last_login_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, hashToken(token), Now()))
}

// DeleteSession logs a session out.
func (db *DB) DeleteSession(ctx context.Context, token string) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// DeleteUserSessions logs a user out everywhere.
func (db *DB) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// DeleteUserSessionsExcept logs a user out everywhere but the session with
// keepToken; an empty keepToken keeps none.
func (db *DB) DeleteUserSessionsExcept(ctx context.Context, userID int64, keepToken string) error {
	if keepToken == "" {
		return db.DeleteUserSessions(ctx, userID)
	}
	_, err := db.W.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, hashToken(keepToken))
	return err
}
