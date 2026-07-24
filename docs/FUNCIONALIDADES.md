# Beskos — Funcionalidades do produto

Documento vivo que lista **todas** as funcionalidades do Beskos: plataforma multi-tenant de
e-mail, armazenamento e produtividade sobre o [Stalwart](https://stalw.art) (usado sem modificação,
só via JMAP — ver [deploy/DIVERGENCES.md](../deploy/DIVERGENCES.md)).

**Legenda de status**

| Status | Significado |
|:---:|---|
| ✅ | Implementado **e testado** (unit + integração/e2e reais) |
| 🎨 | **Prototipado e validado** na UI (protótipo navegável), aguardando implementação |
| 📋 | **Planejado** (escopo acordado, ainda não construído) |

Referência visual: protótipo interativo da UI completa (artifact "ui-completa").

---

## 1. Plataforma e arquitetura

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 1.1 | Multi-tenancy (organizações isoladas por `tenant_id`) | ✅ | Isolamento em banco compartilhado; toda query filtra `tenant_id` |
| 1.2 | Motor de e-mail Stalwart via JMAP (sem modificar o binário) | ✅ | Client Go tipado `internal/stalwart`; AGPL preservada |
| 1.3 | Control plane em Go (API REST + workers) | ✅ | `cmd/api` (chi) + `cmd/worker` |
| 1.4 | Reverse proxy same-origin (SPA + JMAP + auth) | ✅ | Dev: Vite; Prod: `deploy/Caddyfile`. Obrigatório (Stalwart sem CORS) |
| 1.5 | Roteamento tenant → instância Stalwart (escala horizontal) | 📋 | Campo `stalwart_instance` já existe no schema |

## 2. Provisionamento de clientes

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 2.1 | Máquina de estados idempotente e resumível | ✅ | `pending → domain_created → dkim_created → dns_pending → dns_verified → active` |
| 2.2 | Criar domínio no Stalwart | ✅ | `x:Domain/set` via JMAP |
| 2.3 | Geração automática de DKIM | ✅ | Automático no create; polling até o registro aparecer |
| 2.4 | Tela de registros DNS para o cliente publicar | ✅ | Parseados do `dnsZoneFile` (MX/SPF/DKIM/DMARC/MTA-STS/SRV) |
| 2.5 | Verificação automática de DNS | ✅ | Worker faz polling de resolução real |
| 2.6 | Criação de mailboxes (individual) | ✅ | `x:Account/set`; mailbox inicial `postmaster` na ativação |
| 2.7 | Bootstrap inicial do Stalwart | ✅ | `cmd/bootstrap` (passo único + restart) |

## 3. Painel de administração (nosso SaaS)

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 3.1 | Cadastro de organização (signup) + login | ✅ | Sessão por cookie HttpOnly, senha com bcrypt |
| 3.2 | Preferência de idioma persistida por usuário | ✅ | `pt-BR` / `en`; browser como default |
| 3.3 | Gestão de domínios (adicionar, ver estado, DNS) | ✅ | Checagem de posse do tenant antes de toda operação |
| 3.4 | Gestão de mailboxes (criar) | ✅ | Só em domínio ativo |
| 3.5 | Design unificado do painel (mesmo sistema do webmail) | 🎨 | Cartões de métricas, tabelas de domínios/caixas |
| 3.9 | UI: Membros (RBAC), MFA, Billing/Uso, aceite de convite, login 2 passos | ✅ | ligados ao backend; permission-gated |
| 3.6 | **RBAC**: papéis fixos + permissões granulares | ✅ | owner/admin/member/billing (+internal); `internal/rbac`; e2e de segurança |
| 3.7 | **Convites** de usuários por e-mail | ✅ | criar/aceitar/revogar; anti-escalonamento; último-owner protegido |
| 3.8 | Log de auditoria | ✅ | Tabela `audit_log` gravada nas operações sensíveis |

