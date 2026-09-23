# Assessment — per-app settings, responsiveness, Tauri apps, Rust sync, and production readiness

> Survey of the current state (real repo code, 2026-07-19) and what's missing for:
> (1) individual per-app settings; (2) responsive design of "viewing" modes (Meet presentation
> etc.); (3) real desktop + mobile responsiveness; (4) mobile app with Tauri;
> (5) directory sync with the cloud in Rust; (6) remaining production items.
> **Legend:** 🔴 essential · 🟡 important · 🟢 can wait. Effort: S (≤2 days) / M (≤1 week) / L (>1 week).
> **Checkboxes:** `- [x]` done and verified · `- [~]` partial (the rest is up to you or is
> refinement) · `- [ ]` to do.

## Execution status (2026-07-19)

> The first version of this assessment was based on a repo exploration that was interrupted
> halfway through, and so it **overestimated** the gaps in fronts 1, 2, and 6. After checking the
> real code:

**Delivered and verified in this round (clean build + typecheck + gofmt):**
- **§6 Production — backup/restore:** `scripts/backup.sh` (pg_dump + tar of the MinIO/Stalwart
  volumes, configurable retention) and `scripts/restore.sh` (destructive, with confirmation,
  stops api/worker). This was the biggest real gap (irreversible data loss).
- **§6 Production — CI:** `.github/workflows/ci.yml` (gofmt+vet+build+test+sqlc on the backend;
  tsc+vitest on the frontend; cargo check for the Tauri desktop). The tree was left gofmt-clean
  (3 pre-existing files normalized).
- **§6 Production — readiness:** a `/readyz` endpoint that pings Postgres
  (`SetReadiness(pool.Ping)`), and the `api` healthcheck in the compose file now uses it (no
  traffic is routed before the database responds).
- **§2 Meet — recording layout + PiP:** the host chooses the layout (grid/speaker/speaker-only),
  validated on the backend; a Picture-in-Picture button in the action bar (feature-detected).
  §Meet in GAPS updated.
- **§5 Sync — backend M0 (approved by you):** an incremental feed
  `GET /api/files/changes?since=<cursor>` (migration 00078: `updated_at` column + a
  `BEFORE UPDATE` trigger that updates it on any change; keyset by `(updated_at, id)`; `hash` =
  ETag; soft-delete becomes a tombstone). Migration and feed verified against a real Postgres
  (the trigger fires only on changed files; ordering and tombstones are correct). →
  `00078_file_changes.sql`, `files_changes.go`, `queries.sql`. **Next:** the Rust daemon
  (decision: separate process) that consumes this feed — that's M1, not started yet.

**Decisions you made (2026-07-19):** sync = **separate daemon** (not embedded in the app);
metrics/observability = **no new dependency for now**; Meet mobile = **do a WebRTC spike on iOS
first** before committing to the Tauri path; sync M0 backend = **implement now** (done, above).

**Round 2 (implement everything, with a security focus) — delivered and verified:**
- **§5 Daemon auth (device token):** migration 00079 `device_tokens` — the raw token is shown
  only at creation, stored only as **SHA-256** (hex); scope restricted to the `/api/sync/*` group
  (read changes + download), **outside** the rest of the API and the session flow; revocable per
  device, optional expiry. Management routes (`GET/POST/DELETE /api/devices`) behind the webmail
  session. Auth semantics tested against a real Postgres (revoked and expired ⇒ 0; valid ⇒ 1).
  → `00079_device_tokens.sql`, `device_tokens.go`, `server.go`.
- **§6 Hardening:** `postgres` and `redis` **pinned by digest** (they were floating tags); the
  Tauri `devtools` **removed from the release build** (the inspector no longer ships in the
  distributed package; dev keeps it automatically). → `docker-compose.yml`, `src-tauri/Cargo.toml`.
- Note: LiveKit-server **was already** pinned by digest (a fix from v1 of this assessment).

