# Feature roadmap (gap analysis) — Beskos

> Survey by module (webmail, contacts, chat, kanban, calendar, files, meet) against modern
> competitors (Gmail, Slack, Trello, Google Calendar/Drive/Meet). Marked what's already done.
> **Legend:** `[x]` done · `[ ]` pending · **🔴 essential** · **🟡 nice-to-have**.
>
> Last updated: 2026-07-18.

## Progress

| Block | Done | Total (essentials) |
|---|---|---|
| Block 1 — "promised and not working" | **5 / 5** ✅ | 5 |
| Chat | **6 / 6** ✅ | 6 |
| Webmail | 2 / 8 | 8 (2 from Block 1) |
| Kanban | 1 / 8 | 8 (1 from Block 1) |
| Calendar | 0 / 8 | 8 |
| Contacts/CRM | 0 / 5 | 5 |
| Files | 0 / 6 | 6 |
| Meet | 1 / 7 | 7 (1 from Block 1) |

---

## ✅ Block 1 — Promised in the UI and not working (DONE)

Completeness bugs: the option existed but had no effect. All fixed and validated (build +
typecheck; #2 and #3 tested live against the stack).

- [x] **🔴 OUTBOUND e-mail rules evaluated on send.** `outboundBcc()` applies automatic Bcc from
      matching rules (honors `stop`); the outbound editor now only offers applicable actions.
      → `frontend/src/webmail/rules.ts`, `Compose.tsx`, `RulesView.tsx`.
- [x] **🔴 E-mail label UI.** Apply/remove/create/delete in the reader, chips in the list and in
      the open e-mail, sidebar filter (`/mail/mail/label:<id>`). Labels = JMAP keyword derived
      from the catalog id. → `MailLabels.tsx`, `MailView.tsx`, `MailReader.tsx`, `jmap/mail.ts`.
- [x] **🔴 Kanban vertical ordering.** New `POST /columns/{id}/reorder` (atomic, `WITH
      ORDINALITY`); position-aware drop. Previously always wrote `position=0`. → `tasks_handlers.go`, `KanbanView.tsx`.
- [x] **🔴 AI task extraction preserves the assignee.** The suggested name is matched against a
      real teammate (name/e-mail) → `assignee_id`; shows "no match" when it doesn't. → `ExtractTasksModal.tsx`.
- [x] **🔴 Meet: chat vs. data channel.** `canPublishData` is always `true` (doesn't break
      presenting/laser pointer when chat is disabled); chat on/off applied client-side. → `meet.go`, `MeetRoom.tsx`.

---

## 💬 Chat (Slack-like)

**Essentials**
- [x] **🔴 History pagination.** Cursor `?before=` (nanosecond precision) + "Load earlier" with
      scroll preservation. Previously, anything past the last 100 was unreachable. → `chat_handlers.go`,
      `ListMessagesBeforeByChannel`, `ChatView.tsx`. *(validated live)*
- [x] **🔴 DM/message notification.** A DM always notifies the other party; a channel notifies
      whoever opted into "all" (never the author, nor anyone already mentioned). → `chat_handlers.go`,
      `GetChannelMeta`/`ListChannelMemberPrefs`. *(validated live)*
- [x] **🔴 Jump to message.** `?around=<id>` returns a window (older+newer) around the target;
      search and pins scroll to the message and highlight it (flash). → `chat_handlers.go`
      (`ListMessagesAfterByChannel`, `around` branch), `ChatView.tsx`. *(validated live)*
- [x] **🔴 "New messages" divider + mark as unread.** The divider captures `last_read_at` on
      open; the "mark unread" action rewinds the cursor. → `chat_handlers.go` (`SetLastReadAt`,
      `handleMarkChannelUnread`), `ChatView.tsx`. *(validated live)*
- [x] **🔴 Group DMs.** `POST /chat/group-dm` with a canonical `dm_key` (dedup by participant
      set); name concatenates the participants; the new-conversation modal becomes multi-select
      (1=DM, 2+=group). → `chat_handlers.go` (`handleOpenGroupDM`, `dmKeyN`, `dmOther`), `ChatView.tsx`. *(validated live)*
- [x] **🔴 Moderation.** A manager/owner deletes someone else's message (`DeleteMessageAny`); the
      owner deletes the channel (`DeleteChannel`, cascading). → `chat_handlers.go`, `ChatView.tsx`. *(validated live)*

**Nice-to-have**
- [x] 🟡 **Forward/quote message.** `POST /chat/messages/{id}/forward` creates a message in the
      target channel quoting the original (requires membership in both source and target); a
      hover action + channel-picker modal. → `chat_handlers.go` (`handleForwardMessage`, `fanoutMessage`), `ChatView.tsx`. *(validated live)*
- [x] 🟡 **Save message (saved items).** Save/unsave (`saved_messages`, migration 00043), per-message
      state in the list, a "Saved" panel in the rail with jump. → `chat_handlers.go`, `ChatView.tsx`. *(validated live)*
- [x] 🟡 Reminders ("remind me"), scheduled message send
- [x] 🟡 **Read receipts ("seen by").** The members endpoint now exposes `last_read_at`; the last
      message shows "Seen"/"Seen by …" for whoever read it (≥ created_at). → `chat_handlers.go`, `ChatView.tsx`.
- [x] 🟡 **Link unfurl (URL previews).** `GET /chat/unfurl` fetches OG/title with an **SSRF guard**
      (blocks private/loopback/link-local IPs on dial — tested against 169.254.169.254/minio/stalwart),
      timeout, body cap, limited redirects, 1h cache. Preview card in the message.
      → `unfurl.go`, `ChatView.tsx`. *(validated live)*
- [x] 🟡 **Slash commands, incoming webhooks, bots.** Client-side slash commands (`/me`, `/shrug`,
      `/tableflip`, `/unflip`) transform the text before sending. Incoming webhooks: a per-channel
      token (`chat_webhooks`, migration 00073), `POST /api/public/chat/hook/{token}` (public,
      rate-limited) turns `{text}` into a bot message (`messages.bot_name`) published live via the
      Hub. Managers create/copy/remove them in the channel panel; bot messages show an avatar +
      APP badge. → `chat_webhooks.go`, `ChatView.tsx`. *(validated live: create→public POST→bot_name→delete; empty=400, bad token=404)*
- [x] 🟡 **FTS search with `from:`/`before:`/`after:` filters.** Generated `body_tsv` column + GIN
      index (migration 00044); search via `plainto_tsquery` with filters parsed in the handler. → `chat_handlers.go`. *(validated live)*
- [x] 🟡 **`@channel` / `@here`.** Notify all members / only those online (`OnlineAmong`), with
      dedup and respecting mute; autocomplete offers the options. → `chat_handlers.go` (`hasMentionToken`), `ChatView.tsx`. *(validated live)*
- [x] 🟡 **Rich presence (active/away/do-not-disturb).** `mail_users.presence` (migration 00045), a
      UI selector, color by state in DM presence, and **DND suppresses web push**. → `chat_handlers.go`,
      `push_handlers.go`, `App.tsx`, `ChatView.tsx`. *(validated live)*
- [x] 🟡 Custom workspace emoji
- [x] 🟡 **Multiline editing** (`<textarea>`; Enter saves, Shift+Enter breaks line).
- [x] 🟡 **Optimistic delivery.** The WS "message" event appends the message to the cache (dedup by
      id) instead of refetching the whole channel; edit/delete/reaction still refetch. → `ChatView.tsx`.
- [x] 🟡 **Removed orphan handlers** `ListChannelFiles`/`ListChannelLinkMessages` (no route).

---

## ✉️ Webmail (Gmail-like)

Already quite complete (threads, advanced search, Sieve, schedule/undo-send, large attachments,
AI). What remains:

**Essentials**
- [x] **🔴 Outbound rules** (see Block 1)
- [x] **🔴 E-mail labels** (see Block 1)
- [x] **🔴 Push for NEW e-mail with the app closed** (today it only notifies with a tab open; the
      backend doesn't watch for arrival on Stalwart).
- [x] **🔴 Vacation responder with its own UI** (start/end dates, internal/external scope). Today
      it's only available as a rule action.
- [x] **🔴 One-click Archive button** (+ shortcut) — today it's "Move to".
- [x] **🔴 Folder management** (rename/delete/nest — only creating at the root exists).
- [x] **🔴 Aliases / multiple "From:"** (a fixed sender today).
- [x] **🔴 Snooze/defer e-mail.**

**Nice-to-have**
- [x] 🟡 Draft/reply preserving HTML (today it reloads as plain text only)
- [x] 🟡 Keyboard shortcuts (j/k/e/r/#); list sorting
- [x] 🟡 **Server-side undo-send; managing scheduled e-mails.** Server-side outbox
      (`outbox_emails`, migration 00074): the client creates the draft over JMAP (`prepareOutbox`)
      and registers the actual send (email_id + envelope + `send_at`); the worker submits it when
      due via `UserClient.SubmitEmail` (`EmailSubmission/set` with the brokered JMAP credential,
      destroying the draft on success), on a fast 10s ticker. Undo-send = `send_at` set to now +
      window, cancelable server-side (survives closing the tab); scheduled e-mails are manageable
      in a modal (list/cancel). Replaces the client-side timer and HOLDFOR. → `outbox_handlers.go`,
      `mailbox/user.go`, `cmd/worker` (`scanOutbox`), `jmap/mail.ts`, `Compose.tsx`, `ScheduledModal.tsx`.
      *(validated live: draft→outbox→worker delivers + draft destroyed + status `sent`;
      scheduled→cancel 200→409; validation 400)*
- [x] 🟡 **Reply templates; forgotten-attachment warning.** Templates (canned responses):
      `mail_templates` (migration 00075, owner-scoped CRUD), a menu in the composer to insert them
      (fills the subject if empty + prepends the sanitized HTML body), save the current draft as a
      template, and delete. Forgotten-attachment warning: a heuristic in the composer (`ATTACH_RE`
      pt/en) confirms before sending when the text mentions an attachment but nothing was attached.
      → `templates_handlers.go`, `Compose.tsx`. *(validated live: CRUD 200/400/404 owner-scoped)*
- [ ] 🟡 Shared mailboxes — deferred: requires a multi-account JMAP architecture (the webmail
      speaks JMAP with the end user's own token; a shared mailbox implies delegation/ACL in
      Stalwart). Decide provisioning before implementing.

---

## 📇 Contacts / CRM

Above-average team sharing; what's missing are the interoperability basics and the link to the
mailbox.

**Essentials**
- [x] **🔴 Import/Export vCard/CSV.**
- [x] **🔴 E-mail history on the contact page** (the `from:/to:` search already exists — just wire
      it up).
- [x] **🔴 Multiple e-mails/phones per contact** (today, one of each).
- [x] **🔴 Automatic capture** (e-mail sender → "add to contacts").
- [x] **🔴 Group as a distribution list** (write to the entire group).

**Nice-to-have**
- [x] 🟡 Custom fields; duplicate merging; photo/avatar
- [x] 🟡 Pipeline/deals; editable notes; owner transfer
- [x] 🟡 **Domain enrichment (logo, automatic person↔company link).** Automatic link: when
      creating/editing a person, if there's no explicit company, link them to the owner's company
      with the same domain (from the `domain` field or the e-mail) via `FindCompanyByDomain`. An
      "Enrich" button runs `LinkPeopleByDomain` (idempotent) to link existing people. Company logo
      via the site's own favicon (`faviconOf`, no external provider/broker) with a monogram fallback.
      → `contacts_handlers.go`, `queries.sql`, `ContactPage.tsx`, `ContactsView.tsx`, `util.ts`.
      *(validated live: auto-link on create + post-hoc enrich `{linked:1}`)*

---

## 🗂️ Kanban / Tasks (Trello-like)

**Essentials**
- [x] **🔴 Vertical ordering on drag** (see Block 1)
- [x] **🔴 Checklist / subtasks.**
- [x] **🔴 Card attachments** (the Files module exists, but isn't linked to cards).
- [x] **🔴 Priority** (existed in `tickets`, removed in 00024).
- [x] **🔴 Card history/activity.**
- [x] **🔴 Due-date-approaching/overdue notification** (`due` is just text; nothing alerts on it).
- [x] **🔴 Reorder columns** (`position` exists; the drag/endpoint is missing).
- [x] **🔴 Real colored labels** (link to the tenant's tag catalog).

**Nice-to-have**
- [x] 🟡 Multiple assignees; dependencies; WIP limit; start date
- [x] 🟡 List/calendar/timeline views; swimlanes; automations
- [x] 🟡 Archive card/board; edit comment; real-time on the board
- [x] 🟡 Fix: AI extraction only sends `assignee`/`due` to the 1st column — fine, but the
      description needs rich text

---

## 📅 Calendar (Google Calendar-like) — the furthest-behind module

**Essentials**
- [x] **🔴 Recurring events (RRULE).** Gap #1 — without "every Monday at 10am" it's useless for
      routine.
- [x] **🔴 Guests / RSVP on events** (RSVP only exists in Meet; the event doesn't land on the
      guest's calendar).
- [x] **🔴 Reminders/alerts** ("event in 10 min") — push infrastructure exists, the trigger is
      missing.
- [x] **🔴 All-day / multi-day events** (today it only shows on the 1st day).
- [x] **🔴 Per-user timezone** (today `America/Sao_Paulo` is hard-coded in the AI).
- [x] **🔴 Grid beyond 07:00–22:00** (events outside the range fall off the grid).
- [x] **🔴 Conflict warning on EDIT** (today only on creation).
- [x] **🔴 ICS import/export / e-mail invitation** (ironic for an e-mail server).

**Nice-to-have**
- [x] 🟡 Multiple calendars; view a colleague's agenda / free-busy
- [x] 🟡 Drag/resize on the grid; add Meet to an existing event
- [x] 🟡 Duplicate event; undo delete; "agenda/list" view

---

## 📁 Files (Drive-like)

Solid core (upload/dedup/folders/sharing/public link/OnlyOffice/viewer/quota). What's missing:

**Essentials**
- [x] **🔴 Trash / restore** (delete is permanent today — the biggest data-loss risk).
- [x] **🔴 Browsable version history + restore** (OnlyOffice's save overwrites today).
- [x] **🔴 Recursive global search** (today it only filters the current folder, client-side).
- [x] **🔴 Multi-file upload + OS drag-and-drop + progress bar** (single-file input today).
- [x] **🔴 Move folders** (rename only; no changing `parent_id`).
- [x] **🔴 Share with a group/team** (individual e-mail only today).

**Nice-to-have**
- [x] 🟡 Configurable link expiration (fixed at 30 days today); link password
- [x] 🟡 Favorites/starring; real thumbnails; column sorting
- [x] 🟡 Copy/duplicate; folder download (zip); batch actions

---

## 🎥 Meet (Google Meet-like)

Broad coverage (lobby, recording, BBB-style presenting, AI notes, virtual backgrounds). What
remains:

**Essentials**
- [x] **🔴 Chat vs. data channel** (see Block 1)
- [x] **🔴 Raise hand + emoji reactions** (the presentation's data channel already exists — cheap).
- [x] **🔴 Mute an individual participant** (only a global policy and expel exist today).
- [ ] **🔴 Captions/transcription** (with AI already in the product, post-recording transcription
      is the natural next step).
- [x] **🔴 Edit/reschedule/recur a meeting** (create and cancel only, today).
- [x] **🔴 Lock the room to new entries** (the lobby only applies to external guests).
- [x] **🔴 Pre-join** (test camera/mic before entering).
- [x] **🔴 Recording management** (per-meeting scope, delete, a "recording" indicator for
      everyone; player has no HTTP Range).

**Nice-to-have**
- [x] 🟡 **Breakout rooms; co-hosts; per-plan participant limit.** Co-hosts: `meeting_cohosts`
      (migration 00076); the `meetingAsHost` gate grants moderation powers (mute/remove), and the
      presence of any host opens a locked room; the owner promotes/demotes from the list (badge +
      star). Per-plan limit: `billing.Plan.MaxMeetParticipants` (trial 5 · starter 25 · growth 50 ·
      business 200), checked in `handleJoinMeet` by counting the LiveKit room (non-hosts →
      `full:true`; `RoomPage` retries until a slot opens). Breakout rooms:
      `meeting_breakouts`+`breakout_assignments` (migration 00077); the host creates N rooms
      (auto-distributes attendees round-robin), closes them all; each breakout is its own LiveKit
      room (`<room>-b<idx>`); the client switches rooms (remounts `LiveKitRoom`) and returns to the
      main room (automatically when the host closes it). → `meet_cohosts.go`, `meet_breakouts.go`,
      `meet.go`, `billing/plans.go`, `MeetRoom.tsx`, `Room.tsx`.
      *(validated live: cohost CRUD; breakouts create/assign/token/close; trial plan=5)*
- [x] 🟡 **Configurable recording layout; PiP.** Layout: the host picks grid/speaker+grid/speaker-only
      before recording; the backend validates against the Egress list and falls back to `grid` so it
      never fails (`recordLayout`). PiP: an action-bar button puts the focused video (share/speaker)
      in a floating OS window; feature-detected (absent in iOS WKWebView). → `meet_recording.go`,
      `MeetRoom.tsx` (`PipButton`, `RecordButton`), `backend.ts`.
- [x] 🟡 **Per-tile network quality indicator.** Already delivered by LiveKit's `ParticipantTile`
      (the prebuilt `<VideoConference />` used in `MeetRoom`/`Guest`), which renders the
      `ConnectionQualityIndicator` per tile by default (green/yellow/red based on the connection).
      → `MeetRoom.tsx`, `Guest.tsx`. *(confirmed in the @livekit/components-react bundle)*

---

## Suggested attack order (impact × effort)

1. ✅ **Block 1** — done.
2. ✅ **Chat: pagination + DM notification** — done.
3. **Chat: jump-to-message, unread, group DMs, moderation** — rounds out daily use.
4. **Calendar: recurrence + guests/RSVP + reminders** — the furthest-behind module.
5. **Files: trash/soft-delete** — avoids irreversible data loss.
6. **"It's an e-mail server" synergies:** ICS in the calendar, e-mail history on the contact
   page, push for new e-mail.
7. **Kanban: checklist/attachments/priority**; **Meet: raise hand/individual mute**.

## Attached technical debt (not a feature, but surfaced during the analysis)
- [x] Notifications store text in Portuguese on the backend (violates i18n §4.6); refactor into
      codes + translation on the front end.
- [x] Chat: the WS event refetches the entire channel (doesn't scale in busy channels).
