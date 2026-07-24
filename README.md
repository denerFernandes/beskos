# Beskos — plataforma de email + armazenamento multi-tenant

Control plane (Go) + webmail/painel (React) por cima do **Stalwart** (motor de email, usado
sem modificação, só via rede). Ver [project.md](project.md) para o brief completo.

> 📋 **Lista completa de funcionalidades (status de cada uma):** [docs/FUNCIONALIDADES.md](docs/FUNCIONALIDADES.md)

> **Leia antes de tudo:**
> - [CLAUDE.md](CLAUDE.md) — as **leis do projeto** (simplicidade, padrões Go, arquivos ≤600 linhas,
>   separação de lógica, i18n obrigatório). Todo código segue.
> - [deploy/DIVERGENCES.md](deploy/DIVERGENCES.md) — a versão fixada (Stalwart v0.16.4) **removeu a
>   REST Management API** que o brief §6 assume; toda administração é via JMAP. Fonte da verdade da integração.

## i18n

Todo texto de UI vem do i18n (lei §4). Frontend: `react-i18next`, locales em
`frontend/src/i18n/locales/{pt-BR,en}/common.json` (default **pt-BR**, **en** obrigatório).
Erros da API carregam `error_code` estável; o frontend traduz via chave `server.<code>`.

## Status

**Fase 0 — Fundação** ✅ · docker-compose (Stalwart v0.16.4 + Postgres + MinIO), client JMAP
tipado + teste de integração, esqueleto Go (`cmd/api`/`cmd/worker`, goose, sqlc), auth do painel,
esqueleto React com i18n.

**Fase 1 — Provisionamento** ✅

| Componente | Estado |
|---|---|
| Máquina de estados de provisionamento (`internal/provisioning`), idempotente/resumível | ✅ (5 testes unit. com fakes) |
| Worker (`Runner`) processando estados não-terminais em loop | ✅ |
| `cmd/bootstrap` (setup inicial do Stalwart, §D3) | ✅ |
| API: `POST /api/domains`, `GET /api/domains/{id}` (estado+DNS), `POST .../mailboxes` | ✅ |
| Frontend: adicionar domínio, ver estado + registros DNS, criar mailbox | ✅ (build limpo) |
| E2E ponta a ponta (Postgres+Stalwart reais): domínio → active → mailbox | ✅ `scripts/e2e-phase1.sh` |

Máquina de estados (brief §7, sobre JMAP — ver [DIVERGENCES §D6](deploy/DIVERGENCES.md)):
`pending → domain_created → dkim_created → dns_pending → dns_verified → active` (`↘ error`).

**Fase 2 — Webmail** ✅

| Componente | Estado |
|---|---|
| Reverse proxy same-origin (dev: Vite; prod: `deploy/Caddyfile`) — obrigatório (§D7) | ✅ smoke test verde |
| Client JMAP tipado (`src/jmap`): sessão, Mailbox, Email query/get, envio, busca | ✅ integração verde contra Stalwart real |
| Auth do webmail (Basic atrás do proxy, `src/webmail/auth.ts`) | ✅ |
| UI webmail (rail/lista/leitura/compose/busca) no design aprovado, i18n pt-BR/en | ✅ build limpo |

Webmail em `/mail` (login do usuário final, separado do painel). Design: ver artifact "webmail-v1".
⚠️ Arquitetura: o React **não** fala direto com o Stalwart (CORS ausente, §D7) — tudo via proxy same-origin.

**Fases seguintes (escopo ampliado)** ✅ — IA (resumir/escrever), Kanban, Calendário, Contatos,
Armazenamento/Drive (pastas, compartilhamento, visualizadores), **edição de office** (OnlyOffice:
editar/co-editar/criar), **RBAC + convites**, **MFA (TOTP/SMS)**, billing por usuário, e **Meet**
(videochamada LiveKit: chamada, agenda dia/semana/mês, RSVP, notificações in-app/e-mail, efeitos
de fundo). Ver a memória do projeto para o detalhe por área.

