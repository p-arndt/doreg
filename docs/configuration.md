# Configuration

doreg is configured through environment variables only. Everything else (projects, members, robots,
webhooks, GC interval, retention policies) is set in the web UI and stored in the database.

A minimal production setup needs `DOREG_ADMIN_PASSWORD` (or SSO) and a TLS-terminating
[reverse proxy](reverse-proxy.md). Everything below is optional.

## Server

| Variable | Default | Meaning |
|----------|---------|---------|
| `DOREG_ADDR` | `:8080` | Listen address. Also `--addr`. |
| `DOREG_DATA` | `./data` (`/data` in the image) | Data directory: SQLite metadata, upload sessions and, without S3, the blobs. Also `--data`. |
| `DOREG_ADMIN_USER` | `admin` | With `DOREG_ADMIN_PASSWORD`: the admin created on start if no users exist. |
| `DOREG_ADMIN_PASSWORD` | | Password of that admin. Optional with SSO, see [who becomes admin](access.md#who-becomes-admin). |
| `DOREG_IMMUTABLE_TAGS` | `false` | Make tags immutable in every project, whatever the project setting says. See [immutable tags](access.md#immutable-tags). |
| `DOREG_MUTABLE_TAGS` | | With `DOREG_IMMUTABLE_TAGS`: comma-separated tag patterns that stay overwritable everywhere, e.g. `latest,nightly-*`. |
| `DOREG_PUBLIC_BROWSE` | `false` | Show public repositories in the web UI without signing in. See [public repositories](access.md#public-repositories). |
| `DOREG_WEBHOOK_ALLOW_PRIVATE` | `false` | Allow webhook URLs on private, loopback and link-local networks. See [webhooks](webhooks.md). |
| `DOREG_GITHUB_TOKEN` | | GitHub token for README imports: lifts the anonymous rate limit and reaches private repositories (read-only `contents` scope is enough). |

## Metadata database

| Variable | Default | Meaning |
|----------|---------|---------|
| `DATABASE_URL` | SQLite in `DOREG_DATA` | `postgres://…` URL: keep metadata in PostgreSQL instead of SQLite. See [storage](storage.md). |

## S3 blob storage

| Variable | Default | Meaning |
|----------|---------|---------|
| `S3_BUCKET` | | Bucket for blobs. Setting it enables S3 storage. |
| `S3_ENDPOINT` | | Endpoint URL for MinIO, RustFS or minibucket, e.g. `http://localhost:9000`. For AWS use `https://s3.<region>.amazonaws.com`. |
| `S3_REGION` | `us-east-1` | Region. |
| `S3_ACCESS_KEY_ID` | | Access key. |
| `S3_SECRET_ACCESS_KEY` | | Secret key. |
| `S3_FORCE_PATH_STYLE` | | Path-style addressing: `true` for MinIO, RustFS and minibucket, `false` for AWS. |
| `S3_PREFIX` | empty | Optional key prefix inside the bucket. |

## Single sign-on

| Variable | Default | Meaning |
|----------|---------|---------|
| `OIDC_ISSUER` | | Issuer URL, the base URL that serves `/.well-known/openid-configuration`. Setting it enables SSO. |
| `OIDC_CLIENT_ID` | | Client ID (required with issuer). |
| `OIDC_CLIENT_SECRET` | | Client secret (required with issuer). |
| `OIDC_REDIRECT_URI` | | `<public url>/api/auth/oidc/callback`, e.g. `https://reg.example.com/api/auth/oidc/callback` (required with issuer). |
| `OIDC_PROVIDER_NAME` | `Single sign-on` | Label of the login button. |
| `OIDC_SCOPES` | `openid,profile,email` | Scopes, comma- or space-separated. |
| `OIDC_USERNAME_CLAIM` | `preferred_username` | Claim used as the doreg username. |
| `OIDC_GROUPS_CLAIM` | empty | Claim holding group names. Empty: the `groups`, `roles` and `realm_access.roles` claims are all read and merged. |
| `OIDC_ADMIN_ROLE` | empty | A group or role that makes its members system admins. |
| `OIDC_ADMIN_EMAILS` | empty | Comma-separated emails that make their owners system admins, only if the provider marks the email as verified. |

Setup is described in [access: single sign-on](access.md#single-sign-on).

## `doreg import`

Read only by the [`doreg import`](migrating.md#from-the-cli) command, not by the server.

| Variable | Meaning |
|----------|---------|
| `DOREG_FROM_PASSWORD` | Password of the source registry. |
| `DOREG_TO_PASSWORD` | Password or CLI secret of the doreg account that receives the images. |

## Settings in the web UI

| Setting | Where | Who |
|---------|-------|-----|
| Projects, members, public flag, immutable tags, quota | Projects | system admins; all but the quota also project admins |
| Public flag of a single repository | repository settings | project admins |
| Robot accounts | project page or Robots | project admins, system admins for system robots |
| Webhooks | project page or Webhooks | project admins, system admins for global webhooks |
| GC interval, grace period, retention policies | Garbage collection | system admins |
| Users and system roles | Users | system admins |