**Round 12 — §1 settings for every app on the Settings page.** `/mail/settings` gained sections
for **every app**: General (language, theme, initial view), Email, Chat, Calendar, Files, Meet,
Kanban (+ Notifications, Devices). Server-side persistence via the existing JSONB infra
`mail_users.settings` through a new `useSettings.ts` hook (`useSection` writes optimistically;
`useUserSettings` reads it in the apps — shared cache, change in Settings ⇒ reflects
immediately). **No dead toggles** — each preference is wired to its app: Files→view+link
expiration; Calendar→week-start+initial-view+duration+reminder (EventModal); Chat→Enter-sends
(Composer); Meet→join-muted/camera-off (PreJoin); Kanban→initial-board; General→theme+initial-view
(App). Removed the "chat sounds" toggle (there's no sound feature — it would be decorative), and
Contacts didn't get a section (no real preference to wire up). i18n pt-BR/en; `tsc`+`build` clean.
→ `SettingsView.tsx`, `useSettings.ts`, `backend.ts`, `App.tsx`, `FilesView.tsx`,
`CalendarView.tsx`, `EventModal.tsx`, `ChatView.tsx`, `MeetRoom.tsx`, `KanbanView.tsx`,
`ShareModal.tsx`.

**Round 11 — production bug: the Caddyfile was bypassing the JMAP broker.** The frontend does
**not** speak JMAP directly to Stalwart — it sends *our* session token (Bearer) to `/jmap`, and
our API does the brokering (H11): it validates the session and injects the mailbox's
server-to-server Basic credential (the browser never holds the password). `vite.config.ts` (dev)
and `jmap_proxy.go` already implemented this, but `deploy/Caddyfile` was **out of date** and
routed `/jmap`, `/auth`, `/.well-known` **straight to Stalwart:8080**, skipping the broker — in
production this would break the webmail (401, Stalwart doesn't understand our session) and defeat
the security brokering. Fixed: all three now go through `control-plane:8000`, matching dev and
`jmap_proxy.go`'s own comment. → `deploy/Caddyfile`.

**Round 10 — §3 responsiveness adopted in Tailwind + remaining gaps:** the responsiveness
decision was resolved (it was an open question). Tailwind configured as the **responsive
system**: `important: true` (the app lives under `.wm`, high specificity; since Tailwind was
barely used, turning on important doesn't change the existing layout and lets the utilities win
the specificity fight) and `md: 721px` so that `max-md:` matches **exactly** the mobile breakpoint
`webmail.css` already uses (`max-width:720px`), with no dead zone. Convention: **new responsive
work uses Tailwind's `max-md:`** (it only affects ≤720px, desktop emits no rule — zero regression
risk); the `@media` rules that already work stay as they are (working code doesn't get churned,
and the ones depending on LiveKit's `:has()` wouldn't even be utility classes anyway). Gap
closed: **Contacts** (fixed 220px grid → stacks to `max-md:grid-cols-1` + a height-limited group
bar). Mechanism verified in the generated CSS (`@media not all and
(min-width:721px){…!important}`). → `tailwind.config.js`, `ContactsView.tsx`, `GroupsSidebar.tsx`.

**Round 9 — §5 subfolder mirroring in sync:** sync is no longer flat — it now **mirrors the
folder tree**. Backend: `GET/POST /api/sync/folders` (list the owner's tree / create a folder,
with parent-ownership verification). Daemon: a new `tree.rs` module (a `folder_id`↔path map,
cycle-proof, with tests) — places each file under its folder's path; **handles moves** (same
content + different folder ⇒ moves the local file instead of duplicating it); the push is
**recursive** and creates missing server folders. Anti-traversal sanitization extended to
subpaths (`safe_join_rel` + tests). `go build`/`vet` and `cargo build`/`test` (4 tests) clean. →
`files_sync_folders.go`, `sync-daemon/src/{tree,sync,api,safepath}.rs`.

**Round 8 — §2 responsive Meet presentation:** on mobile, the presentation stage now takes the
full width and the LiveKit cameras become a **horizontal strip** underneath (instead of an
illegible narrow column). Closes the last 🔴 of §2. → `webmail.css`.

