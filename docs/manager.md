# Manager — Beskos SaaS Operator Console

> Status: **design approved for kickoff** (2026-07-22). Living document, follows the laws in
> [docs/engineering-standards.md](engineering-standards.md). Code, logs and `error_code` in English (law 4.6).
> One phase at a time (§9): do not start M(n+1) before M(n) is tested.

### Losing access to the console (recovery)

`MANAGER_BOOTSTRAP_EMAIL`/`PASSWORD` only create the first operator **when the table is empty** —
changing the password in the environment after that has no effect at all. To get back in:

1. In the `manager-api` app, confirm `MANAGER_BOOTSTRAP_EMAIL`/`MANAGER_BOOTSTRAP_PASSWORD` and
   add `MANAGER_BOOTSTRAP_RESET=1`.
2. The app restarts: the password is reset and the second factor is cleared (it re-enrolls on the
   next login). Startup logs `manager bootstrap: RESET applied to …`.
3. **Remove `MANAGER_BOOTSTRAP_RESET`** — while it is set, every restart resets the password.

If the given email does not exist as an operator, it is created as `admin`.

## 1. What it is

An **internal SaaS operations** application — used by US (the platform operator), never by
customers. Manages plans, tenants, users, resources, cost and revenue across all organizations.
Fully separate from the product: a different app, a different API, a different auth, a different
origin.

### Structural decisions (from the user, 2026-07-22)
1. **Frontend**: a new **Next.js + shadcn/ui** app (`manager/`), separate from the webmail
   (Vite/React).
2. **API**: its own admin service — a new Go binary, **`cmd/manager-api`**, that reuses
   `internal/db` and the shared packages, but with **its own routes, sessions and port** (real
   isolation: a bug in the product never exposes the manager, and vice versa).
