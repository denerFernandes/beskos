# Manager — Painel de Gestão do SaaS (Beskos)

> Estado: **desenho aprovado para arranque** (2026-07-22). Documento vivo, segue as leis do
> [CLAUDE.md](../CLAUDE.md). Prosa em PT-BR; código, logs e `error_code` em inglês (lei 4.6).
> Uma fase de cada vez (§9): não começar a M(n+1) antes da M(n) testada.

## 1. O que é

Aplicação **interna de operação do SaaS** — quem a usa somos NÓS (operador da plataforma), nunca os
clientes. Gere planos, tenants, utilizadores, recursos, custos e receita de todas as organizações.
Completamente separada do produto: outra app, outra API, outra autenticação, outra origem.

### Decisões estruturais (do utilizador, 2026-07-22)
1. **Frontend**: app **Next.js + shadcn/ui** nova (`manager/`), separada do webmail (Vite/React).
2. **API**: serviço de admin próprio — novo binário Go **`cmd/manager-api`** que reutiliza
   `internal/db` e os pacotes partilhados, mas com **rotas, sessões e porta próprias** (isolamento
   real: um bug no produto nunca expõe o manager, e vice-versa).
3. **Login obrigatório com TOTP** — sem exceções, desde o primeiro utilizador.
4. **Gravação de Meet por plano** (decisão desta sessão):
   - plano de entrada: **sem gravação**;
   - planos pagos: **Track Egress + montagem offline** como padrão (tracks crus saem quase de graça
     durante a reunião; um job compõe o mp4 com ffmpeg fora do caminho quente — "gravação pronta em
     ~10 min"); áudio+transcrição AI incluídos;
   - plano topo: prioridade na fila de montagem (e, se um dia fizer falta, Room Composite ao vivo
     como add-on — hoje NÃO é necessário).

## 2. Arquitetura

```
manager.beskos.com (Caddy) ──► manager (Next.js + shadcn)  ──► manager-api (Go, porta própria)
                                                               │
                                        Postgres (mesmo cluster, role SQL próprio) ─ billing ─ R2 ─ SES
```

- **`cmd/manager-api`** (Go/chi, como o api): autenticação própria (tabela `manager_users`),
  sessões próprias, **TOTP obrigatório** (reusa `internal/totp` + `secretbox`), rate-limit de login,
  e **audit log de toda a ação de escrita** (`manager_audit`). Nunca partilha cookies/sessões com o
  produto.
- **Role SQL dedicado** com o mínimo necessário: leitura ampla para dashboards; escrita apenas nas
  tabelas do manager (`plans`, `tenant_plans`, `manager_*`) e nos knobs que ele controla.
- **Deploy**: no nó core (compose próprio `deploy/manager/`), atrás do Caddy em subdomínio próprio;
  opcional allowlist de IP no Caddy (config, não código).
- **Next.js**: SSR/estático simples, chamadas à manager-api por fetch com o token de sessão; shadcn
  para UI; i18n não obrigatório (ferramenta interna — inglês chega; deixar strings centralizadas na
  mesma para não doer depois).

## 3. Modelo de dados

### 3.1 `plans` — catálogo de planos
| coluna | tipo | nota |
|---|---|---|
| `id` uuid pk / `key` text unique | | ex.: `essential`, `pro`, `business` |
| `name`, `description` | text | exibição |
| `price_month_cents`, `currency` | int, text | preço por utilizador/mês |
| `min_users`, `max_users` | int | **pedido**: tamanho mínimo/máximo da organização (0 = sem teto) |
| `apps_enabled` | jsonb | array das apps do rail permitidas (`mail`,`chat`,`kanban`,`cal`,`files`,`contacts`,`meet`,`wiki`) |
| `storage_gb_per_user` | int | quota de storage (pool do tenant = users × valor) |
| `ai_tokens_per_window` | int | quota da janela de 6h (o motor já a aplica) |
| `meet_max_participants` / `meet_max_minutes` | int | limites de sala (0 = ilimitado) |
| `recording` | text | `none` \| `offline` (track egress + montagem) \| `offline_priority` |
| `recording_retention_days` | int | retenção das gravações |
| `email_daily_per_user` | int | teto de envio via SES |
| `onlyoffice_edit` | bool | edição office (false = só leitura) |
| `wiki_public_links` | bool | links públicos da wiki |
| `active`, `sort`, timestamps | | planos desativados não aceitam novos tenants |

### 3.2 Vínculo tenant→plano — **divergência registada (M2, 22/07/2026)**
O produto JÁ tinha um catálogo em código (`internal/billing/plans.go`) e o vínculo por
`tenants.plano` (usado pelo webhook de billing). A M2 seguiu a realidade (lei 6.4): a tabela
`plans` (chave = `key`) substituiu o catálogo em código como fonte de verdade (código fica como
fallback/seed), e o vínculo continua `tenants.plano` — **não** criámos `tenant_plans`.
Seats/overrides/trial por tenant ficam para quando o billing precisar. Outras adaptações:
storage é **pool por tenant** (`max_storage_gb`, modelo existente do produto), não por utilizador;
moeda é global (`BILLING_CURRENCY`), não por plano.

### 3.3 Atributos extra que sugiro (análise)
- **`trial_days`** no plano + estado de trial no tenant — funil de conversão mede-se aqui.
- **`annual_discount_pct`** — cobrança anual com desconto (o billing já distingue períodos).
- **`extra_seat_price_cents` / `extra_storage_gb_price_cents`** — *overage* explícito em vez de
  bloquear: cliente que estoura storage paga excedente, não perde acesso (menos churn, mais receita).
- **`guest_meet` bool** — convidados externos no Meet (link sem conta) como diferenciador de plano.
- **`api_access` bool + `webhooks` bool** — quando abrirmos API pública, já é um knob de plano.
- **`custom_domains` int** — nº de domínios de email por tenant.
- **`audit_retention_days`** — retenção de auditoria/versões (compliance vende no Business).
- **`support_sla`** text — `community|standard|priority` (aparece no site e no ticket).
- **`dedicated_ip_sending` bool** — IP dedicado SES como add-on Business.

### 3.4 Tabelas do próprio manager
- `manager_users` — email, password_hash (argon2/bcrypt como o produto), `totp_secret` cifrado
  (secretbox), `totp_enabled NOT NULL DEFAULT true` (obrigatório), role (`admin`|`viewer`), last_login.
- `manager_sessions` — token hash, expiração curta (8h), IP/UA.
- `manager_audit` — quem, quando, ação, alvo (tenant/plan), payload before/after (jsonb). Toda escrita.

### 3.5 Medição (usage metering)
- `tenant_daily_usage` (rollup diário por job no worker): `tenant_id, day, users_active, storage_bytes,
  mail_sent, meet_minutes, recordings_count, recordings_bytes, ai_tokens, wiki_pages, cards, files`.
- Fontes: agregados Postgres (dados já existem: `assistant_usage`, storage por `storage_files`,
  gravações, etc.), métricas SES (via SNS/CloudWatch) e R2 (API de usage). O rollup congela o dia —
  dashboards leem do rollup, não de queries pesadas ao vivo.

## 4. Enforcement — onde cada knob liga no produto (mapa)

| Knob do plano | Ponto existente no código |
|---|---|
| `apps_enabled` | `tenant_disabled_apps` (o gating de apps + IA já corta ferramentas/agentes) — o manager escreve o complemento |
| `ai_tokens_per_window` | quota 6h do assistente (`assistantQuotaStatus`) — passa a ler o valor do plano |
| `storage_gb_per_user` | `mail_users.quota_bytes` + checagem de upload no Files |
| `max_users`/`min_users` | handler de convite/criação de membro (recusa acima do teto; `min_users` valida na subscrição/checkout) |
| `recording` | handlers `meetRecordStart` (recusa/escolhe modo egress conforme plano) |
| `email_daily_per_user` | **feito (22/07)**: `max_daily_send` do plano aplicado nos 3 caminhos de envio (outbox criar+worker, JMAP proxy, assistente); contagem por destinatário em `tenant_mail_sent` |
| `onlyoffice_edit` | config da sessão OnlyOffice (modo view/edit) |
| `recording_retention_days` | job de limpeza no worker (apaga do R2 + registo) |

Regra de ouro: o produto **lê** os knobs (join `tenant_plans`→`plans` + overrides, cacheado);
o manager **escreve**. Nenhuma rota do produto escreve em `plans`.

## 5. Dashboard e relatórios

### 5.1 Dashboard (home do manager)
- **Cartões**: tenants (total, ativos, em trial, churn 30d) · utilizadores (total, ativos 7d/30d) ·
  storage total (GB e € estimado R2) · **receita** (MRR, ARR, ARPU) · **custo** (fixo VPS + variável
  SES/R2/AI estimado) · **margem** (receita − custo) · emails enviados 30d · minutos de Meet 30d ·
  tokens de IA 30d.
- **Gráficos** (rollup diário): MRR ao longo do tempo, tenants novos vs churn, storage acumulado,
  uso por app (DAU por app — diz quais apps sustentam retenção).

### 5.2 Relatórios
- **Por tenant**: uso completo (users, GB, minutos, tokens, emails), custo estimado vs receita → 
  **margem por tenant** (identifica clientes deficitários), plano/overrides, histórico de pagamentos.
- **Crescimento**: coortes mensais, conversão trial→pago, churn e motivo (campo no cancelamento).
- **Capacidade**: projeção de storage/CPU vs infra atual (avisa quando escalar o VPS).
- Exportação CSV em todos.

### 5.3 Gestão (CRUD)
- **Planos**: criar/editar/desativar; simulador ("com N users este plano custa/rende X").
- **Tenants**: ver, mudar plano, aplicar override, suspender/reativar, iniciar trial, impersonar
  NÃO (fora de âmbito — auditoria de suporte é outra conversa).
- **Manager users**: convidar operador (setup TOTP no primeiro login, obrigatório), revogar.

## 6. Segurança
- Origem, sessões e utilizadores 100% separados do produto; TOTP obrigatório; rate-limit agressivo
  no login; sessões de 8h; audit log integral de escritas; role SQL de menor privilégio; opcional
  allowlist de IP no Caddy. `error_code` em inglês como no produto.

## 7. Fases (uma de cada vez)

- **M0 — Esqueleto + auth**: `cmd/manager-api` (login+TOTP obrigatório, sessões, audit), app Next.js
  com shadcn (login, shell, navegação), deploy compose + Caddy. Testável: entrar com TOTP.
- **M1 — Visão (read-only)**: lista de tenants e utilizadores com números básicos (contagens,
  storage por tenant via query direta), dashboard v1 com cartões estáticos.
- **M2 — Planos**: tabelas `plans`/`tenant_plans`, CRUD no manager, atribuição de plano a tenant;
  **enforcement dos 3 primeiros knobs** no produto (apps_enabled, max_users, ai_tokens).
- **M3 — Metering**: rollup diário `tenant_daily_usage` no worker; dashboard passa a ler do rollup;
  knobs restantes (storage, recording, email limit, onlyoffice, retenção).
- **M4 — Receita e custo**: integração billing (MRR/ARR/ARPU/churn), custos (fixo configurável +
  variável estimado por driver), margem por tenant.
- **M5 — Relatórios**: coortes, capacidade, exports CSV; polimento.

## 8. Riscos
- **Dois frontends stacks** (Vite no produto, Next no manager): aceite pelo utilizador; manter o
  manager pequeno e sem partilha de componentes com o produto (cópia consciente > acoplamento).
- **Knobs sem enforcement** viram mentira comercial — cada knob só aparece na UI do manager quando o
  enforcement correspondente estiver ligado no produto (a tabela do §4 é o contrato).
- **Custo estimado ≠ fatura real** — marcar sempre estimativas como estimativas; conciliar mensalmente
  com faturas reais (R2/SES) num campo de ajuste.

## Como correr (dev)
- **API**: `docker compose up -d --build manager-api` (em `deploy/`; porta 127.0.0.1:8091). O 1º
  operador vem de `MANAGER_BOOTSTRAP_EMAIL/PASSWORD` no `.env` (só com a tabela vazia).
- **Frontend**: `cd manager && npm run dev` (localhost:3000; o Next faz proxy de `/api/manager/*`
  para a API — `MANAGER_API_URL` para apontar a outro host).
- 1º login: senha → a página mostra o link `otpauth://` + segredo → adicionar no autenticador →
  confirmar com o código. Logins seguintes exigem sempre o código (anti-replay por passo de tempo).

## Progresso de implementação
- [x] **Analytics + alertas de uso** *(22/07/2026): UI refeita fiel ao dashboard de referência do
  shadcn (fonte Inter, team switcher, Search que filtra tenants, tabs). Filtro de período REAL
  (7/30/90/365 dias) comanda gráficos e export. Tab Analytics: storage total, tokens IA/dia,
  minutos Meet/dia, e-mails/dia (série ganhou `mail_sent`), top utilizadores de IA 24h e top
  custo por tenant. `GET /alerts` (refetch 60s na UI): `users_cap` (≥90% warn / teto crítico),
  `storage_cap` (80/95%), `mail_cap` (80/100% do teto diário), `ai_spike` (hoje ≥3× a média dos
  7 dias, piso 50k) e `ai_user_heavy` (UM utilizador ≥100k tokens/24h) — sem estado novo, o
  alerta some quando a condição passa. Testado ao vivo: os 4 tipos dispararam (spike 4× real).
  Backlog de estatística registado: coortes de retenção/churn (billing real), DAU por app,
  deliverability SES (bounces via SNS), funil trial→pago, NPS.)*
