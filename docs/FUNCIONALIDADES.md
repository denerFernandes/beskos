# Beskos — Product features

Living document listing **all** of Beskos's features: a multi-tenant platform for
e-mail, storage, and productivity on top of [Stalwart](https://stalw.art) (used without modification,
only via JMAP — see [deploy/DIVERGENCES.md](../deploy/DIVERGENCES.md)).

**Status legend**

| Status | Meaning |
|:---:|---|
| ✅ | Implemented **and tested** (unit + real integration/e2e) |
| 🎨 | **Prototyped and validated** in the UI (navigable prototype), awaiting implementation |
| 📋 | **Planned** (scope agreed, not yet built) |

Visual reference: interactive prototype of the full UI (artifact "ui-completa").

---

## 1. Platform and architecture

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 1.1 | Multi-tenancy (organizations isolated by `tenant_id`) | ✅ | Isolation in a shared database; every query filters by `tenant_id` |
| 1.2 | Stalwart e-mail engine via JMAP (no modification to the binary) | ✅ | Typed Go client `internal/stalwart`; AGPL preserved |
| 1.3 | Control plane in Go (REST API + workers) | ✅ | `cmd/api` (chi) + `cmd/worker` |
| 1.4 | Same-origin reverse proxy (SPA + JMAP + auth) | ✅ | Dev: Vite; Prod: `deploy/Caddyfile`. Mandatory (Stalwart has no CORS) |
| 1.5 | Tenant → Stalwart instance routing (horizontal scaling) | 📋 | `stalwart_instance` field already exists in the schema |

## 2. Customer provisioning

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 2.1 | Idempotent, resumable state machine | ✅ | `pending → domain_created → dkim_created → dns_pending → dns_verified → active` |
| 2.2 | Create domain in Stalwart | ✅ | `x:Domain/set` via JMAP |
| 2.3 | Automatic DKIM generation | ✅ | Automatic on create; polls until the record appears |
| 2.4 | DNS records screen for the customer to publish | ✅ | Parsed from `dnsZoneFile` (MX/SPF/DKIM/DMARC/MTA-STS/SRV) |
| 2.5 | Automatic DNS verification | ✅ | Worker polls for real resolution |
| 2.6 | Mailbox creation (individual) | ✅ | `x:Account/set`; initial `postmaster` mailbox on activation |
| 2.7 | Initial Stalwart bootstrap | ✅ | `cmd/bootstrap` (single step + restart) |

## 3. Admin panel (our SaaS)

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 3.1 | Organization signup + login | ✅ | HttpOnly cookie session, bcrypt password |
| 3.2 | Language preference persisted per user | ✅ | `pt-BR` / `en`; browser as default |
| 3.3 | Domain management (add, view state, DNS) | ✅ | Tenant ownership check before every operation |
| 3.4 | Mailbox management (create) | ✅ | Only on an active domain |
| 3.5 | Unified panel design (same system as the webmail) | 🎨 | Metric cards, domain/mailbox tables |
| 3.9 | UI: Members (RBAC), MFA, Billing/Usage, invite acceptance, 2-step login | ✅ | wired to the backend; permission-gated |
| 3.6 | **RBAC**: fixed roles + granular permissions | ✅ | owner/admin/member/billing (+internal); `internal/rbac`; security e2e |
| 3.7 | User **invitations** by e-mail | ✅ | create/accept/revoke; anti-escalation; last-owner protected |
| 3.8 | Audit log | ✅ | `audit_log` table written on sensitive operations |

