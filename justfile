# doreg — task runner. Shared recipes live in .just/ (from p-arndt/just-common):
# edit them there and run `just sync-common`. This file holds only doreg's own.

import '.just/common.just'
import '.just/go.just'
import '.just/docker.just'
import '.just/release.just'

# OIDC_* and S3_* for `just dev` come from .env (see .env.example).
set dotenv-load

set allow-duplicate-variables
set allow-duplicate-recipes

BIN_NAME := "doreg"
BUILDINFO_PKG := "github.com/p-arndt/doreg/internal/buildinfo"
MAIN := "./cmd/doreg"
IMAGE := "doreg"

# Install web UI dependencies.
web-install:
    pnpm --dir web install --frozen-lockfile

# Build the web UI into internal/webui/dist (embedded into the binary).
web-build:
    pnpm --dir web build

# Run the UI dev server (proxies /api and /v2 to a local doreg on :8080).
web-dev:
    pnpm --dir web dev

# Type-check, lint and test the web UI.
web-check:
    pnpm --dir web check
    pnpm --dir web lint
    pnpm --dir web test

# Run the doreg API on :8080 with data in ./data (no UI build; `just dev` pairs it with Vite).
dev-api:
    go run {{MAIN}} serve --data ./data --addr :8080

# Develop: API on :8080 plus the UI with hot reload on :5173. Open localhost:5173. Deps: `docker compose up -d`.
[parallel]
dev: dev-api web-dev

# Build a release binary with the UI embedded.
build-release: web-build
    go build -trimpath -ldflags "{{_LDFLAGS}}" -o {{_BIN}} {{MAIN}}

# Run the Go tests against PostgreSQL (needs `docker compose up -d postgres`).
test-postgres:
    DOREG_TEST_DATABASE_URL="postgres://doreg:doreg@localhost:5432/doreg?sslmode=disable" go test ./...

# Every check CI runs.
ci: fmt-check vet test web-check