- [x] **Custo por servidor (VPS individuais)** *(22/07/2026: migração 00108 `manager_servers`
  seedada com o layout OVH do consulting (vps-core €26 + vps-media €26 + vps-mail €13 = €65) e
  `fixed_month_cents` zerado (vira "outros fixos" — domínios/licenças/backups). CRUD
  `GET/PUT/DELETE /servers` + pausar/reativar (admin-only, audit); o custo fixo das finanças =
  soma dos servidores ATIVOS + outros fixos, com breakdown no cartão. UI: card "Servidores" no
  Financeiro (tabela com total + Sheet de criar/editar). Testado ao vivo: seed 65€; adicionar
  backup 9€ → fixo 74€; pausar → 65€; remover ok.)*
- [x] **MAIL FROM próprio por tenant** *(22/07/2026: o SES exige que o MAIL FROM seja subdomínio
  da identidade — `mailer.beskos.com` partilhado é impossível; implementado o equivalente por
  tenant: no provisionamento, `PutEmailIdentityMailFromAttributes` configura **`mailer.<domínio>`**
  (fallback USE_DEFAULT_VALUE se o MX faltar) e a zona ganha 2 registos automáticos — MX 10
  `feedback-smtp.<região>.amazonses.com` + TXT SPF do subdomínio — classificados como `mailfrom`
  no grupo "Enviar" do drawer. Ganho: alinhamento SPF+DKIM no DMARC (Gmail/Yahoo 2024+) e envelope
  com a marca do cliente. Testes: máquina com fake relay verifica mail-from + registos; NÃO
  testado contra AWS real.)*