## 4. Webmail (end user)

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 4.1 | End-user login (Basic auth behind the proxy) | ✅ | Separate from the panel login |
| 4.2 | List folders | ✅ | `Mailbox/get`; mapped by `role` |
| 4.3 | List and read e-mails (body, sender, date) | ✅ | `Email/query` + `Email/get` |
| 4.4 | Mark as read/unread | ✅ | `keywords/$seen` |
| 4.5 | Send e-mail | ✅ | `Email/set` + `EmailSubmission/set` |
| 4.6 | Attachments (view/download) | ✅ | Via `downloadUrl` same-origin |
| 4.7 | Full-text search | ✅ | `Email/query` with `filter.text` |
| 4.8 | **Rich-text editor** (bold, italic, lists, quote, link) | 🎨 | Compose with a formatting toolbar |
| 4.9 | Reply / forward with quote | 🎨 | Prototyped |
| 4.10 | Favorites / colored flags | 🎨 | Flags per tenant |
| 4.11 | Light/dark theme | ✅ | Theme persistence |
| 4.12 | UI: Files app (upload/download/share/delete) | ✅ | 5th app in the webmail, wired to storage |
| 4.13 | Contacts: groups (N:N), tags, search | ✅ | app in the webmail; `contacts`/`contact_groups`/membership |
| 4.14 | Monochrome icons (no emoji) in the webmail | ✅ | `currentColor` SVG set |
| 3.10 | Unified Users and Members (1 list per role) | ✅ | creating a user provisions a mailbox + account; the list merges both by e-mail |

## 5. AI assistant (any OpenAI-compatible endpoint)

> **Generic AI interface** (`internal/ai`): the wire protocol is the OpenAI-compatible chat API,
> which nearly every vendor and local runtime speaks -- OpenAI and Azure, Mistral, Groq, Together,
> OpenRouter, DeepSeek, TensorX, Ollama, vLLM, llama.cpp, LM Studio. Pointing Beskos at a different
> model is an address and a key (`AI_BASE_URL`, `AI_API_KEY`, `AI_MODEL`), not another client.
> The API key lives **only on the backend** (law §3.5/§10), never on the frontend. The `Assistant`
> layer is already implemented and tested; what remains is exposing the HTTP endpoints and wiring the UI.

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 5.1 | **Daily summary** (briefing) | 🎨 | Agenda + tasks + e-mails in one actionable paragraph |
| 5.2 | Summarize e-mail / thread | 🎨 | "AI Summary" card while reading |
| 5.3 | Write e-mail with AI (draft in your tone) | 🎨 | "Write with AI" drawer; "more formal/shorter" variations |
| 5.4 | Extract tasks from an e-mail | 🎨 | Generates tasks → adds to the Kanban |
| 5.5 | AI draft lands directly in the editor | 🎨 | "Use draft" opens compose pre-filled |

## 6. "Today" screen (daily briefing)

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 6.1 | Opens on the first visit of the day | 🎨 | Default view |
| 6.2 | AI daily summary (with greeting and date) | 🎨 | See 5.1 |
| 6.3 | Today's agenda | 🎨 | Click goes to the calendar |
| 6.4 | Pending tasks (with Today/Tomorrow due dates) | 🎨 | Click goes to the Kanban |
| 6.5 | E-mails that need attention | 🎨 | Click goes to the webmail |
| 6.6 | End-of-day summary | 📋 | Suggested |

## 7. Kanban (productivity)

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 7.1 | Board with status columns | 🎨 | To do / In progress / Waiting / Done |
| 7.2 | Drag cards between columns | 🎨 | Counters update |
| 7.3 | E-mail → task ("Turn into task") | 🎨 | Card created from the e-mail |
| 7.4 | Cards with flag, assignee, due date | 🎨 | Our own layer (Postgres), not JMAP |
| 7.5 | Add task manually | 🎨 | "+ Add" button |

## 8. Calendar and meetings

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 8.1 | Weekly view (monday.com style) | 🎨 | Colored events, "today" highlighted |
| 8.2 | Create event | 🎨 | "+ New event" |
| 8.3 | Schedule a meeting from an e-mail | 🎨 | "Schedule meeting" creates an event |
| 8.4 | Real integration via JMAP Calendars | 📋 | Stalwart 0.16 exposes `urn:ietf:params:jmap:calendars` |
| 8.5 | Meeting invitations (iMIP/ICS) | 📋 | Depends on 8.4 |
| 8.6 | Day/month views | 📋 | Week already prototyped |

