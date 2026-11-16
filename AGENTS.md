# doreg

Self-hosted OCI container registry: Go server (`/v2/` registry API, `/api/` JSON API for the UI) with an embedded SvelteKit SPA and online garbage collection. Replaces distribution/registry:2 plus a separate registry UI.

## Layout

- `cmd/doreg` — CLI: `serve`, `user`, `gc`, `login` (device-code SSO login, then `docker login`; the HTTP part is tested against an httptest server), `import` (copy from another registry into a running doreg over HTTP), `version`
- `internal/registry` — OCI distribution API (uploads, manifests, tags, referrers, auth). Integration and GC tests live here (`*_test.go`), including a push-during-GC stress test.
- `internal/importer` — copies repositories from another OCI registry (Basic and Bearer token auth). The server-side import (`/api/import`, UI page `/import`) pushes through `registry.Handler.Transport`, i.e. the normal `/v2/` path in-process, so permissions, quotas, digest locks and events apply unchanged; imports emit with `events.WithoutWebhooks`.
- `internal/gc` — mark and sweep, retention policies, scheduler
- `internal/auth` — `Authenticator`: resolves Basic credentials (robot secret, user password or CLI secret) to a principal, caches them, and decides permissions in `Can`
- `internal/events` — `Bus`: `Emit` writes the audit log and queues webhook deliveries (signed, retried); `Audit` records admin actions that are not webhook events
- `internal/oidc` — single sign-on (OpenID Connect, PKCE); configuration from `OIDC_*`
- `compose.yaml` — dev dependencies only: minisuite (minicloak OIDC, minibucket S3, overview) and a socat push proxy on :5050; doreg itself runs on the host via `just dev` (loads `.env`). `compose.prod.yaml` is the production example. Env names follow the shared `OIDC_*` / `S3_*` convention.
- `internal/meta` — SQLite metadata, one writer connection, read pool. Schema lives in `migrations/NNN_name.sql`, applied in order and tracked with `PRAGMA user_version`; each file runs in its own transaction. Projects, robots, webhooks and audit log are here too.
- `internal/meta/migrations` — schema history. A schema change is a new numbered file; never edit a file that has been applied.
- `internal/storage` — content-addressed blob files, uploads, per-digest locks
- `internal/api` — UI JSON API (session cookie auth): projects, members, robots, webhooks, audit log, auth and SSO login, GC, users
- `internal/webui` — embeds `internal/webui/dist`, built from `web/` by `just web-build`
- `web/` — SvelteKit (adapter-static SPA, Svelte 5 runes); `#lib/...` imports need the `.js` extension for TS modules

## Invariants

- Permissions are decided in `auth.Authenticator.Can` (principal, project, action). Handlers and the registry call it; never compare roles or check membership inline.
- Every content change (push, pull, tag or manifest delete, repository delete, quota exceeded, GC completed) emits an event through `events.Bus.Emit`, which feeds both the audit log and webhooks. Admin actions use `Bus.Audit`.
- A `blobs` row implies the file exists. Upload finalisation and GC blob deletion both hold `storage.FS.Lock(digest)`; never hold the SQLite writer while waiting for a digest lock.
- Manifest bytes are stored verbatim and served as pushed.
- GC mark and sweep run in one write transaction; dry runs execute the same statements and roll back.
- Robot and CLI secrets are stored only as bcrypt hashes and shown once. Any change to a robot, secret or role calls `Authenticator.Invalidate()`, because verified credentials are cached for 5 minutes.

## Checks

`just ci` (gofmt, vet, Go tests, UI check/lint/tests). The OCI conformance suite runs against a container: see the image `ghcr.io/opencontainers/distribution-spec/conformance:v1.1.0` with `OCI_ROOT_URL`, `OCI_NAMESPACE`, `OCI_USERNAME`/`OCI_PASSWORD` and the `OCI_TEST_*` flags.

`just release` owns versions (stamp); never edit `VERSION` by hand.
