# Avaliação — configurações por app, responsividade, apps Tauri, sync em Rust e prontidão para produção

> Levantamento do estado atual (código real do repo, 2026-07-19) e do que falta para:
> (1) configurações individuais por app; (2) design responsivo dos modos de "ver" (apresentação
> no Meet etc.); (3) responsividade real desktop + mobile; (4) app mobile com Tauri;
> (5) sincronização de diretórios com a nuvem em Rust; (6) demais itens de produção.
> **Legenda:** 🔴 essencial · 🟡 importante · 🟢 pode esperar. Esforço: P (≤2 dias) / M (≤1 semana) / G (>1 semana).
> **Checkboxes:** `- [x]` feito e verificado · `- [~]` parcial (o resto depende de si ou é refinamento) · `- [ ]` por fazer.

## Status da execução (2026-07-19)

> A primeira versão desta avaliação baseou-se numa exploração do repo que foi interrompida a meio,
> e por isso **superestimou** as lacunas das frentes 1, 2 e 6. Depois de verificar o código real:

**Entregue e verificado nesta rodada (build + typecheck + gofmt limpos):**
- **§6 Produção — backup/restauro:** `scripts/backup.sh` (pg_dump + tar dos volumes MinIO/Stalwart,
  retenção configurável) e `scripts/restore.sh` (destrutivo, com confirmação, pára api/worker). Era a
  maior lacuna real (perda de dado irreversível).
- **§6 Produção — CI:** `.github/workflows/ci.yml` (gofmt+vet+build+test+sqlc no backend; tsc+vitest
  no frontend; cargo check do Tauri desktop). A árvore foi deixada gofmt-limpa (3 ficheiros
  pré-existentes normalizados).
- **§6 Produção — prontidão:** endpoint `/readyz` que pinga o Postgres (`SetReadiness(pool.Ping)`),
  e o healthcheck do `api` no compose passou a usá-lo (não encaminha tráfego antes da base responder).
- **§2 Meet — layout de gravação + PiP:** host escolhe o layout (grid/orador/só-orador), validado no
  backend; botão de Picture-in-Picture na barra de ações (feature-detected). GAPS §Meet atualizado.
- **§5 Sync — M0 do backend (aprovado por si):** feed incremental `GET /api/files/changes?since=<cursor>`
  (migração 00078: coluna `updated_at` + trigger `BEFORE UPDATE` que a atualiza em qualquer mudança;
  keyset por `(updated_at, id)`; `hash` = ETag; soft-delete vira tombstone). Migração e feed
  verificados contra um Postgres real (trigger dispara só nos ficheiros alterados; ordenação e
  tombstone corretos). → `00078_file_changes.sql`, `files_changes.go`, `queries.sql`. **Próximo:** o
  daemon Rust (decisão: processo separado) que consome este feed — é o M1, ainda não iniciado.

**Decisões que você tomou (2026-07-19):** sync = **daemon separado** (não embutido no app); métricas/
observabilidade = **nenhuma dependência nova por agora**; Meet mobile = **fazer spike do WebRTC no
iOS antes** de comprometer o caminho Tauri; sync M0 backend = **implementar já** (feito, acima).

**Rodada 2 (implementar tudo, com foco em segurança) — entregue e verificado:**
- **§5 Auth do daemon (token de dispositivo):** migração 00079 `device_tokens` — token cru mostrado
  só na criação, guardado apenas como **SHA-256** (hex); escopo restrito ao grupo `/api/sync/*`
  (ler changes + baixar), **fora** do resto da API e do fluxo de sessão; revogável por dispositivo,
  expiração opcional. Rotas de gestão (`GET/POST/DELETE /api/devices`) atrás da sessão do webmail.
  Semântica de auth testada em Postgres real (revogado e expirado ⇒ 0; válido ⇒ 1).
  → `00079_device_tokens.sql`, `device_tokens.go`, `server.go`.
- **§6 Hardening:** `postgres` e `redis` **pinados por digest** (eram tags flutuantes); o `devtools`
  do Tauri **removido do build release** (o inspector deixa de ir no pacote distribuído; dev mantém-no
  automaticamente). → `docker-compose.yml`, `src-tauri/Cargo.toml`.
- Nota: LiveKit-server **já estava** pinado por digest (correção à v1 desta avaliação).