## 9. File storage

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 9.1 | Upload/download over MinIO (S3) | ✅ | `internal/storage`; e2e against real MinIO |
| 9.2 | Dedup by hash, versioning | ✅ | blob = `tenant/sha256`; refcount on delete; version by name |
| 9.3 | Sharing (owner/editor/reader/link) | ✅ | `storage_shares`; validated levels |
| 9.4 | Resumable upload (multipart/tus) | 📋 | Today, standard upload up to 100 MiB |

## 10. Billing and quotas

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 10.1 | **Per-user** subscriptions (Stripe) | ✅ | price per seat; currency BRL/EUR/USD and minimum user count via `.env` |
| 10.5 | Google Workspace-style admin panel | ✅ | sidebar, monochrome icons, modals + drawers (no alert/prompt), Users section |
| 10.2 | Disk and mailbox quotas per tenant | ✅ | 402 enforcement (disk respects dedup); sending via Stalwart 📋 |
| 10.3 | Usage / billing panel | ✅ (API) | `GET /usage`, `GET /billing`, `POST /billing/checkout` |
| 10.4 | Idempotent webhooks | ✅ | Stripe signature verified + dedupe by `event_id` |

## 11. Security and authentication

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 11.1 | Stalwart admin credentials only on the backend | ✅ | Never reach the frontend |
| 11.2 | Tenant ownership check on every admin operation | ✅ | |
| 11.3 | Stable error codes (i18n) | ✅ | `error_code` + translation on the front end |
| 11.4 | **MFA — TOTP** (authenticator app) | ✅ | Plain RFC 6238 (`internal/totp`); + 10 recovery codes (single use) |
| 11.5 | **MFA — SMS** (external provider) | ✅ | Generic provider (`internal/sms`); dev LogSender, Twilio later |
| 11.6 | Rate limiting | 🟡 | MFA: verify limited to 5 attempts/challenge. Global (login/API) still 📋 |

## 12. Operations, compliance and i18n

| # | Feature | Status | Notes |
|---|---|:---:|---|
| 12.1 | Internationalization (pt-BR + en) | ✅ | Automatic key-parity guard |
| 12.2 | Send limits + per-tenant abuse detection | 📋 | Shared IP reputation |
| 12.3 | Reputation hooks / outbound IP pool | 📋 | `outbound_ip_pool` field in the schema |
| 12.4 | LGPD (data export/deletion, DPA, abuse@) | 📋 | Brazilian product |
| 12.5 | Observability (structured logs, metrics, alerts) | 📋 | |

---

## Implementation roadmap (agreed order)

1. ✅ **Phase 0** — Foundation (infra, JMAP client, skeletons, auth, i18n)
2. ✅ **Phase 1** — Provisioning (state machine)
3. ✅ **Phase 2** — Webmail core (read/send/search)
4. 🎨 **Full UI validated** — Today, AI, Kanban, Calendar, unified Admin *(prototype approved)*
5. ✅ **E-mail backend** — webmail session (end user against our API, validated on Stalwart),
   **AI** endpoints (summarize/draft/extract-tasks/briefing, any OpenAI-compatible provider), **Kanban**
   (boards/columns/cards + move), **flags**, and **calendar/events**. E2E green (`scripts/e2e-email-backend.sh`).
   *Still needs wiring into the React UI.*
6. ✅ **UI wired to the backend**: webmail (AI/Kanban/calendar/files) + panel (RBAC/MFA/billing/usage)
7. 📋 **Calendar via JMAP Calendars** (sync with Stalwart) — today it's our own layer
8. ✅ **RBAC** (roles + invitations) → ✅ **MFA** (TOTP + SMS + recovery, 2-step login)
9. ✅ **Storage** (MinIO) → ✅ **Billing/Quotas** (plans, quotas, idempotent webhook)
10. 📋 **Operations/LGPD/Observability**

_Rule (law §6.1): one phase at a time; don't start the next one before the previous is tested._
