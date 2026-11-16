# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 0.2.0 - 2026-11-17

### Added

- DOREG_GITHUB_TOKEN for README imports from private repositories and a higher GitHub rate limit
- Release binaries for Linux, macOS and Windows, with the web UI embedded
- Repository list and page show the image description and link its source (org.opencontainers.image.* labels)
- Repository README: write Markdown on the new Overview tab or import it from GitHub; public repositories show it to visitors

## 0.1.0 - 2026-11-17

### Added

- Blob storage on local disk or S3; metadata in SQLite or PostgreSQL
- `doreg login` (browser approval, then docker login) and `doreg import` from other OCI registries, also available in the UI
- OCI distribution registry with online garbage collection, retention policies and per-project quotas
- Projects with members, roles, robot accounts, public repositories and guest browsing
- Signed, retried webhooks and an audit log for every content change
- Single sign-on via OpenID Connect; admin by verified email or first SSO user
- Web UI for repositories, image configuration, garbage collection and administration

### Fixed

- Rate limit failed logins; changing the password requires the current one and ends other sessions
- Refuse webhook deliveries to internal addresses