- [x] **Deliverability SES (bounces/complaints)** *(22/07/2026): migração 00107 `mail_events`;
  webhook público `POST /api/webhooks/ses` — SNS com **assinatura verificada** (cert só de
  `*.amazonaws.com`, SHA1/SHA256, cache de certs; `internal/ses/sns.go` com testes de assinatura
  válida/adulterada/cert malicioso), confirma a subscrição sozinho, resolve o tenant pelo domínio
  do remetente e grava 1 evento por destinatário. Manager: `GET /deliverability` (enviados 30d vs
  bounces/complaints) + tabela no Analytics; alertas `bounce_rate` (warn ≥5%, crit ≥10%) e
  `complaint_rate` (warn ≥0,1%, crit ≥0,5%) — limiares da AWS; amostras <50 envios não geram
  taxa. Setup AWS documentado no handler: SNS topic → subscrição HTTPS `<PUBLIC_URL>/api/webhooks/ses`
  → notificações da identidade/configuration-set no SES. Testado ao vivo: 403 sem assinatura;
  7% bounce → warn; 0,5% complaint → critical. Gráficos do dashboard refeitos com **Recharts**
  (o mesmo do exemplo shadcn: maxBarSize, tooltip, eixos limpos — sem distorção com poucos pontos).*
- [x] **Pós-M5 — pipeline de gravação offline + Stripe** *(22/07/2026):*
  - *Gravação (decisão §1 implementada): migração 00104 (`recordings.mode/assembly_status/priority`
    + `recording_tracks`); planos offline/offline_priority iniciam um **Track Egress por faixa**
    publicada (as tardias entram pelo webhook `track_published`); parar liga a fila de montagem;
    o worker compõe o MP4 com **ffmpeg** (grelha xstack 640×360 + amix, offsets por adelay/tpad,
    fila com `FOR UPDATE SKIP LOCKED`, prioridade para offline_priority, processing órfão >1h
    reanima; faixas cruas apagadas após sucesso). ffmpeg/ffprobe entraram na imagem do backend.
    RoomComposite continua como fallback para planos legados. **Provado ao vivo** com 3 faixas
    sintéticas → MP4 8s montado em 1s, row ready, raw limpo.*
  - *Relay SES + medição + limite (22/07/2026): migração 00105 `tenant_mail_sent` (conta
    DESTINATÁRIOS, como o SES cobra); enforcement do `max_daily_send` do plano nos 3 caminhos —
    criar outbox (429 `mail.daily_limit_reached`, i18n pt/en), worker no envio real (revalida +
    conta), proxy JMAP (intercepta `EmailSubmission/set`; contagem otimista) e assistente; rollup
    `mail_sent` agora real; bloco `queue.strategy/route` do SES no config.toml do Stalwart
    (comentado em dev, instruções para ligar em produção; envs `SES_SMTP_USER/PASS` no compose).
    Provado ao vivo: contador saturado → 429; 7/5000 → 201; rollup refletiu 7.*
  - *Identidade SES automática por domínio (pedido 22/07/2026): `internal/ses` (cliente SESv2 com
    SigV4 próprio, sem SDK; teste golden da assinatura); a máquina de provisionamento ganhou
    `EmailRelay` — ao provisionar um domínio, regista a identidade no SES (Easy DKIM, idempotente)
    e a zona a publicar ganha os 3 CNAMEs DKIM do SES + `include:amazonses.com` no SPF + DMARC
    (`AugmentZoneForSES`, testada); `dns_verified` só sai quando o próprio SES confirma o DKIM.
    Liga com `SES_REGION` + `AWS_ACCESS_KEY_ID/SECRET` (compose). NÃO testado contra a AWS real
    (sem credenciais em dev); a assinatura e a máquina estão cobertas por testes.*
  - *Remoção de domínio + UI de DNS (22/07/2026): `DELETE /api/domains/{id}` (PermDomainsManage;
    recusa com caixas no domínio → 409 `domains.has_mailboxes`; apaga no Stalwart, remove a
    identidade SES best-effort — `EnableSES` no api —, linha por fim; audit). UI do drawer de
    domínio: registos DNS **classificados por propósito** (`purpose`: mx/spf/dkim/dkim_ses/dmarc)
    e agrupados em "Receber" e "**Enviar (obrigatório)**" com aviso, botão copiar por registo, e
    zona de perigo com confirmação por nome (sem diálogos nativos). i18n pt/en. Provado ao vivo:
    409 com caixa → 200 sem; idempotência (2º delete → 404); zona real do Stalwart classificada.*
  - *Stripe: o checkout passou a usar o **catálogo da BD** (preço/nome editados no manager; antes
    lia o catálogo em código — divergiria), valida plano ativo + teto de utilizadores e cobra no
    mínimo o piso do plano; `customer.subscription.updated` sincroniza só o status (past_due etc.);
    `applyPlan`/webhook validam contra a BD. i18n `billing.plan_too_small` (pt-BR+en).*