Pendências conhecidas: **gravação do Meet** (Egress) só completa em host Linux — ver
[Meet — rede do LiveKit/Egress](#meet-videochamada--rede-do-livekitegress); verificar **e-mail de
notificação** ao vivo (provisionar um mailbox `notify@` + `NOTIFY_*`).

## Layout

```
backend/    Go — control plane (API + worker)
  cmd/api           API REST/JSON do painel
  cmd/worker        workers (máquina de estados de provisionamento — Fase 1)
  internal/stalwart Client JMAP do Stalwart (x:Domain/Account/DkimSignature/Bootstrap)
  internal/db       sqlc gerado + migrations (goose, embutidas)
  internal/{auth,httpapi,config}
frontend/   React — painel/webmail/admin/storage
deploy/     docker-compose, config do Stalwart, DIVERGENCES.md
```

## Rodando (dev)

Backend inteiro no Docker; **só o frontend roda localmente** (o Vite faz proxy para os serviços do compose).

Pré-requisitos: Docker, Node 20+.

```bash
# 1. Infra + control plane (Postgres, Stalwart, MinIO, API, worker)
cd deploy
cp .env.example .env                 # ajuste os segredos
docker compose up -d --build         # sobe TUDO menos o frontend

# 2. Bootstrap do Stalwart — UMA vez (cria o admin permanente; ver DIVERGENCES §D3).
docker compose run --rm --entrypoint /usr/local/bin/bootstrap api
docker compose restart stalwart      # obrigatório para o bootstrap valer

# 3. Frontend local (proxy: /api → :8000, /jmap → :8080)
cd ../frontend
npm install
npm run dev                          # http://localhost:5173
```

Acesse: **http://localhost:5173** (painel) e **http://localhost:5173/mail** (webmail).
Crie a organização no painel, adicione um domínio (provisiona automático com
`PROVISION_SKIP_DNS_VERIFY=true`), crie uma caixa e entre no webmail com ela.

> IA fica desligada sem `AI_API_KEY` (endpoints respondem 503); SMS usa o modo `log`
> (o código aparece em `docker compose logs api`); billing usa o modo `manual`.

## Meet (videochamada) — rede do LiveKit/Egress

O Meet usa **LiveKit** (SFU WebRTC) + **Egress** (gravação), rodando como serviços rede-only.
A videochamada em si funciona em dev; a **gravação exige atenção de rede** porque o WebRTC
precisa de IPs/portas realmente alcançáveis.

**Dev (Docker Desktop / Mac).** O `deploy/livekit/livekit.yaml` usa `node_ip: 127.0.0.1` para que
os candidatos ICE sejam alcançáveis pelo **browser no host** (o Docker Desktop encaminha as portas
publicadas `7880/7881/7882`). A chamada conecta normalmente.

> ⚠️ **Gravação (Egress) não completa no Docker Desktop do Mac.** O Egress sobe um Chrome headless
> que precisa estabelecer **a própria conexão WebRTC** com o SFU; o loopback/UDP do Docker Desktop
> não entrega isso de forma confiável e o Egress aborta com `Start signal not received`. Todo o
> restante (botão Gravar, API `StartRoomCompositeEgress`, webhook `egress_ended`, upload pro MinIO,
> player) está pronto — só a captura do vídeo não fecha **nesta máquina de dev**.

**Produção (host Linux/VPS).** Para chamada **e** gravação funcionarem:

1. **IP público/roteável no LiveKit:** em `livekit.yaml`, troque `node_ip: 127.0.0.1` por
   `rtc: { use_external_ip: true }` (ou fixe `node_ip: <IP público do host>`), e **abra as portas
   UDP** do range RTC no firewall (ou configure **TURN** para redes restritas).
2. **TLS/proxy same-origin:** sirva o LiveKit sob `wss://` (o `deploy/Caddyfile` termina TLS);
   ajuste `LIVEKIT_URL` para o `wss://…` público.
3. **Egress alcançando o SFU:** rode o Egress no **mesmo host** com rede que enxergue o RTC do
   LiveKit (o compose já usa `network_mode: "service:livekit"`, que resolve isso quando o
   `node_ip` é roteável). Em Linux com host network / IP público, o handshake do Egress completa.
4. **Segredos fortes** (lei §2.6): `LIVEKIT_API_SECRET`, `ONLYOFFICE_JWT_SECRET`, `DOC_TOKEN_SECRET`
   e os `*-change-me` do compose precisam de valores reais no ambiente.

Gravação é **opcional**: sem `LIVEKIT_HTTP_URL` + MinIO configurados, o botão responde
`meet.recording_disabled` e o resto do Meet segue funcionando.

## Testes

```bash
# Unitários (sem dependências externas) — inclui a máquina de estados de provisionamento
cd backend && go test ./...

# Frontend: guarda de i18n (chaves iguais em pt-BR e en)
cd frontend && npm test

# E2E da Fase 1 (sobe Postgres + Stalwart, provisiona um cliente de ponta a ponta)
bash scripts/e2e-phase1.sh

# Integração contra o Stalwart de teste (sobe um container efêmero):
docker run -d --name sw-test -e STALWART_RECOVERY_ADMIN='admin:probe-secret' -p 18080:8080 stalwartlabs/stalwart:v0.16.4
# complete o bootstrap (DIVERGENCES §D3), depois:
STALWART_TEST_URL=http://localhost:18080 STALWART_TEST_USER=admin STALWART_TEST_PWD=probe-secret \
  go test ./internal/stalwart -run TestIntegration -v
```

## Princípios (brief)

- **Stalwart nunca modificado** — só falamos por rede (JMAP). Nada linka/patcheia o binário.
- **Multi-tenancy é nossa, no Go** — toda query filtra `tenant_id`; admin do Stalwart nunca vaza ao frontend.
- **Idempotência** em provisionamento e webhooks.
- **Segredos fora do código** — via ambiente/`.env` (não comitado).
