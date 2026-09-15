# Changelog

All notable changes to this project are documented in this file. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

Work preparing the first public, source-available release.

### Added

- A Business Source License 1.1 core license with a four-year conversion to Apache License 2.0,
  plus dual licensing for anyone who wants to offer the software as a competing hosted service
  instead of staying within the license's use grant. See
  [ADR 0006](docs/adr/0006-busl-license.md) for the reasoning and the alternatives considered.
- `ee/LICENSE` (the Enterprise license covering Pro-only code), `TRADEMARK.md`, `SECURITY.md`,
  `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1), and `CONTRIBUTING.md`.
- An `edition` package that determines whether an instance has unlocked Pro capabilities, by
  verifying a signed license key offline (Ed25519) with a 14-day grace period after expiry, and
  a public `GET /api/edition` endpoint so the frontend can gate Pro-only UI before a user is even
  logged in. See [docs/pro/README.md](docs/pro/README.md) for the full activation flow, including
  why a missing key and an invalid key are handled differently.
- A `backend/ee` scaffold: the seam Pro-only backend routes will register through once a feature
  actually moves there, gated by both a Go build tag and the relevant capability. No feature uses
  it yet.
- `deploy/docker-compose.community.yml`, a self-hosted reference deployment: the same product
  without the hosted operator console, running Valkey instead of Redis for license compatibility.
- Public documentation: `docs/README-oss.md`, `docs/architecture.md`,
  `docs/configuration.md` (generated from the config package so it can't drift out of date),
  `docs/editions.md`, `docs/getting-started.md`, `docs/self-hosting.md`, `docs/FUNCTIONALITY.md`,
  and now `docs/adr/` (architecture decision records), `docs/api/README.md` (API orientation),
  and `docs/pro/README.md`.
- CodeQL and Dependabot scanning, `gitleaks` in CI with an allowlist for known test fixtures, and
  a `govulncheck` job for Go stdlib and dependency vulnerabilities.
- A first security threat model, and a `gosec` CI job (report-only while findings are triaged).
- A GoReleaser pipeline building `api`, `worker`, and `bootstrap` for Linux and macOS on amd64 and
  arm64, publishing releases as drafts; issue and PR templates; a `CODEOWNERS` file.
- GitHub Discussions enabled, with `good first issue` and `help wanted` labels for external
  contributors.
- `scripts/export_public.py`, which produces the public repository export with an exclusion list
  and a residue report so nothing internal ships by accident.

### Changed

- Renamed the Go module from its internal name to `github.com/beskosorg/beskos/backend`, across
  every import in the backend.
- Renamed the remaining internal identifiers to match: the Rust sync daemon crate
  (`mailin-sync` to `beskos-sync`) and the desktop app's Rust crate (`mailin` to `beskos`).
- Made `BRAND_NAME`, `BRAND_URL`, and `SALES_EMAIL` configurable through the environment instead
  of hardcoded, defaulting to the values already used in production so behavior doesn't change
  for the existing deployment.
- Split `internal/httpapi/server.go`'s `Server` type and constructor into their own file
  (`server_types.go`) to keep the router file under the project's 600-line limit.
- Fixed the CI workflow to actually run against this repository's `master` branch -- it had been
  configured for `main` and had never run on push.

### Fixed

- Three pre-existing CI failures uncovered the moment the workflow started running for real (it
  had targeted a branch that doesn't exist here): unformatted Rust code in the sync daemon and
  the desktop app's Tauri crate, a frontend CI job installing dependencies without the flag the
  project's own build already required, and a frontend test that assumed a Portuguese locale and
  failed under any other one.
- A malformed line in the `gitleaks` CI configuration that had been silently failing the entire
  workflow.

### Security

- Upgraded Go from 1.25.7 to 1.25.13, resolving 21 standard-library vulnerabilities the newly
  added vulnerability scanner found with real call paths into this codebase (TLS handshake and
  certificate handling, `net/http`, `net/url`, `encoding/xml`, `encoding/asn1`).
- Triaged all 130 pre-existing `gosec` findings by hand; the ones in categories severe enough to
  matter (flagged possible SSRF) were confirmed as already-defended call sites (private-IP
  blocking and DNS-rebinding protection already in place) rather than real gaps.
- Added a heuristic checker (`scripts/check_tenant_scoping.py`) that cross-references every
  tenant-scoped table against the queries that touch it and flags any missing a `tenant_id`
  mention, to support review of the project's multi-tenancy rule (report-only; 219 pre-existing
  hits found, none confirmed as a real gap in spot checks, still awaiting full manual triage).

[Unreleased]: https://github.com/beskosorg/beskos/commits/master
