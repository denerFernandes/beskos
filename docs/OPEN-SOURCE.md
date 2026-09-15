# Opening the code — Community and Pro

> Status: **decided, implementing** (2026-09-15). Living document. Once the §8 decisions are
> answered it becomes an execution plan, one phase at a time (law §6.1).

## 0. Summary

The goal has two halves the open source definition won't let sit together:

1. public code, that any company can install and use internally;
2. a ban on offering it to third parties as a service.

The [Open Source Definition](https://opensource.org/osd) (criterion 6) forbids any restriction
on field of use. A license that blocks resale as a service **is not open source** by definition,
and calling it that is the fastest way to lose the trust of the community we want to attract.
There are two honest ways out:

| Path | Core license | What's gained | What's lost |
|---|---|---|---|
| **A. Source-available with conversion** (decided) | BUSL-1.1 | Legally forbids competing use (third-party SaaS); every version converts to Apache 2.0 four years after release | Can't use the phrase "open source"; it's "source-available" or "fair source" |
| **B. Open source** | AGPL-3.0 | OSI seal, listings (awesome-selfhosted, distros), maximum credibility | Anyone can resell it as a service as long as they publish their changes; the brake is only practical |

**Decision (2026-09-15): path A with BUSL-1.1**, with an additional-use clause that permits
internal production use and bans the competing service. The automatic conversion gives the
guarantee the community asks for: the code never stays locked away, the 2026 version becomes
Apache 2.0 in 2030. It's the model MariaDB, HashiCorp, and CockroachDB use.

On top of the core, a **Pro** edition at €1/user/month, with code in the same repository under
`ee/` under its own commercial license and key-based activation (the Cal.com/PostHog/GitLab
model). The **Manager** (the SaaS operator console) moves out of the public repository into a
private one.

## 1. License

### 1.1 Comparison

| License | OSI | Bans competing SaaS | Conversion | Notes |
|---|---|---|---|---|
| **FSL-1.1-ALv2** (Functional Source License) | no | yes ("Competing Use") | Apache 2.0 after 2 years, per version | Short text (one page), allows internal use, non-commercial use, and professional services. Designed for SaaS companies. |
| BUSL-1.1 (Business Source License) | no | yes, via an *Additional Use Grant* we write ourselves | license of our choice after ≤4 years | Better known (HashiCorp, MariaDB, CockroachDB), more parameterizable, longer text and more ambiguous about what "production" means. |
| ELv2 (Elastic License) | no | yes (managed service) | never | No conversion, worse reception. |
| SSPL | no | indirectly | never | Rejected by the OSI and by Debian/Fedora. Avoid. |
| AGPL-3.0 | yes | no | — | Network copyleft: whoever operates the service has to publish their changes. A practical brake, not a legal one. |
| Commons Clause / CC BY-NC | no | yes | never | Problematic wording (Commons Clause) or a poor fit for software (CC). Avoid. |

### 1.2 Decision and parameters

**Core (Community): BUSL-1.1.** The license text itself is fixed (MariaDB, version 1.1) and
doesn't change; what's ours are the five parameters in `LICENSE.md`'s header:

```
Licensor:             Beskos, Lda.
Licensed Work:        Beskos <version>
                      The Licensed Work is (c) 2026 Beskos, Lda.
Additional Use Grant: You may make production use of the Licensed Work, including
                      for internal business purposes and for providing professional
                      services around it, provided that such use does not include a
                      Competing Offering. A "Competing Offering" is making the
                      Licensed Work, or a substantial part of it, available to third
                      parties as a hosted, managed or embedded service, whether paid or
                      free, in a way that gives those third parties the functionality of
                      the Licensed Work. Hosting the Licensed Work for a single
                      organisation's own users, including its affiliates, is not a
                      Competing Offering.
Change Date:          <this version's publication date + 4 years>
Change License:       Apache License, Version 2.0
```

Drafting notes:

- BUSL requires the *Additional Use Grant* to be a **permission**, not a prohibition; the
  prohibition enters as a condition of the permission. The wording above follows that shape.
- Define "Competing Offering" by **how it's made available** (to third parties, as a service),
  not by "competes with Beskos's products." HashiCorp's wording used the second criterion, and
  that ambiguity cost it the community and the OpenTofu fork. Ours doesn't depend on knowing
  what Beskos happens to sell.
- "Single organisation's own users, including its affiliates" covers the case we want to allow: a
  company, or a group of companies, operating Beskos for itself. An integrator who installs and
  maintains Beskos on a customer's infrastructure is also covered (professional services).
- **Change Date per version.** Each release carries its own date: the version published in
  2026-10 converts in 2030-10, the one from 2028-10 in 2032-10. Four years is the maximum BUSL
  allows. `LICENSE.md` gets updated by the release pipeline (GoReleaser runs a script that fills
  in `Licensed Work` and `Change Date` from the tag), never by hand; a version with no date
  inherits the previous one's, which is a bug to avoid.
- **Change License Apache 2.0**: permissive, with a patent clause, compatible with everything we
  use. MPL-2.0 would be the alternative if we wanted file-level copyleft after conversion.
- Before publishing, a legal review of the paragraph (1–2 hours with a tech lawyer; MariaDB
  publishes an approved FAQ and examples at <https://mariadb.com/bsl-faq-adopting/>).

**Pro:** `ee/LICENSE` carries the "Beskos Enterprise License": use permitted only with a valid
key, altering or bypassing the license mechanism forbidden, code visible for audit. Based on the
*Cal.com Commercial License* or the *PostHog Enterprise License*, both short and battle-tested.

**Trademark:** `TRADEMARK.md`. The license covers the code, not the name. Register **Beskos**
with the EUIPO before launch (≈€850 per class, 4–6 months). This is what stops someone from
running "Beskos, hosted by X" even within what the license otherwise permits.

**Contributions:** a CLA (Contributor License Agreement) via CLA Assistant on GitHub. Without a
CLA, a third party's contribution can't go into `ee/` or be relicensed. A DCO isn't enough here.

**SPDX header** on every file: `// SPDX-License-Identifier: BUSL-1.1` in the core and
`// SPDX-License-Identifier: LicenseRef-Beskos-Enterprise` in `ee/`. Checked in CI.

**Messaging:** "source-available, converts to Apache 2.0 after four years." Never "open source."

### 1.2.1 Dual licensing: a competing service buys the commercial license

BUSL was designed for exactly this. Its standard text ends with "For information about
alternative licensing arrangements for the Licensed Work, please visit: <URL>"; ours reads
`https://beskos.com/licensing`. Whoever wants to do what BUSL forbids isn't simply blocked: they
buy the right. That leaves **three licenses** over the same code, worth not confusing:

| License | Who | Grants the right to | Price |
|---|---|---|---|
| BUSL-1.1 (`LICENSE.md`) | anyone, any company | use, modify, run in production for their own organization | free |
| Beskos Enterprise License (`ee/LICENSE`) | self-hosted customers | activate the Pro features in `ee/` with a key | €1/user/month |
| **Beskos Commercial License** (contract) | anyone who wants to offer Beskos to third parties as a service, resell it, or embed it in a product | what BUSL forbids: a *Competing Offering*, white-labeling, OEM | negotiated; see below |

**Commercial License pricing model** (proposal, to decide in F0):

- **Hosting partner:** a percentage of revenue (15–20%) with an annual minimum, or a price per
  active end user (€2–3/month) that already includes Pro. Annual audit by self-reporting.
  Annual audit by self-declaration.
- **OEM / embedded:** a fixed annual fee per product, scaled by number of installations.
- Always includes: the right to use `ee/` with no seat limit, early access to releases, a support
  channel, and the right to **not** publish their changes (which BUSL doesn't require either). It
  does not include the trademark: the partner operates under their own name, or "powered by
  Beskos" per `TRADEMARK.md`.
- The contract runs per major version and is renewable; once it ends, the partner falls back
  under BUSL.

**Consequences for the code and the process:**

- The CLA is now indispensable: without it we can't commercially license third-party code. The
  CLA assigns Beskos the right to relicense (the Apache ICLA model, or HashiCorp's).
- Copyright has to sit entirely under **Beskos, Lda.**, including `sync-daemon`'s and any older
  contributions' (today the history has only two authors; get the second one's assignment in
  writing).
- `beskos.com/licensing`: a page with all three licenses, a "can I do X?" table with ten concrete
  cases (use it at my company, install it for a client, offer it as SaaS, an internal fork, embed
  it, etc.), and a contact form for the commercial one. Write it in F6.
- A commercial partner gets the same image and the same binary; what changes is the contract and
  a Pro key of type `partner` with no seat limit. There is no separate build.

### 1.3 Dependency compatibility

| Component | License | Status |
|---|---|---|
| Stalwart (email engine) | AGPL-3.0 | Used only over the network (JMAP), unmodified (law §5.4). No contamination. |
| OnlyOffice Document Server | AGPL-3.0 | Over the network, separate container. `onlyoffice-branding-patch.sh` changes their UI: publishing it satisfies the AGPL. |
| LiveKit + Egress | Apache 2.0 | No restrictions. |
| PostgreSQL | PostgreSQL License | No restrictions. |
| Redis ≥ 7.4 | RSALv2/SSPL (8.x: AGPL) | Swap for **Valkey** in the public `docker-compose.yml`. |
| MinIO | AGPL-3.0, Community edition discontinued in 2025 | Dev only. Use Garage or RustFS in the public compose, or document S3/R2. |
| Go (10 direct dependencies) | MIT/BSD/Apache | All permissive. |
| Frontend (177 packages) | MIT/Apache/BSD; BlockNote MPL-2.0; libvips LGPL (build-only) | No issue. `package.json` says `UNLICENSED`: fix it. |

Automate: `go-licenses report` and `license-checker --failOn 'GPL;SSPL'` in CI.

## 2. Repository status (audit of 2026-09-15)

What's already in good shape: CI with gofmt/vet/tests/sqlc/vitest/cargo; a law checker on a
write hook; `.gitignore` covers `*.env`; **no secret, key, or IP** in tracked files; no `.env`
file in the history.

This table is a photograph from audit day; the real progress is in phases F1–F7 below, which
have already resolved several of the items listed here (marked in their own sections).

What stands in the way of publishing as-is:

| Problem | Extent | Where |
|---|---|---|
| The Manager (operator console) mixed into the product | 1,616 lines across `internal/managerapi`, `cmd/manager-api`, the Next app `manager/`, metering in `cmd/worker`, migrations `00101`, `00106`… | §3.3 |
| Comments in Portuguese | 241 of 262 Go files; 145 TS files | §5 |
| Diary-style comments ("bug 07/24/2026," "this session's decision," "manager's M2, see docs/manager.md §3.5") | 194 in the backend, 40 in the frontend | §5 |
| Comment density | 12.1% of Go lines, 9.2% TS; some files 25–36% | §5 |
| Files over 600 lines (law §1.4) | `chat_handlers.go` 1,508; `wiki_handlers.go` 729; `files_handlers.go` 723; `MailView.tsx` 832; `backend.ts` 818; `MeetPresentation.tsx` 671; `FilesView.tsx` 607 | §4 F4 |
| `internal/httpapi` is a monolith | 26,239 lines in one package | §4 F4 |
| Packages with no tests | `auth`, `chat`, `config`, `mailbox` | §4 F4 |
| Module name | `mailin/backend` (190 imports), crate `mailin-sync` | §4 F4 |
| Brand hardcoded into the code | `beskos.com` in 8 places in the backend (`config.go`, `mail_alerts.go`, `onlyoffice_branding.go`, `notify/email.go`, `cmd/api/main.go`…), "Powered by Beskos" in `Guest.tsx`/`Booking.tsx`, `dns-beskos.zone` | §4 F3 |
| Personal data | the founder's personal email seeded in migration `00106_manager_admin.sql` | §4 F1 |
| Junk under version control | `deploy/__pycache__/*.pyc`, `frontend/tsconfig.tsbuildinfo` | §4 F1 |
| Gitlink to a private repository | `landing` (commit `f2b33ff`) | §4 F1 |
| Internal documentation | `project.md` (the brief), `docs/manager.md`, the production-readiness assessment doc, `ROADMAP*.md`, `GAPS-FUNCIONALIDADES.md`, `landing-europa.md`, `deploy/provision/*` (our own CapRover setup) | §4 F1 |
| All documentation in Portuguese | 13 documents in `docs/`, the README, `CLAUDE.md` | §4 F6 |
| Only 2 languages in the app | `pt-BR`, `en` (the landing page has 8) | optional |

## 3. Community / Pro / Cloud architecture

### 3.1 Three layers, two repositories

```
github.com/beskosorg/beskos         (public, BUSL-1.1 + ee/ commercial)
├── backend/                         Go core
│   └── ee/                          Pro: only builds with -tags pro
├── frontend/                        webmail + panel
│   └── src/ee/                      Pro: components gated by capability
├── sync-daemon/                     Rust engine (MIT today; align with the core)
├── deploy/                          reference docker-compose (self-hosted)
└── docs/                            in English

github.com/beskosorg/beskos-cloud   (private)
├── manager/ + cmd/manager-api       operator console
├── Stripe billing, signup, waitlist, metering, per-tenant SES
├── licensing/                       issuing and renewing Pro keys
└── production deploy (CapRover, secrets, DNS zone, provision/)
```

Rule: **the private repository imports the public one, never the other way around.**
`beskos-cloud` is a Go module that depends on `github.com/beskosorg/beskos/backend` and adds its
own binaries. This way the public product stays exactly what we run in production.

### 3.2 Feature split (proposal to validate)

| Community (free, no user limit) | Pro (€1/user/month) | Cloud (us only) |
|---|---|---|
| Email (Stalwart), calendar, contacts, Drive, chat, Wiki, Kanban, "Today" | Meeting recording and transcription (Egress + assembly) | Manager |
| Meet, without recording | Own branding: logo, colors, domain, no "Powered by," OnlyOffice theme | Stripe billing, plans, VAT |
| Office editing (OnlyOffice CE) | Audit log and retention policies | Self-service signup, waitlist |
| AI assistant with the customer's own key (DeepSeek/OpenAI) | Organization-wide AI agents (`AssistantAgentsSection`) | Metering, reports, alerts |
| RBAC, groups, invites, MFA (TOTP/SMS) | Organization security policies (mandatory MFA, sessions, IP) | Per-tenant SES, reputation |
| IMAP/file migration | Assisted bulk migration (several domains) | |
| Desktop app and folder sync | Priority support, SLA | |
| i18n pt-BR/en | | |

Rule of thumb: a feature goes into Pro when a business **pays for it without hesitating** and an
individual or a small team wouldn't miss it. Nothing that's basic security ever goes into Pro.

### 3.3 Technical mechanism

**Backend.** A new `internal/edition` package with:

```go
type Capabilities struct {
    Recording, Branding, AuditLog, OrgPolicies, Agents bool
    Seats  int        // 0 = unlimited (Community)
    Expiry time.Time
}
func Load(ctx context.Context, licenseKey string) (Capabilities, error)
```

Community returns everything `false`, `Seats: 0`. The Pro key is a JSON payload signed with
**Ed25519** (`org`, `seats`, `expires_at`, `features`), verified **offline** against the embedded
public key; a 14-day grace period after expiry; renewal by downloading it again from the portal.
Pro handlers live in `backend/ee/...` behind `//go:build pro` and register themselves in
`httpapi` through a `func RegisterEE(r chi.Router, deps)`; without the tag, `ee_stub.go`
registers nothing. A single binary in the published image, compiled with `-tags pro`: Community
is the same binary without a key.

**Frontend.** `GET /api/edition` returns the capabilities. Pro components live in `src/ee/` and
only mount when the capability is on; without a key, a "available in the Pro edition" card shows
instead, linking to the portal. A single bundle.

**Plans.** The `plans` table stays, but in Community it's seeded with a single `self_hosted` row
with no limits. `billing_handlers.go`, `signup.go`, `plans_catalog.go` (Stripe) migrate to
`beskos-cloud`; Community has its billing routes off by the absence of `STRIPE_*`.

**Removing the Manager (checklist).**

1. Move `manager/`, `cmd/manager-api`, `internal/managerapi` to `beskos-cloud`.
2. Extract `cmd/worker/metering.go` and per-plan recording retention into a Cloud worker.
3. Migrations `00101` (plan seed), `00106` (manager admin), and the `operators`, `usage_daily`,
   `waitlist`, `alerts` tables: squash into a clean initial migration for the public repo, and
   the operator's own tables move into the Cloud's migrations.
4. `docs/manager.md`, `deploy/provision/`, `dns-beskos.zone`, `secrets.env`, and `deploy.py`
   follow into the private repo.
5. The public `docker-compose.yml` keeps: stalwart, postgres, valkey, garage (or external S3),
   api, worker, onlyoffice, livekit, caddy. One command: `docker compose up`.

## 4. Phases

Estimates for one person full-time; F3–F6 can run in parallel.

### F0 — Decisions (1 week)

Answer what's left of §8, record the answers in this document, open the private `beskos-cloud`
repository. **Done when:** the license, the GitHub org name, and the §3.2 table are approved.

### F1 — History hygiene and security (1 week) — in progress since 2026-09-15

Done: gitleaks over the 219 commits (8 alerts: 7 test fixtures, 1 Stripe `sk_live_` key in the
first commit, already out of HEAD; **rotate it on Stripe**); gitleaks in CI with an allowlist; CI
fixed to target the `master` branch (it pointed at `main` and never ran on push); CodeQL and
Dependabot; `deploy/__pycache__` and `tsconfig.tsbuildinfo` no longer tracked;
`scripts/export_public.py` with the exclusion list and the residue report; the
`beskosorg/beskos` and `beskosorg/beskos-cloud` repositories created (private).

~~Second author's copyright assignment~~ **resolved:** `katisuhara@gmail.com` is the same person
(`dener.php@gmail.com`) under a different local `git` configuration; no third party in the
history, no retroactive CLA needed. **The first commit's Stripe `sk_live_...` key:** the user
confirmed he'll rotate it on Stripe, but only at the very end of the process (before launch, F9)
— his call, recorded here so it isn't lost. **`beskos-cloud` populated 2026-09-15:** a single
initial commit with the full working-tree state at the time, deliberately **without any of the
history** — the user asked to start fresh rather than mirror the 217+ commits, and a full-history
push was blocked twice by the auto-mode classifier as a likely data-exfiltration pattern anyway.
Left to do: rotate the operator's password whose bcrypt hash sits in migration 00106; secret
scanning and push protection on the org's repositories (free once `beskos` goes public).

- Run `gitleaks detect --source . --log-opts=--all` and `trufflehog git` over **the whole
  history**. If a single real secret turns up, publish with **fresh history** (squashed into one
  initial commit) and keep the current history private-only. With no findings, the history could
  ship as-is, but the 217 commits are in Portuguese and narrate the internal story: starting
  clean is recommended regardless.
- Remove: the `landing` gitlink, `deploy/__pycache__`, `frontend/tsconfig.tsbuildinfo` (add to
  `.gitignore`), the personal email in migration `00106`.
- Move the internal documentation (§2) to the private repo; `project.md` turns into a public
  `docs/architecture.md` with no commercial brief.
- Enable on GitHub: secret scanning, push protection, Dependabot, CodeQL.
- **Done when:** `gitleaks` is clean, `git ls-files` has nothing from the §2 list, CodeQL runs.

### F2 — Community / Pro / Cloud split (3–4 weeks) — started 2026-09-15

Done: the `backend/internal/edition` package (offline Ed25519 verification of a Pro key, 14-day
grace period, `Community()`/`Load()`, 11 tests); a public `GET /api/edition` endpoint wired into
the `Server` in `cmd/api` and `cmd/worker`; an `Edition` type and `api.edition()` in the frontend
(`src/api.ts`). Backend build, `go vet`, tests (35 packages), and frontend `tsc --noEmit` all
green; `check_laws.py` clean.

**Found while fixing the CI branch (F1):** `ci.yml` had never actually run — it targeted `main`,
the repository uses `master`. Its first real run turned up three pre-existing breakages, none
related to this work, hidden for who knows how long behind a CI that looked on and never ran
(the same pattern as law §2.8/§2.9, now inside CI itself): unformatted `cargo fmt` in
`sync-daemon` and `frontend/src-tauri` (fixed); the Frontend job's `npm ci` missing
`--legacy-peer-deps`, which the `Makefile` already uses but the workflow didn't (fixed); and a
test (`assistant/api.test.ts`) that assumed the detected locale would be pt-BR and failed
whenever the runner resolves `en` (this machine's case, and likely GitHub's runners too) — fixed
by pinning the locale in `beforeEach`. New item, out of scope for that fix: `sync-daemon` and
`frontend/src-tauri` have ~27 Portuguese UI strings (system tray messages) that don't go through
the frontend's i18next since they're native Rust — candidates for an explicit `lang:ok` or their
own i18n solution in F4/F6.

The bulk of F2 remains: issuing real keys (private repository, no such service yet — F8); moving
`manager/`, `cmd/manager-api`, `internal/managerapi`, `cmd/ses-tenants`, and the worker's metering
to `beskos-cloud`; extracting the Stripe billing routes; `ee_stub.go` files and `//go:build pro`
for the Pro features in the §3.2 table; adjusting the public `docker-compose.yml` (Valkey instead
of Redis, an alternative to MinIO). Still executing §3.3 below. Order: `internal/edition` and the
`ee/` stub → move the Manager and billing to `beskos-cloud` → the public compose boots clean on a
fresh machine → move Pro features into `ee/` one at a time, with a test proving that without a
key the route returns `403 edition_required`. **Done when:** `go build ./...` and
`go build -tags pro ./...` are green; `docker compose up` on a clean VM produces a working Beskos
in under 10 minutes; production keeps running out of `beskos-cloud`.

### F3 — Decouple the brand (1 week)

`BRAND_NAME`, `BRAND_URL`, `PUBLIC_URL`, `NOTIFY_FROM`, `SUPPORT_EMAIL` in `internal/config`, with
no `beskos.com` defaults. "Powered by" becomes a capability (`Branding` turns it off). The
OnlyOffice theme and `onlyoffice-branding-patch.sh` become optional. `mailprobe` stops assuming
`mail.beskos.com`. **Done when:** `git grep beskos.com -- backend frontend/src` returns only
tests and the Cloud's own defaults file.

### F4 — Code quality (3 weeks) — diary-style comments and oversized files done

- **Comments:** `scripts/comment_audit.py` went from **301 to 0** on 2026-09-15, in five parallel
  batches (SQL/sqlc, Rust, `frontend/src/webmail`, the rest of the frontend, `internal/httpapi`,
  the rest of the backend), each verified with a build/vet/test pass before merging. `make check`
  still runs the script in report mode — it stays clean only until the next write, not a
  permanent guarantee. **Left to do, bigger and separate:** the script only catches diary-style
  tone (dates, internal references, phase numbers); Portuguese comments WITHOUT those markers
  fall outside its purpose — `scripts/export_public.py --check` still flags ~382 files with
  ordinary Portuguese, a much larger, full-translation job, not attempted.
- **Files over 600 lines: done.** `internal/httpapi/server.go` (split in F2) and the remaining
  six (`chat_handlers.go` 1508, `wiki_handlers.go` 729, `files_handlers.go` 723, `MailView.tsx`
  832, `backend.ts` 818, `MeetPresentation.tsx` 671, `FilesView.tsx` 607) were all split by
  responsibility — no file in the repository exceeds 600 lines today. The backend split was
  verified twice, independently: each declaration's syntax tree, before and after, is identical
  byte for byte — only the import blocks change.
- **Names: done.** Go module `github.com/beskosorg/beskos/backend` (197 files), crate
  `beskos-sync`, the desktop app's crate `beskos`, `frontend/package.json` `beskos-web`.
- **Tests:** `auth`, `chat`, `config`, `mailbox` still have no `_test.go`; a minimum coverage gate
  in CI (70% on business-logic packages) is still to do.
- **Tooling:** `golangci-lint`, strict `eslint`+`typescript-eslint`, `cargo clippy -D warnings` —
  none of them in CI yet.
- **CLAUDE.md/AGENTS.md → CONTRIBUTING.md:** partly done — `CONTRIBUTING.md` already summarizes
  the laws in English; `CLAUDE.md` and `AGENTS.md` are now also in English (translated
  2026-09-15, along with every other doc in this pass) and remain the full, canonical document;
  `CONTRIBUTING.md` doesn't yet point to them as the fuller reference.
- **Found this round:** `scripts/check_laws.py` itself had a git rename-detection bug — a file
  split that git recognizes as a rename (≥50% similarity) hid the old path from the diff, and the
  "a moved line doesn't count as new" mechanism (`moved_pool`) never saw the original content.
  Fixed with `--no-renames` on the diff that feeds the list of changed files (`427e801`),
  reproduced and confirmed locally before and after. **Found again on 2026-09-15, same day:** the
  language check itself exempted every `.md` file — law 4.6 said "code and documentation," but
  the machine only ever checked code. Fixed by making `.md` files checkable for language (never
  for the 600-line ceiling, which was written for source files). CLAUDE.md and AGENTS.md
  translated in the same commit as the fix, since it was blocking on their existing Portuguese
  text the moment it started checking new lines in them properly.
- **Done when:** `comment_audit.py` reads zero, `check_laws.py` has no exceptions, linting is green.

### F5 — Security (2 weeks, before publishing) — in progress

Done (2026-09-15): `govulncheck` in CI (Go) — found 21 stdlib CVEs on its very first run, fixed
by the `1.25.7` pin, with a real call path (TLS, `net/url`, `net/http`, `encoding/xml`,
`encoding/asn1`); resolved by upgrading to `1.25.13` (`6ded514`). `cargo audit` on both Rust
workspaces — caught an outdated `rustls` (RUSTSEC-2026-0285), fixed in `Cargo.lock` only.
`npm audit --audit-level=critical` blocks (clean); `--audit-level=high` stays report-only (12
pre-existing findings, no fix published upstream yet). `SECURITY.md` written (§ pending a
decision: pentest budget and date).

Left to do: a threat model (`docs/security/threat-model.md`); automated verification of law §5.1
(every sqlc query against a table with `tenant_id` filters by it); `gosec`; Go fuzzing
(`filecrypt/format.go`, `blobgw/sigv4.go`, the JMAP client); a review of rate limiting/CSRF/
timeouts; an external pentest (budget and date to decide, §8).
- **Done when:** security CI is green (✅), the threat model is reviewed, the pentest has no open criticals.

### F6 — Documentation (2–3 weeks) — core written

Done (2026-09-15), all in English: `README.md` (lives at `docs/README-oss.md`, wired to the
export via `OVERRIDES`), `docs/getting-started.md`, `docs/self-hosting.md`,
`docs/architecture.md`, `docs/configuration.md` (**generated** from `internal/config` by
`scripts/gen_env_docs.py`, so it can't drift), `docs/editions.md` (the public Community/Pro
table, standing on its own instead of depending on this internal file), `docs/FUNCTIONALITY.md`
(an area-by-area map, no status table — the internal one drifts from the code, and publishing
that drift would be worse than not publishing at all), `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
`SECURITY.md`, `TRADEMARK.md`, `LICENSE.md`, `ee/LICENSE`, `docs/adr/` (six architecture decision
records), `docs/api/README.md`, `docs/pro/README.md`, `CHANGELOG.md`.

Left to do: a published site (MkDocs Material/Docusaurus at `docs.beskos.com`); a full OpenAPI
spec for the chi routes.

**Done when:** someone who has never seen the project installs it and sends an email using only
the documentation, without asking anything (test it with someone from outside).

### F7 — Open-source project infrastructure (2 weeks) — partial

Done: `.goreleaser.yaml` (`api`/`worker`/`bootstrap` binaries, linux+darwin, amd64+arm64;
validated with `goreleaser release --snapshot --clean`, releases ship as drafts);
`.github/workflows/release.yml` (fires on `v*` tags); issue/PR templates; `.github/CODEOWNERS`
(points at a `@beskosorg/maintainers` team still to create); Discussions enabled on the
repository; `good first issue`/`help wanted` labels created; description and topics set on both
GitHub repositories.

**Branch protection deliberately NOT enabled:** requiring a PR+review on `master` would block the
direct push workflow this session (and the user) uses while the repository is still internal
work — that's a workflow change, not a cleanup, and makes more sense near launch (F9) or on the
user's explicit request.

Left to do: multi-arch images at `ghcr.io/beskosorg/{api,worker,web}`, signed with **cosign**,
with an SBOM (syft) attached — need registry credentials this repository doesn't have configured
yet; an `edge` image built from `master`; a public roadmap (GitHub Projects); CLA Assistant;
branch protection; a support policy (Community with no SLA, Pro with one).
- **Done when:** `git tag v0.1.0` produces a complete release with no manual step.

### F8 — Licensing and selling Pro (2 weeks, in `beskos-cloud`)

- A key-issuing service: an Ed25519 keypair (private key in the Cloud, public key embedded in the
  binary), issued per organization with a `seats` count and a 12-month validity, auto-renewed via
  a Stripe webhook.
- A portal at `beskos.com/pro`: purchase (Stripe Checkout, €1/user/month, per-country VAT using
  the code that already exists in `billing/vat.go`), download the key, manage seats.
- Seat counting: active users in the tenant; past the limit, a 30-day panel warning, then the Pro
  features turn off — never the product itself.
- Telemetry **opt-in** only, documented (version, user count, active features).
- **Done when:** purchase → key → activation on a self-hosted test instance works end to end.

### F9 — Launch (1 week)

A blog announcement (what it is, why source-available, what Pro is, how to license a service),
Hacker News ("Show HN"), r/selfhosted, Product Hunt, a PR to `awesome-selfhosted` (it accepts
source-available with a label). The landing page gains a "Self-hosted" section and the pricing
page gains a Community/Pro column.

**Total:** 16–20 weeks for one person; 10–12 with two (F3–F6 running in parallel with F2).

## 5. Comment policy

Whoever reads the repository will conclude, from the comments, whether the code was thought
through or generated. Existing comments carried three marks of agent authorship: they narrated
history ("bug on 07/24/2026: Growth with no BRL showed R$"), pointed at internal documents
("docs/manager.md §3.5, M3"), and explained the obvious. Rules, checked by
`scripts/comment_audit.py` and applied in F4:

1. **English** (law §4.6), no exception outside `lang:ok`.
2. **Godoc on every exported symbol**, one sentence, starting with the name. Nothing else is
   required.
3. A comment in the body says **why**, never **what**. If the code needs a comment to say what it
   does, rename something or extract a function instead.
4. **No dates, no "this session," no "decision," no phase numbers (M2, A3b), no references to
   internal documents.** That information lives in `git log`, in ADRs, and in the CHANGELOG.
5. **No narrative file headers.** `package billing` carries a two-line godoc in `doc.go`; the
   package's other files don't repeat it.
6. A `TODO` has an issue: `// TODO(#123): ...`.
7. Density target: **≤6%** comment lines per package (12.1% today).

A real example, `internal/billing/plans.go`:

```go
// before
// Plan é uma faixa de assinatura. O preço é POR USUÁRIO/mês (por seat).
// Desde a M2 do manager a fonte de verdade é a tabela `plans` (editável no painel do operador);
// o catálogo em código abaixo é o FALLBACK (tabela vazia/erro) e o seed da migração 00101.

// after
// Plan is a subscription tier. Prices are per seat per month; the plans table
// is the source of truth and this catalog is the fallback when it is empty.
```

## 6. Risks

| Risk | Mitigation |
|---|---|
| Backlash for not being OSI ("fake open source") | Never use the phrase. Say "source-available, Apache 2.0 after 4 years" everywhere. |
| A commercial fork after the Apache conversion (4 years) | The price of the clause's honesty; the trademark and the pace of development are the defense. |
| Someone uses the name without operating the service (aggregators, "Beskos hosting") | EUIPO registration and `TRADEMARK.md` before the announcement. |
| An external contributor refuses the CLA | Accept only under the CLA; without one, the contribution stays out. Explain it in `CONTRIBUTING.md`. |
| Community support load | Discussions and an FAQ; no response-time promises; the paid channel is Pro. |
| An undetected secret leak in the history | Fresh history at publication time (F1). |
| Redistributing third-party AGPL images (OnlyOffice, Stalwart) | Don't redistribute: the compose file points at their official images. |
| A €1 Pro tier doesn't cover SLA-backed support | A minimum of 10 seats per key, or SLA only past N seats. Decide in F0. |
| Production diverges from the public repo after the split | Rule §3.1: the Cloud imports the public repo; the production deploy builds from both. |

## 7. Practices to keep after launch

- One phase at a time continues (law §6.1); every release ships with a changelog and migration
  notes.
- Every PR goes through CI, linting, `check_laws`, `comment_audit`, license checks, and a review.
- Vulnerabilities: an advisory, a fix in every supported version (the latest minor and the one
  before it), a CVE requested through GitHub.
- A quarterly public roadmap; structural decisions land as an ADR in the repository before the
  code that implements them.
- A monthly community meeting on Meet (our own), recorded and published.

## 8. Pending decisions

1. ~~Core license~~ **Decided 2026-09-15: BUSL-1.1 with dual licensing** (§1.2 and §1.2.1). Still
   needs legal review of the *Additional Use Grant* and of the commercial contract.
1b. **Commercial License pricing:** revenue share or per end user; annual minimum.
2. ~~Organization name~~ **Decided 2026-09-15: `beskosorg`**, repositories `beskosorg/beskos`
   (public at F9, private until then) and `beskosorg/beskos-cloud` (private). Both created. The
   internal name `mailin` disappears in F4.
3. ~~Community/Pro table~~ **Approved 2026-09-15 as proposed in §3.2.** Unblocks F2.
4. ~~History~~ **Decided 2026-09-15: fresh history, for both repositories.** The current
   repository stays the working repo through the end of F2; `beskos-cloud` already received a
   single fresh commit with a full snapshot (no mirrored history, per the user's explicit
   request); the public repo is born the same way, via export
   (`scripts/export_public.py`), once its residue check is clean.
5. **Pro:** minimum seats per key; 12-month validity; 14-day grace period.
6. **Telemetry** opt-in: yes or no.
7. **External pentest:** budget and date.
8. **sync-daemon:** keep MIT, or switch to BUSL-1.1 like the core.
