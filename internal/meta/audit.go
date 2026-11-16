package meta

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// AuditEntry is one audit log row.
type AuditEntry struct {
	ID        int64  `json:"id"`
	At        int64  `json:"at"`
	Actor     string `json:"actor"`
	ActorKind string `json:"actorKind"`
	Action    string `json:"action"`
	Project   string `json:"project"`
	Resource  string `json:"resource"`
	Detail    string `json:"detail"`
	IP        string `json:"ip"`
}

// AddAudit appends an entry.
func (db *DB) AddAudit(ctx context.Context, e AuditEntry) error {
	_, err := db.W.ExecContext(ctx, `
		INSERT INTO audit_log(at, actor, actor_kind, action, project, resource, detail, ip) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		e.At, e.Actor, e.ActorKind, e.Action, e.Project, e.Resource, e.Detail, e.IP)
	return err
}

// AuditQuery filters the audit log. Empty fields match everything.
type AuditQuery struct {
	Projects []string // restrict to these projects (nil: all)
	Project  string
	Actor    string
	Action   string
	Search   string // substring of the resource
	Before   int64  // id cursor for paging
	Limit    int
}

// Audit returns entries newest first.
func (db *DB) Audit(ctx context.Context, q AuditQuery) ([]AuditEntry, error) {
	where := []string{"1 = 1"}
	var args []any
	if q.Projects != nil {
		if len(q.Projects) == 0 {
			return []AuditEntry{}, nil
		}
		where = append(where, `project IN (?`+strings.Repeat(`, ?`, len(q.Projects)-1)+`)`)
		for _, p := range q.Projects {
			args = append(args, p)
		}
	}
	if q.Project != "" {
		where = append(where, `project = ?`)
		args = append(args, q.Project)
	}
	if q.Actor != "" {
		where = append(where, `actor = ?`)
		args = append(args, q.Actor)
	}
	if q.Action != "" {
		where = append(where, `action = ?`)
		args = append(args, q.Action)
	}
	if q.Search != "" {
		where = append(where, `instr(resource, ?) > 0`)
		args = append(args, q.Search)
	}
	if q.Before > 0 {
		where = append(where, `id < ?`)
		args = append(args, q.Before)
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	args = append(args, q.Limit)
	rows, err := db.R.QueryContext(ctx, `
		SELECT id, at, actor, actor_kind, action, project, resource, detail, ip FROM audit_log
		WHERE `+strings.Join(where, " AND ")+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.ActorKind, &e.Action, &e.Project, &e.Resource, &e.Detail, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneAudit deletes entries older than cutoff.
func (db *DB) PruneAudit(ctx context.Context, cutoff int64) (int64, error) {
	res, err := db.W.ExecContext(ctx, `DELETE FROM audit_log WHERE at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Webhook is an HTTP endpoint notified about events.
type Webhook struct {
	ID        int64    `json:"id"`
	ProjectID int64    `json:"projectId,omitempty"`
	Project   string   `json:"project,omitempty"` // empty: every project
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Secret    string   `json:"-"`
	HasSecret bool     `json:"hasSecret"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
	CreatedAt int64    `json:"createdAt"`
	// Last delivery outcome, for the list view.
	LastAt     int64  `json:"lastAt"`
	LastStatus int    `json:"lastStatus"`
	LastError  string `json:"lastError"`
}

const webhookCols = `w.id, coalesce(w.project_id, 0), coalesce(p.name, ''), w.name, w.url, w.secret, w.events, w.enabled, w.created_at,
	coalesce(d.at, 0), coalesce(d.status_code, 0), coalesce(d.error, '')`

const webhookFrom = ` FROM webhooks w LEFT JOIN projects p ON p.id = w.project_id
	LEFT JOIN webhook_deliveries d ON d.id = (SELECT max(id) FROM webhook_deliveries WHERE webhook_id = w.id)`

func scanWebhook(s scanner) (Webhook, error) {
	var w Webhook
	var events string
	err := s.Scan(&w.ID, &w.ProjectID, &w.Project, &w.Name, &w.URL, &w.Secret, &events, &w.Enabled, &w.CreatedAt,
		&w.LastAt, &w.LastStatus, &w.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	_ = json.Unmarshal([]byte(events), &w.Events)
	if w.Events == nil {
		w.Events = []string{}
	}
	w.HasSecret = w.Secret != ""
	return w, err
}

// Webhooks lists hooks; project "" lists all, "-" only system-wide hooks.
func (db *DB) Webhooks(ctx context.Context, project string) ([]Webhook, error) {
	q := `SELECT ` + webhookCols + webhookFrom
	var args []any
	switch project {
	case "":
	case "-":
		q += ` WHERE w.project_id IS NULL`
	default:
		q += ` WHERE p.name = ?`
		args = append(args, project)
	}
	return db.queryWebhooks(ctx, q+` ORDER BY w.name`, args...)
}

func (db *DB) queryWebhooks(ctx context.Context, q string, args ...any) ([]Webhook, error) {
	rows, err := db.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WebhookByID looks a hook up.
func (db *DB) WebhookByID(ctx context.Context, id int64) (Webhook, error) {
	return scanWebhook(db.R.QueryRowContext(ctx, `SELECT `+webhookCols+webhookFrom+` WHERE w.id = ?`, id))
}

// WebhooksFor returns the enabled hooks subscribed to event in project
// (system-wide hooks included).
func (db *DB) WebhooksFor(ctx context.Context, project, event string) ([]Webhook, error) {
	all, err := db.queryWebhooks(ctx, `SELECT `+webhookCols+webhookFrom+
		` WHERE w.enabled = 1 AND (w.project_id IS NULL OR p.name = ?)`, project)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, w := range all {
		for _, e := range w.Events {
			if e == event {
				out = append(out, w)
				break
			}
		}
	}
	return out, nil
}

// SaveWebhook inserts (ID 0) or updates a hook. An empty Secret on update
// keeps the existing one.
func (db *DB) SaveWebhook(ctx context.Context, w Webhook) (int64, error) {
	events, _ := json.Marshal(w.Events)
	if w.ID == 0 {
		var pid any
		if w.ProjectID > 0 {
			pid = w.ProjectID
		}
		var id int64
		err := db.W.QueryRowContext(ctx, `
			INSERT INTO webhooks(project_id, name, url, secret, events, enabled, created_at) VALUES(?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			pid, w.Name, w.URL, w.Secret, string(events), w.Enabled, Now()).Scan(&id)
		return id, err
	}
	q := `UPDATE webhooks SET name = ?, url = ?, events = ?, enabled = ?`
	args := []any{w.Name, w.URL, string(events), w.Enabled}
	if w.Secret != "" {
		q += `, secret = ?`
		args = append(args, w.Secret)
	}
	res, err := db.W.ExecContext(ctx, q+` WHERE id = ?`, append(args, w.ID)...)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	return w.ID, nil
}

// DeleteWebhook removes a hook and its deliveries.
func (db *DB) DeleteWebhook(ctx context.Context, id int64) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

// Delivery is one webhook attempt.
type Delivery struct {
	ID         int64  `json:"id"`
	WebhookID  int64  `json:"webhookId"`
	Event      string `json:"event"`
	At         int64  `json:"at"`
	Attempt    int    `json:"attempt"`
	StatusCode int    `json:"statusCode"`
	Error      string `json:"error"`
	DurationMs int64  `json:"durationMs"`
	Payload    string `json:"payload"`
}

const keepDeliveries = 100

// AddDelivery records an attempt and keeps only the newest per hook.
func (db *DB) AddDelivery(ctx context.Context, d Delivery) error {
	if _, err := db.W.ExecContext(ctx, `
		INSERT INTO webhook_deliveries(webhook_id, event, at, attempt, status_code, error, duration_ms, payload)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, d.WebhookID, d.Event, d.At, d.Attempt, d.StatusCode, d.Error, d.DurationMs, d.Payload); err != nil {
		return err
	}
	_, err := db.W.ExecContext(ctx, `
		DELETE FROM webhook_deliveries WHERE webhook_id = ? AND id NOT IN (
			SELECT id FROM webhook_deliveries WHERE webhook_id = ? ORDER BY id DESC LIMIT ?)`,
		d.WebhookID, d.WebhookID, keepDeliveries)
	return err
}

// Deliveries lists recent attempts of a hook.
func (db *DB) Deliveries(ctx context.Context, webhookID int64) ([]Delivery, error) {
	rows, err := db.R.QueryContext(ctx, `
		SELECT id, webhook_id, event, at, attempt, status_code, error, duration_ms, payload
		FROM webhook_deliveries WHERE webhook_id = ? ORDER BY id DESC LIMIT ?`, webhookID, keepDeliveries)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &d.At, &d.Attempt, &d.StatusCode, &d.Error, &d.DurationMs, &d.Payload); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
