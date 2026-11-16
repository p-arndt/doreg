package meta

import (
	"context"
	"database/sql"
	"errors"
)

// Device statuses.
const (
	DevicePending  = "pending"
	DeviceApproved = "approved"
	DeviceDenied   = "denied"
	DeviceExpired  = "expired"
)

// Device is a pending `doreg login`.
type Device struct {
	UserCode  string `json:"userCode"`
	Client    string `json:"client"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
	Status    string `json:"status"`
	UserID    int64  `json:"-"`
	Token     string `json:"-"`
}

const maxPendingDevices = 200

// ErrTooMany limits unauthenticated device requests.
var ErrTooMany = errors.New("too many pending logins")

// CreateDevice stores a new device request. deviceHash is the SHA-256 of the
// secret the CLI keeps.
func (db *DB) CreateDevice(ctx context.Context, deviceHash string, d Device) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		now := Now()
		if _, err := tx.ExecContext(ctx, `DELETE FROM cli_devices WHERE expires_at < ?`, now); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM cli_devices`).Scan(&n); err != nil {
			return err
		}
		if n >= maxPendingDevices {
			return ErrTooMany
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO cli_devices(device_code_hash, user_code, client, created_at, expires_at) VALUES(?, ?, ?, ?, ?)`,
			deviceHash, d.UserCode, d.Client, d.CreatedAt, d.ExpiresAt)
		return err
	})
}

func scanDevice(s scanner) (Device, error) {
	var d Device
	var uid sql.NullInt64
	err := s.Scan(&d.UserCode, &d.Client, &d.CreatedAt, &d.ExpiresAt, &d.Status, &uid, &d.Token)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	d.UserID = uid.Int64
	if d.Status == DevicePending && d.ExpiresAt < Now() {
		d.Status = DeviceExpired
	}
	return d, err
}

const deviceCols = `user_code, client, created_at, expires_at, status, user_id, token`

// DeviceByUserCode looks a request up by the code the user sees.
func (db *DB) DeviceByUserCode(ctx context.Context, code string) (Device, error) {
	return scanDevice(db.R.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM cli_devices WHERE user_code = ?`, code))
}

// DecideDevice approves (with the issued token) or denies a pending request.
func (db *DB) DecideDevice(ctx context.Context, code, status string, userID int64, token string) error {
	res, err := db.W.ExecContext(ctx, `
		UPDATE cli_devices SET status = ?, user_id = ?, token = ?
		WHERE user_code = ? AND status = 'pending' AND expires_at >= ?`, status, userID, token, code, Now())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TakeDevice returns the request for the CLI's secret and deletes it once it
// is decided, so the token is handed out exactly once.
func (db *DB) TakeDevice(ctx context.Context, deviceHash string) (Device, error) {
	var d Device
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		d, err = scanDevice(tx.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM cli_devices WHERE device_code_hash = ?`, deviceHash))
		if err != nil {
			return err
		}
		if d.Status == DevicePending {
			return nil
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM cli_devices WHERE device_code_hash = ?`, deviceHash)
		return err
	})
	return d, err
}

// CLIToken is a credential issued by `doreg login`.
type CLIToken struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"createdAt"`
	ExpiresAt  int64  `json:"expiresAt"`
	LastUsedAt int64  `json:"lastUsedAt"`
}

// CreateCLIToken stores a token by its hash.
func (db *DB) CreateCLIToken(ctx context.Context, userID int64, name, tokenHash string, expiresAt int64) error {
	_, err := db.W.ExecContext(ctx, `
		INSERT INTO cli_tokens(user_id, name, token_hash, created_at, expires_at) VALUES(?, ?, ?, ?, ?)`,
		userID, name, tokenHash, Now(), expiresAt)
	return err
}

// CLITokenByHash resolves a token to its row if it is not expired.
func (db *DB) CLITokenByHash(ctx context.Context, tokenHash string) (CLIToken, error) {
	var t CLIToken
	err := db.R.QueryRowContext(ctx, `
		SELECT id, user_id, name, created_at, expires_at, last_used_at FROM cli_tokens
		WHERE token_hash = ? AND expires_at > ?`, tokenHash, Now()).
		Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// TouchCLIToken records a use (throttled).
func (db *DB) TouchCLIToken(ctx context.Context, t CLIToken) {
	if now := Now(); now-t.LastUsedAt > touchInterval {
		_, _ = db.W.ExecContext(ctx, `UPDATE cli_tokens SET last_used_at = ? WHERE id = ?`, now, t.ID)
	}
}

// CLITokens lists a user's tokens, newest first, dropping expired ones.
func (db *DB) CLITokens(ctx context.Context, userID int64) ([]CLIToken, error) {
	if _, err := db.W.ExecContext(ctx, `DELETE FROM cli_tokens WHERE expires_at < ?`, Now()); err != nil {
		return nil, err
	}
	rows, err := db.R.QueryContext(ctx, `
		SELECT id, user_id, name, created_at, expires_at, last_used_at FROM cli_tokens
		WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CLIToken{}
	for rows.Next() {
		var t CLIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteCLIToken revokes one of the user's tokens.
func (db *DB) DeleteCLIToken(ctx context.Context, userID, id int64) error {
	res, err := db.W.ExecContext(ctx, `DELETE FROM cli_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
