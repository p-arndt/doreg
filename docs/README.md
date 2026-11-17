# doreg documentation

New here? Start with the [quick start](../README.md#-quick-start) in the main README.

## Setting up

| | |
|---|---|
| [Configuration](configuration.md) | Every environment variable in one place, and which settings live in the UI. |
| [Reverse proxy](reverse-proxy.md) | Headers, body limits and timeouts, with Traefik, Caddy and nginx examples. |
| [Storage](storage.md) | Local disk or S3 for blobs, SQLite or PostgreSQL for metadata, backups. |
| [Migrating](migrating.md) | Import from registry:2, Harbor, GHCR or Docker Hub, in the UI or with `doreg import`. |

## Running it

| | |
|---|---|
| [Access](access.md) | Projects and roles, public repositories, robot accounts, single sign-on, `doreg login`. |
| [Webhooks and audit log](webhooks.md) | Events, payload, signature verification. |
| [Garbage collection](gc.md) | Schedule, retention policies, why it is safe while pushes run. |

## Deployment examples

- [`deploy/compose.yaml`](../deploy/compose.yaml): local disk, behind Traefik, optional SSO.
- [`deploy/compose.s3.yaml`](../deploy/compose.s3.yaml): blobs in a bundled RustFS.
- [`deploy/compose.postgres.yaml`](../deploy/compose.postgres.yaml): metadata in a bundled PostgreSQL.