**Rodada 12 — §1 configs de todas as apps na página de Definições.** `/mail/settings` ganhou
secções para **todas as apps**: Geral (idioma, tema, vista inicial), E-mail, Chat, Calendário,
Ficheiros, Meet, Kanban (+ Notificações, Dispositivos). Persistência server-side pela infra JSONB
`mail_users.settings` via um hook novo `useSettings.ts` (`useSection` escreve otimista; `useUserSettings`
lê nas apps — cache partilhado, muda nas Definições ⇒ reflete já). **Sem toggles mortos** — cada
preferência está ligada à sua app: Ficheiros→vista+expiração-de-link; Calendário→início-da-semana+
vista-inicial+duração+lembrete (EventModal); Chat→Enter-envia (Composer); Meet→entrar-mudo/câmara-off
(PreJoin); Kanban→quadro-inicial; Geral→tema+vista-inicial (App). Removi o toggle de "sons do chat"
(não há feature de som — seria decorativo) e Contactos não ganhou secção (sem preferência real a
ligar). i18n pt-BR/en; `tsc`+`build` limpos. → `SettingsView.tsx`, `useSettings.ts`, `backend.ts`,
`App.tsx`, `FilesView.tsx`, `CalendarView.tsx`, `EventModal.tsx`, `ChatView.tsx`, `MeetRoom.tsx`,
`KanbanView.tsx`, `ShareModal.tsx`.

**Rodada 11 — bug de produção: Caddyfile contornava o broker JMAP.** O frontend **não** fala JMAP
direto ao Stalwart — manda o *nosso* token de sessão (Bearer) para `/jmap`, e a nossa API faz o
brokering (H11): valida a sessão e injeta a credencial Basic da caixa server-to-server (o browser
nunca segura a senha). O `vite.config.ts` (dev) e o `jmap_proxy.go` já implementavam isto, mas o
`deploy/Caddyfile` estava **desatualizado** e roteava `/jmap`, `/auth`, `/.well-known` **direto ao
Stalwart:8080**, saltando o broker — em produção quebraria o webmail (401, o Stalwart não entende a
nossa sessão) e anularia o brokering de segurança. Corrigido: os três passam agora por
`control-plane:8000`, alinhado com o dev e com o comentário do próprio `jmap_proxy.go`. → `deploy/Caddyfile`.

**Rodada 10 — §3 responsividade adotada em Tailwind + últimos vãos:** decisão de responsividade
resolvida (era pergunta em aberto). Tailwind configurado como o **sistema responsivo**: `important: true`
(a app vive sob `.wm`, de alta especificidade; como Tailwind estava praticamente por usar, ligar o
important não altera o layout existente e faz os utilitários ganharem a disputa) e `md: 721px` para
que `max-md:` case **exatamente** o breakpoint móvel que o `webmail.css` já usa (`max-width:720px`),
sem zona morta. Convenção: **novo trabalho responsivo usa `max-md:` do Tailwind** (só afeta ≤720px,
o desktop não emite regra — zero risco de regressão); as regras `@media` que já funcionam ficam como
estão (não se churna código a funcionar, e as que dependem de `:has()` do LiveKit nem seriam
utilitários). Vão fechado: **Contactos** (grelha fixa 220px→empilha em `max-md:grid-cols-1` + barra
de grupos com altura limitada). Mecanismo verificado no CSS gerado (`@media not all and
(min-width:721px){…!important}`). → `tailwind.config.js`, `ContactsView.tsx`, `GroupsSidebar.tsx`.

**Rodada 9 — §5 espelho de subpastas no sync:** o sync deixou de ser plano — passa a **espelhar a
árvore de pastas**. Backend: `GET/POST /api/sync/folders` (listar a árvore do dono / criar pasta,
com verificação de posse do pai). Daemon: novo módulo `tree.rs` (mapa `folder_id`↔caminho, à prova
de ciclos, com testes) — coloca cada ficheiro sob o caminho da sua pasta; **trata movimentos**
(mesmo conteúdo + pasta diferente ⇒ move o local em vez de rebaixar); o push é **recursivo** e cria
as pastas do servidor em falta. Sanitização anti-traversal estendida a subcaminhos (`safe_join_rel`
+ testes). `go build`/`vet` e `cargo build`/`test` (4 testes) limpos. → `files_sync_folders.go`,
`sync-daemon/src/{tree,sync,api,safepath}.rs`.

**Rodada 8 — §2 apresentação do Meet responsiva:** no telemóvel o palco da apresentação passa a
ocupar a largura toda e as câmaras do LiveKit viram uma **fita horizontal** por baixo (em vez de
uma coluna estreita ilegível). Fecha o último 🔴 do §2. → `webmail.css`.