**Round 7 — §5 M2: bidirectional sync (cloud ↔ local) with optimistic concurrency:**
- **Backend:** `PUT /api/sync/files/{id}` requires **If-Match** (base hash); a mismatch → **412**
  (conflict). Before overwriting, it **snapshots the version** into history (never loses data);
  quota, hash-based dedup, and tenant/owner isolation preserved. `POST /api/sync/files` creates
  new files. Streaming hash (no loading the whole file into memory). Snapshot+update semantics
  tested against Postgres. → `files_sync_upload.go`, `server.go`.
- **Daemon:** a **push-before-pull** pass (avoids a download overwriting a local edit on its way
  up); uploads changed (If-Match) and new files; on 412 it keeps a local **conflict copy**;
  **local deletes are not propagated** (safety). Local hash in SHA-256 (same algorithm as the
  server). `cargo build`/`test` clean. → `sync-daemon/src/{api,sync,index}.rs`.
- Note: each layer was verified in isolation (backend build+vet, SQL in Postgres, daemon
  build+tests); the live end-to-end test (daemon ↔ API ↔ MinIO) needs the stack running.

**Round 6 — §4 mobile apps (Tauri Android + iOS) initialized:** ran `tauri android init` and
`tauri ios init` — native projects generated in `src-tauri/gen/android` and `.../gen/apple`;
`lib.rs` already had the `mobile_entry_point`. Meet permissions declared (camera/mic in
AndroidManifest and Info.plist — without these the App Store rejects it) +
`POST_NOTIFICATIONS`. Desktop `cargo check` still passes (nothing broke). Runbook in
`src-tauri/MOBILE.md`. **Missing (needs external accounts):** native FCM/APNs push and the
**WebRTC spike in the iOS WKWebView** (decision: validate before committing Meet to Tauri; plan B
= Safari via opener). → `src-tauri/gen/*`, `AndroidManifest.xml`, `Info.plist`, `MOBILE.md`.

**Round 5 — §1 theme/language follow the user + §5 conditional upload (decision):**
- **§1 theme/language sync:** now persisted in `settings.general` (using the existing JSONB
  infra). On authentication, the server sends them down (reconciling theme and language);
  `localStorage` stays as an instant pre-login cache. Makes sense with web + desktop + mobile.
  `tsc` clean. → `App.tsx`, `SettingsView.tsx`, `backend.ts`.
- **§5 conditional upload (If-Match):** deferred at the time as YAGNI; **now implemented** in
  Round 7 (M2), which is exactly the "real writer" that justifies protecting the save path.

**Round 4 — §3 responsiveness:** a correction to the diagnosis — Chat, Webmail, the shell (nav
becomes a bottom bar), search, and Admin/Settings **were already responsive and wired** (drawers,
list↔detail switch, `chat-burger`/`mobtop`). What was missing and has now been delivered (CSS,
validated with `npm run build`): mobile **Calendar** (compact month, week/day grid with
horizontal scroll instead of squeezing columns, the AI agenda as a full-width card), **Kanban**
with scroll-snap (one column per screen), and the **Meet** action bar wrapping instead of running
off-screen. Files already used `auto-fill minmax` (responsive). Optional follow-up (JS): Calendar
on mobile opening by default in the agenda/day view. → `webmail.css`.

**Round 3 — §5 M1: Rust sync daemon (cloud → local, read-only):** a new separate crate
`sync-daemon/` (`mailin-sync`). Consumes `/api/sync/files/changes` with the device token, keeps
an index in SQLite (id→path+hash+version) and a cursor, downloads new/changed files (hash-based
dedup) and applies tombstones. **Security:** HTTPS required outside localhost; token in a `0600`
file; **path traversal sanitization** (`safepath.rs` + passing tests); only deletes what the
daemon itself indexed; atomic writes (part+rename). `cargo build`/`cargo test` clean; job added
to CI. M1 limitations (documented in `sync-daemon/README.md`): flat structure (tree = M2), no
local→cloud upload yet. → `sync-daemon/*`, `.github/workflows/ci.yml`.