- [x] M0 — esqueleto + auth TOTP *(22/07/2026: migração 00100 (`manager_users/sessions/audit`),
  `internal/managerapi` + `cmd/manager-api` (login 1-endpoint com enrollment forçado, sessões 8h,
  rate-limit IP+conta, anti-enumeração por timing, audit integral), app `manager/` Next 16 + shadcn
  (login 3 estados, shell com navegação, proxy.ts), serviço `manager-api` no compose. Testado ao
  vivo: enrollment, sessão, anti-replay de código, logout revoga no servidor, audit com 3 linhas.)*
- [x] M1 — visão read-only *(22/07/2026: endpoints `/overview`, `/tenants`, `/tenants/{id}`
  (queries diretas com contagens + storage por soma de `storage_files` vivos); dashboard com totais
  reais (tenants/ativos, users/ativos 7d, storage, domínios; receita "—" até M4), tabela de tenants
  com pesquisa e detalhe com utilizadores (papel, MFA, storage, quota, visto). Formatação via
  `Intl` (format.ts). Testado ao vivo com 2 tenants/4 users reais.)*
- [x] M2 — planos + enforcement inicial *(22/07/2026: migração 00101 (`plans` seedada com os 4
  planos do código, mesmos limites); produto lê o catálogo da BD com cache 60s + fallback
  (`plans_catalog.go`); enforcement ao vivo: `apps_enabled` (união com disabled_apps do tenant —
  vale para o rail E para as ferramentas/agentes da IA), `max_users` (gate existente de caixas
  passa a ler da BD), `ai_tokens_per_window` (quota 6h por plano), teto Meet por plano idem;
  manager: CRUD de planos (PUT upsert + ativar/desativar, admin-only, audit before/after) e
  atribuição de plano no detalhe do tenant; UI: página Planos com Sheet lateral de edição
  (checklist de apps, select de gravação, switches). Testado ponta a ponta: tenant novo →
  manager atribui growth sem kanban → /api/apps do produto devolve kanban desligado; quota IA
  reflete 500k do plano.)*