**Rodada 7 — §5 M2: sync bidirecional (nuvem ↔ local) com concorrência otimista:**
- **Backend:** `PUT /api/sync/files/{id}` exige **If-Match** (hash base); divergência → **412**
  (conflito). Antes de sobrescrever, **fotografa a versão** no histórico (nunca perde); cota, dedup
  por hash e isolamento por tenant/dono preservados. `POST /api/sync/files` cria ficheiros novos.
  Hash em streaming (sem carregar o ficheiro em memória). Semântica de snapshot+update testada em
  Postgres. → `files_sync_upload.go`, `server.go`.
- **Daemon:** passagem **push-antes-de-pull** (evita o download sobrescrever uma edição local por
  subir); sobe alterados (If-Match) e novos; no 412 guarda **cópia de conflito** local; **delete
  local não é propagado** (segurança). Hash local em SHA-256 (mesmo algoritmo do servidor).
  `cargo build`/`test` limpos. → `sync-daemon/src/{api,sync,index}.rs`.
- Nota: cada camada foi verificada isoladamente (backend build+vet, SQL em Postgres, daemon
  build+testes); o teste ponta-a-ponta ao vivo (daemon ↔ API ↔ MinIO) precisa da stack a correr.

**Rodada 6 — §4 apps móveis (Tauri Android + iOS) inicializados:** corri `tauri android init` e
`tauri ios init` — projetos nativos gerados em `src-tauri/gen/android` e `.../gen/apple`; o
`lib.rs` já tinha o `mobile_entry_point`. Permissões do Meet declaradas (câmara/micro no
AndroidManifest e no Info.plist — sem estas a App Store recusa) + `POST_NOTIFICATIONS`. O
`cargo check` do desktop continua a passar (não quebrou). Runbook em `src-tauri/MOBILE.md`.
**Falta (precisa de contas externas):** push nativo FCM/APNs e o **spike do WebRTC no WKWebView iOS**
(decisão: validar antes de comprometer o Meet ao Tauri; plano B = Safari via opener).
→ `src-tauri/gen/*`, `AndroidManifest.xml`, `Info.plist`, `MOBILE.md`.

**Rodada 5 — §1 tema/idioma seguem o utilizador + §5 upload condicional (decisão):**
- **§1 sync de tema/idioma:** agora persistidos em `settings.general` (usa a infra JSONB existente).
  Ao autenticar, o servidor manda (reconcilia tema e idioma); o `localStorage` fica como cache
  instantâneo pré-login. Faz sentido com web + desktop + mobile. `tsc` limpo. → `App.tsx`,
  `SettingsView.tsx`, `backend.ts`.
- **§5 upload condicional (If-Match):** na altura foi adiado por YAGNI; **agora implementado** na
  Rodada 7 (M2), que é exatamente o "escritor real" que justifica proteger o caminho de gravação.

**Rodada 4 — §3 responsividade:** correção ao diagnóstico — Chat, Webmail, o shell (nav vira barra
inferior), a busca e Admin/Definições **já eram responsivos e wired** (drawers, troca lista↔detalhe,
`chat-burger`/`mobtop`). O que faltava e foi entregue (CSS, validado com `npm run build`): **Calendário**
mobile (mês compacto, grade semana/dia com scroll horizontal em vez de esmagar colunas, agenda da IA
como cartão de largura total), **Kanban** com scroll-snap (uma coluna por ecrã) e a barra de ações do
**Meet** a quebrar linha em vez de sair do ecrã. Files já usava `auto-fill minmax` (responsivo).
Follow-up opcional (JS): o Calendário no telemóvel abrir por defeito na vista agenda/dia.
→ `webmail.css`.

**Rodada 3 — §5 M1: daemon de sync em Rust (nuvem → local, só leitura):** novo crate separado
`sync-daemon/` (`mailin-sync`). Consome `/api/sync/files/changes` com o token de dispositivo,
mantém índice em SQLite (id→caminho+hash+versão) e cursor, baixa o novo/alterado (dedup por hash) e
aplica tombstones. **Segurança:** HTTPS obrigatório fora de localhost; token em ficheiro `0600`;
**sanitização de path contra traversal** (`safepath.rs` + testes que passam); só apaga o que o
próprio daemon indexou; escrita atómica (part+rename). `cargo build`/`cargo test` limpos; adicionado
job no CI. Limitações do M1 (documentadas no `sync-daemon/README.md`): estrutura plana (árvore = M2),
sem upload local→nuvem ainda. → `sync-daemon/*`, `.github/workflows/ci.yml`.