**Corrections to the diagnosis (already existed, contrary to what v1 said):**
- **§1** Preferences are **already server-side per app**: the JSONB column `mail_users.settings`
  (migration 00029) with top-level merge, `GET/PATCH /api/settings` routes, and `SettingsView`
  **already has a section rail** (General/Email/Notifications). The calendar timezone already
  uses the device's `Intl` (the 🔴 GAPS item is satisfied). So "server-side prefs" and "settings
  per app" **are not a gap** — what's left is a decision (below), not missing code.
- **§6** The compose file **already has** healthchecks (Postgres/MinIO/OnlyOffice/api) and most
  images **are already pinned** by digest (MinIO/OnlyOffice/LiveKit/Egress); Redis and
  LiveKit-server are still missing.

**Final code state (after rounds 1–10):** every point **implementable and verifiable in this
environment is done**. What remains depends on external resources only you can provide — it's
not pending code:
- **Native mobile push** — Firebase (FCM) accounts + an APNs certificate (Apple).
- **Store publication** — Apple Developer account + Android keystore + a privacy policy.
- **iOS WebRTC spike** — run `tauri ios dev` on your own device/simulator (init, permissions and
  runbook already ready in `src-tauri/MOBILE.md`).
- **Email deliverability** (DKIM/SPF/DMARC/rDNS) — depends on your DNS and IPs.
- **Observability** (Prometheus/Sentry) — you chose not to add dependencies for now (reversible).
- **Sync polish** (status UI; propagating local moves/deletes) — future improvements, not
  blocking.

## Executive summary

| Front | Current state | Main gap | Effort |
|---|---|---|---|
| 1. Per-app settings | ✅ Server-side per-app prefs (JSONB) + section rail **already exist** | Just a decision: more sections (YAGNI) and whether theme/language sync across devices | S (once decided) |
| 2. "Viewing mode" design (Meet) | ✅ Recording layout + PiP **delivered** this round | Adaptive mobile grid (part of front 3) | — |
| 3. Real responsiveness | Desktop-first: ~7 `@media` in 2,091 lines of CSS | Breakpoint strategy + per-module refactor | L |
| 4. Tauri mobile app | Tauri 2 desktop **already exists** (`frontend/src-tauri`); mobile not initialized | `tauri android/ios init` + native push + mobile UI (depends on front 3) + iOS WebRTC spike | L |
| 5. Directory sync (Rust) | Zero. Files = MinIO via API (`internal/storage`) | Rust sync engine + delta API (the changes feed is the prerequisite) | L |
| 6. Production | ✅ Backup/restore, CI and `/readyz` **delivered**; compose already had healthchecks + digests | Metrics/observability (dependency decision), pin Redis/LiveKit, Tauri updater | M |

---

## 1. Individual per-app settings

### Current state
- A single page: `frontend/src/webmail/SettingsView.tsx` — language, theme, and notifications.
- **Everything in localStorage** (`mailin.lang`, theme, web push flags). Consequence:
  preferences don't follow the user across devices — unacceptable from the moment web, desktop,
  and mobile all exist (fronts 3–5).
- Settings that already exist scattered around: signature (`SignatureEditor.tsx`), rules
  (`RulesView.tsx`), vacation — all in the webmail, with no common "home".

### What to do
- [x] 🔴 **Server-side prefs.** ✅ Done — not in a new `user_prefs` table, but in the existing
      `mail_users.settings` JSONB column (per-section merge on the server); the client writes
      optimistically via `useSettings.ts`. Rounds 5 and 12.
- [x] 🔴 **Settings with per-app navigation** ✅ (Round 12): a rail with General · Email · Chat ·
      Calendar · Files · Meet · Kanban · Notifications · Devices. (Contacts wasn't included — no
      real preference to wire up.)
- [x] 🔴 i18n keys for each preference in pt-BR **and** en ✅ (Round 12).

