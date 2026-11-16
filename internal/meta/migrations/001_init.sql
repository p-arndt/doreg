CREATE TABLE IF NOT EXISTS repositories (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL UNIQUE,
	public     INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

-- Invariant: a row here implies the blob file exists on disk. A crash can only
-- leave files without rows, which GC removes.
CREATE TABLE IF NOT EXISTS blobs (
	digest     TEXT PRIMARY KEY,
	size       INTEGER NOT NULL,
	created_at INTEGER NOT NULL
);

-- Which repositories may serve a blob. touched_at protects a link from GC while
-- a client is between "blob exists" and the manifest PUT that references it.
CREATE TABLE IF NOT EXISTS repo_blobs (
	repo_id    INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	digest     TEXT NOT NULL REFERENCES blobs(digest),
	touched_at INTEGER NOT NULL,
	PRIMARY KEY (repo_id, digest)
);
CREATE INDEX IF NOT EXISTS repo_blobs_digest ON repo_blobs(digest);

-- Manifest bytes are stored verbatim; re-marshalling would change the digest.
CREATE TABLE IF NOT EXISTS manifests (
	id             INTEGER PRIMARY KEY,
	repo_id        INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	digest         TEXT NOT NULL,
	media_type     TEXT NOT NULL,
	artifact_type  TEXT NOT NULL DEFAULT '',
	content        BLOB NOT NULL,
	subject        TEXT NOT NULL DEFAULT '',
	created_at     INTEGER NOT NULL,
	last_pulled_at INTEGER NOT NULL DEFAULT 0,
	UNIQUE (repo_id, digest)
);
CREATE INDEX IF NOT EXISTS manifests_subject ON manifests(repo_id, subject) WHERE subject != '';

-- Descriptors a manifest points to: kind 'blob' (config, layers) or 'manifest'
-- (index children).
CREATE TABLE IF NOT EXISTS manifest_refs (
	manifest_id INTEGER NOT NULL REFERENCES manifests(id) ON DELETE CASCADE,
	kind        TEXT NOT NULL,
	digest      TEXT NOT NULL,
	size        INTEGER NOT NULL,
	media_type  TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (manifest_id, kind, digest)
);
CREATE INDEX IF NOT EXISTS manifest_refs_digest ON manifest_refs(digest);

CREATE TABLE IF NOT EXISTS tags (
	repo_id     INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	name        TEXT NOT NULL,
	manifest_id INTEGER NOT NULL REFERENCES manifests(id) ON DELETE CASCADE,
	updated_at  INTEGER NOT NULL,
	PRIMARY KEY (repo_id, name)
);
CREATE INDEX IF NOT EXISTS tags_manifest ON tags(manifest_id);

CREATE TABLE IF NOT EXISTS uploads (
	id         TEXT PRIMARY KEY,
	repo_id    INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	started_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY,
	username      TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role          TEXT NOT NULL,
	created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS retention_policies (
	id            INTEGER PRIMARY KEY,
	name          TEXT NOT NULL,
	repo_pattern  TEXT NOT NULL,
	tag_pattern   TEXT NOT NULL DEFAULT '',
	keep_pattern  TEXT NOT NULL DEFAULT '',
	keep_last     INTEGER NOT NULL DEFAULT 0,
	older_than_days INTEGER NOT NULL DEFAULT 0,
	enabled       INTEGER NOT NULL DEFAULT 1,
	created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS gc_runs (
	id                INTEGER PRIMARY KEY,
	trigger           TEXT NOT NULL,
	dry_run           INTEGER NOT NULL,
	started_at        INTEGER NOT NULL,
	finished_at       INTEGER NOT NULL DEFAULT 0,
	status            TEXT NOT NULL,
	deleted_tags      INTEGER NOT NULL DEFAULT 0,
	deleted_manifests INTEGER NOT NULL DEFAULT 0,
	deleted_blobs     INTEGER NOT NULL DEFAULT 0,
	freed_bytes       INTEGER NOT NULL DEFAULT 0,
	error             TEXT NOT NULL DEFAULT '',
	report            TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
