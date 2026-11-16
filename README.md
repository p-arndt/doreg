# doreg

A self-hosted container registry with a web UI that is pleasant to use and garbage collection that runs online, without read-only mode or downtime.

- OCI distribution spec 1.1: passes the official conformance suite (pull, push, content discovery, content management), works with docker, buildx (multi-arch, SBOM/provenance attestations), containerd, podman, crane, oras.
- Projects with per-project roles (read, write, admin), members from local users or OIDC groups, public projects, and storage quotas.
- Robot accounts for CI: pull by default, optional push and delete, expiry, disable and secret rotation.
- Single sign-on with any OpenID Connect provider, with an optional admin group.
- Audit log and signed webhooks for pushes, pulls, deletes, quota hits and GC runs.
- Online garbage collection: removes untagged manifests and unreferenced layers while pushes and pulls continue, safe against concurrent pushes. Every run can be previewed first (same code path, rolled back).
- Retention policies: keep the newest N tags, delete tags older than X days, per repository glob, with tag filters and protected tags, and a live preview of what a policy would delete.
- Digest-pinned deployments stay safe: untagged manifests that were pulled recently are kept.
- Web UI: projects, repositories, tags with platforms and sizes, image layers, config, environment, labels, build history, attestations and referrers, copyable pull commands, deletes, GC history, robots, webhooks, audit log, users.
- One static binary (or a 22 MB distroless image), SQLite metadata plus content-addressed blobs on local disk or in any S3 bucket.

## Install

```sh
docker run -d --name doreg -p 8080:8080 -v doreg-data:/data \
  -e DOREG_ADMIN_PASSWORD='choose-a-long-password' ghcr.io/p-arndt/doreg:latest
```

Run it behind a TLS-terminating proxy (Traefik, Caddy, nginx); docker only sends credentials over HTTPS (except to `localhost`). See `compose.prod.yaml`. Raise the proxy's request read timeout: large layers take longer than 60 seconds.

## Quick start

```sh
docker login reg.example.com
docker tag my-app reg.example.com/my-app:1.0
docker push reg.example.com/my-app:1.0
```

Open `https://reg.example.com` for the UI.

System users have one of two roles: `admin` (everything) or `user` (access only through project membership). Project access is described below.

```sh
echo 'secret-password' | doreg user add --role user ci
doreg user list
```

Garbage collection runs every 24 hours by default (configurable in the UI). From the CLI:

```sh
doreg gc --dry-run --retention
```

## Projects and permissions

A repository belongs to the project named by its first path segment: `team/api` is in project `team`, and names without a slash are in `library`.

| Role    | Pull | Push, delete tags and manifests | Members, robots, webhooks, repository settings |
|---------|------|---------------------------------|------------------------------------------------|
| `read`  | yes  |                                 |                                                |
| `write` | yes  | yes                             |                                                |
| `admin` | yes  | yes                             | yes                                            |

- Members are local users or OIDC groups. A user gets the highest role they hold directly or through a group.
- Public projects allow anonymous pull of their repositories.
- Each project can have a storage quota in bytes (0 means no quota). Pushes that would exceed it are rejected and emit a `quota_exceeded` event. Only system admins set quotas.
- System admins can push to any project; a push to a new project creates it. Other users need an existing project and a membership with the right role.
- Accounts that had the old global `read` or `write` role became `user` during the upgrade; give them project memberships to restore access.

## Robot accounts

Robots are non-human accounts for CI and deployments. Their names are `robot$<project>+<name>` for project robots and `robot$<name>` for system robots. Names use lowercase letters, digits and the separators `.`, `_` and `-`.

- A project robot works only in its project. A system robot works in every project and can only be created by system admins.
- Pull is always allowed. Push and delete are separate options.
- Optional expiry; a robot can be disabled without being deleted.
- The secret is shown once, when the robot is created or its secret is rotated. Rotating invalidates the old secret.

Quote the `$` in shells:

```sh
echo "$ROBOT_SECRET" | docker login reg.example.com -u 'robot$team+ci' --password-stdin
```

## Single sign-on (OIDC)

doreg logs users in through an OpenID Connect provider (authorization code flow with PKCE). It is enabled when `OIDC_ISSUER` is set.

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

### Who becomes admin

1. If `OIDC_ADMIN_ROLE` or `OIDC_ADMIN_EMAILS` is set, they decide at every SSO login: users matching either become admins, everyone else is a normal user. Removing the role at the provider (or the email from the list) demotes at the next login.
2. If neither is set, roles are managed in doreg (Users page). While the instance has no admin at all, the first person who signs in via SSO becomes admin; doreg logs a warning and an audit entry. Only accounts of your identity provider can sign in, so set one of the variables before opening a provider to the public.

`DOREG_ADMIN_PASSWORD` is optional with SSO. Keep a local admin only as a fallback for when the provider is down.

Setup, with any provider (Authentik, Keycloak, and others alike):

1. Create a confidential OpenID client. Set its redirect URI to `OIDC_REDIRECT_URI`.
2. Make sure the ID token carries the groups or roles (see `OIDC_GROUPS_CLAIM`). Many providers need an explicit groups scope or mapper for this.
3. Set the variables above on the doreg container and restart it. The login button appears on the sign-in page.

Notes:

- The first SSO login creates the account. If the name is taken by a local account, the SSO user gets a suffix (`alice-2`). Local accounts are never taken over.
- SSO accounts have no password. To use `docker login`, create a CLI secret on the account page and use it as the password. A new CLI secret replaces the old one.
- Groups can also be project members: add an OIDC group to a project with the role you want.