**Correções ao diagnóstico (já existia, ao contrário do que a v1 dizia):**
- **§1** As preferências **já são server-side por app**: coluna JSONB `mail_users.settings` (migração
  00029) com merge de topo, rotas `GET/PATCH /api/settings`, e `SettingsView` **já tem rail de secções**
  (Geral/E-mail/Notificações). O fuso do calendário já usa o `Intl` do dispositivo (item 🔴 do GAPS
  satisfeito). Logo, "prefs server-side" e "settings por app" **não são lacuna** — o que resta é
  decisão (abaixo), não código em falta.
- **§6** O compose **já tem** healthchecks (Postgres/MinIO/OnlyOffice/api) e a maioria das imagens
  **já está pinada por digest** (MinIO/OnlyOffice/LiveKit/Egress); faltam pinar Redis e LiveKit-server.

**Estado final do código (após rodadas 1–10):** todos os pontos **implementáveis e verificáveis
neste ambiente estão feitos**. O que resta depende de recursos externos que só você fornece — não é
código pendente:
- **Push nativo mobile** — contas Firebase (FCM) + certificado APNs (Apple).
- **Publicação nas lojas** — conta Apple Developer + keystore Android + política de privacidade.
- **Spike WebRTC iOS** — correr `tauri ios dev` num dispositivo/simulador seu (init, permissões e
  runbook já prontos em `src-tauri/MOBILE.md`).
- **Entregabilidade de e-mail** (DKIM/SPF/DMARC/rDNS) — depende do seu DNS e IPs.
- **Observabilidade** (Prometheus/Sentry) — você optou por não adicionar dependências agora (reversível).
- **Polimento de sync** (UI de status; propagar move/delete locais) — melhorias futuras, não bloqueiam.

## Sumário executivo

| Frente | Estado atual | Gap principal | Esforço |
|---|---|---|---|
| 1. Configurações por app | ✅ Prefs server-side por app (JSONB) + rail de secções **já existem** | Só decisão: mais secções (YAGNI) e se tema/idioma sincronizam entre aparelhos | P (se decidido) |
| 2. Design dos modos de ver (Meet) | ✅ Layout de gravação + PiP **entregues** nesta rodada | Grid adaptativo no mobile (parte da frente 3) | — |
| 3. Responsividade real | Desktop-first: ~7 `@media` em 2.091 linhas de CSS | Estratégia de breakpoints + refactor por módulo | G |
| 4. App mobile Tauri | Tauri 2 desktop **já existe** (`frontend/src-tauri`); mobile não inicializado | `tauri android/ios init` + push nativo + UI mobile (depende da frente 3) + spike WebRTC iOS | G |
| 5. Sync de diretórios (Rust) | Zero. Files = MinIO via API (`internal/storage`) | Motor de sync em Rust + API de deltas (feed de changes é o pré-requisito) | G |
| 6. Produção | ✅ Backup/restauro, CI e `/readyz` **entregues**; compose já tinha healthchecks + digests | Métricas/observabilidade (decisão de dep), pinar Redis/LiveKit, updater Tauri | M |

---

## 1. Configurações individuais por app

### Estado atual
- Uma única página: `frontend/src/webmail/SettingsView.tsx` — idioma, tema e notificações.
- **Tudo em localStorage** (`mailin.lang`, tema, flags de web push). Consequência: preferências
  não seguem o usuário entre aparelhos — inaceitável a partir do momento em que existem web,
  desktop e mobile (frentes 3–5).
- Configurações que já existem espalhadas: assinatura (`SignatureEditor.tsx`), regras
  (`RulesView.tsx`), férias — todas do webmail, sem um "lar" comum.

### O que fazer
- [x] 🔴 **Prefs server-side.** ✅ Feito — não numa tabela `user_prefs` nova, mas na coluna JSONB
      `mail_users.settings` (merge por secção no servidor) já existente; cliente com escrita otimista
      via `useSettings.ts`. Rodadas 5 e 12.
- [x] 🔴 **Settings com navegação por app** ✅ (Rodada 12): rail com Geral · E-mail · Chat ·
      Calendário · Ficheiros · Meet · Kanban · Notificações · Dispositivos. (Contactos não entrou —
      sem preferência real a ligar.)
- [x] 🔴 Chaves i18n de cada preferência em pt-BR **e** en ✅ (Rodada 12).

