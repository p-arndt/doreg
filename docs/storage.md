# Storage

doreg keeps two kinds of data: blobs (layers, configs, manifests) and metadata (projects, users, tags,
audit log). Each has two backends; the variables are listed in [configuration](configuration.md).

| | Default | Alternative |
|---|---|---|
| Blobs | files in `DOREG_DATA` | any S3-compatible bucket (`S3_BUCKET`) |
| Metadata | SQLite, `doreg.db` in `DOREG_DATA` | PostgreSQL (`DATABASE_URL`) |

## Local disk

Everything lives in the data directory (`DOREG_DATA`, `/data` in the image). Back up the directory and
you've backed up the registry; stop doreg or take a filesystem snapshot so SQLite is consistent.

## S3

With `S3_BUCKET` set, blobs go to the bucket, which is created on startup if missing. Upload sessions
stay on local disk, so keep the data volume either way.

- AWS: `S3_ENDPOINT=https://s3.<region>.amazonaws.com`, `S3_REGION=<region>`, `S3_FORCE_PATH_STYLE=false`.
- MinIO, RustFS, minibucket: `S3_ENDPOINT=http://host:9000`, `S3_FORCE_PATH_STYLE=true`.

[`deploy/compose.s3.yaml`](../deploy/compose.s3.yaml) runs doreg with a bundled RustFS.

## PostgreSQL

Point `DATABASE_URL` at PostgreSQL, for example
`postgres://doreg:secret@db:5432/doreg?sslmode=disable`. The schema is created and migrated on start.

- Run one doreg per database: digest locks and the GC scheduler live in the process.
- There is no migration between SQLite and PostgreSQL; pick one when setting up.

[`deploy/compose.postgres.yaml`](../deploy/compose.postgres.yaml) runs doreg with a bundled PostgreSQL.