## 4. Webmail (usuário final)

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 4.1 | Login do usuário final (Basic auth atrás do proxy) | ✅ | Separado do login do painel |
| 4.2 | Listar pastas | ✅ | `Mailbox/get`; mapeadas por `role` |
| 4.3 | Listar e ler e-mails (corpo, remetente, data) | ✅ | `Email/query` + `Email/get` |
| 4.4 | Marcar como lido/não lido | ✅ | `keywords/$seen` |
| 4.5 | Enviar e-mail | ✅ | `Email/set` + `EmailSubmission/set` |
| 4.6 | Anexos (visualizar/baixar) | ✅ | Via `downloadUrl` same-origin |
| 4.7 | Busca full-text | ✅ | `Email/query` com `filter.text` |
| 4.8 | **Editor rich-text** (negrito, itálico, listas, citação, link) | 🎨 | Compose com barra de formatação |
| 4.9 | Responder / encaminhar com citação | 🎨 | Prototipado |
| 4.10 | Favoritos / marcadores coloridos | 🎨 | Marcadores por tenant |
| 4.11 | Tema claro/escuro | ✅ | Persistência de tema |
| 4.12 | UI: app de Arquivos (upload/baixar/compartilhar/excluir) | ✅ | 5º app no webmail, ligado ao storage |
| 4.13 | Contatos: grupos (N:N), tags, busca | ✅ | app no webmail; `contacts`/`contact_groups`/membership |
| 4.14 | Ícones monocromáticos (sem emoji) no webmail | ✅ | conjunto SVG `currentColor` |
| 3.10 | Usuários e Membros unificados (1 lista por papel) | ✅ | criar user provisiona caixa + conta; lista une ambos por email |

## 5. Assistente de IA (provider genérico — DeepSeek v4)

> **Interface genérica** de IA (`internal/ai`): a `Provider` abstrai o modelo, então trocar/adicionar
> fornecedor (Claude, OpenAI, …) é só configuração (`AI_PROVIDER`). Provider atual: **DeepSeek v4**.
> Chave da API **só no backend** (lei §3.5/§10), nunca no frontend. Camada `Assistant` já implementada
> e testada; falta expor os endpoints HTTP e ligar na UI.

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 5.1 | **Resumo do dia** (briefing) | 🎨 | Agenda + tarefas + e-mails num parágrafo acionável |
| 5.2 | Resumir e-mail / thread | 🎨 | Card "Resumo por IA" na leitura |
| 5.3 | Escrever e-mail com IA (rascunho no seu tom) | 🎨 | Gaveta "Escrever com IA"; variações "mais formal/curto" |
| 5.4 | Extrair tarefas de um e-mail | 🎨 | Gera tarefas → adiciona ao Kanban |
| 5.5 | Rascunho da IA cai direto no editor | 🎨 | "Usar rascunho" abre o compose preenchido |

## 6. Tela "Hoje" (briefing diário)

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 6.1 | Abre no primeiro acesso do dia | 🎨 | Visão default |
| 6.2 | Resumo do dia por IA (com saudação e data) | 🎨 | Ver 5.1 |
| 6.3 | Agenda de hoje | 🎨 | Clique leva ao calendário |
| 6.4 | Tarefas pendentes (com prazo Hoje/Amanhã) | 🎨 | Clique leva ao Kanban |
| 6.5 | E-mails que precisam de atenção | 🎨 | Clique leva ao webmail |
| 6.6 | Resumo de fim de dia | 📋 | Sugerido |

## 7. Kanban (produtividade)

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 7.1 | Quadro com colunas de status | 🎨 | A fazer / Em andamento / Aguardando / Concluído |
| 7.2 | Arrastar cards entre colunas | 🎨 | Contadores atualizam |
| 7.3 | E-mail → tarefa ("Virar tarefa") | 🎨 | Card criado a partir do e-mail |
| 7.4 | Cards com marcador, responsável, prazo | 🎨 | Nossa camada (Postgres), não JMAP |
| 7.5 | Adicionar tarefa manual | 🎨 | Botão "+ Adicionar" |

## 8. Calendário e reuniões

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 8.1 | Visão semanal (estilo monday.com) | 🎨 | Eventos coloridos, "hoje" destacado |
| 8.2 | Criar evento | 🎨 | "+ Novo evento" |
| 8.3 | Agendar reunião a partir de um e-mail | 🎨 | "Agendar reunião" cria evento |
| 8.4 | Integração real via JMAP Calendars | 📋 | Stalwart 0.16 expõe `urn:ietf:params:jmap:calendars` |
| 8.5 | Convites de reunião (iMIP/ICS) | 📋 | Depende de 8.4 |
| 8.6 | Visões dia/mês | 📋 | Semana já prototipada |