3. **Mandatory TOTP login** — no exceptions, from the very first user.
4. **Meet recording by plan** (decision from this session):
   - entry plan: **no recording**;
   - paid plans: **Track Egress + offline assembly** as the default (raw tracks come out almost
     free during the meeting; a job assembles the mp4 with ffmpeg off the hot path — "recording
     ready in ~10 min"); audio + AI transcription included;
   - top plan: priority in the assembly queue (and, if it's ever needed, live Room Composite as an
     add-on — NOT needed today).

## 2. Architecture

```
manager.beskos.com (Caddy) ──► manager (Next.js + shadcn)  ──► manager-api (Go, own port)
                                                               │
                                        Postgres (same cluster, own SQL role) ─ billing ─ R2 ─ SES
```

- **`cmd/manager-api`** (Go/chi, like api): its own auth (`manager_users` table), its own
  sessions, **mandatory TOTP** (reuses `internal/totp` + `secretbox`), login rate limiting, and an
  **audit log for every write action** (`manager_audit`). Never shares cookies/sessions with the
  product.
- **Dedicated SQL role** with the minimum required: broad read access for dashboards; write access
  only to the manager's own tables (`plans`, `tenant_plans`, `manager_*`) and the knobs it
  controls.
- **Deploy**: on the core node (its own `deploy/manager/` compose file), behind Caddy on its own
  subdomain; optional IP allowlist in Caddy (config, not code).
- **Next.js**: simple SSR/static, calls to manager-api via fetch with the session token; shadcn
  for UI; i18n not mandatory (internal tool — English is enough; still keep strings centralized so
  it doesn't hurt later).

## 3. Data model

### 3.1 `plans` — plan catalog
| column | type | note |
|---|---|---|
| `id` uuid pk / `key` text unique | | e.g. `essential`, `pro`, `business` |
| `name`, `description` | text | display |
| `price_month_cents`, `currency` | int, text | price per user/month |
| `min_users`, `max_users` | int | **requested**: minimum/maximum organization size (0 = no ceiling) |
| `apps_enabled` | jsonb | array of allowed rail apps (`mail`,`chat`,`kanban`,`cal`,`files`,`contacts`,`meet`,`wiki`) |
| `storage_gb_per_user` | int | storage quota (tenant pool = users × value) |
| `ai_tokens_per_window` | int | 6h window quota (the engine already applies it) |
| `meet_max_participants` / `meet_max_minutes` | int | room limits (0 = unlimited) |
| `recording` | text | `none` \| `offline` (track egress + assembly) \| `offline_priority` |
| `recording_retention_days` | int | recording retention |
| `email_daily_per_user` | int | SES sending ceiling |
| `onlyoffice_edit` | bool | office editing (false = read-only) |
| `wiki_public_links` | bool | public wiki links |
| `active`, `sort`, timestamps | | deactivated plans do not accept new tenants |

### 3.2 Tenant→plan link — **recorded divergence (M2, 2026-07-22)**
The product ALREADY had a catalog in code (`internal/billing/plans.go`) and the link via
`tenants.plano` (used by the billing webhook). M2 followed reality (law 6.4): the `plans` table
(key = `key`) replaced the in-code catalog as the source of truth (the code stays as
fallback/seed), and the link is still `tenants.plano` — we did **not** create `tenant_plans`.
Per-tenant seats/overrides/trial are deferred until billing needs them. Other adaptations: storage
is a **per-tenant pool** (`max_storage_gb`, the product's existing model), not per user; currency
is global (`BILLING_CURRENCY`), not per plan.

### 3.3 Extra attributes suggested (analysis)
- **`trial_days`** on the plan + trial state on the tenant — this is where the conversion funnel
  is measured.
- **`annual_discount_pct`** — discounted annual billing (billing already distinguishes periods).
- **`extra_seat_price_cents` / `extra_storage_gb_price_cents`** — explicit *overage* instead of
  blocking: a customer who exceeds storage pays the excess instead of losing access (less churn,
  more revenue).
- **`guest_meet` bool** — external guests in Meet (link, no account) as a plan differentiator.
- **`api_access` bool + `webhooks` bool** — once we open a public API, this is already a plan
  knob.
- **`custom_domains` int** — number of email domains per tenant.
- **`audit_retention_days`** — audit/version retention (compliance sells on Business).
- **`support_sla`** text — `community|standard|priority` (shown on the site and on the ticket).
- **`dedicated_ip_sending` bool** — dedicated SES IP as a Business add-on.

### 3.4 Manager's own tables
- `manager_users` — email, password_hash (argon2/bcrypt, like the product), encrypted
  `totp_secret` (secretbox), `totp_enabled NOT NULL DEFAULT true` (mandatory), role
  (`admin`|`viewer`), last_login.
- `manager_sessions` — token hash, short expiry (8h), IP/UA.
- `manager_audit` — who, when, action, target (tenant/plan), before/after payload (jsonb). Every
  write.

### 3.5 Usage metering
- `tenant_daily_usage` (daily rollup by a worker job): `tenant_id, day, users_active,
  storage_bytes, mail_sent, meet_minutes, recordings_count, recordings_bytes, ai_tokens,
  wiki_pages, cards, files`.
- Sources: Postgres aggregates (data already exists: `assistant_usage`, storage via
  `storage_files`, recordings, etc.), SES metrics (via SNS/CloudWatch), and R2 (usage API). The
  rollup freezes the day — dashboards read from the rollup, not from heavy live queries.

## 4. Enforcement — where each knob plugs into the product (map)

| Plan knob | Existing point in the code |
|---|---|
| `apps_enabled` | `tenant_disabled_apps` (app gating + AI already cuts off tools/agents) — the manager writes the complement |
| `ai_tokens_per_window` | assistant's 6h quota (`assistantQuotaStatus`) — now reads the value from the plan |
| `storage_gb_per_user` | `mail_users.quota_bytes` + upload check in Files |
| `max_users`/`min_users` | invite/member-creation handler (refuses above the ceiling; `min_users` validates at subscription/checkout) |
| `recording` | `meetRecordStart` handlers (refuses/chooses egress mode per plan) |
| `email_daily_per_user` | **done (07/22)**: the plan's `max_daily_send` enforced across the 3 sending paths (outbox create+worker, JMAP proxy, assistant); counted per recipient in `tenant_mail_sent` |
| `onlyoffice_edit` | OnlyOffice session config (view/edit mode) |
| `recording_retention_days` | worker cleanup job (deletes from R2 + record) |

Golden rule: the product **reads** the knobs (join `tenant_plans`→`plans` + overrides, cached);
the manager **writes** them. No route in the product writes to `plans`.

## 5. Dashboard and reports

### 5.1 Dashboard (manager home)
- **Cards**: tenants (total, active, in trial, 30d churn) · users (total, active 7d/30d) · total
  storage (GB and estimated R2 €) · **revenue** (MRR, ARR, ARPU) · **cost** (fixed VPS + estimated
  variable SES/R2/AI) · **margin** (revenue − cost) · emails sent 30d · Meet minutes 30d · AI
  tokens 30d.
- **Charts** (daily rollup): MRR over time, new tenants vs. churn, cumulative storage, usage per
  app (DAU per app — shows which apps drive retention).

### 5.2 Reports
- **Per tenant**: full usage (users, GB, minutes, tokens, emails), estimated cost vs. revenue →
  **margin per tenant** (identifies loss-making customers), plan/overrides, payment history.
- **Growth**: monthly cohorts, trial→paid conversion, churn and reason (field on cancellation).
- **Capacity**: storage/CPU projection vs. current infra (warns when to scale the VPS).
- CSV export on all of them.

### 5.3 Management (CRUD)
- **Plans**: create/edit/deactivate; simulator ("with N users this plan costs/yields X").
- **Tenants**: view, change plan, apply override, suspend/reactivate, start trial, impersonate
  NO (out of scope — support auditing is a separate conversation).
- **Manager users**: invite an operator (TOTP setup on first login, mandatory), revoke.

## 6. Security
- Origin, sessions and users 100% separate from the product; mandatory TOTP; aggressive login rate
  limiting; 8h sessions; full write audit log; least-privilege SQL role; optional IP allowlist in
  Caddy. `error_code` in English, as in the product.

## 7. Phases (one at a time)

- **M0 — Skeleton + auth**: `cmd/manager-api` (login + mandatory TOTP, sessions, audit), Next.js
  app with shadcn (login, shell, navigation), compose + Caddy deploy. Testable: log in with TOTP.
- **M1 — View (read-only)**: list of tenants and users with basic numbers (counts, storage per
  tenant via direct query), v1 dashboard with static cards.
- **M2 — Plans**: `plans`/`tenant_plans` tables, CRUD in the manager, plan assignment to tenant;
  **enforcement of the first 3 knobs** in the product (apps_enabled, max_users, ai_tokens).
- **M3 — Metering**: daily `tenant_daily_usage` rollup in the worker; dashboard now reads from the
  rollup; remaining knobs (storage, recording, email limit, onlyoffice, retention).
- **M4 — Revenue and cost**: billing integration (MRR/ARR/ARPU/churn), costs (configurable fixed +
  estimated variable per driver), margin per tenant.
- **M5 — Reports**: cohorts, capacity, CSV exports; polish.

## 8. Risks
- **Two frontend stacks** (Vite in the product, Next in the manager): accepted by the user; keep
  the manager small and without sharing components with the product (a deliberate copy beats
  coupling).
- **Knobs without enforcement** become a commercial lie — each knob only appears in the manager UI
  once the corresponding enforcement is live in the product (the table in §4 is the contract).
- **Estimated cost ≠ actual invoice** — always mark estimates as estimates; reconcile monthly
  against actual invoices (R2/SES) in an adjustment field.

## How to run (dev)
- **API**: `docker compose up -d --build manager-api` (in `deploy/`; port 127.0.0.1:8091). The
  first operator comes from `MANAGER_BOOTSTRAP_EMAIL/PASSWORD` in `.env` (only while the table is
  empty).
- **Frontend**: `cd manager && npm run dev` (localhost:3000; Next proxies `/api/manager/*` to the
  API — `MANAGER_API_URL` to point at another host).
- 1st login: password → the page shows the `otpauth://` link + secret → add it to the
  authenticator → confirm with the code. Subsequent logins always require the code (anti-replay
  per time step).

## Implementation progress
- [x] **Usage analytics + alerts** *(2026-07-22): UI rebuilt to faithfully match shadcn's
  reference dashboard (Inter font, team switcher, Search that filters tenants, tabs). REAL period
  filter (7/30/90/365 days) drives charts and export. Analytics tab: total storage, AI tokens/day,
  Meet minutes/day, emails/day (the series gained `mail_sent`), top AI users in the last 24h and
  top cost per tenant. `GET /alerts` (60s refetch in the UI): `users_cap` (≥90% warn / critical
  ceiling), `storage_cap` (80/95%), `mail_cap` (80/100% of the daily ceiling), `ai_spike` (today
  ≥3× the 7-day average, floor 50k) and `ai_user_heavy` (ONE user ≥100k tokens/24h) — no new
  state, the alert disappears once the condition clears. Tested live: all 4 types fired (a real 4×
  spike). Statistics backlog recorded: retention/churn cohorts (real billing), DAU per app, SES
  deliverability (bounces via SNS), trial→paid funnel, NPS.)*
- [x] **Per-server cost (individual VPS)** *(2026-07-22: migration 00108 `manager_servers` seeded
  with the consulting's OVH layout (vps-core €26 + vps-media €26 + vps-mail €13 = €65) and
  `fixed_month_cents` zeroed (becomes "other fixed" — domains/licenses/backups). CRUD
  `GET/PUT/DELETE /servers` + pause/reactivate (admin-only, audit); finance's fixed cost = sum of
  ACTIVE servers + other fixed, with a breakdown card. UI: "Servers" card in Finance (table with
  total + create/edit Sheet). Tested live: €65 seed; add a €9 backup → €74 fixed; pause → €65;
  remove works.)*
- [x] **Tenant-specific MAIL FROM** *(2026-07-22: SES requires the MAIL FROM to be a subdomain of
  the identity — a shared `mailer.beskos.com` is impossible; implemented the per-tenant
  equivalent: at provisioning time, `PutEmailIdentityMailFromAttributes` configures
  **`mailer.<domain>`** (falls back to USE_DEFAULT_VALUE if the MX is missing), and the zone gains
  2 automatic records — MX 10 `feedback-smtp.<region>.amazonses.com` + subdomain SPF TXT —
  classified as `mailfrom` under the drawer's "Send" group. Benefit: SPF+DKIM alignment under
  DMARC (Gmail/Yahoo 2024+) and an envelope carrying the customer's own brand. Tests: a machine
  with a fake relay verifies the mail-from + records; NOT tested against real AWS.)*
- [x] **SES deliverability (bounces/complaints)** *(2026-07-22): migration 00107 `mail_events`;
  public webhook `POST /api/webhooks/ses` — SNS with **verified signature** (cert only from
  `*.amazonaws.com`, SHA1/SHA256, cert cache; `internal/ses/sns.go` with tests for a
  valid/tampered/malicious-cert signature), confirms the subscription on its own, resolves the
  tenant by sender domain and records 1 event per recipient. Manager: `GET /deliverability` (sent
  30d vs. bounces/complaints) + table in Analytics; `bounce_rate` alerts (warn ≥5%, crit ≥10%) and
  `complaint_rate` (warn ≥0.1%, crit ≥0.5%) — AWS thresholds; samples <50 sends produce no rate.
  AWS setup documented in the handler: SNS topic → HTTPS subscription
  `<PUBLIC_URL>/api/webhooks/ses` → identity/configuration-set notifications in SES. Tested live:
  403 without a signature; 7% bounce → warn; 0.5% complaint → critical. Dashboard charts rebuilt
  with **Recharts** (the same one from the shadcn example: maxBarSize, tooltip, clean axes — no
  distortion with few points).*
- [x] **Post-M5 — offline recording pipeline + Stripe** *(2026-07-22):*
  - *Recording (§1 decision implemented): migration 00104
    (`recordings.mode/assembly_status/priority` + `recording_tracks`); offline/offline_priority
    plans start a **Track Egress per published track** (late ones come in via the
    `track_published` webhook); stopping enqueues assembly; the worker composes the MP4 with
    **ffmpeg** (xstack 640×360 grid + amix, offsets via adelay/tpad, queue with
    `FOR UPDATE SKIP LOCKED`, priority for offline_priority, orphaned processing >1h revives it;
    raw tracks deleted after success). ffmpeg/ffprobe were added to the backend image.
    RoomComposite remains as a fallback for legacy plans. **Proven live** with 3 synthetic tracks
    → an 8s MP4 assembled in 1s, row ready, raw cleaned up.*
  - *SES relay + metering + limit (2026-07-22): migration 00105 `tenant_mail_sent` (counts
    RECIPIENTS, the way SES bills); the plan's `max_daily_send` enforced across the 3 paths —
    outbox creation (429 `mail.daily_limit_reached`, i18n pt/en), worker at actual send
    (revalidates + counts), JMAP proxy (intercepts `EmailSubmission/set`; optimistic counting) and
    the assistant; the `mail_sent` rollup is now real; SES relay configured on the Stalwart
    registry (not in a file — see deploy/stalwart/README.md) (commented out in dev, instructions
    to enable in production; `SES_SMTP_USER/PASS` envs in compose). Proven live: saturated
    counter → 429; 7/5000 → 201; rollup reflected 7.*
  - *Automatic per-domain SES identity (requested 2026-07-22): `internal/ses` (a custom SESv2
    client with its own SigV4, no SDK; golden signature test); the provisioning state machine
    gained `EmailRelay` — when provisioning a domain, it registers the identity in SES (Easy DKIM,
    idempotent), and the published zone gains SES's 3 DKIM CNAMEs + `include:amazonses.com` in
    SPF + DMARC (`AugmentZoneForSES`, tested); `dns_verified` only fires once SES itself confirms
    DKIM. Configured via `SES_REGION` + `AWS_ACCESS_KEY_ID/SECRET` (compose). NOT tested against
    real AWS (no credentials in dev); the signature and the state machine are covered by tests.*
  - *Domain removal + DNS UI (2026-07-22): `DELETE /api/domains/{id}` (PermDomainsManage; refuses
    with a 409 `domains.has_mailboxes` if the domain has mailboxes; deletes from Stalwart, removes
    the SES identity best-effort — `EnableSES` in the api —, row deletion last; audit). Domain
    drawer UI: DNS records **classified by purpose** (`purpose`: mx/spf/dkim/dkim_ses/dmarc) and
    grouped into "Receive" and "**Send (required)**" with a warning, a per-record copy button, and
    a danger zone with name confirmation (no native dialogs). i18n pt/en. Proven live: 409 with a
    mailbox → 200 without; idempotency (2nd delete → 404); real Stalwart zone correctly
    classified.*
  - *Stripe: checkout now uses the **DB catalog** (price/name edited in the manager; it used to
    read the in-code catalog — would have diverged), validates the active plan + user ceiling, and
    charges at least the plan's floor; `customer.subscription.updated` syncs only the status
    (past_due etc.); `applyPlan`/webhook validate against the DB. i18n `billing.plan_too_small`
    (pt-BR+en).*
- [x] M0 — skeleton + TOTP auth *(2026-07-22: migration 00100
  (`manager_users/sessions/audit`), `internal/managerapi` + `cmd/manager-api` (single-endpoint
  login with forced enrollment, 8h sessions, IP+account rate limiting, anti-enumeration by timing,
  full audit), `manager/` app: Next 16 + shadcn (3-state login, shell with navigation, proxy.ts),
  `manager-api` service in compose. Tested live: enrollment, session, code anti-replay, logout
  revokes server-side, audit with 3 lines.)*
- [x] M1 — read-only view *(2026-07-22: `/overview`, `/tenants`, `/tenants/{id}` endpoints
  (direct queries with counts + storage as a sum of live `storage_files`); dashboard with real
  totals (tenants/active, users/active 7d, storage, domains; revenue "—" until M4), tenant table
  with search and a detail view with users (role, MFA, storage, quota, last seen). Formatting via
  `Intl` (format.ts). Tested live with 2 real tenants/4 users.)*
- [x] M2 — plans + initial enforcement *(2026-07-22: migration 00101 (`plans` seeded with the 4
  plans from the code, same limits); the product reads the catalog from the DB with a 60s cache +
  fallback (`plans_catalog.go`); live enforcement: `apps_enabled` (union with the tenant's
  disabled_apps — applies to both the rail AND the AI's tools/agents), `max_users` (the existing
  mailbox gate now reads from the DB), `ai_tokens_per_window` (6h quota per plan), Meet ceiling per
  plan likewise; manager: plan CRUD (PUT upsert + activate/deactivate, admin-only, before/after
  audit) and plan assignment on the tenant detail page; UI: a Plans page with a side Sheet for
  editing (app checklist, recording select, switches). Tested end to end: new tenant → manager
  assigns growth without kanban → the product's /api/apps returns kanban disabled; AI quota
  reflects the plan's 500k.)*
- [x] M3 — metering + remaining knobs *(2026-07-22: migration 00102 `tenant_daily_usage`
  (PK tenant+day); the worker runs an idempotent HOURLY rollup of today+yesterday
  (`cmd/worker/metering.go`) — users/active, storage, files, AI tokens, Meet minutes (joined→left
  sessions), recordings, wiki pages, cards; `mail_sent` stays 0 until the SES relay measures it
  (§4). Recording retention per plan on the same ticker (deletes the blob from MinIO, then the
  row; fail-safe ordering). New knobs in the product: `recording=none` → 403 on starting a
  recording; `onlyoffice_edit=false` → the editor downgrades to view; `wiki_public_links=false` →
  refuses to enable a public link AND stops serving existing ones (fail-closed on read). Manager:
  `GET /tenants/{id}/usage` + `GET /usage/series`; a 14-day usage table on the tenant detail page.
  `meet_max_minutes` and `email_daily` remain WITHOUT enforcement (they depend on the room timer
  and the SES relay — logged in the §4 table). Tested live: real rollup (Dev: 3 users/13MB/54k
  tokens/2 pages/4 cards; yesterday reconstructed).)*
- [x] M4 — revenue/cost/margin *(2026-07-22: migration 00103 `manager_settings` (key→jsonb) with
  the `cost_model` seeded from the consulting figures (fixed €65/month, storage 2c/GB-month, AI
  30c/1M tokens, email 10c/1k); `GET/PUT /settings/costs` (admin-only, before/after audit);
  `GET /finance` — MRR/ARR/ARPU as CONTRACTED revenue (plan price × users of active tenants;
  billable = current users, no minimum — the actual billed minimum comes in with real billing,
  noted here), estimated variable cost per driver (storage+recordings from the latest rollup, AI
  tokens and emails from the 30d rollup) and margin per tenant (negative shown in red). UI: a
  Finance page (6 cards + per-tenant table + cost-model editor) and the dashboard's Revenue card
  wired to real MRR. Estimate disclaimer on the page (§8). Tested live: MRR €15 (PlanTest/growth),
  AI cost 2c over 94k tokens, margin recalculates on editing the model.)*
- [x] M5 — reports/exports *(2026-07-22: a Reports page with monthly growth (new + cumulative
  tenants/users via `generate_series`) and 3 CSV exports (tenants, 30d usage, finance — direct
  links carrying the session cookie); the dashboard gained 2 mini SVG charts (active users/day and
  AI tokens/day, 30d, no charting library); the Operators page completes §5.3 — list, create
  (Sheet; temporary password ≥12 chars; TOTP completed on first login), reactivate/deactivate
  (deactivating kills the sessions; cannot deactivate yourself), all admin-only with audit. Churn
  cohorts are deferred to real billing (no cancellation events yet). Tested live: growth 2026-07 =
  3 tenants/5 users; 3 correct CSVs; a viewer operator created and self-deactivation refused.)*
