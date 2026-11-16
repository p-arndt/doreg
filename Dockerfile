# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY web/ ./
RUN mkdir -p ../internal/webui/dist && pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
ARG TARGETOS TARGETARCH VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/p-arndt/doreg/internal/buildinfo.Version=${VERSION}" \
      -o /out/doreg ./cmd/doreg
# The data volume must belong to the nonroot user so a fresh named volume is writable.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/doreg /doreg
COPY --from=build --chown=65532:65532 /out/data /data
ENV DOREG_DATA=/data DOREG_ADDR=:8080
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/doreg"]
CMD ["serve"]
