# Garbage collection

GC removes untagged manifests and blobs nothing references any more, while pushes and pulls keep
running. It runs every 24 hours by default. Interval, grace period and retention policies live in the
UI (system admins), and every run can be previewed before it deletes anything. Or from the shell:

```sh
doreg gc --dry-run --retention
```

A preview runs the same statements as a real run and rolls them back.

## Retention policies

Policies untag images before the sweep: keep the newest N tags, drop tags older than X days, per
repository glob, with protected tags that are never removed. Recently pulled digests survive, so pinned
deployments stay safe. The policy preview shows which tags would go.

## How GC stays safe online

Mark and sweep of metadata run in one write transaction; a manifest push checks its references in its
own write transaction, and doreg funnels all writes through one database connection (on PostgreSQL too),
so the two are serialised. Content younger than the grace period (default 60 minutes) is never
collected, which covers clients between uploading layers and pushing the manifest. Files are deleted
under a per-digest lock after their database row is gone, so a concurrent upload of the same layer never
loses its file.
