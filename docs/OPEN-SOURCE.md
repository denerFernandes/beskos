# Opening the code — Community and Pro

> Status: **implementation paused for complete planning** (2026-09-16).
> The current execution baseline is [section 9](#9-complete-execution-plan).
> Earlier sections preserve decisions and historical status; section 9 supersedes stale progress claims.

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
| Internal documentation | `project.md` (the brief), `docs/manager.md`, the production-readiness assessment doc, `ROADMAP*.md`, `FEATURE-GAPS.md`, `landing-europa.md`, `deploy/provision/*` (our own CapRover setup) | §4 F1 |
| All documentation in Portuguese | 13 documents in `docs/`, the README, the project laws | §4 F6 |
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
with no limits. `plans_catalog.go` and `internal/billing/plans.go` stay in `httpapi`/`internal/billing`
unconditionally — self-hosted quota enforcement (mailboxes, storage, meeting participants) reads
them regardless of edition. Stripe, VAT, and self-service signup (formerly `billing_handlers.go`,
`signup.go`, `internal/billing/stripe.go`) moved into `backend/ee/billing_*_pro.go` behind
`-tags pro`, the same boundary as agents/branding/security-policy: the source ships in full in the
public repo, and `billing_routes_community.go` mounts none of the routes in a Community build.

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

**Public export boundary hardened (2026-09-16):** operator-only plan mutations, usage reports,
alerts, and deliverability queries now live in the excluded Manager query file. The export runs
sqlc against the retained schema and queries, removing private database models as well as their
configuration overrides. Residue checks inspect the final generated tree, including `Manager*`
Go identifiers and `manager_*` SQL objects. Export regression tests and a private export CI job
cover this boundary; CI also checks reproducible generation and both backend build editions.
The workflow is excluded from public exports because it depends on private export sources.
The main CI law check now supports orphan commits by using the empty tree as its initial base.
Local validation passed: backend vet, build, and tests with and without `pro`, in both the
working tree and a fresh public export; deterministic sqlc regeneration; export regression
tests; language, register, and documentation checks. The full export report still has 21 file
findings across brand domains and legacy comments, with no private-implementation matches. No repositories
were published as part of this increment.

**Metering and hosted billing export separation (2026-09-16):** the public worker uses an empty
edition hook and excludes daily rollups, their table and generated queries, and plan-based
recording retention. The complete worker retains these jobs and now shares its storage client.
Retention preserves rows when blob deletion fails or storage is unavailable, scopes row deletion
to the tenant, and reports only successful removals. Both jobs escalate repeated failures and
announce recovery. Configured storage initialization errors now stop worker startup.

Hosted checkout, Stripe, VAT calculation, and commercial signup handlers and screens are excluded
from public snapshots. Public route tests reject hosted endpoints and retain usage statistics.
The public catalog contains only `self_hosted`, without commercial seat/storage/send limits, and
account access bypasses hosted trial/subscription evaluation. The working tree retains its
hosted routes and catalog. Public configuration documentation is regenerated from the exported
source. Both frontend editions build and pass 47 tests, with one integration test skipped.
Private billing schema fields, marketing endpoints, per-tenant SES, full-stack startup validation,
and physical Pro feature extraction remain outstanding; this is not the completion of F2.

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
`manager/`, `cmd/manager-api`, `internal/managerapi`, `cmd/ses-tenants`, and the private worker jobs
into an independently composed `beskos-cloud` module (their exclusion from public snapshots is
implemented); removing remaining hosted schema and endpoints; `ee_stub.go` files and `//go:build pro`
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
- **Project laws → CONTRIBUTING.md:** done — the laws live in `docs/engineering-standards.md`,
  in English, and `CONTRIBUTING.md` points to it as the fuller reference.
- **Found this round:** `scripts/check_laws.py` itself had a git rename-detection bug — a file
  split that git recognizes as a rename (≥50% similarity) hid the old path from the diff, and the
  "a moved line doesn't count as new" mechanism (`moved_pool`) never saw the original content.
  Fixed with `--no-renames` on the diff that feeds the list of changed files (`427e801`),
  reproduced and confirmed locally before and after. **Found again on 2026-09-15, same day:** the
  language check itself exempted every `.md` file — law 4.6 said "code and documentation," but
  the machine only ever checked code. Fixed by making `.md` files checkable for language (never
  for the 600-line ceiling, which was written for source files). The laws file and AGENTS.md
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

## 9. Complete execution plan

Planning baseline: 2026-09-16. The user requested complete planning before further implementation.
This section proposes the remaining work; it does not authorize a deployment, purchase, legal
engagement, repository rewrite, or launch. No implementation changes are part of this planning pass.
The working tree remains available for review, including staged new files and unstaged edits.

### 9.1 Objective and fixed decisions

Deliver a maintainable, independently installable Community edition, a separately licensed Pro
extension, and a private Cloud edition, with reproducible publication and no private source or
production data in public artifacts.

- Core: BUSL-1.1 with the previously selected additional-use grant and Apache 2.0 conversion.
- Pro: commercial code under `ee/`, with the stated target price of EUR 1 per user per month.
- Cloud: Manager, hosted subscriptions, commercial onboarding, marketing endpoints, metering,
  and per-tenant SES administration remain private. Manager does not belong in public `ee/`.
- Repositories: `denerFernandes/beskos` remains the working source during this transition;
  `beskosorg/beskos` receives sanitized public snapshots; `beskosorg/beskos-cloud` remains private.
- Preserve the decision not to transfer the working repository's history. Initial publication
  uses fresh history. Do not infer permission to force-push existing destinations on every update.
- Public wording is **source-available**, not OSI open source. The license itself makes that
  distinction; the OSI definition also disallows restrictions on fields of use. See the
  [BUSL text](https://mariadb.com/bsl11/) and [Open Source Definition](https://opensource.org/osd).
- No changes to Stalwart itself. Individual MFA, authentication, tenant isolation, and basic
  security remain Community features. The commercial boundary must not weaken these controls.

### 9.2 Verified baseline and limits of the evidence

| Area | Observed state | Still required |
|---|---|---|
| Working tree | Substantial local changes across export, database queries, worker, API, and frontend; not yet committed at the time of this review | Review and separate cohesive commits; preserve pre-existing `landing` and `.codex/` changes |
| Public exclusion | Fresh local exports omit Manager, daily rollups, hosted checkout/Stripe/VAT/signup screens, marketing handlers, SES administration, and their dedicated tables/queries | Artifact-level security review, migration compatibility, remote verification |
| Community access | Local export uses a `self_hosted` catalog and does not evaluate hosted trial expiration | Verify real onboarding, upgrade behavior, and end-to-end access |
| Metering safety | Local code preserves recording rows after failed blob deletion, scopes deletion to the tenant, reports actual removal counts, and escalates repeated failures | Review runtime behavior and storage-startup failure handling |
| Shared AWS signing | Extracted from SES into `internal/awssign`, retaining the existing signing vector and SMS support | Include SMS MFA in isolated integration testing |
| Backend verification | Vet, build, and tests passed for complete and exported trees, with and without `pro`, after the latest Cloud extraction | Fresh database migration and service integration tests; remote CI |
| Frontend verification | Complete and billing-separated exports built; 47 tests passed and one was skipped | Final artifact build after all remaining edits; browser smoke tests |
| Pro implementation | Offline capability verifier exists; `backend/ee` route registration is still a stub | Physical feature extraction and real server-side enforcement |
| Deployment | Community compose exists; Valkey is already used despite the service name `redis`; MinIO remains the object-storage default | Complete clean-install/upgrade/restore exercise; decide storage packaging |
| Release | Draft GoReleaser workflow exists; no `pro` build tag configured there; conversion date is fixed in `LICENSE.md` | Versioned license metadata, image publication, signing, SBOM, provenance, artifact tests |
| Remote state | Prior publication status and commit IDs are supplied by the user | Verify visibility, branches, protections, exposed history, and current destination heads; none was rechecked in this planning pass |

Passing the export's targeted scanner proves only its current checks. It is not proof of absence
of all private material or of a working production deployment. The last full residue report
before the SES extraction contained 21 file findings, including expected legal brand references;
regenerate and classify the final report instead of treating that count as a fixed backlog.

### 9.3 Decisions to settle before their dependent work

These are structural or commercial choices, not requests to reconfirm decisions already made.
Record the resolution before implementing the dependent step, following project law 6.2.

| Decision | Recommended direction | Alternative and consequence | Needed before |
|---|---|---|---|
| Long-term Cloud composition | Keep deterministic snapshots for the current transition; design a small exported integration API before introducing an independently versioned Cloud module | A wholesale move of `internal` packages exposes internals and creates a larger compatibility surface. A separate repository cannot simply import the current `backend/internal/*` paths | Independent Cloud module work |
| Public migration compatibility | Preserve an upgrade path from the already published public snapshot; keep the production migration ledger unchanged | Squashing to a new initial schema is simpler only for fresh installs; existing installations require a tested migration/restore route | Further schema removal or squashing |
| First self-hosted owner | Explicit local bootstrap or installation-only enrollment, disabled after initialization; existing invitations remain supported | Permanent anonymous organization signup exposes a hosted-style provisioning surface and requires a separate abuse model | Clean-install completion |
| Storage default | Retain the current S3 abstraction; evaluate the pinned default with license and maintenance review before a provider change | Garage or external S3 changes packaging, credentials, operational documentation, and migration testing | Final compose and support baseline |
| Pro expiration and seats | Document downgrade rules per feature; retain data, individual MFA, safe access, and persisted security protections | Blindly disabling an enforced organization policy could weaken security; allowing unlimited grace changes the commercial model | Pro enforcement |
| Verification trust root | Embed production verification keys in release artifacts, with a versioned rotation policy and explicit development override | Trusting any operator-provided `EDITION_PUBLIC_KEY` is not an issuer-controlled production trust root | Real Pro activation |
| Commercial terms | Confirm minimum seats, billing period, taxes, support scope/SLA, and competitor-license pricing separately | EUR 1 per user alone does not define support commitments or minimum commercial viability | Checkout and public pricing |
| Legal and external costs | User approves counsel/pentest scope, provider, budget, and trademark work | These cannot be completed by source-code changes or inferred purchasing authority | External engagements and final launch |
| Desktop/sync license | Resolve the existing Rust/MIT versus BUSL decision and contributor rights | An inconsistent license boundary makes redistribution terms unclear | Release license inventory |
| Launch scope | Launch only features with completed acceptance evidence; decide explicitly whether Community can launch before commercial Pro sales | A combined launch depends on the private licensing portal and its legal/commercial readiness | Release scheduling |

Telemetry remains absent or disabled by default. No telemetry implementation is required to
complete the initial release. Any later collection requires an explicit product/privacy decision.

### 9.4 Ordered work packages

Implement one package at a time. Each package ends with reviewable changes, evidence of the checks
below, and an updated status entry. A failed gate is resolved before its dependent package starts.
Legal/commercial preparation may be scheduled independently, but does not bypass technical gates.

#### P0 — Reconcile and review the current branch

1. Inventory existing changes, test results, generated files, and source-to-export overrides.
2. Separate the local work into review units: export/database boundary; metering safety;
   hosted billing and self-hosted access; marketing/SES separation and shared signing.
3. Review behavior changes explicitly, especially worker startup on storage failure and the
   self-hosted subscription bypass. Do not treat a refactor label as evidence of no behavior change.
4. Verify current repository visibility and heads read-only. Compare the current public tree
   with the proposed export, including migrations and scripts left over from hosted deployment.
5. Record a reproducible baseline, preserving unrelated user changes. Do not commit or publish
   until the implementation review and the user's requested planning step are complete.

**Gate:** a reviewed inventory and a known baseline for each edition, with no ambiguous ownership
of local changes and no unreviewed files silently included in a publication.

#### P1 — Finish the public/private boundary

1. Inventory source, scripts, migrations, generated models, templates, documentation, assets,
   workflow files, containers, and release archives by edition.
2. Remove residual hosted subscription/trial/VAT/lead-management implementation and unused
   generated bindings from public artifacts, retaining neutral shared contracts only where needed.
3. Resolve the migration decision above before changing schema history. Test both a clean
   database and upgrades from the currently published public snapshot. Production keeps its
   complete schema and data.
4. Make the export reproducible from an identified commit; define how intentional local edits
   are previewed without becoming an untracked publication source.
5. Extend automated boundary checks to dependency graphs, final frontend bundles, generated
   code, image filesystems, and packaged documentation. Missing inputs and generator failures
   must stop the export. Audit symlinks and path handling.
6. Keep the production Cloud composition working. Any later independent Go module must depend
   on an intentional exported integration API, not an invalid cross-repository `internal` import.

**Gate:** Community and Pro public artifacts contain no Cloud implementations or data; complete
Cloud still builds and passes its tests; supported public database upgrades succeed.

#### P2 — Make Community self-hosting complete

1. Finalize the first-owner bootstrap, login, invitation, and password-recovery path.
2. Validate the `self_hosted` plan across API, worker, database defaults, quotas, and frontend;
   no hosted trial, checkout, production service URL, or SaaS account is needed.
3. Complete compose/env/healthcheck setup with documented secrets, pinned dependencies, and
   consistent internal hostnames. Keep operator credentials entirely server-side.
4. Exercise SMTP/IMAP/JMAP, domain DNS and DKIM, attachments, Drive, office editing, meetings,
   chat, calendar, contacts, Wiki, Kanban, and AI with an operator-supplied provider key.
5. Verify SMS/TOTP MFA, tenant isolation, encryption, background processing, backup, restore,
   restart, and upgrade using disposable infrastructure and synthetic data.
6. Replace hosted-only demonstration and test scripts in the public tree with applicable
   self-hosted checks. No test may default to real production endpoints or customer data.

**Gate:** an operator unfamiliar with the project completes installation and core email/file
flows using only the docs; restart preserves data; restore and supported upgrades are demonstrated.

#### P3 — Extract and enforce Pro capabilities

For each feature, first enumerate its routes, service code, jobs, queries, storage objects,
frontend components, and basic-security dependencies. Move only the commercial portion to `ee/`.

Recommended feature order: branding; organization AI agents; recording/transcription; audit and
retention administration; organization-wide security policy administration; assisted bulk migration.
The order may change if the inventory shows a smaller independent dependency boundary.

For every feature:

1. Place commercial code under the appropriate Enterprise license and build boundary.
2. Wire explicit dependencies through the existing Enterprise registration point. Keep
   transport, business logic, and database access separated; do not introduce speculative APIs.
3. Enforce capability and tenant authorization on the server, including worker jobs, callbacks,
   direct URL access, exports, and storage operations. Frontend hiding is insufficient.
4. Move UI components to `frontend/src/ee`, using the existing shell and complete translations.
5. Return a stable translated error contract when unavailable; show a clear edition state.
6. Test Community builds, `pro` builds without a key, valid permitted/forbidden features,
   expired/malformed keys, grace boundaries, seat overage, restart, and cross-tenant access.
7. Apply the approved downgrade policy: retain customer data and safe access. Basic individual
   MFA, core authorization, ordinary migration, and operational security logs remain available.

**Gate:** every advertised Pro feature is physically separated and demonstrably enforced; no
feature is sold solely because a UI switch exists. Pro expiry never disables Community itself.

#### P4 — Remove deployment branding and finish localization

1. Classify domains and names: configurable deployment values, legitimate project/license
   identity, third-party attribution, and test data. Do not replace all occurrences blindly.
2. Remove production endpoints and brand-domain defaults from runtime code, native clients,
   CORS, email templates, DNS helpers, Vite, compose, and maintenance scripts.
3. Make self-hosted public URLs, mail hostnames, sender/support identity, and permitted branding
   settings explicit. Gate white-label capabilities according to the approved table.
4. Inventory hardcoded UI and native Rust strings; do not assume the earlier count of about
   fourteen is exhaustive. Check every supported locale, interpolation, plurals, dates, and tone.
5. Review OnlyOffice customization and all third-party attribution/distribution obligations
   against the pinned versions before selecting the release default.

**Gate:** a non-Beskos test deployment completes core flows without contacting Beskos production;
all new user-visible text uses the localization system; brand and license notices remain correct.

#### P5 — Security and quality release gate

1. Review the threat model for public signup/initialization, sessions, MFA, tenant isolation,
   JMAP credentials/proxying, webhooks, links, uploads, office callbacks, and license enforcement.
2. Audit tenant scoping and resource ownership. Document intentional cross-tenant maintenance
   in the private edition instead of giving the scanner a broad exception.
3. Re-run secret scanning on the actual publication inputs and outputs. Verify rotation of the
   previously identified live credential and exposed operator password before further release;
   deletion from a current tree does not establish revocation.
4. Triage dependency findings and report-only security jobs with evidence and explicit release
   disposition. Add focused fuzz/integration tests for parsers and security-sensitive boundaries.
5. Run Go formatting/vet/tests/lint, frontend build/tests, Rust checks, language/register rules,
   documentation checks, and both backend editions on the exported tree as well as the working tree.
6. Conduct the approved external review/pentest and retest actionable findings.

**Gate:** no unresolved critical security defects; other findings have an approved disposition;
secret-rotation and tenant-isolation evidence is recorded; release checks actually block failure.

#### P6 — Documentation and contributor readiness

1. Align README, architecture, Community/Pro matrix, setup, configuration, upgrade/restore,
   troubleshooting, API documentation, and changelog with measured behavior.
2. Remove stale references to private documents and unsupported routes, APIs, or commands.
3. Document feature downgrade behavior, supported versions/platforms, support channels,
   contribution process, vulnerability reporting, and source-available terminology.
4. Finalize CLA text with legal review, then wire contribution checks; do not claim a CLA bot
   is active before it has been tested. Verify maintainers/CODEOWNERS and issue/PR templates.
5. Keep internal planning in this already-excluded document. Public roadmap material must be
   separately curated. A hosted documentation site is optional for the first release if the
   repository documentation is complete and usable.

**Gate:** an external installation review succeeds; public links and examples work; contributor
and security-reporting flows have been tested without requiring private knowledge.

#### P7 — Reproducible releases and supply-chain artifacts

1. Build only the sanitized public source for public binaries/images. A tag in the private
   working repository must not accidentally package Cloud code for the public destination.
2. Reconcile GoReleaser build tags with the chosen single Pro-capable distribution and optional
   Community-only binary. Test runtime behavior in the artifacts themselves.
3. Generate version and conversion-date metadata from an immutable release record. Do not
   reset a version's conversion clock when rebuilding it. Validate licensed-work identification
   and every packaged license/notice, including the Enterprise boundary and third-party assets.
4. Produce the supported architecture images and binaries; attach checksums, SBOM, signatures,
   provenance, migration notes, and source references. Verify artifacts with independent commands.
5. Configure registry/release permissions with minimum scope. Use tested signing identity and
   key handling; select tooling/providers before adding dependencies or credentials.
6. Run a draft release rehearsal; install its images on clean infrastructure and upgrade from
   the supported previous version. Define edge versus stable support and retention.

**Gate:** a release candidate is reproducible, verifiable, installable, and contains the correct
edition and license material; release automation produces a complete draft without manual repair.

#### P8 — Private Pro licensing and commerce

1. Implement key issuance and renewal in Cloud with protected signing material, auditable
   issuance, rotation, and the approved organization/seat/expiry semantics.
2. Complete purchase, invoice/tax treatment, key download, subscription changes, cancellation,
   refund, and renewal flows under the approved commercial terms.
3. Make webhooks idempotent and recoverable after partial failures. Reconcile entitlement
   state instead of treating every database failure as a duplicate event.
4. Test purchase through activation on an isolated self-hosted instance, offline operation,
   expiration, grace, seat changes, and restoration after renewal.
5. Publish accurate support/SLA terms only when the service can fulfill them.

**Gate:** the commercial journey works end to end and key verification uses the intended issuer;
no hosted outage prevents ordinary Community use. Required before selling Pro, not automatically
before an explicitly approved Community-only launch.

#### P9 — Legal, operational, and publication readiness

1. Counsel reviews the actual BUSL parameters, Enterprise terms, competitor commercial license,
   CLA, licensor identity, contributor rights, and third-party compatibility/attribution.
2. Resolve trademark and approved marketing terminology; verify obligations for bundled or
   modified components rather than assuming official images remove all obligations.
3. Confirm credential rotation, repository visibility, security settings, branch protections,
   maintainers, incident handling, backups, and rollback ownership.
4. Select a release candidate and freeze its source, artifact hashes, licenses, migrations,
   public README, prices, and supported feature claims.
5. Rehearse destination updates without transferring private history; review public and private
   file manifests separately. Obtain authorization for any destructive destination-history update.

**Gate:** a concrete release packet is ready for publication review. Pending legal/commercial
work is explicitly identified; it is not marked complete because technical CI passed.

#### P10 — Publish and operate

1. Publish the reviewed source/artifacts to the intended repositories using the approved
   history strategy. Verify destination visibility, heads, manifests, signatures, and links.
2. Smoke-test the published installation path and confirm the private service's behavior.
3. Publish the approved announcement and pricing/support pages; no unsolicited outreach or
   messages to third parties are implied by this engineering plan.
4. Watch the initial release under an explicitly configured operational process; define
   incident thresholds, hotfix ownership, rollback, and the next maintenance release.

**Gate:** users can obtain, verify, install, upgrade, and report issues against the released
version; no Cloud source or private history is exposed; operations have a tested recovery path.

### 9.5 Acceptance evidence matrix

| Scenario | Required evidence |
|---|---|
| Public source boundary | File manifest, generated-code scan, dependency inventory, secret scan, bundle/image inspection |
| Community build | Build/tests without `pro`; no Cloud packages or premium implementation linked |
| Pro-capable build without key | Community behavior; direct premium operations rejected consistently |
| Valid Pro key | Only granted capabilities enabled; tenant ownership and roles still enforced |
| Expired/invalid/over-seat license | Approved downgrade and recovery behavior; data and basic security preserved |
| Cloud edition | Hosted billing, Manager, SES, and metering regression tests; production-like isolated smoke test |
| Fresh public installation | Migration from an empty database, owner setup, login, send/receive, core collaboration flows |
| Existing public installation | Upgrade from the actual supported published snapshot, with preserved data and migration ledger |
| Recovery | Backup/restore drill, interrupted migration recovery, worker restart, storage failure behavior |
| Release artifacts | Runtime edition verification, licenses/dates, checksums, SBOM, signature verification, provenance |

Unit-test success cannot substitute for the migration, browser, live-service, or artifact rows.
Skipped integration tests must be listed with their missing prerequisites and rerun for the
release candidate where required.

### 9.6 Sequencing, ownership, and completion

Technical sequence: **P0 → P1 → P2 → P3 → P4 → P5 → P6 → P7**. Legal and commercial decisions
must be available at the relevant gates; **P8** follows the stable entitlement design, and
**P9 → P10** completes the selected launch scope. No implementation resumes merely because this
plan has been written: the user's current request is planning first.

The engineering work can produce code, tests, documentation, and release candidates. The user
owns structural/product decisions and authority over repository publication, production rollout,
spending, and external engagements. Counsel and an approved security reviewer supply their
respective external assessments. No dates or budgets are treated as agreed in this plan.

Estimate each review unit after P0 and the migration/composition decisions. The older 16–20-week
estimate is historical; neither it nor a percentage complete accurately represents the current
unreviewed working tree. Track completion by accepted evidence, not by changed-file counts.

Overall completion requires the selected release scope to satisfy all applicable gates, the
private service to retain its supported behavior, and all publication claims to match the actual
released artifacts. A successful local export is an intermediate deliverable.
