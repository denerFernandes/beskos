# Beskos — multi-tenant email + storage platform

Control plane (Go) + webmail/panel (React) on top of **Stalwart** (email engine, used without
modification, only over the network). See [project.md](project.md) for the full brief.

> 📋 **Full feature list (status of each one):** [docs/FUNCIONALIDADES.md](docs/FUNCIONALIDADES.md)

> **Read before anything else:**
> - [CLAUDE.md](CLAUDE.md) — the **project laws** (simplicity, Go standards, files ≤600 lines,
>   separation of concerns, mandatory i18n). All code follows them.
> - [deploy/DIVERGENCES.md](deploy/DIVERGENCES.md) — the pinned version (Stalwart v0.16.4) **removed
>   the REST Management API** that brief §6 assumes; all administration goes through JMAP. Source of
>   truth for the integration.

## i18n

All UI text comes from i18n (law §4). Frontend: `react-i18next`, locales in
`frontend/src/i18n/locales/{pt-BR,en}/common.json` (default **pt-BR**, **en** mandatory).
API errors carry a stable `error_code`; the frontend translates it via the `server.<code>` key.

## Public export validation

Install `sqlc` v1.27.0, then run
`python3 scripts/export_public.py --check --private-only` to validate the operator console,
metering, and hosted billing boundary in a temporary public export. The export regenerates
database code and configuration documentation from the retained sources. Run without
`--private-only` for the full residue report, including remaining branding and comment findings.

`python3 scripts/export_public.py /path/to/empty-directory --private-only` creates a local
snapshot without Git history. The private `public-export.yml` workflow checks the boundary,
database generation reproducibility, backend vet, build, and tests with and without `pro`,
and the exported frontend build and tests. `--manager-only` remains an alias for existing tools.

The public snapshot omits hosted checkout, Stripe, VAT calculation, commercial signup screens,
daily usage rollups, and plan-based recording retention. It uses a `self_hosted` catalog without
commercial seat, storage, or sending limits; account access has no hosted trial deadline.
The complete working tree retains the hosted behavior. Its worker shares one storage client,
fails startup if configured storage cannot initialize, and preserves recording rows until blob
deletion succeeds. Metering logs repeated failures and recovery independently for each job.

Remaining work includes per-tenant SES, marketing endpoints, legacy billing schema cleanup,
Pro feature extraction, and a fresh full-stack deployment test. Passing the export checks does
not certify that this remaining separation is complete.

## Status

**Phase 0 — Foundation** ✅ · docker-compose (Stalwart v0.16.4 + Postgres + MinIO), typed JMAP
client + integration test, Go skeleton (`cmd/api`/`cmd/worker`, goose, sqlc), panel auth,
React skeleton with i18n.

**Phase 1 — Provisioning** ✅

| Component | Status |
|---|---|
| Provisioning state machine (`internal/provisioning`), idempotent/resumable | ✅ (5 unit tests with fakes) |
| Worker (`Runner`) processing non-terminal states in a loop | ✅ |
| `cmd/bootstrap` (initial Stalwart setup, §D3) | ✅ |
| API: `POST /api/domains`, `GET /api/domains/{id}` (state+DNS), `POST .../mailboxes` | ✅ |
| Frontend: add domain, view state + DNS records, create mailbox | ✅ (clean build) |
| End-to-end (real Postgres+Stalwart): domain → active → mailbox | ✅ `scripts/e2e-phase1.sh` |

State machine (brief §7, over JMAP — see [DIVERGENCES §D6](deploy/DIVERGENCES.md)):
`pending → domain_created → dkim_created → dns_pending → dns_verified → active` (`↘ error`).

**Phase 2 — Webmail** ✅

| Component | Status |
|---|---|
| Same-origin reverse proxy (dev: Vite; prod: `deploy/Caddyfile`) — mandatory (§D7) | ✅ green smoke test |
| Typed JMAP client (`src/jmap`): session, Mailbox, Email query/get, sending, search | ✅ green integration against real Stalwart |
| Webmail auth (Basic behind the proxy, `src/webmail/auth.ts`) | ✅ |
| Webmail UI (rail/list/reading/compose/search) in the approved design, i18n pt-BR/en | ✅ clean build |

