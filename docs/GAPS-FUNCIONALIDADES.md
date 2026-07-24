# Roadmap de funcionalidades (análise de lacunas) — Beskos

> Levantamento por módulo (webmail, contatos, chat, kanban, calendário, ficheiros, meet) frente aos
> concorrentes modernos (Gmail, Slack, Trello, Google Calendar/Drive/Meet). Marcado o que já ficou
> pronto. **Legenda:** `[x]` feito · `[ ]` pendente · **🔴 essencial** · **🟡 nice-to-have**.
>
> Última atualização: 2026-07-18.

## Progresso

| Bloco | Feito | Total (essenciais) |
|---|---|---|
| Bloco 1 — "prometido e não funciona" | **5 / 5** ✅ | 5 |
| Chat | **6 / 6** ✅ | 6 |
| Webmail | 2 / 8 | 8 (2 do Bloco 1) |
| Kanban | 1 / 8 | 8 (1 do Bloco 1) |
| Calendário | 0 / 8 | 8 |
| Contatos/CRM | 0 / 5 | 5 |
| Ficheiros | 0 / 6 | 6 |
| Meet | 1 / 7 | 7 (1 do Bloco 1) |

---

## ✅ Bloco 1 — Prometido na UI e não funcionava (CONCLUÍDO)