### Preferências propostas por app (primeira leva)
| App | Preferências |
|---|---|
| Geral | idioma, tema (claro/escuro/sistema), densidade, página inicial |
| Webmail | assinatura, janela de undo-send, comportamento de arquivar, confirmação de leitura, imagens remotas on/off, atalhos on/off |
| Chat | notificar (all/menções/nada) default, sons, presença default, Enter envia |
| Calendário | fuso horário, primeiro dia da semana, horário visível da grade, duração default, lembrete default |
| Ficheiros | visão (grid/lista), ordenação, expiração default de link |
| Meet | câmera/mic/alto-falante default, entrar mutado, fundo virtual default, qualidade |
| Kanban | quadro inicial, visão default (kanban/lista/calendário) |

Nota: fuso horário do calendário já é 🔴 no GAPS (§Calendário) — resolver junto, guardando em `user_prefs`.

---

## 2. Design responsivo dos modos de "ver" (Meet e afins)

### Estado atual
- Meet usa o prebuilt `<VideoConference />` do LiveKit (`MeetRoom.tsx`, `Guest.tsx`) + camada
  própria de apresentação (`MeetPresentation.tsx`, estilo BBB), efeitos, reações, notas.
- Pendentes no GAPS (§Meet, 🟡): **layout de gravação configurável; PiP**.

### O que fazer
- [x] 🔴 **Layout de apresentação responsivo:** ✅ (Rodada 8) — no telemóvel o palco fica em largura
      total e as câmaras viram uma fita horizontal por baixo.
- [x] 🟡 **PiP nativo** ✅ (Rodada 2) — botão de Picture-in-Picture na barra de ações, feature-detected.
- [x] 🟡 **Layout de gravação configurável** ✅ (Rodada 2) — host escolhe grid/orador/só-orador,
      validado no backend (`recordLayout`).
- [ ] 🟡 Grid de tiles com regras por largura (1 col <480px, 2 col <900px…) — **não feito**; o grid
      normal ainda usa o default do prebuilt do LiveKit (só a apresentação foi tornada responsiva).

---

## 3. Responsividade real (desktop e mobile)

### Estado atual — o gap mais estrutural do frontend
- `webmail.css`: **2.091 linhas com apenas 7 `@media`**, e só 3 breakpoints reais
  (720/860/1024px), quase todos para o burger do chat. `index.css`: zero. Tailwind está
  configurado, mas só **1 arquivo** usa prefixos responsivos (`sm:`/`md:`/`lg:`).
- Na prática a UI é desktop-only: webmail 3 colunas, calendário em grade, kanban horizontal —
  nada disso opera bem <768px hoje. A janela Tauri já permite `minWidth: 380`, ou seja, o
  desktop atual já expõe o problema.

### Estratégia proposta
- [x] 🔴 **Contrato de breakpoints único** ✅ (Rodada 10) — Tailwind `sm 640 / md 721 / lg 1024 /
      xl 1280` (md=721 casa o `max-width:720` que o `webmail.css` já usava), `important:true`.
- [x] 🔴 **Padrão "lista → detalhe" empilhado no mobile** ✅ — já existia e wired em webmail e chat
      (drawers, `mobtop`, `chat-burger`); confirmado na Rodada 4.
- [x] 🔴 **Rail de módulos vira bottom-nav** ✅ — já existia (`@media ≤720` no shell).
- [x] 🔴 **Calendário mobile** ✅ (Rodada 4) — mês compacto + semana/dia com scroll horizontal +
      agenda como cartão. (Abrir por defeito na vista agenda no telemóvel fica como opção JS.)
- [x] 🟡 Kanban mobile: scroll-snap, uma coluna por ecrã ✅ (Rodada 4).
- [~] 🟡 Compose/modais full-screen sheet <md ✅ (já existia: `.cmp`/`.tk` a 96vw); **falta** só
      "tabelas do admin viram cards" (o painel admin fica desktop-first por agora).
- [ ] 🟡 Auditoria de toque (long-press/swipe, alvos ≥44px, `safe-area-inset`) — **não feito**;
      refinamento para quando o mobile for a sério (frente 4).

**Ordem por módulo (impacto × uso mobile):** Chat → Webmail → Meet (frente 2) → Calendário →
Ficheiros → Contatos → Kanban → Painel admin (pode ficar desktop-first 🟢).

**Decisão a tomar (lei 6.2):** migrar gradualmente para utilitários Tailwind responsivos
(recomendado — já está instalado; evita crescer os .css monolíticos) vs. continuar em CSS puro
com `@media`. Recomendo Tailwind para código novo/refatorado, sem reescrita big-bang.

---

## 4. App mobile com Tauri

