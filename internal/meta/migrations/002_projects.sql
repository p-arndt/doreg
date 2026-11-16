-- Projects group repositories by the first path segment ("team/api" belongs to
-- "team"; names without a slash belong to "library").
CREATE TABLE projects (
	id          INTEGER PRIMARY KEY,
	name        TEXT NOT NULL UNIQUE,
	public      INTEGER NOT NULL DEFAULT 0,
	quota_bytes INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL
);

-- A member is either a user or an OIDC group (user_id NULL, group_name set).
CREATE TABLE project_members (
	id         INTEGER PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
	group_name TEXT NOT NULL DEFAULT '',
	role       TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX project_members_user ON project_members(project_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX project_members_group ON project_members(project_id, group_name) WHERE group_name != '';

ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN oidc_subject TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN groups TEXT NOT NULL DEFAULT '[]';
ALTER TABLE users ADD COLUMN cli_secret_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN last_login_at INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX users_oidc_subject ON users(oidc_subject) WHERE oidc_subject != '';
-- Global read/write roles are replaced by per-project membership.
UPDATE users SET role = 'user' WHERE role IN ('read', 'write');

CREATE TABLE robots (
	id           INTEGER PRIMARY KEY,
	name         TEXT NOT NULL UNIQUE,
	description  TEXT NOT NULL DEFAULT '',
	project_id   INTEGER REFERENCES projects(id) ON DELETE CASCADE,
	secret_hash  TEXT NOT NULL,
	can_push     INTEGER NOT NULL DEFAULT 0,
	can_delete   INTEGER NOT NULL DEFAULT 0,
	expires_at   INTEGER NOT NULL DEFAULT 0,
	disabled     INTEGER NOT NULL DEFAULT 0,
	created_by   TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audit_log (
	id         INTEGER PRIMARY KEY,
	at         INTEGER NOT NULL,
	actor      TEXT NOT NULL,
	actor_kind TEXT NOT NULL,
	action     TEXT NOT NULL,
	project    TEXT NOT NULL DEFAULT '',
	resource   TEXT NOT NULL DEFAULT '',
	detail     TEXT NOT NULL DEFAULT '',
	ip         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_at ON audit_log(at);
CREATE INDEX audit_log_project ON audit_log(project, id);

-- project_id NULL = system-wide hook for every project.
CREATE TABLE webhooks (
	id         INTEGER PRIMARY KEY,
	project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	url        TEXT NOT NULL,
	secret     TEXT NOT NULL DEFAULT '',
	events     TEXT NOT NULL DEFAULT '[]',
	enabled    INTEGER NOT NULL DEFAULT 1,
	created_at INTEGER NOT NULL
);

CREATE TABLE webhook_deliveries (
	id          INTEGER PRIMARY KEY,
	webhook_id  INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
	event       TEXT NOT NULL,
	at          INTEGER NOT NULL,
	attempt     INTEGER NOT NULL,
	status_code INTEGER NOT NULL DEFAULT 0,
	error       TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	payload     TEXT NOT NULL
);
CREATE INDEX webhook_deliveries_hook ON webhook_deliveries(webhook_id, id);

-- Existing repositories get their projects.
INSERT INTO projects(name, public, created_at)
SELECT DISTINCT
	CASE WHEN instr(name, '/') > 0 THEN substr(name, 1, instr(name, '/') - 1) ELSE 'library' END,
	0,
	CAST(strftime('%s', 'now') AS INTEGER)
FROM repositories WHERE true
ON CONFLICT(name) DO NOTHING;