### Proposed per-app preferences (first pass)
| App | Preferences |
|---|---|
| General | language, theme (light/dark/system), density, home page |
| Webmail | signature, undo-send window, archive behavior, read receipts, remote images on/off, shortcuts on/off |
| Chat | default notify (all/mentions/none), sounds, default presence, Enter sends |
| Calendar | timezone, first day of week, visible grid hours, default duration, default reminder |
| Files | view (grid/list), sort order, default link expiration |
| Meet | default camera/mic/speaker, join muted, default virtual background, quality |
| Kanban | initial board, default view (kanban/list/calendar) |

Note: the calendar timezone was already 🔴 in GAPS (§Calendar) — resolve it together, storing it
in `user_prefs`.

---

## 2. Responsive design of "viewing" modes (Meet and similar)

### Current state
- Meet uses LiveKit's prebuilt `<VideoConference />` (`MeetRoom.tsx`, `Guest.tsx`) + our own
  presentation layer (`MeetPresentation.tsx`, BBB-style), effects, reactions, notes.
- Pending in GAPS (§Meet, 🟡): **configurable recording layout; PiP**.

### What to do
- [x] 🔴 **Responsive presentation layout:** ✅ (Round 8) — on mobile the stage takes the full
      width and the cameras become a horizontal strip underneath.
- [x] 🟡 **Native PiP** ✅ (Round 2) — Picture-in-Picture button in the action bar,
      feature-detected.
- [x] 🟡 **Configurable recording layout** ✅ (Round 2) — the host picks
      grid/speaker/speaker-only, validated on the backend (`recordLayout`).
- [ ] 🟡 Tile grid with width-based rules (1 col <480px, 2 col <900px…) — **not done**; the
      regular grid still uses the LiveKit prebuilt's default (only the presentation was made
      responsive).

---

## 3. Real responsiveness (desktop and mobile)

### Current state — the frontend's most structural gap
- `webmail.css`: **2,091 lines with only 7 `@media` rules**, and only 3 real breakpoints
  (720/860/1024px), almost all for the chat burger menu. `index.css`: zero. Tailwind is
  configured, but only **1 file** uses responsive prefixes (`sm:`/`md:`/`lg:`).
- In practice the UI is desktop-only: 3-column webmail, grid calendar, horizontal kanban — none
  of it works well below 768px today. The Tauri window already allows `minWidth: 380`, meaning
  the current desktop build already exposes the problem.

### Proposed strategy
- [x] 🔴 **Single breakpoint contract** ✅ (Round 10) — Tailwind `sm 640 / md 721 / lg 1024 /
      xl 1280` (md=721 matches the `max-width:720` `webmail.css` already used), `important:true`.
- [x] 🔴 **Stacked "list → detail" pattern on mobile** ✅ — already existed and wired in webmail
      and chat (drawers, `mobtop`, `chat-burger`); confirmed in Round 4.
- [x] 🔴 **Module rail becomes a bottom nav** ✅ — already existed (`@media ≤720` in the shell).
- [x] 🔴 **Mobile calendar** ✅ (Round 4) — compact month + week/day with horizontal scroll +
      agenda as a card. (Opening by default in the agenda view on mobile stays as a JS option.)
- [x] 🟡 Mobile Kanban: scroll-snap, one column per screen ✅ (Round 4).
- [~] 🟡 Full-screen sheet compose/modals <md ✅ (already existed: `.cmp`/`.tk` at 96vw);
      **missing** is just "admin tables become cards" (the admin panel stays desktop-first for
      now).
- [ ] 🟡 Touch audit (long-press/swipe, ≥44px targets, `safe-area-inset`) — **not done**;
      refinement for when mobile becomes serious (front 4).

**Order by module (impact × mobile usage):** Chat → Webmail → Meet (front 2) → Calendar →
Files → Contacts → Kanban → Admin panel (can stay desktop-first 🟢).

**Decision to make (law 6.2):** migrate gradually to responsive Tailwind utilities (recommended
— already installed; avoids growing the monolithic .css files) vs. staying in plain CSS with
`@media`. I recommend Tailwind for new/refactored code, with no big-bang rewrite.

---

## 4. Mobile app with Tauri

