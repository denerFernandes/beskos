# Beskos — Arquitetura de Produção & Runbook de Deploy

> Estado: **PLANO para revisão**. Nada é executado nas VPS até aprovação.
> Decisões tomadas: **CapRover** (sobre Docker Swarm) · armazenamento **Cloudflare R2** (sem MinIO) ·
> **Stalwart recebe + SES envia** · DNS/TLS no **Cloudflare** · Postgres com **réplica + backup diário**.

Índice:
1. [Princípios](#1-princípios)
2. [Topologia dos 3 nós](#2-topologia-dos-3-nós)
3. [O modelo híbrido: CapRover + serviços Swarm](#3-modelo-híbrido)
4. [Domínios, DNS (Cloudflare) e TLS](#4-domínios-dns-e-tls)
5. [Armazenamento: Cloudflare R2](#5-armazenamento-r2)
6. [Base de dados: primário + réplica + backup](#6-base-de-dados)
7. [E-mail: Stalwart (inbound) + SES (outbound)](#7-e-mail)
8. [Vídeo: LiveKit + Egress](#8-vídeo-livekit)
9. [Segredos](#9-segredos)
10. [CI/CD](#10-cicd)
11. [Segurança / hardening](#11-hardening)
12. [Backups & restauro (runbook)](#12-backups--restauro)
13. [Monitorização](#13-monitorização)
14. [Fases de execução (checklist)](#14-fases-de-execução)
15. [Inputs em falta](#15-inputs-em-falta)

---

## 1. Princípios

- **Simples > sofisticado.** CapRover dá painel + TLS + deploy-por-git; o que não encaixa (mail, vídeo)
  corre como serviço Swarm cru — CapRover É Swarm por baixo, convivem.
- **Sem estado nos nós de app.** Todo o estado vive em: Postgres (nó de dados + réplica), **R2**
  (ficheiros/backups/gravações), Stalwart (nó de mail).
- **Segredos nunca no git.** Docker/CapRover secrets + GitHub Actions secrets.
- **Reprodutível.** Tudo declarado (captain-definition, stack .yml, Actions). Uma VPS perdida
  reconstrói-se do zero + restauro de backup.

---

## 2. Topologia dos 3 nós

> Afinar quando tivermos as specs reais. Regra: OnlyOffice (~2–4 GB) e LiveKit isolados do nó de dados.

| Nó | Papel | Serviços | Portas públicas |
|----|-------|----------|-----------------|
| **VPS-1** | **Dados + CapRover leader** | CapRover (painel+nginx ingress), Postgres **primário**, Redis, **Stalwart** | 80, 443, 3000(painel, fechar depois), 25, 587, 465, 143, 993 |
| **VPS-2** | **App** | api ×2, worker, manager-api, frontend, manager, **Postgres réplica** | (nenhuma direta; via ingress do leader) |
| **VPS-3** | **Média** | OnlyOffice, LiveKit, Egress | 7880, 7881(tcp), 50000–60000/udp (WebRTC) |

- CapRover: **VPS-1 = leader**, VPS-2 e VPS-3 = **workers** (join ao Swarm). Ingress (nginx do
  CapRover) vive no leader; as apps web podem correr em qualquer nó (o mesh do Swarm encaminha).
- **Fixação por nó** (placement): Postgres, Redis, Stalwart → VPS-1; réplica PG → VPS-2; OnlyOffice/
  LiveKit/Egress → VPS-3. Feito por `node label` (`beskos.role=data|app|media`) e constraints.

---

## 3. Modelo híbrido

**Geridos pelo CapRover (apps HTTP — deploy por imagem/Dockerfile, TLS automático):**
- `frontend` (webmail, estático + nginx que faz proxy de `/api`,`/jmap`,`/ws`)
- `api` (Go) · `worker` (Go, sem porta) · `manager-api` (Go)
- `manager` (Next.js) · `onlyoffice` (documentserver)

**Serviços Swarm "à mão" (não-HTTP — fora do proxy do CapRover):**
- `stalwart` — TCP cru 25/587/465/143/993 + volume, **network host** ou portas publicadas no VPS-1.
- `livekit` + `egress` — **network host** no VPS-3 (WebRTC precisa de UDP direto), apontados por
  registo DNS-only.

> Ficam em `deploy/swarm/mail.yml` e `deploy/swarm/media.yml`, aplicados com `docker stack deploy`.
> Coexistem com o CapRover no mesmo Swarm.

### Roteamento do webmail (mesma origem)
O frontend web assume **mesma origem** (cookie + header CSRF `X-Mailin-Client`). Então `app.beskos.com`:
- `/` → estático do webmail
- `/api/*` → serviço `api`
- `/jmap/*` → Stalwart (JMAP)
- `/ws` (push/chat) → `api`

Isto é feito pelo **nginx do container `frontend`** (config incluída na imagem) ou por regras de
localização no CapRover. `manager.beskos.com` faz o equivalente para `manager` + `manager-api`.

---

## 4. Domínios, DNS e TLS

**Cloudflare** gere a zona `beskos.com`. Laranja = proxied (CDN/TLS edge); Cinzento = DNS-only.

| Registo | Tipo | Valor | Proxy | Notas |
|---------|------|-------|-------|-------|
| `beskos.com` | A | `185.232.71.70` | 🟠 | **landing page** (submódulo `landing`) |
| `www` | CNAME | `beskos.com` | 🟠 | |
| `app` | A | `185.232.71.70` | 🟠 | webmail (ingress CapRover) |
| `api` | A | `185.232.71.70` | 🟠 | *(opcional; por defeito o api vai via `app/api`)* |
| `manager` | A | `185.232.71.70` | 🟠 | consola SaaS |
| `docs` | A | `185.232.71.70` | 🟠 | OnlyOffice |
| `meet` | A | `185.232.69.0` | ⚪ | LiveKit (WebRTC/UDP — **não** proxied) |
| `mail` | A | `185.232.71.70` | ⚪ | Stalwart (SMTP/IMAP — **não** proxied) |
| `beskos.com` | MX | `10 mail.beskos.com` | — | inbound |
| `beskos.com` | TXT (SPF) | `v=spf1 include:amazonses.com include:_spf.mail.beskos.com -all` | — | SES + mail próprio |
| `resend._domainkey`/`<sel>._domainkey` | CNAME/TXT | DKIM do SES + DKIM da Stalwart | — | 2 assinaturas |
| `_dmarc` | TXT | `v=DMARC1; p=quarantine; rua=mailto:dmarc@beskos.com` | — | |
| `autoconfig`,`autodiscover` | A/CNAME | `mail.beskos.com` | ⚪ | descoberta de clientes |

- **PTR (reverso)** do `185.232.71.70` → `mail.beskos.com`: configurar no **painel Netcup** (obrigatório p/ entregabilidade).
- **TLS das apps web:** Cloudflare **Full (strict)** com **Origin Certificate** do Cloudflare instalado
  no CapRover; **ou** Let's Encrypt no CapRover via **DNS-01** (token Cloudflare). Recomendo Origin Cert
  (simples, sem renovações a falhar atrás do proxy).
- **TLS mail/meet:** Let's Encrypt **direto** no serviço (DNS-only), HTTP-01/TLS-ALPN ou DNS-01.

---

## 5. Armazenamento — Cloudflare R2

Substitui o MinIO. O backend fala **S3**; muda só a configuração:

```
S3_ENDPOINT      = https://<ACCOUNT_ID>.r2.cloudflarestorage.com
S3_REGION        = auto
S3_BUCKET        = beskos-files
S3_ACCESS_KEY    = <r2 access key>
S3_SECRET_KEY    = <r2 secret>
S3_FORCE_PATH_STYLE = true      # R2 usa path-style
```
Buckets:
- `beskos-files` — anexos/Drive/fotos (uso da app)
- `beskos-backups` — dumps do Postgres + WAL
- `beskos-recordings` — saídas do Egress (LiveKit)

> **A validar na Fase repo:** o cliente S3 do backend (presigned URLs, multipart) contra R2. R2 suporta
> presigned e multipart; confirmar estilo de URL e expiração. Guardar em `deploy/DIVERGENCES.md` se algo diferir.

---

## 6. Base de dados

- **Primário:** Postgres 16 (pgvector) no VPS-1, volume local, `pg_hba` só rede interna + réplica.
- **Réplica:** hot standby **assíncrona** no VPS-2 (`primary_conninfo`, replication slot). Sobrevive à
  perda do VPS-1 → **promoção manual** (`pg_ctl promote`) em minutos; a app aponta para a réplica.
- **Backup diário:** `pg_dump -Fc` (ou **WAL-G**) → `beskos-backups` (R2), retenção 14 dias, cron 03:00.
- **PITR (fase 2):** WAL archiving contínuo para o R2 (WAL-G) → recuperação ao minuto.
- Password do Postgres e do utilizador de replicação → **secrets**.

Failover (v1, manual): documentado em §12. Auto-failover (Patroni) fica como upgrade futuro.

---

## 7. E-mail

**Inbound + store:** Stalwart no VPS-1.
- Portas: 25 (SMTP recv), 587/465 (submission), 143/993 (IMAP), + JMAP (via `app/jmap`).
- **Porta 25 na Netcup:** confirmar desbloqueio (contas novas vêm bloqueadas).
- **PTR** do IP → `mail.beskos.com`; **DKIM** próprio (chave da Stalwart) publicado no DNS.
- Dados em volume `stalwart_data` (VPS-1) — incluir em snapshots/backup.

**Outbound:** relay via **Amazon SES** (já configurado no projeto — melhor entregabilidade que enviar
da VPS). SES precisa: identidade de domínio verificada, **DKIM do SES**, SPF a incluir `amazonses.com`.
A app cria identidade SES por domínio automaticamente (ver memória do projeto).

**Credenciais SES** (SMTP user/pass ou chaves IAM) → **secrets**.

### Avisos transacionais (noreply@beskos.com)

Conta criada, teste iniciado, subscrição, cobrança, novo início de sessão, convites de evento a
externos e ficheiros partilhados com externos saem por SMTP autenticado. **Sem estas variáveis o
envio fica desligado em silêncio** (a app continua a funcionar; só ninguém recebe nada):

```
NOTIFY_SMTP_HOST=mail.beskos.com     # o Stalwart
NOTIFY_SMTP_PORT=465                 # 465 = TLS implícito (o 587 do Stalwart larga a ligação)
NOTIFY_SMTP_USER=noreply@beskos.com
NOTIFY_SMTP_PASSWORD=<senha da caixa>
NOTIFY_FROM=Beskos <noreply@beskos.com>   # já é o valor por omissão
```

Estas variáveis já estão postas na app `api` em produção (28/07/2026); a senha da caixa vive no
`deploy/secrets.env` como `NO_REPLAY_PASSWORD`.

A caixa `noreply@beskos.com` tem de **existir no Stalwart** (criar como qualquer utilizador) e o
domínio `beskos.com` tem de estar ativo lá, senão o servidor recusa o envio.

---

## 8. Vídeo — LiveKit

- `livekit` + `egress` no VPS-3, **`network_mode: host`** (WebRTC precisa de UDP direto; o mesh do
  Swarm não lida bem com gamas UDP grandes).
- Config: `livekit.yaml` com `rtc.udp_port_range: 50000-60000`, chaves API, e **TURN** (para NAT
  restritivo) em `turn.beskos.com` (ou usar Cloudflare Calls/TURN mais tarde).
- Firewall VPS-3: abrir 7880/tcp, 7881/tcp, 50000-60000/udp, 3478/udp (TURN).
- Egress grava para **R2** (`beskos-recordings`).
- O Go assina o JWT da sala (já implementado). `meet.beskos.com` → `185.232.69.0` (DNS-only).

---

## 9. Segredos

Geridos como **CapRover/Swarm secrets** (cifrados no Raft) e injetados por env/ficheiro. Lista:

| Segredo | Onde |
|---------|------|
| `POSTGRES_PASSWORD`, `PG_REPL_PASSWORD` | Postgres, api, worker, réplica |
| `DATABASE_URL` | api, worker, manager-api |
| `SECRETBOX_KEY` (cifra TOTP/credenciais) | api, worker |
| `JWT_SECRET` / chaves de sessão | api |
| `SES_SMTP_USER` / `SES_SMTP_PASS` (ou IAM) | worker (relay) |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` (R2) | api, worker, egress |
| `LIVEKIT_API_KEY` / `LIVEKIT_API_SECRET` | api, livekit, egress |
| `ONLYOFFICE_JWT_SECRET` | api, onlyoffice |
| `ONLYOFFICE_SOURCE_URL` | api — **obrigatório**: sem ele o boot falha em produção (oferta de fonte, AGPL §13). Valor: `https://github.com/denerFernandes/beskos-docs-server` |
| `STRIPE_*` | manager-api |
| `CLOUDFLARE_API_TOKEN` | CI (DNS-01) / provisioning |
| `GHCR_TOKEN` (pull) | CapRover/Swarm registry auth |

Regra do projeto (§2.6): **config só por ambiente**. Mapear cada secret → env no serviço.

---

## 10. CI/CD

**Fluxo:** `git push master` → **GitHub Actions**:
1. **Build & test** (Go: `go build/test`; front: `tsc`+`vite build`; manager: `next build`).
2. **Build imagens** e push para **ghcr.io/denerfernandes/beskos-{api,frontend,manager}:{sha}` (privado).
3. **Deploy**: chamar a **API do CapRover** (`caprover deploy` / webhook) por app, com a tag `:{sha}`
   → rolling update. (Alternativa: SSH ao leader + `docker service update --image`.)

**Dockerfiles:** `backend/Dockerfile` já existe (serve api/worker/manager-api com comandos diferentes).
**A criar:** `frontend/Dockerfile` (build Vite → nginx com o proxy `/api`,`/jmap`,`/ws`) e
`manager/Dockerfile` (Next.js `output: standalone`).

Esboço do workflow (`.github/workflows/deploy.yml`):
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
      # repetir por app
```
Secrets do Actions: `GHCR_TOKEN`, `CAPROVER_URL`, `CAPROVER_API_TOKEN`.

---

## 10b. Setup das apps no CapRover (uma vez)

O CI faz deploy por imagem; as apps criam-se uma vez no painel (com o domínio + comando + env):

| App | Imagem | Comando (override) | Domínio | Nós |
|-----|--------|--------------------|---------|-----|
| `api` | beskos-api | `/usr/local/bin/api` (default) | proxy interno (`srv-captain--api`) | app |
| `worker` | beskos-api | `/usr/local/bin/worker` | — (sem web) | app |
| `manager-api` | beskos-api | `/usr/local/bin/manager-api` | interno | app |
| `frontend` | beskos-frontend | — | `app.beskos.com` (+ `beskos.com`? não; landing à parte) | app |
| `manager` | beskos-manager | — | `manager.beskos.com` | app |
| `docs` | **beskos-docs** (a nossa: oficial pinada + marca Beskos) | — | `docs.beskos.com` | media |

> **O `onlyoffice` deixou de correr a imagem oficial pura.** Corre `beskos-docs`, construída em
> `deploy/onlyoffice/Dockerfile` a partir da imagem oficial **pinada por digest** com o patch de
> marca aplicado no build. É obrigatório: o CapRover faz deploy por imagem e não monta nada do
> repositório, portanto sem esta imagem o editor em produção volta à marca do ONLYOFFICE.
> O fonte modificado é público (AGPL v3 §13): https://github.com/denerFernandes/beskos-docs-server
>
> A app chama-se **`docs`** (não `onlyoffice`) e precisa de `AI_PROXY_URL=http://srv-captain--api:8000`
> além do JWT: a nossa imagem injeta a config do plugin de IA no arranque e, sem essa variável,
> escreve o default de dev (`localhost:5173`) e a IA do editor fica apontada ao sítio errado.

- **Registry ghcr:** Cluster → Docker Registries → adicionar `ghcr.io` com um PAT (read:packages) → o CapRover puxa as imagens privadas.
- **Env/secrets por app:** `DATABASE_URL`, `SECRETBOX_KEY`, `MINIO_ENDPOINT=<acct>.r2.cloudflarestorage.com`, `MINIO_SSL=true`, `MINIO_REGION=auto`, `MINIO_BUCKET=beskos-files`, `MINIO_ACCESS_KEY/SECRET`, `LIVEKIT_*`, `ONLYOFFICE_JWT_SECRET`, `ONLYOFFICE_SOURCE_URL`, `SES_*`, `STRIPE_*`, `MANAGER_API_URL=http://srv-captain--manager-api:8091`, `API_UPSTREAM=srv-captain--api:8000`.
- **Placement:** apps em nós `app` (`beskos.role=app`) via *Node Selector*; `onlyoffice` no nó `media`.
- **Segredos do CI (repo):** `CAPROVER_URL`, `CAPROVER_PASSWORD`.

---

## 11. Hardening

Por nó, na Fase 1:
- SSH **só por chave**, root login off, porta alternativa opcional, **fail2ban**.
- **ufw**: por defeito nega inbound; abre por nó só o necessário:
  - VPS-1: 22, 80, 443, 25, 587, 465, 143, 993, + porta Swarm (2377/tcp, 7946, 4789) só entre nós.
  - VPS-2: 22, + Swarm interno.
  - VPS-3: 22, 7880, 7881, 50000-60000/udp, 3478/udp, + Swarm interno.
- **Rede overlay** encriptada (`--opt encrypted`) entre nós.
- Painel CapRover (3000) fechado ao público depois do setup (só via SSH tunnel / atrás de `captain.` proxied + auth).
- Atualizações automáticas de segurança (`unattended-upgrades`).
- Fumo: só o Cloudflare deve falar 80/443 com as VPS → allowlist dos IPs do Cloudflare no ufw das apps web.

---

## 12. Backups & restauro (runbook)

**Diário (cron no VPS-1):**
- `pg_dump -Fc beskos | wal-g/aws s3 cp → r2://beskos-backups/pg/AAAA-MM-DD.dump` (retenção 14d).
- Stalwart: `tar` do `stalwart_data` → `r2://beskos-backups/mail/` (semanal + antes de updates).

**Restauro Postgres (perda de dados):** `pg_restore` do dump mais recente do R2 para um Postgres novo.
**Failover (perda do VPS-1):** promover réplica no VPS-2 (`pg_ctl promote`), reapontar `DATABASE_URL`
(secret), subir Stalwart/Redis num nó vivo a partir do backup, atualizar DNS `mail`/`app` para o novo IP.
**Teste de restauro:** trimestral (restaurar para um Postgres descartável e validar).

---

## 13. Monitorização

Leve, sem stack pesada:
- **Uptime Kuma** (1 container) — checks HTTP/porta de cada serviço + alertas (email/telegram).
- Métricas do CapRover (por app) + `docker stats`.
- Logs: `docker service logs` / painel CapRover; (fase 2: Loki+Grafana se precisar).
- Alerta de **quota SES** e de espaço em disco (dados).

---

## 14. Fases de execução

- [x] **F0 — Plano** (este doc).
- [x] **F1 — Provisionar:** 3 VPS Ubuntu 24.04.4 (16 GB/8 vCPU/512 GB) · hardening (SSH só-chave,
      ufw, fail2ban) · **Docker 27.5.1 fixado** (o CapRover não fala a API do Docker ≥28) · user `deploy`.
- [x] **F2 — Cluster:** CapRover no leader (71.70) · Swarm com 3 nós · labels `beskos.role=data|app|media`
      · rede overlay encriptada `beskos_net` · painel a responder em :3000 (fechar após HTTPS).
- [x] **F3 — DNS/TLS:** registos Cloudflare (wildcard `*.beskos.com`→71.70, mail/meet DNS-only, MX/SPF
      corrigidos) · CapRover root domain=`beskos.com` + **HTTPS Let's Encrypt** + force-SSL · password
      trocada · porta 3000 fechada. *(falta `_dmarc`+`www` e o PTR na Netcup — F6/mail.)*
- [x] **F4 — Repo (parcial):** `frontend/Dockerfile`+`nginx.conf` (proxy same-origin) · `manager/Dockerfile`
      (Next standalone) · **R2**: `storage.New` aceita região (`auto`), env `MINIO_*`→R2 · CI
      `.github/workflows/deploy.yml` (build→ghcr→CapRover). *(mail/media Swarm ficam para F6/F7.)*
- [ ] **F5 — 1º deploy:** apps no CapRover (ver "Setup das apps" abaixo) + Postgres(+réplica) + Redis. **← depois do DNS (F3).**
- [ ] **F6 — Mail:** Stalwart + porta 25 + PTR + DKIM + relay SES; testar envio/receção (mail-tester).
- [ ] **F7 — Vídeo:** LiveKit/Egress host-net + TURN; testar sala + gravação → R2.
- [ ] **F8 — CI/CD:** GitHub Actions build→ghcr→CapRover; deploy automático no push.
- [ ] **F9 — Backups + monitorização.**
- [ ] **F10 — Cutover:** apontar `beskos.com`, smoke test end-to-end, go-live.

---

## 15. Inputs em falta

1. **Specs** (RAM/vCPU/disco) e **região** das 3 VPS.
2. **IPs** (v4/v6) das 3 VPS.
3. **Porta 25** — desbloqueada na Netcup? (senão, abrir pedido).
4. **Cloudflare API token** (Zone: DNS Edit em beskos.com).
5. **R2** — account ID + buckets + access key/secret.
6. **Netcup API** (nº cliente, key, password) — opcional, p/ reinstalar SO + PTR.
7. **Chave SSH** pública para deploy (ou geramos par novo dedicado).
8. **SES** — chaves/identidade prontas? Região SES?


### Credenciais

> 🔒 **NÃO ficam aqui** (isto está no git). Estão em **`deploy/secrets.env`** (ignorado pelo git).
> Guardado: R2 (account `bbe64be…`, endpoint EU, access/secret) + AWS/SES (IAM key). **Rotacionar** as
> chaves quando o deploy estabilizar (estiveram em texto claro num ficheiro local).