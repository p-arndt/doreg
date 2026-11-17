# Migrating from another registry

doreg copies repositories from registry:2, Harbor, GHCR, Docker Hub or any other OCI registry over the
registry API and stores them like a push.

## From the web UI

Open **Import** in the UI (system admins only), enter the old registry's URL and, if it needs one, its
login, hit **Preview** to see what will be copied, then **Start import**.

- Manifests are copied byte for byte, so digests don't change and `image@sha256:...` references keep working. Multi-arch indexes come along with all their platforms.
- Logins: htpasswd (Basic) and token auth (`auth: token`, Docker Hub, GHCR) both work. The password is used for that one import and never stored. A self-signed certificate needs **Skip TLS certificate check**.
- Narrow it down with names or patterns like `team/*`. Registries without `/v2/_catalog` (Docker Hub) need plain repository names.
- Tags already up to date are skipped and shared layers are transferred once, so an interrupted import is simply started again.
- Pushes land in the audit log under the admin who started the import but fire **no webhooks**, so old images don't trigger deployments. Names without a slash land in the `library` project.
- Only tagged images are copied; untagged manifests and referrers without a tag (e.g. OCI 1.1 signatures) stay behind.

## From the CLI

The same works from any machine with the CLI, against a running doreg:

```sh
export DOREG_FROM_PASSWORD=...   # old registry
export DOREG_TO_PASSWORD=...     # doreg admin password or CLI secret
doreg import --from https://old.example.com:5000 --from-user migrate \
             --to https://registry.example.com --to-user admin --dry-run
```

Drop `--dry-run` to copy. `--repo 'team/*'` (repeatable) limits it, `--from-insecure` skips the TLS
check, and `--concurrency` sets how many tags are copied at once (default 4). Run with the doreg image
instead of a local binary:
`docker run --rm -e DOREG_FROM_PASSWORD -e DOREG_TO_PASSWORD ghcr.io/p-arndt/doreg import ...`.