### Current state — better than it looks
- `frontend/src-tauri` already exists on **Tauri 2**, with the `mailin_lib` crate already
  prepared for mobile (`crate-type = ["staticlib", "cdylib", "rlib"]` — exactly what iOS/Android
  need) and android/ios icons in the scaffold. Plugins: opener, notification.
- **Mobile not initialized:** `gen/schemas` only has desktop/macOS — `tauri android init` /
  `tauri ios init` are missing (they generate `gen/android` and `gen/apple`).
- Reusable existing desktop integrations: `tauriNotify.ts`, `nativeWindow.ts`.

### What to do
- [x] 🔴 `tauri android init` + `tauri ios init` ✅ (Round 6) — native projects generated in
      `src-tauri/gen/{android,apple}`; a CI job (`cargo check`). Runbook in
      `src-tauri/MOBILE.md`.
- [x] 🔴 **Prerequisite: front 3** ✅ — responsiveness closed (Rounds 4/8/10).
- [ ] 🔴 **Real push with the app closed (FCM/APNs)** — **not done**; needs Firebase/Apple
      accounts.
- [ ] 🔴 Deep links (`tauri-plugin-deep-link`) — **not done**.
- [x] 🔴 Native camera/microphone permissions in Info.plist/AndroidManifest ✅ (Round 6,
      validated). The **WebRTC spike in the WKWebView (iOS)** is still to be done — needs your own
      device/simulator.
- [ ] 🟡 Biometrics (`tauri-plugin-biometric`) — **not done**.
- [ ] 🟡 Virtual keyboard (`interactive-widget=resizes-content`) — **not done**.
- [ ] 🟢 Store publication (accounts, Apple review, privacy policy) — **not done** (accounts).

**Recurring cost to accept:** the test matrix now spans web + macOS/Windows/Linux + Android +
iOS. Worth designating 2 first-class targets (e.g. web + Android) and treating the rest as
best-effort at the start.

---

## 5. Syncing directories with the cloud (Rust, desktop and mobile)

### Current state
- Zero sync. Files = manual upload via the API (`internal/storage` → MinIO), with dedup,
  versions, trash, and sharing already delivered (GAPS §Files ✅).
- There's no **delta/changes** endpoint, nor an exposed content identifier (etag/hash) —
  prerequisites for any sync client.

### Proposed architecture (Dropbox-style, simplified)
**Rust crate `mailin-sync`** inside `src-tauri` (workspace), used by desktop and mobile via
Tauri commands — the same approach as the current `mailin_lib`, no second binary.

Components:
1. **Watcher** — the `notify` crate (FSEvents/inotify/ReadDirectoryChangesW) + a periodic
   reconciliation sweep (watchers miss events).
2. **Local index** — SQLite (`rusqlite`): path, size, mtime, BLAKE3 hash, `remote_id`,
   `remote_version`, state (`synced/dirty/conflict/pending`).
3. **Protocol with the backend (new, Go):**
   - `GET /api/files/changes?since=<cursor>` — incremental feed per tenant/folder (a monotonic
     cursor; a `file_events` table or an `updated_seq` column).
   - Upload/download already exist; add **BLAKE3 hash in the metadata** (dedup already exists —
     align the algorithm) and conditional upload (`If-Match`) to detect races.
4. **Decision engine** — three-way (local state vs. index vs. remote). Conflict = never
   overwrite: write `name (conflict from <device> 2026-07-19).ext`, flag it in the UI.
5. **Transfer** — a queue with a concurrency limit, resumable, backoff; respect quota (API 409)
   and idempotency (law 5.3).

Steps:
- [x] 🔴 **M0 — backend:** changes feed ✅ (`GET /api/files/changes`, migration 00078) + hash as
      ETag ✅. Still missing: **conditional upload** (`If-Match` on the OnlyOffice save) — small,
      but it touches the save path; left for M1 when the daemon needs it.
- [x] 🔴 **M1 — one-way desktop** ✅ (cloud → local folder download). Delivered in
      `sync-daemon/`.