### Estado atual — melhor do que parece
- `frontend/src-tauri` já existe em **Tauri 2**, com o crate `mailin_lib` já preparado para
  mobile (`crate-type = ["staticlib", "cdylib", "rlib"]` — exatamente o que iOS/Android pedem)
  e ícones android/ios no scaffold. Plugins: opener, notification.
- **Mobile não inicializado:** `gen/schemas` só tem desktop/macOS — falta `tauri android init`
  / `tauri ios init` (geram `gen/android` e `gen/apple`).
- Integrações desktop já existentes reaproveitáveis: `tauriNotify.ts`, `nativeWindow.ts`.

### O que fazer
- [x] 🔴 `tauri android init` + `tauri ios init` ✅ (Rodada 6) — projetos nativos gerados em
      `src-tauri/gen/{android,apple}`; job no CI (`cargo check`). Runbook em `src-tauri/MOBILE.md`.
- [x] 🔴 **Pré-requisito: frente 3** ✅ — responsividade fechada (Rodadas 4/8/10).
- [ ] 🔴 **Push real com app fechado (FCM/APNs)** — **não feito**; precisa de contas Firebase/Apple.
- [ ] 🔴 Deep links (`tauri-plugin-deep-link`) — **não feito**.
- [x] 🔴 Permissões nativas câmera/microfone no Info.plist/AndroidManifest ✅ (Rodada 6, validadas).
      O **spike do WebRTC no WKWebView (iOS)** continua por fazer — precisa de dispositivo/simulador seu.
- [ ] 🟡 Biometria (`tauri-plugin-biometric`) — **não feito**.
- [ ] 🟡 Teclado virtual (`interactive-widget=resizes-content`) — **não feito**.
- [ ] 🟢 Publicação nas lojas (contas, review Apple, política de privacidade) — **não feito** (contas).

**Custo recorrente a assumir:** matriz de teste passa a ser web + macOS/Windows/Linux + Android
+ iOS. Vale definir 2 alvos de 1ª classe (ex.: web + Android) e o resto best-effort no início.

---

## 5. Sincronizar diretórios com a nuvem (Rust, desktop e mobile)

### Estado atual
- Zero sync. Ficheiros = upload manual via API (`internal/storage` → MinIO), com dedup,
  versões, lixeira e partilha já entregues (GAPS §Ficheiros ✅).
- Não há endpoint de **deltas/changes** nem identificador de conteúdo exposto (etag/hash) —
  pré-requisitos de qualquer cliente de sync.

### Arquitetura proposta (estilo Dropbox, simplificado)
**Crate Rust `mailin-sync`** dentro de `src-tauri` (workspace), usado pelo desktop e mobile via
comandos Tauri — mesma abordagem do `mailin_lib` atual, sem segundo binário.

Componentes:
1. **Watcher** — crate `notify` (FSEvents/inotify/ReadDirectoryChangesW) + varredura periódica
   de reconciliação (watchers perdem eventos).
2. **Índice local** — SQLite (`rusqlite`): caminho, tamanho, mtime, hash BLAKE3, `remote_id`,
   `remote_version`, estado (`synced/dirty/conflict/pending`).
3. **Protocolo com o backend (novo, Go):**
   - `GET /api/files/changes?since=<cursor>` — feed incremental por tenant/pasta (cursor
     monotônico; tabela `file_events` ou coluna `updated_seq`).
   - Upload/download já existem; adicionar **hash BLAKE3 nos metadados** (dedup já existe —
     alinhar o algoritmo) e upload condicional (`If-Match`) para detectar corrida.
4. **Motor de decisão** — three-way (estado local vs. índice vs. remoto). Conflito = nunca
   sobrescrever: gravar `nome (conflito de <dispositivo> 2026-07-19).ext`, marcar na UI.
5. **Transferência** — fila com limite de concorrência, retomável, backoff; respeitar cota
   (409 da API) e idempotência (lei 5.3).

Etapas:
- [x] 🔴 **M0 — backend:** feed de changes ✅ (`GET /api/files/changes`, migração 00078) + hash como
      ETag ✅. Falta ainda o **upload condicional** (`If-Match` no save do OnlyOffice) — pequeno, mas
      toca no caminho de gravação; deixado para o M1 quando o daemon precisar dele.
