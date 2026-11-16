# doreg — task runner. Shared recipes live in .just/ (from p-arndt/just-common):
# edit them there and run `just sync-common`. This file holds only doreg's own.

import '.just/common.just'
import '.just/go.just'
import '.just/docker.just'
import '.just/release.just'

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

# Run the UI dev server (proxies /api and /v2 to a local doreg on :5000).
web-dev:
    pnpm --dir web dev

# Type-check, lint and test the web UI.
web-check:
    pnpm --dir web check
    pnpm --dir web lint
    pnpm --dir web test

# Run doreg locally with data in ./data.
dev: web-build
    go run {{MAIN}} serve --data ./data --addr :5000

# Build a release binary with the UI embedded.
build-release: web-build
    go build -trimpath -ldflags "{{_LDFLAGS}}" -o {{_BIN}} {{MAIN}}

# Every check CI runs.
ci: fmt-check vet test web-check