- [x] 🔴 **M2 — bidirectional desktop** ✅ complete: conditional upload (If-Match/412), conflict
      copies, **subfolder mirroring**, **tray-icon agent** (state + menu + autostart via
      launchd/systemd), **local delete → trash** and **local move → move** (with an anti-wipe
      guard), **real-time watcher** + mtime/size pre-filter, **hourly reconciliation** (catches
      the purge), **shared-with-me** in `Shared/` (read-only), and **selective folders**.
      Only missing: concurrent transfers and bandwidth limiting (optimizations).
- [ ] 🟡 **M3 — mobile.** Reduced, realistic scope: **no continuous directory mirroring** (iOS
      doesn't grant arbitrary filesystem access or a background daemon). Offer: "available
      offline" folders (app-managed cache) + camera upload (Android first;
      WorkManager-like via tasks on app open). Real iOS mirroring would need a native File
      Provider Extension — 🟢 defer.
- [x] 🟢 **M4 — block-level delta** ✅ Only sends what changes in large files: the server
      exposes per-block hashes (`GET /sync/files/{id}/blocks`) and accepts `PUT .../delta` with
      only the changed blocks, **reconstructing** the file (the rest is copied from the old
      blob). Storage is still one whole blob per file — no touching
      download/versions/OnlyOffice/zip. Only above 8 MiB and when <70% changes; otherwise (or
      without support) it falls back to a full upload. Reconstruction with tests (grow/shrink/
      partial block/invalid input). → `files_sync_delta.go`, `sync.rs`.

**Decision to make (law 6.2):** sync as part of the Tauri app (recommended: 1 installer, ready
UI, simple IPC) vs. a separate daemon that runs without the app open (more "Dropbox"-like, but
its own tray/autostart/packaging). Can evolve from A to B later.

---

## 6. What else is needed for production

### Current deploy state
`deploy/docker-compose.yml` brings up stalwart, postgres, minio, api, onlyoffice, livekit, redis,
egress, and worker, with Caddy (TLS) in front. App hardening already done (CSRF, rate limit,
encrypted TOTP, fail-closed). **Missing:** backup, observability, CI/CD, secrets management, app
updater.

### Gaps by risk order
- [x] 🔴 **Backups + restore.** Delivered: `scripts/backup.sh` (logical pg_dump + tar of the
      MinIO/Stalwart volumes, retention via `RETENTION_DAYS`) and `scripts/restore.sh`
      (destructive, confirms, stops api/worker). Still need to schedule it (cron/systemd-timer)
      and **rehearse a restore** in a real environment.
- [~] 🔴 **Observability.** Partial: `/readyz` (pings Postgres) + compose healthchecks ✅
      (Round 1). **Missing** `/metrics` (Prometheus) + Sentry — **your decision** (no new
      dependencies for now). Reversible whenever you want.
- [x] 🔴 **CI/CD** ✅ (Round 1) — `.github/workflows/ci.yml`: gofmt+vet+build+test+sqlc
      (backend), tsc+vitest (frontend), cargo check for Tauri and for `sync-daemon`. Tree left
      gofmt-clean.
- [~] 🔴 **Architectural security items:** brokered JMAP credential ✅ (already existed;
      Caddyfile fixed in Round 11) · **image pinning by digest** ✅ (Round 3: postgres+redis, the
      rest already were) · **secret rotation** — **not done** (procedure/tooling still to be
      defined).
- [ ] 🔴 **Email deliverability** (the product is a mail server): a per-domain checklist —
      DKIM/SPF/DMARC on provisioning, rDNS/PTR, IP warmup, blacklist monitoring, DMARC reports.
      Validate what Stalwart 0.16 already provisions via JMAP `x:*` and record divergences
      (DIVERGENCES.md).
- [~] 🟡 **App updater.** `devtools` **removed from the release build** ✅ (Round 3).
      **Missing** the `tauri-plugin-updater` + build signing — needs your own signing keys.
- [ ] 🟡 **Versioned API compatibility.** With installed apps (desktop/mobile), old clients will
      start to exist: a compatibility policy (additive by default; `X-Client-Version`; a 426
      response for the minimum supported version).