## 9. Armazenamento de arquivos

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 9.1 | Upload/download sobre MinIO (S3) | ✅ | `internal/storage`; e2e contra MinIO real |
| 9.2 | Dedup por hash, versionamento | ✅ | blob = `tenant/sha256`; refcount na exclusão; versão por nome |
| 9.3 | Compartilhamento (owner/editor/leitor/link) | ✅ | `storage_shares`; níveis validados |
| 9.4 | Upload resumável (multipart/tus) | 📋 | Hoje upload padrão até 100 MiB |

## 10. Billing e cotas

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 10.1 | Assinaturas **por usuário** (Stripe) | ✅ | preço por seat; moeda BRL/EUR/USD e mínimo de usuários via `.env` |
| 10.5 | Painel admin estilo Google Workspace | ✅ | sidebar, ícones monocromáticos, modais + drawers (sem alert/prompt), seção Usuários |
| 10.2 | Cotas de disco e de caixas por tenant | ✅ | enforcement 402 (disco respeita dedup); envio via Stalwart 📋 |
| 10.3 | Painel de uso / faturamento | ✅ (API) | `GET /usage`, `GET /billing`, `POST /billing/checkout` |
| 10.4 | Webhooks idempotentes | ✅ | assinatura Stripe verificada + dedupe por `event_id` |

## 11. Segurança e autenticação

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 11.1 | Credenciais de admin do Stalwart só no backend | ✅ | Nunca chegam ao frontend |
| 11.2 | Checagem de posse do tenant em toda operação admin | ✅ | |
| 11.3 | Códigos de erro estáveis (i18n) | ✅ | `error_code` + tradução no front |
| 11.4 | **MFA — TOTP** (app autenticador) | ✅ | RFC 6238 puro (`internal/totp`); + 10 códigos de recuperação (uso único) |
| 11.5 | **MFA — SMS** (provedor externo) | ✅ | Provider genérico (`internal/sms`); LogSender dev, Twilio depois |
| 11.6 | Rate limiting | 🟡 | MFA: verify limitado a 5 tentativas/desafio. Global (login/API) ainda 📋 |

## 12. Operação, conformidade e i18n

| # | Funcionalidade | Status | Notas |
|---|---|:---:|---|
| 12.1 | Internacionalização (pt-BR + en) | ✅ | Guarda automática de paridade de chaves |
| 12.2 | Limites de envio + detecção de abuso por tenant | 📋 | Reputação compartilhada de IP |
| 12.3 | Ganchos de reputação / pool de IP de saída | 📋 | Campo `outbound_ip_pool` no schema |
| 12.4 | LGPD (exportação/exclusão de dados, DPA, abuse@) | 📋 | Produto brasileiro |
| 12.5 | Observabilidade (logs estruturados, métricas, alertas) | 📋 | |

---

## Roadmap de implementação (ordem acordada)

1. ✅ **Fase 0** — Fundação (infra, client JMAP, esqueletos, auth, i18n)
2. ✅ **Fase 1** — Provisionamento (máquina de estados)
3. ✅ **Fase 2** — Webmail core (ler/enviar/buscar)
4. 🎨 **UI completa validada** — Hoje, IA, Kanban, Calendário, Admin unificado *(protótipo aprovado)*
5. ✅ **Backend de e-mail** — sessão de webmail (usuário final na nossa API, validada no Stalwart),
   endpoints de **IA** (summarize/draft/extract-tasks/briefing, provider **DeepSeek v4**), **Kanban**
   (boards/colunas/cards + mover), **marcadores** e **calendário/eventos**. E2E verde (`scripts/e2e-email-backend.sh`).
   *Falta ligar na UI React.*
6. ✅ **UI ligada ao backend**: webmail (IA/Kanban/calendário/arquivos) + painel (RBAC/MFA/billing/uso)
7. 📋 **Calendário via JMAP Calendars** (sync com o Stalwart) — hoje é camada própria
8. ✅ **RBAC** (papéis + convites) → ✅ **MFA** (TOTP + SMS + recovery, login 2 passos)
9. ✅ **Armazenamento** (MinIO) → ✅ **Billing/Cotas** (planos, cotas, webhook idempotente)
10. 📋 **Operação/LGPD/Observabilidade**

_Regra (lei §6.1): uma fase por vez; não iniciar a próxima sem a anterior testada._
