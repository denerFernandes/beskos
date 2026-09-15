# Beskos — Production Architecture & Deploy Runbook

> Status: **PLAN under review**. Nothing runs on the VPSs until approved.
> Decisions made: **CapRover** (over Docker Swarm) · storage on **Cloudflare R2** (no MinIO) ·
> **Stalwart receives + SES sends** · DNS/TLS on **Cloudflare** · Postgres with **replica + daily
> backup**.

Contents:
1. [Principles](#1-principles)
2. [3-node topology](#2-3-node-topology)
3. [The hybrid model: CapRover + Swarm services](#3-hybrid-model)
4. [Domains, DNS (Cloudflare) and TLS](#4-domains-dns-and-tls)
5. [Storage: Cloudflare R2](#5-storage-cloudflare-r2)
6. [Database: primary + replica + backup](#6-database)
7. [Email: Stalwart (inbound) + SES (outbound)](#7-email)
8. [Video: LiveKit + Egress](#8-video-livekit)
9. [Secrets](#9-secrets)
10. [CI/CD](#10-cicd)
11. [Security / hardening](#11-hardening)
12. [Backups & restore (runbook)](#12-backups-restore-runbook)
13. [Monitoring](#13-monitoring)
14. [Execution phases (checklist)](#14-execution-phases)
15. [Missing inputs](#15-missing-inputs)

---

## 1. Principles

- **Simple > sophisticated.** CapRover gives us a panel + TLS + deploy-by-git; whatever doesn't
  fit (mail, video) runs as a raw Swarm service — CapRover IS Swarm underneath, they coexist.
- **No state on the app nodes.** All state lives in: Postgres (data node + replica), **R2**
  (files/backups/recordings), Stalwart (mail node).
- **Secrets never in git.** Docker/CapRover secrets + GitHub Actions secrets.
- **Reproducible.** Everything declared (captain-definition, stack .yml, Actions). A lost VPS
  rebuilds from scratch + restoring from backup.

---

## 2. 3-node topology

> To be tuned once we have the real specs. Rule: OnlyOffice (~2–4 GB) and LiveKit isolated from
> the data node.

| Node | Role | Services | Public ports |
|----|-------|----------|-----------------|
| **VPS-1** | **Data + CapRover leader** | CapRover (panel+nginx ingress), **primary** Postgres, Redis, **Stalwart** | 80, 443, 3000(panel, close afterward), 25, 587, 465, 143, 993 |
| **VPS-2** | **App** | api ×2, worker, manager-api, frontend, manager, **Postgres replica** | (none direct; via the leader's ingress) |
| **VPS-3** | **Media** | OnlyOffice, LiveKit, Egress | 7880, 7881(tcp), 50000–60000/udp (WebRTC) |

- CapRover: **VPS-1 = leader**, VPS-2 and VPS-3 = **workers** (join the Swarm). Ingress
  (CapRover's nginx) lives on the leader; web apps can run on any node (the Swarm mesh routes
  them).
- **Node pinning** (placement): Postgres, Redis, Stalwart → VPS-1; PG replica → VPS-2;
  OnlyOffice/LiveKit/Egress → VPS-3. Done via `node label` (`beskos.role=data|app|media`) and
  constraints.

---

## 3. Hybrid model

**Managed by CapRover (HTTP apps — deploy by image/Dockerfile, automatic TLS):**
- `frontend` (webmail, static + nginx proxying `/api`,`/jmap`,`/ws`)
- `api` (Go) · `worker` (Go, no port) · `manager-api` (Go)
- `manager` (Next.js) · `onlyoffice` (documentserver)

**Swarm services run "by hand" (non-HTTP — outside CapRover's proxy):**
- `stalwart` — raw TCP 25/587/465/143/993 + volume, **host network** or published ports on VPS-1.
- `livekit` + `egress` — **host network** on VPS-3 (WebRTC needs direct UDP), pointed to by a
  DNS-only record.

> These live in `deploy/swarm/mail.yml` and `deploy/swarm/media.yml`, applied with
> `docker stack deploy`. They coexist with CapRover on the same Swarm.

### Webmail routing (same origin)
The web frontend assumes **same origin** (cookie + `X-Mailin-Client` CSRF header). So
`app.beskos.com`:
- `/` → webmail static files
- `/api/*` → the `api` service
- `/jmap/*` → Stalwart (JMAP)
- `/ws` (push/chat) → `api`

This is done by the **nginx inside the `frontend` container** (config bundled in the image) or by
location rules in CapRover. `manager.beskos.com` does the equivalent for `manager` +
`manager-api`.

---

## 4. Domains, DNS and TLS

**Cloudflare** manages the `beskos.com` zone. Orange = proxied (CDN/edge TLS); Grey = DNS-only.

| Record | Type | Value | Proxy | Notes |
|---------|------|-------|-------|-------|
| `beskos.com` | A | `185.232.71.70` | 🟠 | **landing page** (`landing` submodule) |
| `www` | CNAME | `beskos.com` | 🟠 | |
| `app` | A | `185.232.71.70` | 🟠 | webmail (CapRover ingress) |
| `api` | A | `185.232.71.70` | 🟠 | *(optional; by default api goes via `app/api`)* |
| `manager` | A | `185.232.71.70` | 🟠 | SaaS console |
| `docs` | A | `185.232.71.70` | 🟠 | OnlyOffice |
| `meet` | A | `185.232.69.0` | ⚪ | LiveKit (WebRTC/UDP — **not** proxied) |
| `mail` | A | `185.232.71.70` | ⚪ | Stalwart (SMTP/IMAP — **not** proxied) |

### Swarm services: never `docker restart`

On 2026-07-30, mail and webmail were down for half an hour because of this. Stalwart runs as a
**Swarm service** (`docker service ls`), not as a compose container. Restarting the task by hand
breaks routing:

```sh
docker restart stalwart.1.xxxx          # WRONG
docker service update --force stalwart  # correct
```

**Why:** the container comes back with a new IP, and the service keeps routing to the old one.
Every client uses the name (`stalwart:8080`), hits the dead VIP, and the symptom is the worst
possible one — the TCP connection is **accepted** and then closed right after, on every protocol.
It looks like the server is crashing. The Swarm logs held the proof: three failed startups with
`Bind for 0.0.0.0:25 failed: port is already allocated`, because the manually restarted container
was still holding the port while the Swarm tried to create its replacement.

**What was done so it doesn't happen again:** the service was switched to
`--endpoint-mode dnsrr`. The name now resolves straight to the task, with no VIP — the piece that
got stuck no longer exists. It's safe because the mail ports are published in `host` mode, outside
the Swarm mesh.

**And the `healthcheck` doesn't catch this.** The image's healthcheck runs
`curl http://127.0.0.1:8080/healthz/live` INSIDE the container: throughout the outage it kept
responding, and Docker correctly reported `healthy`. A loopback test never goes through the path
that's failing. What catches it is the **sentinel in the worker**
(`cmd/worker/sentinela.go`), which connects through the same name and the same network the API
uses. That's the difference between "the process is up" and "the service is actually serving."

> **`mail` never goes through a CDN, WAF, or any protection — ever.** It happened: on 2026-07-29
> the zone moved from Cloudflare to Bunny, and **Bunny Shield** got turned on for the `mail`
> record. The name started resolving to a Bunny IP and **mail delivery stopped** — the `MX`
> points to `mail`, and anyone trying to deliver hits port 25 on a proxy that only speaks HTTP and
> gets "connection refused." The server was fine the whole time:
> `telnet 185.232.71.70 25` answered with `220 … Stalwart ESMTP`.
>
> This applies to any provider and any mail hostname: an HTTP proxy does not forward SMTP, IMAP,
> or implicit TLS on 465. `app`, `captain`, and the root can be accelerated; `mail` cannot.
>
> **How to catch it in ten seconds:** `dig +short mail.beskos.com A` must return the VPS's IP. If
> it returns anything else, mail is down even if everything else looks fine.
| `beskos.com` | MX | `10 mail.beskos.com` | — | inbound |
| `beskos.com` | TXT (SPF) | `v=spf1 include:amazonses.com include:_spf.mail.beskos.com -all` | — | SES + our own mail |
| `resend._domainkey`/`<sel>._domainkey` | CNAME/TXT | SES DKIM + Stalwart DKIM | — | 2 signatures |
| `_dmarc` | TXT | `v=DMARC1; p=quarantine; rua=mailto:dmarc@beskos.com` | — | |
| `autoconfig`,`autodiscover` | A/CNAME | `mail.beskos.com` | ⚪ | client discovery |

- **Reverse PTR** for `185.232.71.70` → `mail.beskos.com`: configure in the **Netcup panel**
  (required for deliverability).
- **TLS for the web apps:** Cloudflare **Full (strict)** with a Cloudflare **Origin Certificate**
  installed in CapRover; **or** Let's Encrypt in CapRover via **DNS-01** (Cloudflare token).
  Recommend the Origin Cert (simple, no renewals failing behind the proxy).
- **TLS for mail/meet:** Let's Encrypt **directly** on the service (DNS-only), HTTP-01/TLS-ALPN or
  DNS-01.

---

## 5. Storage — Cloudflare R2

Replaces MinIO. The backend speaks **S3**; only the config changes:

```
S3_ENDPOINT      = https://<ACCOUNT_ID>.r2.cloudflarestorage.com
S3_REGION        = auto
S3_BUCKET        = beskos-files
S3_ACCESS_KEY    = <r2 access key>
S3_SECRET_KEY    = <r2 secret>
S3_FORCE_PATH_STYLE = true      # R2 uses path-style
```
Buckets:
- `beskos-files` — attachments/Drive/photos (app usage)
- `beskos-backups` — Postgres dumps + WAL
- `beskos-recordings` — Egress (LiveKit) outputs

> **To validate in the repo phase:** the backend's S3 client (presigned URLs, multipart) against
> R2. R2 supports presigned URLs and multipart; confirm URL style and expiry. Record in
> `deploy/DIVERGENCES.md` if anything differs.

---

## 6. Database

- **Primary:** Postgres 16 (pgvector) on VPS-1, local volume, `pg_hba` restricted to internal
  network + replica.
- **Replica:** **asynchronous** hot standby on VPS-2 (`primary_conninfo`, replication slot).
  Survives the loss of VPS-1 → **manual promotion** (`pg_ctl promote`) within minutes; the app
  points at the replica.
- **Daily backup:** `pg_dump -Fc` (or **WAL-G**) → `beskos-backups` (R2), 14-day retention, cron at
  03:00.
- **PITR (phase 2):** continuous WAL archiving to R2 (WAL-G) → point-in-time recovery.
- Postgres and replication-user passwords → **secrets**.

Failover (v1, manual): documented in §12. Auto-failover (Patroni) is a future upgrade.

---

## 7. Email

**Inbound + store:** Stalwart on VPS-1.
- Ports: 25 (SMTP recv), 587/465 (submission), 143/993 (IMAP), + JMAP (via `app/jmap`).
- **Port 25 on Netcup:** confirm it's unblocked (new accounts come blocked by default).
- **PTR** for the IP → `mail.beskos.com`; Stalwart's own **DKIM** (Stalwart's key) published in
  DNS.
- Data on the `stalwart_data` volume (VPS-1) — include it in snapshots/backup.

**Outbound:** relayed via **Amazon SES** (already configured in the project — better
deliverability than sending from the VPS). SES needs: a verified domain identity, **SES DKIM**,
SPF including `amazonses.com`. The app creates an SES identity per domain automatically (see the
project's memory).

**SES credentials** (SMTP user/pass or IAM keys) → **secrets**.

### Transactional notices (noreply@beskos.com)

Account created, trial started, subscription, billing, new login, event invites to external
guests, and files shared with external guests all go out over authenticated SMTP. **Without these
variables, sending is silently disabled** (the app keeps working; nobody just receives anything):

```
NOTIFY_SMTP_HOST=mail.beskos.com     # Stalwart
NOTIFY_SMTP_PORT=465                 # 465 = implicit TLS (Stalwart's 587 drops the connection)
NOTIFY_SMTP_USER=noreply@beskos.com
NOTIFY_SMTP_PASSWORD=<mailbox password>
NOTIFY_FROM=Beskos <noreply@beskos.com>   # already the default value
```

These variables are already set on the `api` app in production (2026-07-28); the mailbox password
lives in `deploy/secrets.env` as `NO_REPLAY_PASSWORD`.

The `noreply@beskos.com` mailbox has to **exist in Stalwart** (create it like any user), and the
`beskos.com` domain has to be active there, otherwise the server refuses to send.

---

## 8. Video — LiveKit

- `livekit` + `egress` on VPS-3, **`network_mode: host`** (WebRTC needs direct UDP; the Swarm mesh
  doesn't handle large UDP ranges well).
- Config: `livekit.yaml` with `rtc.udp_port_range: 50000-60000`, API keys, and **TURN** (for
  restrictive NAT) at `turn.beskos.com` (or use Cloudflare Calls/TURN later).
- VPS-3 firewall: open 7880/tcp, 7881/tcp, 50000-60000/udp, 3478/udp (TURN).
- Egress records to **R2** (`beskos-recordings`).
- Go signs the room JWT (already implemented). `meet.beskos.com` → `185.232.69.0` (DNS-only).

---

## 9. Secrets

Managed as **CapRover/Swarm secrets** (encrypted in the Raft log) and injected via env/file. List:

| Secret | Where |
|---------|------|
| `POSTGRES_PASSWORD`, `PG_REPL_PASSWORD` | Postgres, api, worker, replica |
| `DATABASE_URL` | api, worker, manager-api |
| `SECRETBOX_KEY` (TOTP/credential encryption) | api, worker |
| `JWT_SECRET` / session keys | api |
| `SES_SMTP_USER` / `SES_SMTP_PASS` (or IAM) | worker (relay) |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` (R2) | api, worker, egress |
| `LIVEKIT_API_KEY` / `LIVEKIT_API_SECRET` | api, livekit, egress |
| `ONLYOFFICE_JWT_SECRET` | api, onlyoffice |
| `ONLYOFFICE_SOURCE_URL` | api — **required**: without it, boot fails in production (source offer, AGPL §13). Value: `https://github.com/denerFernandes/beskos-docs-server` |
| `STRIPE_*` | manager-api |
| `CLOUDFLARE_API_TOKEN` | CI (DNS-01) / provisioning |
| `GHCR_TOKEN` (pull) | CapRover/Swarm registry auth |

Project rule (§2.6): **config only via the environment**. Map each secret → env var on the
service.

---

## 10. CI/CD

**Flow:** `git push master` → **GitHub Actions**:
1. **Build & test** (Go: `go build/test`; front: `tsc`+`vite build`; manager: `next build`).
2. **Build images** and push to
   `ghcr.io/denerfernandes/beskos-{api,frontend,manager}:{sha}` (private).
3. **Deploy**: call the **CapRover API** (`caprover deploy` / webhook) per app, with the `:{sha}`
   tag → rolling update. (Alternative: SSH to the leader + `docker service update --image`.)

**Dockerfiles:** `backend/Dockerfile` already exists (serves api/worker/manager-api with
different commands).
**To be created:** `frontend/Dockerfile` (Vite build → nginx with the `/api`,`/jmap`,`/ws` proxy)
and `manager/Dockerfile` (Next.js `output: standalone`).

Workflow sketch (`.github/workflows/deploy.yml`):
```yaml
on: { push: { branches: [master] } }
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
      - # go test, vite build, next build
      - # docker build+push api/frontend/manager -> ghcr (sha + latest)
  deploy:
    needs: build
    steps:
      - # npx caprover deploy --imageName ghcr.io/.../api:${sha} --appName api --caproverUrl ... --appToken ${{ secrets.CAPROVER_API }}
      # repeat per app
```
Actions secrets: `GHCR_TOKEN`, `CAPROVER_URL`, `CAPROVER_API_TOKEN`.

---

## 10b. App setup in CapRover (one-time)

CI deploys by image; the apps are created once in the panel (with domain + command + env):

| App | Image | Command (override) | Domain | Nodes |
|-----|--------|--------------------|---------|-----|
| `api` | beskos-api | `/usr/local/bin/api` (default) | internal proxy (`srv-captain--api`) | app |
| `worker` | beskos-api | `/usr/local/bin/worker` | — (no web) | app |
| `manager-api` | beskos-api | `/usr/local/bin/manager-api` | internal | app |
| `frontend` | beskos-frontend | — | `app.beskos.com` (+ `beskos.com`? no; landing is separate) | app |
| `manager` | beskos-manager | — | `manager.beskos.com` | app |
| `docs` | **beskos-docs** (ours: official image pinned + Beskos branding) | — | `docs.beskos.com` | media |

> **`onlyoffice` no longer runs the pure official image.** It runs `beskos-docs`, built in
> `deploy/onlyoffice/Dockerfile` from the official image **pinned by digest**, with the branding
> patch applied at build time. This is required: CapRover deploys by image and doesn't mount
> anything from the repository, so without this image the production editor would revert to
> ONLYOFFICE's own branding.
> The modified source is public (AGPL v3 §13): https://github.com/denerFernandes/beskos-docs-server
>
> The app is named **`docs`** (not `onlyoffice`) and needs `AI_PROXY_URL=http://srv-captain--api:8000`
> in addition to the JWT: our image injects the AI plugin's config at startup, and without that
> variable it writes the dev default (`localhost:5173`), leaving the editor's AI pointed at the
> wrong place.

- **ghcr registry:** Cluster → Docker Registries → add `ghcr.io` with a PAT (read:packages) → CapRover
  pulls the private images.
- **Env/secrets per app:** `DATABASE_URL`, `SECRETBOX_KEY`,
  `MINIO_ENDPOINT=<acct>.r2.cloudflarestorage.com`, `MINIO_SSL=true`, `MINIO_REGION=auto`,
  `MINIO_BUCKET=beskos-files`, `MINIO_ACCESS_KEY/SECRET`, `LIVEKIT_*`, `ONLYOFFICE_JWT_SECRET`,
  `ONLYOFFICE_SOURCE_URL`, `SES_*`, `STRIPE_*`, `MANAGER_API_URL=http://srv-captain--manager-api:8091`,
  `API_UPSTREAM=srv-captain--api:8000`.
- **Placement:** apps on `app` nodes (`beskos.role=app`) via *Node Selector*; `onlyoffice` on the
  `media` node.
- **CI secrets (repo):** `CAPROVER_URL`, `CAPROVER_PASSWORD`.

---

## 11. Hardening

Per node, in Phase 1:
- SSH **key-only**, root login off, optional alternate port, **fail2ban**.
- **ufw**: deny inbound by default, open per node only what's needed:
  - VPS-1: 22, 80, 443, 25, 587, 465, 143, 993, + Swarm ports (2377/tcp, 7946, 4789) between nodes
    only.
  - VPS-2: 22, + internal Swarm.
  - VPS-3: 22, 7880, 7881, 50000-60000/udp, 3478/udp, + internal Swarm.
- Encrypted **overlay network** (`--opt encrypted`) between nodes.
- CapRover panel (3000) closed to the public after setup (only via SSH tunnel / behind proxied
  `captain.` + auth).
- Automatic security updates (`unattended-upgrades`).
- Smoke test: only Cloudflare should talk 80/443 to the VPSs → allowlist Cloudflare's IPs in the
  web apps' ufw.

---

## 12. Backups & restore (runbook)

**Daily (cron on VPS-1):**
- `pg_dump -Fc beskos | wal-g/aws s3 cp → r2://beskos-backups/pg/YYYY-MM-DD.dump` (14d retention).
- Stalwart: `tar` of `stalwart_data` → `r2://beskos-backups/mail/` (weekly + before updates).

**Postgres restore (data loss):** `pg_restore` the most recent dump from R2 into a new Postgres.
**Failover (loss of VPS-1):** promote the replica on VPS-2 (`pg_ctl promote`), repoint
`DATABASE_URL` (secret), bring Stalwart/Redis up on a live node from backup, update the
`mail`/`app` DNS to the new IP.
**Restore test:** quarterly (restore into a disposable Postgres and validate).

---

## 13. Monitoring

Lightweight, no heavy stack:
- **Uptime Kuma** (1 container) — HTTP/port checks per service + alerts (email/telegram).
- CapRover metrics (per app) + `docker stats`.
- Logs: `docker service logs` / CapRover panel; (phase 2: Loki+Grafana if needed).
- **SES quota** and disk-space (data) alerts.

---

## 14. Execution phases

- [x] **F0 — Plan** (this doc).
- [x] **F1 — Provision:** 3 Ubuntu 24.04.4 VPSs (16 GB/8 vCPU/512 GB) · hardening (SSH key-only,
      ufw, fail2ban) · **Docker 27.5.1 pinned** (CapRover doesn't speak the Docker API ≥28) ·
      `deploy` user.
- [x] **F2 — Cluster:** CapRover on the leader (71.70) · 3-node Swarm · labels
      `beskos.role=data|app|media` · encrypted overlay network `beskos_net` · panel responding on
      :3000 (close after HTTPS).
- [x] **F3 — DNS/TLS:** Cloudflare records (wildcard `*.beskos.com`→71.70, mail/meet DNS-only,
      MX/SPF fixed) · CapRover root domain=`beskos.com` + **HTTPS Let's Encrypt** + force-SSL ·
      password changed · port 3000 closed. *(`_dmarc`+`www` and the Netcup PTR still missing —
      F6/mail.)*
- [x] **F4 — Repo (partial):** `frontend/Dockerfile`+`nginx.conf` (same-origin proxy) ·
      `manager/Dockerfile` (Next standalone) · **R2**: `storage.New` accepts a region (`auto`),
      env `MINIO_*`→R2 · CI `.github/workflows/deploy.yml` (build→ghcr→CapRover). *(mail/media
      Swarm are left for F6/F7.)*
- [ ] **F5 — 1st deploy:** apps in CapRover (see "App setup" above) + Postgres(+replica) + Redis.
      **← after DNS (F3).**
- [ ] **F6 — Mail:** Stalwart + port 25 + PTR + DKIM + SES relay; test sending/receiving
      (mail-tester).
- [ ] **F7 — Video:** LiveKit/Egress host-net + TURN; test a room + recording → R2.
- [ ] **F8 — CI/CD:** GitHub Actions build→ghcr→CapRover; automatic deploy on push.
- [ ] **F9 — Backups + monitoring.**
- [ ] **F10 — Cutover:** point `beskos.com`, end-to-end smoke test, go-live.

---

## 15. Missing inputs

1. **Specs** (RAM/vCPU/disk) and **region** for the 3 VPSs.
2. **IPs** (v4/v6) for the 3 VPSs.
3. **Port 25** — unblocked on Netcup? (otherwise, open a ticket).
4. **Cloudflare API token** (Zone: DNS Edit on beskos.com).
5. **R2** — account ID + buckets + access key/secret.
6. **Netcup API** (customer number, key, password) — optional, for reinstalling the OS + PTR.
7. **SSH key** public for deploy (or we generate a new dedicated pair).
8. **SES** — keys/identity ready? SES region?


### Credentials

> 🔒 **NOT kept here** (this file is in git). They're in **`deploy/secrets.env`** (git-ignored).
> Stored: R2 (account `bbe64be…`, EU endpoint, access/secret) + AWS/SES (IAM key). **Rotate** the
> keys once the deploy stabilizes (they were in plaintext in a local file for a while).