- [x] M3 — metering + knobs restantes *(22/07/2026: migração 00102 `tenant_daily_usage`
  (PK tenant+dia); worker roda rollup HORÁRIO idempotente de hoje+ontem (`cmd/worker/metering.go`)
  — users/ativos, storage, ficheiros, tokens IA, minutos de Meet (sessões joined→left), gravações,
  páginas wiki, cards; `mail_sent` fica 0 até o relay SES medir (§4). Retenção de gravações por
  plano no mesmo ticker (apaga blob no MinIO e depois a linha; fail-safe na ordem). Knobs novos no
  produto: `recording=none` → 403 no iniciar gravação; `onlyoffice_edit=false` → editor rebaixa
  para view; `wiki_public_links=false` → recusa ligar link público E deixa de servir os
  existentes (fail-closed no read). Manager: `GET /tenants/{id}/usage` + `GET /usage/series`;
  tabela de uso 14 dias no detalhe do tenant. `meet_max_minutes` e `email_daily` continuam SEM
  enforcement (dependem do timer de sala e do relay SES — registados na tabela §4). Testado ao
  vivo: rollup real (Dev: 3 users/13MB/54k tokens/2 páginas/4 cards; ontem reconstruído).)*
- [x] M4 — receita/custo/margem *(22/07/2026: migração 00103 `manager_settings` (chave→jsonb) com
  o `cost_model` seedado do consulting (fixo €65/mês, storage 2c/GB-mês, IA 30c/1M tokens, e-mail
  10c/1k); `GET/PUT /settings/costs` (admin-only, audit before/after); `GET /finance` — MRR/ARR/ARPU
  como receita CONTRATADA (preço do plano × utilizadores de tenants ativos; billable = users atuais,
  sem mínimo — o mínimo cobrado entra com o billing real, registado aqui), custo variável estimado
  por driver (storage+gravações do último rollup, tokens IA e e-mails de 30d do rollup) e margem
  por tenant (negativa a vermelho). UI: página Financeiro (6 cartões + tabela por tenant + editor
  do modelo de custos) e cartão Receita do dashboard ligado ao MRR real. Aviso de estimativa na
  página (§8). Testado ao vivo: MRR €15 (PlanTest/growth), custo IA 2c em 94k tokens, margem
  recalcula ao editar o modelo.)*
- [x] M5 — relatórios/exports *(22/07/2026: página Relatórios com crescimento mensal
  (tenants/users novos + acumulados via `generate_series`) e 3 exports CSV (tenants, uso 30d,
  financeiro — links diretos com o cookie de sessão); dashboard ganhou 2 mini-gráficos SVG
  (utilizadores ativos/dia e tokens IA/dia, 30d, sem lib de gráficos); página Operadores completa
  o §5.3 — listar, criar (Sheet; senha temporária ≥12; TOTP completa no 1º login), reativar/
  desativar (desativar corta as sessões; não se desativa a si próprio), tudo admin-only com audit.
  Coortes de churn ficam para o billing real (sem eventos de cancelamento). Testado ao vivo:
  growth 2026-07 = 3 tenants/5 users; 3 CSVs corretos; operador viewer criado e auto-desativação
  recusada.)*