Webmail lives at `/mail` (end-user login, separate from the panel). Design: see artifact "webmail-v1".
⚠️ Architecture: React does **not** talk directly to Stalwart (no CORS, §D7) — everything goes
through the same-origin proxy.

**Later phases (expanded scope)** ✅ — AI (summarize/write), Kanban, Calendar, Contacts,
Storage/Drive (folders, sharing, viewers), **office editing** (OnlyOffice:
edit/co-edit/create), **RBAC + invites**, **MFA (TOTP/SMS)**, per-user billing, and **Meet**
(LiveKit video calls: calling, day/week/month agenda, RSVP, in-app/email notifications, background
effects). See the project memory for the per-area detail.

Known pending items: **Meet recording** (Egress) only completes on a Linux host — see
[Meet — LiveKit/Egress networking](#meet-video-calling--livekitegress-networking); verify
**notification email** live (provision a `notify@` mailbox + `NOTIFY_*`).

## Layout

```
backend/    Go — control plane (API + worker)
  cmd/api           panel's REST/JSON API
  cmd/worker        workers (provisioning state machine — Phase 1)
  internal/stalwart Stalwart JMAP client (x:Domain/Account/DkimSignature/Bootstrap)
  internal/db       sqlc-generated code + migrations (goose, embedded)
  internal/{auth,httpapi,config}
frontend/   React — panel/webmail/admin/storage
deploy/     docker-compose, Stalwart config, DIVERGENCES.md
```

## Running (dev)

The whole backend runs in Docker; **only the frontend runs locally** (Vite proxies to the
compose services).

Prerequisites: Docker, Node 20+.

```bash
# 1. Infra + control plane (Postgres, Stalwart, MinIO, API, worker)
cd deploy
cp .env.example .env                 # adjust the secrets
docker compose up -d --build         # brings up EVERYTHING except the frontend

# 2. Bootstrap Stalwart — ONCE (creates the permanent admin; see DIVERGENCES §D3).
docker compose run --rm --entrypoint /usr/local/bin/bootstrap api
docker compose restart stalwart      # required for the bootstrap to take effect

# 3. Local frontend (proxy: /api → :8000, /jmap → :8080)
cd ../frontend
npm install
npm run dev                          # http://localhost:5173
```

Open: **http://localhost:5173** (panel) and **http://localhost:5173/mail** (webmail).
Create the organization in the panel, add a domain (auto-provisions with
`PROVISION_SKIP_DNS_VERIFY=true`), create a mailbox, and log into the webmail with it.

> AI is disabled without `AI_API_KEY` (endpoints respond 503); SMS uses `log` mode
> (the code shows up in `docker compose logs api`); billing uses `manual` mode.

## AI assistant

The assistant speaks the OpenAI-compatible chat API (`POST {base}/chat/completions`), so it works
with any endpoint that implements it: OpenAI and Azure OpenAI, Mistral, Groq, Together, OpenRouter,
DeepSeek, TensorX, and local runtimes such as Ollama, vLLM, llama.cpp or LM Studio.

| Variable | |
|---|---|
| `AI_API_KEY` | Enables the assistant. Empty = off, and the AI endpoints answer 503. A runtime with no authentication, such as Ollama, accepts any value. |
| `AI_PROVIDER` | A label for logs. `deepseek`, `tensorx`, `openai` and `ollama` also fill in the address. |
| `AI_BASE_URL` | Required for any provider that is not one of those four. |
| `AI_MODEL` | Required, except with `deepseek` and `tensorx`, which carry a default. |
| `AI_REASONING` | `on` lets the model reason before answering, where the endpoint supports it. |

```bash
AI_PROVIDER=openai  AI_MODEL=gpt-4o-mini  AI_API_KEY=sk-...
AI_PROVIDER=ollama  AI_MODEL=llama3.1     AI_API_KEY=unused        # nothing leaves the machine
AI_PROVIDER=groq    AI_BASE_URL=https://api.groq.com/openai/v1  AI_MODEL=llama-3.3-70b-versatile  AI_API_KEY=...
```

The wiki's semantic search uses a separate embeddings endpoint, configured the same way:
`EMBEDDINGS_API_KEY`, `EMBEDDINGS_BASE_URL` and `EMBEDDINGS_MODEL`, all three required together,
plus `EMBEDDINGS_DIMENSIONS` (default 1536, which must match the database column).

At startup the API logs `AI enabled (provider=…)`, `AI disabled (no AI_API_KEY)`, or
`AI MISCONFIGURED` followed by the variable that is missing.

## Meet (video calling) — LiveKit/Egress networking

Meet uses **LiveKit** (WebRTC SFU) + **Egress** (recording), running as network-only services.
The call itself works in dev; **recording needs network attention** because WebRTC
needs IPs/ports that are actually reachable.

**Dev (Docker Desktop / Mac).** `deploy/livekit/livekit.yaml` uses `node_ip: 127.0.0.1` so that
the ICE candidates are reachable from the **browser on the host** (Docker Desktop forwards the
published ports `7880/7881/7882`). The call connects normally.

> ⚠️ **Recording (Egress) does not complete on Docker Desktop for Mac.** Egress spins up a
> headless Chrome that needs to establish **its own WebRTC connection** to the SFU; Docker
> Desktop's loopback/UDP does not deliver that reliably, and Egress aborts with
> `Start signal not received`. Everything else (the Record button, the
> `StartRoomCompositeEgress` API, the `egress_ended` webhook, the upload to MinIO, the player)
> is ready — only the video capture doesn't close **on this dev machine**.

**Production (Linux host/VPS).** For the call **and** the recording to both work:

1. **Public/routable IP on LiveKit:** in `livekit.yaml`, replace `node_ip: 127.0.0.1` with
   `rtc: { use_external_ip: true }` (or pin `node_ip: <host's public IP>`), and **open the UDP
   ports** of the RTC range in the firewall (or configure **TURN** for restrictive networks).
2. **TLS/same-origin proxy:** serve LiveKit under `wss://` (`deploy/Caddyfile` terminates TLS);
   set `LIVEKIT_URL` to the public `wss://…`.
3. **Egress reaching the SFU:** run Egress on the **same host** with network visibility into
   LiveKit's RTC (compose already uses `network_mode: "service:livekit"`, which handles this when
   `node_ip` is routable). On Linux with host networking / a public IP, the Egress handshake
   completes.