- [x] 🔴 **M1 — desktop one-way** ✅ (download da nuvem → pasta local). Entregue no `sync-daemon/`.
- [x] 🔴 **M2 — desktop bidirecional** ✅ completo: upload condicional (If-Match/412), cópias de
      conflito, **espelho de subpastas**, **agente com ícone na barra** (estado + menu + autostart via
      launchd/systemd), **delete local → lixeira** e **move local → move** (com guarda anti-apagão),
      **watcher em tempo real** + pré-filtro mtime/size, **reconciliação horária** (apanha a purga),
      **partilhados comigo** em `Partilhados/` (só-leitura) e **pastas seletivas**.
      Falta só: transferências concorrentes e limite de banda (otimizações).
- [ ] 🟡 **M3 — mobile.** Escopo reduzido e realista: **sem espelho contínuo de diretório**
      (iOS não dá acesso a FS arbitrário nem daemon em background). Oferecer: pastas
      "disponíveis offline" (cache gerido pelo app) + camera upload (Android primeiro;
      WorkManager-like via tarefas ao abrir o app). Espelho real no iOS exigiria File Provider
      Extension nativa — 🟢 adiar.
- [x] 🟢 **M4 — delta de blocos** ✅ Enviado só o que muda em ficheiros grandes: o servidor expõe
      hashes por bloco (`GET /sync/files/{id}/blocks`) e aceita `PUT .../delta` com apenas os blocos
      alterados, **reconstruindo** o ficheiro (o resto é copiado do blob antigo). O armazenamento
      continua a ser um blob inteiro por ficheiro — sem tocar em download/versões/OnlyOffice/zip.
      Só acima de 8 MiB e quando muda <70%; senão (ou sem suporte) cai no envio completo. Reconstrução
      com testes (crescer/encolher/bloco parcial/input inválido). → `files_sync_delta.go`, `sync.rs`.

**Decisão a tomar (lei 6.2):** sync como parte do app Tauri (recomendado: 1 instalador, UI
pronta, IPC simples) vs. daemon separado que roda sem o app aberto (mais "Dropbox", porém
tray/autostart/empacotamento próprios). Dá para evoluir de A para B depois.

---

## 6. O que mais falta para produção

### Estado atual do deploy
`deploy/docker-compose.yml` sobe stalwart, postgres, minio, api, onlyoffice, livekit, redis,
egress e worker, com Caddy (TLS) na frente. Hardening de app já feito (CSRF, rate limit, TOTP
cifrado, fail-closed). **Não existem:** backup, observabilidade, CI/CD, gestão de segredos,
updater dos apps.

### Lacunas por ordem de risco
- [x] 🔴 **Backups + restore.** Entregue: `scripts/backup.sh` (pg_dump lógico + tar dos volumes
      MinIO/Stalwart, retenção via `RETENTION_DAYS`) e `scripts/restore.sh` (destrutivo, confirma,
      pára api/worker). Falta agendar (cron/systemd-timer) e **ensaiar o restauro** num ambiente real.
- [~] 🔴 **Observabilidade.** Parcial: `/readyz` (pinga o Postgres) + healthchecks no compose ✅
      (Rodada 1). **Falta** `/metrics` (Prometheus) + Sentry — **por decisão sua** (sem dependências
      novas por agora). Reversível quando quiser.
- [x] 🔴 **CI/CD** ✅ (Rodada 1) — `.github/workflows/ci.yml`: gofmt+vet+build+test+sqlc (backend),
      tsc+vitest (frontend), cargo check do Tauri e do `sync-daemon`. Árvore deixada gofmt-limpa.
- [~] 🔴 **Itens arquiteturais de segurança:** credencial JMAP brokerada ✅ (existia; Caddyfile
      corrigido na Rodada 11) · **pin de imagens por digest** ✅ (Rodada 3: postgres+redis, restantes
      já estavam) · **rotação de segredos** — **não feito** (procedimento/tooling por definir).
- [ ] 🔴 **Entregabilidade de e-mail** (produto é servidor de e-mail): checklist por domínio —
      DKIM/SPF/DMARC no provisionamento, rDNS/PTR, aquecimento de IP, monitor de blacklists,
      relatórios DMARC. Validar o que o Stalwart 0.16 já provisiona via JMAP `x:*` e registrar
      divergências (DIVERGENCES.md).
- [~] 🟡 **Updater dos apps.** `devtools` **removido do release** ✅ (Rodada 3). **Falta** o
      `tauri-plugin-updater` + assinatura de builds — precisa de chaves de assinatura suas.
- [ ] 🟡 **Compat de API versionada.** Com apps instalados (desktop/mobile), clientes velhos
      passam a existir: política de compatibilidade (aditivo por padrão; `X-Client-Version`;
      resposta 426 para mínimo suportado).
