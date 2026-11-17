# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 0.4.0 - 2026-11-17

### Added

- Pull counts per repository and per manifest, shown in the repository list, the repository page and the manifest page
- Repository Tags tab shows the latest pushed tag with its pull reference and runtime configuration

### Changed

- Pull reference field shows name and tag even when space is short, cutting the host first; the copy format switch lives only on the account page

### Fixed

- READMEs sit centered on the repository overview, and centered READMEs (align=center) line up in one column

## 0.3.0 - 2026-11-17

### Added

- Multi-platform image indexes show a Configuration tab with a platform switch, so the config is one click away instead of behind the platform image

### Fixed

- Anonymous docker pull of public repositories: the registry now issues Bearer tokens at /v2/token instead of a Basic challenge, which made Docker fail with no basic auth credentials
- READMEs imported from GitHub render like on GitHub: centered headers, logos and screenshots, inline badges, SVG images and working in-page links

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