Bugs de completude: a opção existia mas não tinha efeito. Todos corrigidos e validados (build +
typecheck; #2 e #3 testados ao vivo contra a stack).

- [x] **🔴 Regras de e-mail de SAÍDA avaliadas no envio.** `outboundBcc()` aplica Bcc automático das
      regras que casam (honra `stop`); o editor de saída passou a oferecer só ações aplicáveis.
      → `frontend/src/webmail/rules.ts`, `Compose.tsx`, `RulesView.tsx`.
- [x] **🔴 UI de marcadores (labels) de e-mail.** Aplicar/remover/criar/apagar no reader, chips na
      lista e no e-mail aberto, filtro na barra lateral (`/mail/mail/label:<id>`). Labels = keyword
      JMAP derivada do id do catálogo. → `MailLabels.tsx`, `MailView.tsx`, `MailReader.tsx`, `jmap/mail.ts`.
- [x] **🔴 Ordenação vertical do Kanban.** Novo `POST /columns/{id}/reorder` (atômico, `WITH
      ORDINALITY`); drop posição-aware. Antes gravava sempre `position=0`. → `tasks_handlers.go`, `KanbanView.tsx`.
- [x] **🔴 Extração de tarefas por IA preserva o responsável.** O nome sugerido é casado com um colega
      real (nome/e-mail) → `assignee_id`; mostra "sem correspondência" quando não bate. → `ExtractTasksModal.tsx`.
- [x] **🔴 Meet: chat vs. data channel.** `canPublishData` sempre `true` (não quebra apresentação/laser
      ao desligar o chat); chat on/off aplicado no cliente. → `meet.go`, `MeetRoom.tsx`.

---

## 💬 Chat (Slack-like)

**Essenciais**
- [x] **🔴 Paginação de histórico.** Cursor `?before=` (nano-precisão) + "Carregar anteriores" com
      preservação de scroll. Antes, além das 100 últimas era inacessível. → `chat_handlers.go`,
      `ListMessagesBeforeByChannel`, `ChatView.tsx`. *(validado ao vivo)*
- [x] **🔴 Notificação de DM/mensagem.** DM sempre avisa o outro; canal avisa quem escolheu "all"
      (nunca o autor nem quem já recebeu menção). → `chat_handlers.go`, `GetChannelMeta`/`ListChannelMemberPrefs`. *(validado ao vivo)*
- [x] **🔴 Saltar para a mensagem.** `?around=<id>` devolve uma janela (antigas+novas) em torno do
      alvo; a busca e os pins rolam até a mensagem e a destacam (flash). → `chat_handlers.go`
      (`ListMessagesAfterByChannel`, branch `around`), `ChatView.tsx`. *(validado ao vivo)*
- [x] **🔴 Linha de "novas mensagens" + marcar como não lido.** Divisória captura o `last_read_at` na
      abertura; ação "marcar não lido" recua o cursor. → `chat_handlers.go` (`SetLastReadAt`,
      `handleMarkChannelUnread`), `ChatView.tsx`. *(validado ao vivo)*
- [x] **🔴 DMs em grupo.** `POST /chat/group-dm` com `dm_key` canônico (dedup por conjunto de
      pessoas); nome junta os participantes; modal de nova conversa vira multi-seleção (1=DM, 2+=grupo).
      → `chat_handlers.go` (`handleOpenGroupDM`, `dmKeyN`, `dmOther`), `ChatView.tsx`. *(validado ao vivo)*
- [x] **🔴 Moderação.** Gestor/owner apaga msg de terceiro (`DeleteMessageAny`); owner exclui o canal
      (`DeleteChannel`, cascata). → `chat_handlers.go`, `ChatView.tsx`. *(validado ao vivo)*

**Nice-to-have**
- [x] 🟡 **Encaminhar/citar mensagem.** `POST /chat/messages/{id}/forward` cria no destino uma
      mensagem citando a original (exige ser membro da origem e do destino); ação no hover + modal de
      escolha do canal. → `chat_handlers.go` (`handleForwardMessage`, `fanoutMessage`), `ChatView.tsx`. *(validado ao vivo)*
- [x] 🟡 **Salvar mensagem (saved items).** Salvar/dessalvar (`saved_messages`, migração 00043), estado
      por mensagem na lista, painel "Salvos" no rail com jump. → `chat_handlers.go`, `ChatView.tsx`. *(validado ao vivo)*
- [x] 🟡 Lembretes ("remind me"), agendar envio de mensagem
- [x] 🟡 **Recibos de leitura ("visto por").** O endpoint de membros passou a expor `last_read_at`; a
      última mensagem mostra "Visto"/"Visto por …" de quem leu (≥ created_at). → `chat_handlers.go`, `ChatView.tsx`.
- [x] 🟡 **Link unfurl (preview de URLs).** `GET /chat/unfurl` busca OG/title com **guarda de SSRF**
      (bloqueia IP privado/loopback/link-local no dial — testado contra 169.254.169.254/minio/stalwart),
      timeout, teto de corpo, redirects limitados, cache 1h. Cartão de preview na mensagem.
      → `unfurl.go`, `ChatView.tsx`. *(validado ao vivo)*
- [x] 🟡 **Slash commands, webhooks de entrada, bots.** Slash client-side (`/me`, `/shrug`,
      `/tableflip`, `/unflip`) transformam o texto antes do envio. Webhooks de entrada: token por
      canal (`chat_webhooks`, migração 00073), `POST /api/public/chat/hook/{token}` (público, rate-
      limited) com `{text}` vira mensagem-bot (`messages.bot_name`) publicada em tempo real via Hub.
      Gestores criam/copiam/removem no painel do canal; mensagens de bot mostram avatar + badge APP.
      → `chat_webhooks.go`, `ChatView.tsx`. *(validado ao vivo: criar→POST público→bot_name→apagar; empty=400, bad token=404)*
- [x] 🟡 **Busca FTS com filtros `from:`/`before:`/`after:`.** Coluna `body_tsv` gerada + índice GIN
      (migração 00044); busca por `plainto_tsquery` e filtros parseados no handler. → `chat_handlers.go`. *(validado ao vivo)*
- [x] 🟡 **`@channel` / `@here`.** Notificam todos os membros / só os online (`OnlineAmong`), com dedup
      e respeitando mute; autocomplete oferece as opções. → `chat_handlers.go` (`hasMentionToken`), `ChatView.tsx`. *(validado ao vivo)*
- [x] 🟡 **Presença rica (ativo/ausente/não-perturbe).** `mail_users.presence` (migração 00045), seletor
      na UI, cor por estado na presença dos DMs, e **DND suprime o web push**. → `chat_handlers.go`,
      `push_handlers.go`, `App.tsx`, `ChatView.tsx`. *(validado ao vivo)*
- [x] 🟡 Emoji custom do workspace
- [x] 🟡 **Edição multilinha** (`<textarea>`; Enter salva, Shift+Enter quebra linha).
- [x] 🟡 **Entrega otimista.** O evento WS "message" apenda a mensagem ao cache (dedup por id) em vez
      de refazer o fetch inteiro do canal; edição/remoção/reação ainda recarregam. → `ChatView.tsx`.
- [x] 🟡 **Removidos handlers órfãos** `ListChannelFiles`/`ListChannelLinkMessages` (sem rota).

---

## ✉️ Webmail (Gmail-like)

Já muito completo (threads, busca avançada, Sieve, agendar/undo-send, anexos grandes, IA). Restam:

**Essenciais**
- [x] **🔴 Regras de saída** (ver Bloco 1)
- [x] **🔴 Labels de e-mail** (ver Bloco 1)
- [x] **🔴 Push de e-mail NOVO com app fechado** (hoje só notifica com aba aberta; o backend não observa a chegada no Stalwart).
- [x] **🔴 Resposta de férias com UI própria** (datas início/fim, escopo interno/externo). Hoje só como ação de "regra".
- [x] **🔴 Botão Arquivar de 1 clique** (+ atalho) — hoje é "Mover para".
- [x] **🔴 Gestão de pastas** (renomear/apagar/aninhar — só criar na raiz existe).
- [x] **🔴 Aliases / múltiplos "De:"** (remetente fixo hoje).
- [x] **🔴 Snooze/adiar e-mail.**

**Nice-to-have**
- [x] 🟡 Rascunho/resposta preservando HTML (hoje recarrega só texto)
- [x] 🟡 Atalhos de teclado (j/k/e/r/#); ordenação da lista
- [x] 🟡 **Undo-send server-side; gerir e-mails agendados.** Outbox no servidor (`outbox_emails`,
      migração 00074): o cliente cria o rascunho no JMAP (`prepareOutbox`) e regista o envio real
      (email_id + envelope + `send_at`); o worker submete quando vence via `UserClient.SubmitEmail`
      (`EmailSubmission/set` com a credencial JMAP brokerada, destrói o rascunho no sucesso), num
      ticker rápido de 10s. Undo-send = `send_at` agora+janela, cancelável no servidor (sobrevive
      fechar a aba); agendados gerenciáveis num modal (listar/cancelar). Substitui o timer client-side
      e o HOLDFOR. → `outbox_handlers.go`, `mailbox/user.go`, `cmd/worker` (`scanOutbox`),
      `jmap/mail.ts`, `Compose.tsx`, `ScheduledModal.tsx`. *(validado ao vivo: rascunho→outbox→worker
      entrega + rascunho destruído + status `sent`; agendado→cancelar 200→409; validação 400)*
- [x] 🟡 **Templates de resposta; aviso de anexo esquecido.** Templates (canned responses):
      `mail_templates` (migração 00075, CRUD por dono), menu no compositor para inserir (preenche
      assunto se vazio + prepende o corpo HTML sanitizado), salvar o rascunho atual como modelo e
      apagar. Aviso de anexo esquecido: heurística no compositor (`ATTACH_RE` pt/en) confirma antes
      de enviar quando o texto menciona anexo mas nada foi anexado. → `templates_handlers.go`,
      `Compose.tsx`. *(validado ao vivo: CRUD 200/400/404 owner-scoped)*
- [ ] 🟡 Caixas partilhadas (shared mailboxes) — adiado: exige arquitetura JMAP multi-conta (o
      webmail fala JMAP com o token do próprio utilizador; caixa partilhada implica delegação/ACL no
      Stalwart). Decidir provisionamento antes de implementar.

---

## 📇 Contatos / CRM

Partilha por equipa acima da média; faltam os básicos de interoperabilidade e o elo com a caixa.

**Essenciais**
- [x] **🔴 Importar/Exportar vCard/CSV.**
- [x] **🔴 Histórico de e-mails na ficha do contato** (a busca `from:/to:` já existe — é ligar).
- [x] **🔴 Múltiplos e-mails/telefones por contato** (hoje 1 de cada).
- [x] **🔴 Captura automática** (remetente do e-mail → "adicionar aos contatos").
- [x] **🔴 Grupo como lista de distribuição** (escrever para o grupo inteiro).

**Nice-to-have**
- [x] 🟡 Campos customizados; fusão de duplicados; foto/avatar
- [x] 🟡 Pipeline/negócios (deals); notas editáveis; transferência de dono
- [x] 🟡 **Enriquecimento por domínio (logo, pessoa↔empresa automático).** Vínculo automático:
      ao criar/editar uma pessoa, se não houver empresa explícita, liga à empresa do dono com o mesmo
      domínio (do campo `domain` ou do e-mail) via `FindCompanyByDomain`. Botão "Enriquecer" roda
      `LinkPeopleByDomain` (idempotente) para ligar as pessoas já existentes. Logo da empresa via
      favicon do próprio site (`faviconOf`, sem provedor externo/broker) com fallback ao monograma.
      → `contacts_handlers.go`, `queries.sql`, `ContactPage.tsx`, `ContactsView.tsx`, `util.ts`.
      *(validado ao vivo: auto-link no create + enrich pós-fato `{linked:1}`)*

---

## 🗂️ Kanban / Tarefas (Trello-like)

**Essenciais**
- [x] **🔴 Ordenação vertical no drag** (ver Bloco 1)
- [x] **🔴 Checklist / subtarefas.**
- [x] **🔴 Anexos no card** (o módulo Files existe, mas não liga a cards).
- [x] **🔴 Prioridade** (existia em `tickets`, removida na 00024).
- [x] **🔴 Histórico/atividade do card.**
- [x] **🔴 Notificação de prazo vencendo/vencido** (o `due` é só texto; nada avisa).
- [x] **🔴 Reordenar colunas** (`position` existe; falta o drag/endpoint).
- [x] **🔴 Labels reais com cor** (ligar ao catálogo de tags do tenant).

**Nice-to-have**
- [x] 🟡 Múltiplos responsáveis; dependências; WIP limit; data de início
- [x] 🟡 Visões lista/calendário/timeline; swimlanes; automações
- [x] 🟡 Arquivar card/quadro; editar comentário; tempo real no board
- [x] 🟡 Corrigir: extração IA envia `assignee`/`due` só p/ 1ª coluna — ok, mas descrição rich-text

---

## 📅 Calendário (Google Calendar-like) — módulo mais atrás

**Essenciais**
- [x] **🔴 Eventos recorrentes (RRULE).** Lacuna nº1 — sem "toda segunda 10h" não serve p/ rotina.
- [x] **🔴 Convidados / RSVP em eventos** (RSVP só existe no Meet; evento não entra na agenda do convidado).
- [x] **🔴 Lembretes/alertas** ("evento em 10 min") — há infra de push, falta o disparo.
- [x] **🔴 Evento de dia inteiro / multi-dia** (hoje só aparece no 1º dia).
- [x] **🔴 Fuso horário por usuário** (hoje `America/Sao_Paulo` hard-coded na IA).
- [x] **🔴 Grade além de 07h–22h** (eventos fora da faixa saem da grade).
- [x] **🔴 Aviso de conflito na EDIÇÃO** (só na criação hoje).
- [x] **🔴 Import/export ICS / convite por e-mail** (irônico num servidor de e-mail).

**Nice-to-have**
- [x] 🟡 Múltiplos calendários; ver agenda de colega / free-busy
- [x] 🟡 Arrastar/redimensionar na grade; adicionar Meet a evento existente
- [x] 🟡 Duplicar evento; desfazer exclusão; visão "agenda/lista"

---

## 📁 Ficheiros (Drive-like)

Núcleo sólido (upload/dedup/pastas/partilha/link público/OnlyOffice/viewer/cota). Faltam:

**Essenciais**
- [x] **🔴 Lixeira / restaurar** (delete é definitivo — maior risco de perda de dado).
- [x] **🔴 Histórico de versões navegável + restaurar** (o save do OnlyOffice sobrescreve).
- [x] **🔴 Busca global recursiva** (hoje filtra só a pasta atual, no cliente).
- [x] **🔴 Upload múltiplo + drag do SO + barra de progresso** (input single-file hoje).
- [x] **🔴 Mover pastas** (só renomear; sem mudar `parent_id`).
- [x] **🔴 Compartilhar com grupo/equipe** (hoje só e-mail individual).

**Nice-to-have**
- [x] 🟡 Expiração configurável do link (hoje fixo 30d); senha no link
- [x] 🟡 Favoritos/estrelas; thumbnails reais; ordenação por coluna
- [x] 🟡 Copiar/duplicar; download de pasta (zip); ações em lote

---

## 🎥 Meet (Google Meet-like)

Cobertura ampla (lobby, gravação, apresentação estilo BBB, notas com IA, fundos virtuais). Restam:

**Essenciais**
- [x] **🔴 Chat vs. data channel** (ver Bloco 1)
- [x] **🔴 Mão levantada + reações emoji** (o data channel da apresentação já existe — barato).
- [x] **🔴 Mutar participante individual** (só há política global e expulsar).
- [ ] **🔴 Legendas/transcrição** (com a IA no produto, transcrição pós-gravação é o passo natural).
- [x] **🔴 Editar/reagendar/recorrer reunião** (só criar e cancelar).
- [x] **🔴 Trancar sala a novas entradas** (lobby só vale p/ convidado externo).
- [x] **🔴 Pré-join** (testar câmera/mic antes de entrar).
- [x] **🔴 Gestão de gravações** (escopo por reunião, apagar, indicador "gravando" p/ todos; player sem HTTP Range).

**Nice-to-have**
- [x] 🟡 **Breakout rooms; co-hosts; limite de participantes por plano.** Co-hosts: `meeting_cohosts`
      (migração 00076); gate `meetingAsHost` dá poderes de moderação (mutar/remover) e presença de
      qualquer host abre a sala trancada; dono promove/despromove na lista (badge + estrela).
      Limite por plano: `billing.Plan.MaxMeetParticipants` (trial 5 · starter 25 · growth 50 ·
      business 200), checado no `handleJoinMeet` contando a sala LiveKit (não-hosts → `full:true`;
      `RoomPage` reintenta até vagar). Breakout rooms: `meeting_breakouts`+`breakout_assignments`
      (migração 00077); host cria N salas (auto-distribui os presentes em rodízio), fecha tudo; cada
      breakout é uma sala LiveKit própria (`<sala>-b<idx>`); o cliente troca de sala (remonta o
      `LiveKitRoom`) e volta à principal (auto quando o host fecha). → `meet_cohosts.go`,
      `meet_breakouts.go`, `meet.go`, `billing/plans.go`, `MeetRoom.tsx`, `Room.tsx`.
      *(validado ao vivo: cohost CRUD; breakouts criar/atribuir/token/fechar; plano trial=5)*
- [x] 🟡 **Layout de gravação configurável; PiP.** Layout: o host escolhe grid/orador+grade/só-orador
      antes de gravar; o backend valida contra a lista do Egress e cai em `grid` para nunca falhar
      (`recordLayout`). PiP: botão na barra de ações põe o vídeo em foco (partilha/orador) numa janela
      flutuante do SO; feature-detected (some no WKWebView do iOS). → `meet_recording.go`,
      `MeetRoom.tsx` (`PipButton`, `RecordButton`), `backend.ts`.
- [x] 🟡 **Indicador de qualidade de rede por tile.** Já entregue pelo `ParticipantTile` do
      LiveKit (prebuilt `<VideoConference />` em uso no `MeetRoom`/`Guest`), que renderiza o
      `ConnectionQualityIndicator` por tile por padrão (verde/amarelo/vermelho conforme a conexão).
      → `MeetRoom.tsx`, `Guest.tsx`. *(confirmado no bundle @livekit/components-react)*

---

## Ordem de ataque sugerida (impacto × esforço)

1. ✅ **Bloco 1** — feito.
2. ✅ **Chat: paginação + notificação de DM** — feito.
3. **Chat: saltar-para-mensagem, não-lidas, DMs em grupo, moderação** — completa o uso diário.
4. **Calendário: recorrência + convidados/RSVP + lembretes** — módulo mais atrás.
5. **Ficheiros: lixeira/soft-delete** — evita perda de dado irreversível.
6. **Sinergias do "é um servidor de e-mail":** ICS no calendário, histórico de e-mails na ficha do
   contato, push de e-mail novo.
7. **Kanban: checklist/anexos/prioridade**; **Meet: mão levantada/mute individual**.

## Débito técnico anexo (não é feature, mas apareceu na análise)
- [x] Notificações guardam texto em PT no backend (viola i18n §4.6); refatorar p/ códigos + tradução no front.
- [x] Chat: evento WS refaz o fetch inteiro do canal (não escala em canais movimentados).