```sh
docker login reg.example.com -u alice   # password: the CLI secret from the account page
```

### Command-line login

With SSO, `doreg login` does the same without copying a secret:

```sh
doreg login reg.example.com
```

It prints a code and a link, opens the browser (`--no-browser` only prints the link), and waits until you approve the code on the account page. Then it runs `docker login` with a new token. The token is valid for 90 days and can be revoked on the account page under Command-line logins.

- `--docker-host HOST` is the host passed to `docker login`, if it differs from the registry you log in to.
- `--print` writes the username and token to stdout instead of running docker, for CI and other tools.
- Machines without a browser can use the CLI secret from the account page, as shown above.

Development setup: the UI and SSO run on Vite at localhost:5173, while pushes go through the proxy on port 5050:

```sh
doreg login --docker-host localhost:5050 localhost:5173
```

## Storage

Blobs go to the data directory (`DOREG_DATA`, default `/data` in the image) unless S3 is configured. Set `S3_BUCKET` to store blobs in an S3 bucket instead. The bucket is created on startup if it is missing.

| Variable | Default | Meaning |
|----------|---------|---------|
| `S3_BUCKET` | | Bucket for blobs. Setting it enables S3 storage. |
| `S3_ENDPOINT` | | Endpoint URL for MinIO or minibucket, e.g. `http://localhost:9000`. For AWS use `https://s3.<region>.amazonaws.com`. |
| `S3_REGION` | `us-east-1` | Region. |
| `S3_ACCESS_KEY_ID` | | Access key. |
| `S3_SECRET_ACCESS_KEY` | | Secret key. |
| `S3_FORCE_PATH_STYLE` | | Path-style addressing: `true` for MinIO and minibucket, `false` for AWS. |
| `S3_PREFIX` | empty | Optional key prefix inside the bucket. |

Upload sessions stay on local disk in the data directory, so the data volume is still needed with S3.

## Webhooks

Project admins add webhooks to their project. System admins add system-wide webhooks, which receive events from every project. Each webhook has a URL, an optional secret and a list of events: `push`, `pull`, `delete_tag`, `delete_manifest`, `delete_repository`, `quota_exceeded`, `gc_completed`. The test button sends a `ping` event and the delivery list shows the outcome of each attempt.

Deliveries are `POST` requests with a JSON body and these headers:

- `X-Doreg-Event`: the event type.
- `X-Doreg-Attempt`: 1 for the first try, then 2 and 3.
- `X-Doreg-Signature-256`: `sha256=<hex>`, the HMAC-SHA256 of the body with the webhook secret. Sent only when a secret is set.

A delivery fails on a network error or any status other than 2xx. It is retried after 5 and 30 seconds, then dropped. Redirects are not followed.

Webhook URLs that resolve to loopback, private, link-local or CGNAT addresses are refused, so project admins cannot reach internal services. Set `DOREG_WEBHOOK_ALLOW_PRIVATE=true` if your receivers live on the internal network.

Example body for a push (`occurredAt` is a Unix timestamp; `operatorKind` is `user`, `robot` or `anonymous`):

```json
{
  "type": "push",
  "occurredAt": 1763280000,
  "operator": "ci",
  "operatorKind": "robot",
  "project": "team",
  "repository": "team/api",
  "tag": "1.0",
  "digest": "sha256:9f2c4e7b1a...",
  "resourceUrl": "reg.example.com/team/api:1.0"
}
```

Verify the signature in Go, over the raw body:

```go
body, _ := io.ReadAll(r.Body)
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(body)
want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
valid := hmac.Equal([]byte(want), []byte(r.Header.Get("X-Doreg-Signature-256")))
```

Every change is also recorded in the audit log in the UI. Administrators see all entries; project admins see entries of their projects.

### Migrating from registry:2

Copy images over the API, e.g. with [regclient](https://github.com/regclient/regclient):

```sh
regctl repo ls old.example.com | while read repo; do
  regctl image copy --all-tags old.example.com/$repo new.example.com/$repo
done
```

### How GC stays safe online

Mark and sweep of metadata run in one SQLite write transaction; a manifest push checks its references in its own write transaction, so the two are serialised. Content younger than the grace period (default 60 minutes) is never collected, which covers clients between uploading layers and pushing the manifest. Files are deleted under a per-digest lock after their database row is gone, so a concurrent upload of the same layer never loses its file.

## Development

```sh
cp .env.example .env
docker compose up -d   # minisuite (OIDC, S3, mail) and the push proxy
just dev               # API on localhost:8080 plus the UI with hot reload on localhost:5173
```

Open http://localhost:5173: Vite serves the UI with hot reload and proxies `/api` and `/v2` to the API, and the dev SSO login returns there too. `just dev-api` runs only the API; `just web-build` embeds the UI into the binary.

`compose.yaml` holds only the dependencies, [minisuite](https://github.com/p-arndt/minisuite); doreg runs on the host. Sign in with SSO as `alice` (admin) or `bob` (normal user); the passwords are the same as the usernames. The overview page at localhost:9900 lists all URLs.

Docker Desktop's daemon cannot reach a process on the host's localhost, so push and pull through the proxy on port 5050:

```sh
docker login localhost:5050 -u alice   # password: CLI secret from the account page
docker push localhost:5050/team/api:dev
```

`go run ./cmd/doreg login ...` runs the CLI without installing it, for example `go run ./cmd/doreg login --docker-host localhost:5050 localhost:5173`.

```sh
just          # list recipes
just ci       # gofmt, vet, tests, UI check, lint and tests
just release  # cut a release (stamp)
```

## License

MIT