4. **Strong secrets** (law §2.6): `LIVEKIT_API_SECRET`, `ONLYOFFICE_JWT_SECRET`,
   `DOC_TOKEN_SECRET`, and the compose file's `*-change-me` values need real values in the
   environment.

Recording is **optional**: without `LIVEKIT_HTTP_URL` + MinIO configured, the button responds
with `meet.recording_disabled` and the rest of Meet keeps working.

## Tests

```bash
# Unit tests (no external dependencies) — includes the provisioning state machine
cd backend && go test ./...

# Frontend: i18n guard (matching keys in pt-BR and en)
cd frontend && npm test

# Phase 1 E2E (brings up Postgres + Stalwart, provisions a customer end to end)
bash scripts/e2e-phase1.sh

# Integration against the test Stalwart (spins up an ephemeral container):
docker run -d --name sw-test -e STALWART_RECOVERY_ADMIN='admin:probe-secret' -p 18080:8080 stalwartlabs/stalwart:v0.16.4
# complete the bootstrap (DIVERGENCES §D3), then:
STALWART_TEST_URL=http://localhost:18080 STALWART_TEST_USER=admin STALWART_TEST_PWD=probe-secret \
  go test ./internal/stalwart -run TestIntegration -v
```

## Principles (brief)

- **Stalwart never modified** — we only talk to it over the network (JMAP). Nothing links/patches the binary.
- **Multi-tenancy is ours, in Go** — every query filters by `tenant_id`; Stalwart admin never leaks to the frontend.
- **Idempotency** in provisioning and webhooks.
- **Secrets outside the code** — via environment/`.env` (not committed).