- [ ] 🟡 **E2E smoke** (Playwright): login → enviar e-mail → receber → chat → subir arquivo →
      criar evento. Roda contra a stack do compose no CI. Cobre o risco nº 1 (regressão entre
      módulos integrados).
- [ ] 🟡 **LGPD/privacidade:** política de privacidade (obrigatória nas lojas), exportação e
      apagamento de conta/tenant, DPA para clientes, retenção de gravações do Meet.
- [ ] 🟡 **Operação multi-tenant:** limites por plano aplicados no backend (cota já existe p/
      arquivos; falta p/ gravações, participantes Meet — GAPS §Meet), página de status/uptime.
- [~] 🟢 Docs de operação: já há `sync-daemon/README.md` e `src-tauri/MOBILE.md`; **falta** o
      runbook geral (subir stack do zero, restaurar backup, girar segredos, onboarding de domínio).

---

## Ordem de ataque sugerida

1. **§6 Backups + observabilidade mínima + CI** — protege o que já existe; independe do resto. (M)
2. **§1 Prefs server-side + Settings por app** — destrava fuso do calendário e é pré-requisito
   de multi-dispositivo. (M)
3. **§3 Responsividade: Chat → Webmail → Meet** (+ §2 no passo do Meet). (G, incremental)
4. **§4 Tauri mobile init + spike WebRTC iOS + push FCM/APNs** — spike cedo; o resultado do
   WebRTC no WKWebView pode mudar o plano do Meet mobile. (G)
5. **§5 Sync M0 (backend) → M1/M2 desktop** — mobile (M3) só depois do app mobile estabilizar. (G)

## Débito técnico tocado por esta avaliação
- `webmail.css` (2.091 linhas) e `panel.css` monolíticos — quebrar junto com o refactor
  responsivo (lei 1.4 vale em espírito para CSS).
- Preferências device-local espalhadas (`mailin.lang`, `mailin.theme`, flags de push) — migrar para
  `mail_users.settings` **se** decidirmos sincronizar entre aparelhos (ver Perguntas, Q1).
- `devtools` habilitado no build release do Tauri (comentário no próprio Cargo.toml pede remoção).

---

## Perguntas em aberto (decisões suas antes de eu avançar)

Estes itens ficaram **deliberadamente não implementados** — dependem de uma escolha estrutural
(lei 6.2) ou de ferramentas/ambiente que exigem a sua direção. Ordem = maior impacto primeiro.

1. **§4 Meet mobile / WebRTC no iOS.** O caminho Tauri para o Meet no iOS depende de o LiveKit
   (WebRTC) funcionar no WKWebView — historicamente frágil. Quer que eu faça um **spike** para
   validar antes de comprometer o Meet mobile ao Tauri? Plano B é abrir o Meet no Safari.
2. **§5 Sync em Rust — M1 (daemon) precisa de auth para cliente headless.** ✅ Decidido: daemon
   separado; ✅ M0 (feed) entregue. **Novo bloqueio antes do M1:** a API só autentica por cookie de
   sessão + CSRF (fluxo de browser). Um daemon headless precisa de outra via. Qual?
   - (a) **Token de dispositivo** de longa duração, emitido no login e revogável (recomendado: simples,
     revogação por dispositivo). Exige um endpoint de emissão + tabela `device_tokens`.
     — decisão de schema/auth, por isso pergunto (lei 6.2).
   - (b) Reusar a sessão do app Tauri (o daemon partilha o cookie) — só serve se o daemon viver dentro
     do app; contradiz "daemon separado".
   Escolhida a via, implemento o M1 (poll do feed → índice SQLite local → download one-way).
3. **§6 Observabilidade — dependência.** Adiciono `prometheus/client_golang` para `/metrics` +
   um SDK de erros (Sentry/GlitchTip)? São dependências novas (leis 2.5/6.2), por isso pergunto.
   Sem sdk, deixe para depois
4. **§1 Settings — sincronizar tema/idioma entre aparelhos?** Hoje são device-local (localStorage),
   o que é defensável. Movo para `mail_users.settings` (seguem o usuário, custo: flash inicial a
   resolver)? E quais preferências novas realmente quer agora (só crio secção nova com uso real —
   YAGNI)?
   Sim
5. **§3 Responsividade — profundidade.** É um refactor grande (desktop-first hoje). Faço por módulo
   na ordem Chat → Webmail → Meet → Calendário, entregando um de cada vez? Confirmo também a
   estratégia: utilitários Tailwind responsivos no código novo (recomendado).
   R. Sim