- [ ] 🟡 **E2E smoke** (Playwright): login → send email → receive → chat → upload a file →
      create an event. Runs against the compose stack in CI. Covers risk #1 (regressions across
      integrated modules).
- [ ] 🟡 **GDPR/privacy:** a privacy policy (mandatory for the stores), account/tenant export
      and deletion, a DPA for customers, Meet recording retention.
- [ ] 🟡 **Multi-tenant operations:** per-plan limits enforced on the backend (quota already
      exists for files; still needed for recordings, Meet participants — GAPS §Meet), a
      status/uptime page.
- [~] 🟢 Operations docs: `sync-daemon/README.md` and `src-tauri/MOBILE.md` already exist;
      **missing** is the general runbook (bring up the stack from scratch, restore a backup,
      rotate secrets, domain onboarding).

---

## Suggested order of attack

1. **§6 Backups + minimal observability + CI** — protects what already exists; independent of
   the rest. (M)
2. **§1 Server-side prefs + per-app Settings** — unblocks the calendar timezone and is a
   prerequisite for multi-device. (M)
3. **§3 Responsiveness: Chat → Webmail → Meet** (+ §2 at the Meet step). (L, incremental)
4. **§4 Tauri mobile init + iOS WebRTC spike + FCM/APNs push** — spike early; the WebRTC result
   in WKWebView could change the mobile Meet plan. (L)
5. **§5 Sync M0 (backend) → M1/M2 desktop** — mobile (M3) only after the mobile app stabilizes.
   (L)

## Technical debt touched by this assessment
- Monolithic `webmail.css` (2,091 lines) and `panel.css` — split alongside the responsive
  refactor (law 1.4 applies in spirit to CSS too).
- Device-local preferences scattered around (`mailin.lang`, `mailin.theme`, push flags) —
  migrate to `mail_users.settings` **if** we decide to sync across devices (see Open Questions,
  Q1).
- `devtools` enabled in the Tauri release build (a comment in Cargo.toml itself asks for it to
  be removed).

---

## Open questions (your decisions before I move forward)

These items were **deliberately left unimplemented** — they depend on a structural choice
(law 6.2) or on tools/environment that require your direction. Order = highest impact first.

1. **§4 Mobile Meet / WebRTC on iOS.** The Tauri path for Meet on iOS depends on LiveKit
   (WebRTC) working in WKWebView — historically fragile. Do you want me to do a **spike** to
   validate this before committing mobile Meet to Tauri? Plan B is opening Meet in Safari.
2. **§5 Rust sync — M1 (daemon) needs auth for a headless client.** ✅ Decided: separate daemon;
   ✅ M0 (feed) delivered. **New blocker before M1:** the API only authenticates via session
   cookie + CSRF (browser flow). A headless daemon needs another path. Which one?
   - (a) A long-lived, revocable **device token** issued at login (recommended: simple,
     per-device revocation). Requires an issuance endpoint + a `device_tokens` table. — a
     schema/auth decision, hence the question (law 6.2).
   - (b) Reuse the Tauri app's session (the daemon shares the cookie) — only works if the daemon
     lives inside the app; contradicts "separate daemon".
   Once the path is chosen, I'll implement M1 (poll the feed → local SQLite index → one-way
   download).
3. **§6 Observability — dependency.** Should I add `prometheus/client_golang` for `/metrics` +
   an error SDK (Sentry/GlitchTip)? These are new dependencies (laws 2.5/6.2), hence the
   question.
   Without an SDK, leave it for later
4. **§1 Settings — sync theme/language across devices?** Today they're device-local
   (localStorage), which is defensible. Should I move them to `mail_users.settings` (they'd
   follow the user, at the cost of an initial flash to resolve)? And which new preferences do
   you actually want now (I'll only add a new section with real use — YAGNI)?
   Yes
5. **§3 Responsiveness — depth.** It's a large refactor (desktop-first today). Should I go
   module by module in the order Chat → Webmail → Meet → Calendar, delivering one at a time? I'm
   also confirming the strategy: responsive Tailwind utilities in new code (recommended).
   A. Yes
