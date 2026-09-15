# Wiki / Knowledge Base (Confluence-style) — Specification

> Status: **design draft** (Phase 0). Living document. Follows the laws in [CLAUDE.md](../CLAUDE.md)
> and the constraints in [project.md](project.md). Prose in English; **code, logs and `error_code`
> always in English** (law 4.6). One phase at a time (§9): do not start Phase N+1 before N is tested.

## 1. What it is and why

A **knowledge base** app for the organization — an internal wiki in the spirit of Confluence:
spaces, hierarchical pages, rich collaborative editing, version history, and access control by
**group** or **specific people**. Besides being useful on its own, the wiki **feeds the AI
assistant as a RAG source**: the assistant can answer questions based on the company's written
knowledge — **always respecting the permissions of whoever is asking**.

### Goals
- **Write and organize** knowledge: spaces → pages → subpages (tree), with an embedded editor
  similar to Confluence's (blocks, headings, lists, tables, code, callouts, images, mentions, links).
- **Collaborate**: multiple people edit; version history with diff and restore; comments.
- **Control access**: per space and per page, to user groups ([[user_groups]]) or to
  specific people, with roles (view / comment / edit / manage).
- **Feed the AI (RAG)**: index the content and give the assistant a `wiki.search` tool that
  retrieves relevant excerpts **filtered by the asking user's permission** and answers with a
  citation to the page.
- **Be governable**: the admin toggles the Wiki app on/off (existing apps system); disabling it
  removes the app from the menu **and** cuts the AI's access to this knowledge (tool + agent), like
  the other apps.

### Non-goals (v1)
- Not a public CMS or an external site (no anonymous publishing in v1; see §11 future).
- Does not replace the Office document editor (OnlyOffice) — the wiki is **native**, searchable
  content, not `.docx` files. Attachments and "insert file from Drive" are an integration, not the
  page body.
- No real-time concurrent editing (CRDT/OT) in v1 — we use optimistic locking by version (§6).

## 2. Non-negotiable constraints (recap of project.md §2 and CLAUDE.md)
- **Multi-tenant**: every query filters by the authenticated user's `tenant_id` (context), never
  from the request (law 5.1). Every page read/write checks **ownership/permission** first (law 5.2).
- **Layers** (law 1.3): `httpapi` (transport) → service/logic (wiki) → `db` (sqlc). Handlers don't
  build SQL or inline permission rules when there is real logic; extract it into the service.
- **Files ≤600 lines** (aim ≤300); short functions (law 1.4/1.5). One component per file in the
  frontend; every API call goes through `src/api.ts`/`backend.ts` (law 3.4).
- **i18n mandatory** (law 4): no hardcoded visible string; `pt-BR` default + `en` at parity;
  API errors come back with `error_code` and the frontend translates via `server.<code>`.
- **AI security** (project.md §6): content read is **untrusted data**, never an instruction; the
  user must never see, in the RAG, a page they wouldn't have access to in the UI (permission is the
  same surface).
- **No unnecessary dependencies** (law 2.5): every new library (e.g. the editor) needs justification.

## 3. Domain model (proposed schema)

New tables (final names to be confirmed in Phase 1). All with `tenant_id UUID NOT NULL REFERENCES
tenants(id)` and an index on `tenant_id`. IDs `UUID DEFAULT gen_random_uuid()`.

### 3.1. `wiki_spaces` — spaces (top of the tree)
| column | type | note |
|---|---|---|
| `id` | uuid pk | |
| `tenant_id` | uuid | owning tenant |
| `key` | text | short key, unique per tenant (e.g. `ENG`, `HR`) for clean URLs |
| `name` | text | display name |
| `description` | text | optional |
| `icon` | text | emoji/icon |
| `owner_user_id` | uuid | creator/owner |
| `visibility` | text | `restricted` \| `org` \| `public` — `org` = everyone in the tenant can see it; `public` = anonymous read-only link (see §5) |
| `public_slug` | text null | public link slug (when `visibility=public`); unique per tenant |
| `ai_indexed` | bool | whether the content feeds the RAG (default true; admin/owner controls it) |
| `created_at` / `updated_at` | timestamptz | |

`UNIQUE (tenant_id, key)`. Each row in this table **is a wiki** — there can be **N** per tenant.

### 3.2. `wiki_pages` — pages (tree inside a space)
| column | type | note |
|---|---|---|
| `id` | uuid pk | |
| `tenant_id` | uuid | |
| `space_id` | uuid → wiki_spaces | |
| `parent_id` | uuid → wiki_pages null | tree (root = null) |
| `title` | text | |
| `slug` | text | derived from the title, unique per space for the URL |
| `body_json` | jsonb | editor document (ProseMirror/TipTap) — the **source of truth** |
| `body_text` | text | derived plain text, for search/RAG and `tsvector` |
| `body_html` | text | cached render for fast reading (regenerated on save) |
| `position` | int | order among siblings |
| `status` | text | `draft` \| `published` (the wiki shows published; draft only to those who can edit) |
| `author_user_id` | uuid | last editor |
| `version` | int | incrementing integer (optimistic locking, §6) |
| `search_tsv` | tsvector | GENERATED from `title`+`body_text` (GIN index) — RAG Phase 1 |
| `created_at` / `updated_at` | timestamptz | |

`UNIQUE (space_id, slug)`. Indexes: `(tenant_id, space_id, parent_id, position)`, GIN on `search_tsv`.

### 3.3. `wiki_page_versions` — history
Every save records the previous version: `id, page_id, tenant_id, version, title, body_json,
body_text, author_user_id, created_at`. Enables diff and restore (§6). Retention configurable (§8).

### 3.4. Permissions — `wiki_space_grants` and `wiki_page_grants`
> **Grounding**: the existing sharing pattern is **per team** — `contact_group_shares`
> (`00023_contacts_unified.sql:28`) shares a *resource group* with a `user_group` (team), keyed
> `(group_id, user_group_id)`; the owner keeps edit/delete, the team gets view. The minimal
> house-style table would be `wiki_space_shares(space_id, user_group_id)`. The user's request
> includes "**or specific people**", which the precedent doesn't cover (teams only) — so we
> generalize with `subject_type` (`group`|`user`) and `role`, a small, explicit extension of the
> pattern. One row per grant:
| column | type | note |
|---|---|---|
| `id` | uuid pk | |
| `tenant_id` | uuid | |
| `space_id` (or `page_id`) | uuid | target |
| `subject_type` | text | `user` \| `group` \| `org` (whole organization) |
| `subject_id` | uuid null | → mail_users.id or user_groups.id; **null** when `subject_type=org` |
| `role` | text | `viewer` \| `commenter` \| `editor` \| `manager` (see §5) |

`subject_type=org` is "everyone within the organization" (the whole tenant, without picking a
group). **Public** (anonymous, read-only) access **is not** a grant — it lives in
`visibility=public` + `public_slug` (§5.4).

A page **inherits** from its space; a grant on the page **adds to/raises** it (never lowers it
below the space in v1; explicit rule in §5). `UNIQUE (space_id, subject_type, subject_id)`
likewise per page.

### 3.5. `wiki_comments` (v1.1)
`id, tenant_id, page_id, author_user_id, body_text, anchor (optional json for inline comments),
created_at`. Commenting requires at least the `commenter` role.

### 3.6. `wiki_attachments`
Links files (existing MinIO/Drive) to a page: `id, tenant_id, page_id, file_id, ...`. Reuses the
current storage — no reinvented upload.

### 3.7. `wiki_chunks` — semantic index (RAG, §7.2)
`id, tenant_id, page_id, space_id, chunk_index, chunk_text, embedding vector(D)`. One chunk per
row; recomputed by a worker on page save (idempotent). **pgvector** index (`vector_cosine_ops`,
ivfflat/hnsw). Requires `CREATE EXTENSION IF NOT EXISTS vector` (Phase 3 migration). `D` = the
dimension of the chosen embeddings provider (§7.2).

## 4. Embedded editor (Confluence-style)

### 4.1. Library choice
Proposal: **TipTap** (built on ProseMirror) — React-first, extensible, produces **structured
JSON** (ideal for versioning, rendering, and extracting text for RAG). Alternatives considered:
Lexical (Meta, newer/less mature in extensions), Slate (lower-level, more work), Quill
(HTML-centric, worse for blocks). **Structural decision → confirm with the user before installing**
(law 6.2); dependency justification: a rich block editor is impractical to hand-write (law 2.5
accepted).
> **Grounding (confirmed)**: **no rich editor library exists** in `package.json`
> (no tiptap/prosemirror/slate/quill/lexical). The current document editing is the **OnlyOffice
> Document Server** ([EditorView.tsx:28](../frontend/src/webmail/EditorView.tsx)) — a heavy
> full-screen editor, keyed by `file_id` + a signed JWT session; **it doesn't serve** inline
> block editing Confluence-style. Only its *signed session + theme/language* pattern is reused
> from it, not its UX. So the wiki's editor is a **new** dependency (to be confirmed in §12).

### 4.2. Supported blocks (v1)
Headings H1–H3, paragraph, **bold/italic/code/link**, lists (ordered/unordered/checkbox), quote,
code block (with language), **callout/panel** (info/warning/success), table, image/attachment,
divider, **`@person` mention** and internal **`[[page]]` link**. Slash menu (`/`) to insert
blocks, as in Confluence/Notion. Markdown shortcuts while typing (`## `, `- `, ``` ``` ```).

### 4.3. Persistence and rendering
- **Source of truth = `body_json`** (ProseMirror document). On save, the backend (or a shared
  util) derives `body_text` (for search/RAG) and `body_html` (for fast reading without loading the
  editor). Rendering must be **deterministic and safe** (sanitize HTML; no script in the content).
- Autosave with debounce + explicit save; version conflicts handled in §6.

## 5. Permissions and access

### 5.1. Roles (per space and per page)
- `viewer` — reads.
- `commenter` — reads + comments.
- `editor` — reads + comments + edits content and creates subpages.
- `manager` — everything + manages space/page permissions, deletes, moves.

### 5.2. Subjects and access types (user decision)
Four scopes, two access types:
- **Person** (`mail_users`) — `subject_type=user`.
- **Group/team** ([[user_groups]]) — `subject_type=group`.
- **Whole organization** (entire tenant) — `subject_type=org` (or the `visibility=org` shortcut).
- **Public, read-only** — `visibility=public` + `public_slug` (anonymous link, §5.4). Not a grant.

Access type = the **role**: **view** (`viewer`/`commenter`) or **edit** (`editor`); `manager`
manages. The owner and the `manager`s manage the list in a sharing UI similar to Files' (View /
Edit / Manage, + a "Public read-only access" toggle). Each **wiki** (space) has its own policy —
there are **N** wikis.

### 5.3. Access resolution — the SQL `vis` CTE pattern (as already done in the house)
> **Grounding**: resource access is **not a Go helper** — it's done **in SQL**, always with
> `AND tenant_id = $tenant`, via a visibility CTE. E.g. `ListContacts`/`GetContact`
> ([queries.sql:507](../backend/db/queries/queries.sql)) use
> `WITH vis AS (SELECT ... WHERE owner_user_id = $user OR EXISTS (member-of-shared-team)) ...`.
> The wiki replicates this: a page is visible if you're the `owner` **or** the space/page was
> shared with a team (`user_group`) the user belongs to **or** a direct grant to the user.

Effective rule (composed in the query and re-checked in the service for `needed_role`):
1. `tenant_id` matches (otherwise 404 — never reveal existence).
2. `space.visibility = org` → everyone in the tenant has at least `viewer` (grants raise it).
3. Otherwise, effective role = the **maximum** among: space grants (user/user's groups) and page
   grants (user/groups). Space owner = `manager`.
4. Is `needed_role` satisfied? Yes → proceed; no → 403 `wiki.forbidden` (or 404 if not even
   `viewer`).

The service exposes a thin `canAccess(ctx, user, page, needed_role)` function that wraps the
**same `vis` queries** used for listing — the UI and the RAG share the same surface. The assistant
never retrieves a page the user wouldn't see (§7.4). Critical security point.

### 5.4. Public access (read-only, anonymous)
A wiki with `visibility=public` becomes reachable via an **anonymous link** (`/w/<public_slug>`),
**read-only** and **published pages only** (drafts never go out). Same family as
`file_link_access` ([00087](../backend/internal/db/migrations/00087_file_link_access.sql)): the
public route is separate, doesn't go through session auth, serves sanitized `body_html`,
**doesn't** expose editing/history/comments and never enters the assistant's RAG (which is always
per authenticated user). Toggling public access on/off belongs to the `manager`/owner.
Regenerating `public_slug` invalidates the old link.

### 5.5. Multi-tenancy
Every query gets `tenant_id` from the authenticated context (law 5.1). Never trust a
`space_id`/`page_id` from the request without revalidating ownership (law 5.2). Idempotency in
creations where it makes sense (law 5.3). The public route resolves the tenant from `public_slug`,
never from a request parameter.

## 6. Versions, concurrent editing and history
- **Optimistic locking**: the editor keeps the `version` it loaded; on `PUT`, if the server's
  `version` diverges → 409 `wiki.version_conflict`, and the UI offers "see differences / reload".
  Simple and honest in v1 (no OT/CRDT; §1 non-goal).
- Every save: `version++`, records the previous one in `wiki_page_versions`. History with **diff**
  (text) and **restore** (creates a new version from the old one — never deletes history).
- Show "edited by X Y ago" and light presence ("N viewing this page") reusing Chat's realtime if
  it's cheap; otherwise defer.

## 7. Integration with the AI Assistant (RAG)

The assistant already injects durable context (memory, §8.3 of the assistant's design) via
`RunOptions.Memory` and has a tool catalog (Registry). The wiki enters through **two paths**:

### 7.1. `wiki.search` tool (on-demand retrieval — preferred)
A read-only tool in the assistant's catalog: given a term/question, returns the **N most
relevant** excerpts from pages **the user can see**, with `page_id`, title, space, and citation.
- The handler receives the user's identity (tenant+user from the **context**, never from a model
  parameter — project.md §6.2) and calls the search service with the **same `canAccess`** as the
  UI (§5.3).
- The retrieved text is **untrusted data** (assistant §6.1): it enters as context/content, never
  as an instruction. The system prompt makes this explicit.
- The assistant's answer **cites the page** (link) so the user can confirm the source.

### 7.2. Semantic retrieval — **pgvector** (user decision)
Primary approach: **embeddings + pgvector**. Table `wiki_chunks(id, tenant_id, page_id, space_id,
chunk_index, chunk_text, embedding vector(D))` — the page is split into chunks (by
section/heading, ~500–1000 tokens with overlap) and each chunk is embedded. **ivfflat/hnsw** index
on `vector_cosine_ops`.
- **Idempotent (re)embedding worker** on page save/change/delete (existing queue pattern):
  recomputes that page's chunks; never leaves orphan embeddings (law 5.3).
- **Search** = cosine kNN over `wiki_chunks`, **filtered by the asking user's permission and by
  `tenant_id`** (join with the `vis` CTE, §5.3) and by `ai_indexed=true`.
> **Grounding / new infra**: **pgvector/embeddings don't exist today** in the project — the
> Postgres `pgvector` extension is new infra (approved, §12). The Postgres Docker image must
> include/enable it (`CREATE EXTENSION IF NOT EXISTS vector` in a migration). The **embeddings
> provider** is still to be chosen (an operational decision that doesn't block the schema —
> `vector(D)` just pins the dimension to the provider): DeepSeek
> ([deepseek.go](../backend/internal/ai/deepseek.go)) isn't strong at embeddings; options =
> a dedicated embeddings endpoint (key/URL via `internal/config`, law 2.6) or a local model. Set
> `D` and the embeddings client at the start of Phase 3.

### 7.3. FTS as a hybrid pre-filter (optional, cheap)
`tsvector`/GIN over `title`+`body_text` (Chat precedent,
[00044_chat_fts.sql](../backend/internal/db/migrations/00044_chat_fts.sql)) can serve as a cheap
**pre-filter** before the kNN, or as a fallback when embeddings haven't run yet. Not its own
phase — it rides along with the pages schema.

### 7.4. RAG security (non-negotiable)
Retrieval **always filters by the asking user's permission**. Never index into a "global" index
without the permission field; the search query applies `canAccess`. Spaces/pages with
`ai_indexed = false` stay out of the RAG even if readable (owner/admin control, §8). Disabling the
Wiki app (§9) removes **all** `wiki.*` tools and the `AgentKnowledge` agent. **Writes** by the AI
revalidate `needed_role=editor` in the handler and request **confirmation** with the exact content
(§6.3) — the model never writes without approval, nor beyond the user's permission.

### 7.5. Knowledge agent (reads **and** writes) + wiring (grounded)
User decision: a **dedicated `AgentKnowledge` agent** that **reads and writes** pages. Tools:
- **Read** (`Writes:false, Confirm:false`): `wiki.search` (semantic kNN, §7.2) and `wiki.read`
  (returns a page's content by id/slug). Both filtered by `canAccess` (§5.3).
- **Write** (`Writes:true, Confirm:true` — project.md §6.3, every write requests approval with
  the exact content in view): `wiki.create_page` (new draft/page) and `wiki.edit_page` (changes an
  existing page — only if the user has role ≥ `editor` on it; the handler revalidates `canAccess`
  with `needed_role=editor`, **never** trusting the model). Writes respect versioning (§6).

Concrete steps (catalog pattern):
1. `internal/assistant/tools.go`: consts `ToolWikiSearch="wiki.search"`, `ToolWikiRead="wiki.read"`,
   `ToolWikiCreatePage="wiki.create_page"`, `ToolWikiEditPage="wiki.edit_page"`. `Handler` =
   `func(ctx, id Identity, args json.RawMessage) (Result, error)` — Identity from the **session**,
   never from `args`; `Parameters` (JSON Schema) **don't** declare tenant/user. Templates: read
   `tools_contacts.go:35`; write-with-confirmation — copy the `mail.send` pattern
   (`Writes:true, Confirm:true`, a card with the exact content).
2. `internal/assistant/tools_wiki.go`: `wikiTools(w WikiService)` returns the 4 tools; they use
   the wiki service with the same `vis`/`canAccess` surface.
3. `internal/assistant/tools_drive.go`: `Deps` gains `Wiki WikiService`; `BuildRegistry` gains
   `if d.Wiki != nil { tools = append(tools, wikiTools(d.Wiki)...) }`.
4. `internal/httpapi/assistant_handlers.go` (`buildAssistant`): wires the service into
   `assistant.Deps{...}`.
5. `internal/assistant/agent.go`: new builtin agent `AgentKnowledge` with `Tools:
   {ToolWikiSearch, ToolWikiRead, ToolWikiCreatePage, ToolWikiEditPage}` (+ whatever else makes
   sense).
6. `internal/assistant/apps.go`: `AppTools["wiki"] = {ToolWikiSearch, ToolWikiRead,
   ToolWikiCreatePage, ToolWikiEditPage}` and `AppAgent["wiki"] = AgentKnowledge`. Disabling the
   app → `AllowedTools`/`AgentAvailable` cut the tools **and** the agent (existing mechanism).
7. `internal/httpapi/apps_handlers.go`: `toggleableApps += "wiki"`.

**How retrieved content enters the prompt** (precedent): `MemoryContext(facts)`
([memory.go:184](../backend/internal/assistant/memory.go)) already wraps facts as **CONTEXT ONLY,
never an instruction**, injected via `RunOptions.Memory` into `RunStream`
([engine.go:144](../backend/internal/assistant/engine.go)). The wiki excerpts returned by the tool
follow the same "this is context, not an order" fence.

## 8. Configuration and settings
> **RBAC grounding** ([rbac.go:12](../backend/internal/rbac/rbac.go)): existing permissions —
> `PermDomainsView/Manage`, `PermMailboxManage`, `PermMembersView/Manage`, `PermBillingManage`,
> `PermUsageView`, `PermTenantDelete`, `PermOrgSettings`. `requirePerm(perm)`
> ([server.go:768]) gates admin routes; `mailUserFrom(ctx)` gives the authenticated user. **RBAC
> is for org admin**; day-to-day page access uses the SQL `vis` pattern (§5.3), not RBAC.
- **Organization level (admin, `PermOrgSettings`)**: toggle the Wiki app on/off (apps system →
  disappears from the menu and cuts off the AI); default visibility policy for new spaces
  (`restricted` vs `org`); globally toggle AI indexing; version retention. (Optional: a new
  `PermWikiManage` in `rbac.go` if we want dedicated admin gating, distinct from
  `PermOrgSettings`.)
- **Space level (owner/`manager`)**: name/key/icon, visibility, members and roles, `ai_indexed`
  on/off, archive space.
- **Page level**: additional grants, move, publish/unpublish, restore version.
- Secrets/config **only via environment** (`internal/config`, law 2.6) — e.g. the embeddings
  key/endpoint in Phase 2. No secret in code or in the frontend.

## 9. Gating via the apps system (consistency with the rest)
Reuse the mechanism already delivered in this session. **Grounding — exact points to touch** (the
comment in `apps_handlers.go:10` asks that these stay in sync):
- Backend: `toggleableApps += "wiki"` ([apps_handlers.go:12]); `disabled_apps` already goes into
  `/api/me`.
- Frontend [App.tsx](../frontend/src/webmail/App.tsx): the `View` union (`:29`), `VIEWS` (`:32`),
  `SEARCHABLE` (`:31`, if searchable), the `allApps` array (`:172`, filtered by `disabled_apps` at
  `:182`), the mobile reorder list (`:118`), and the render `switch` (`:228`). Each view is its
  own file.
- i18n: `webmail.apps.wiki` in `en/common.json` (`:192`) **and** `pt-BR/common.json` (parity).
- AI: `AppTools["wiki"]` + (optional) `AppAgent["wiki"]` → disabling the app removes the tool and
  the agent from the assistant and from the picker, exactly like Mail/Files/etc.

## 10. Implementation architecture

### Backend (Go)
- `internal/db/migrations/00090_wiki.sql` (**next free number confirmed**; the last one is
  `00089_drop_deals.sql`) — the tables from §3, goose format (`-- +goose Up/Down`). `sqlc generate`
  (config `backend/sqlc.yaml`, pgx/v5) for the models/queries; queries in
  `backend/db/queries/queries.sql` with `-- name: ListWikiPages :many` annotations etc., using the
  `vis` CTE (§5.3) and `@tenant_id`. Generated output lands in `internal/db/queries.sql.go` +
  `models.go`.
- `internal/wiki/` — the service with the logic: page tree, `body_text`/`body_html` rendering/
  derivation, `canAccess`, versioning, search (FTS Phase 1). Thin HTTP handlers in
  `internal/httpapi/wiki_handlers.go` calling the service (law 1.3).
- `internal/assistant/tools_wiki.go` — the `wiki.search` tool (Registry), using the search service
  with the context's identity.
- Routes in `server.go` behind auth; mutations that share/delete keep requiring ownership
  verification.

### Frontend (React/TS)
- `src/webmail/WikiView.tsx` (+ subcomponents ≤300 lines): side tree of spaces/pages, reader, and
  the TipTap editor in its own component (`WikiEditor.tsx`). Server state via TanStack Query;
  calls via `backend.ts` (law 3.4).
- Routes `/mail/wiki`, `/mail/wiki/:space`, `/mail/wiki/:space/:slug`. Registered in `allApps`
  (§9).
- i18n `wiki.*` in `pt-BR` and `en` (full parity).

## 11. Phases (one at a time — project.md §9)
- **Phase 1 — Foundation and read/write**: schema (**N** wikis/spaces, pages, versions, grants),
  CRUD, tree, basic TipTap editor (essential blocks), **permissions by person/group/org
  (access|edit)**, i18n, app gating. No AI yet.
- **Phase 2 — History, collaboration and public**: versions (diff/restore), optimistic 409 lock,
  comments, attachments (Drive reuse), internal mentions/links, **public read-only sharing**
  (anonymous link, `file_link_access` pattern; public route serves only `published` pages).
- **Phase 3 — RAG (pgvector) + knowledge agent**: `pgvector` extension, `wiki_chunks` table
  (embedding), idempotent (re)embedding worker on save, cosine kNN search **filtered by the
  asking user's permission**, citations, `ai_indexed` on/off. Dedicated `AgentKnowledge` agent
  with **read** tools (`wiki.search`/`wiki.read`) **and write** tools
  (`wiki.create_page`/`wiki.edit_page`, with confirmation §6.3). **Delivers "feeds the assistant"
  and "read and write".**
- **Future**: real-time editing (CRDT), page templates, export to PDF (reusing the
  presentation/OnlyOffice stack), hybrid FTS as a cheap kNN pre-filter.

## 12. Structural decisions — **RESOLVED** (2026-07-22, by the user)
1. **Editor**: ✅ **TipTap/ProseMirror**. New dependency authorized.
2. **RAG**: ✅ **pgvector** (semantic) as the primary approach — the Postgres `pgvector`
   extension is approved new infra. FTS (`tsvector`) stays as an optional cheap/hybrid filter, not
   a separate phase. Only the **embeddings provider** is still to be chosen (§7.3) — an
   operational decision that doesn't block the schema.
3. **Permissions**: ✅ by **person**, **group**, and **whole organization** (org-wide), with
   **access (view)** or **edit** as the type; plus **public read-only access** (anonymous link).
   Must support creating **N independent wikis** (each "wiki" = a space with its own tree and
   permissions). See §5.
4. **Assistant**: ✅ a **dedicated knowledge agent** (`AgentKnowledge`) that **reads and writes**
   pages — `wiki.search`/`wiki.read` tools (read) **and** `wiki.create_page`/`wiki.edit_page`
   (write, with confirmation §6.3). See §7.5.

## 13. Risks and mitigation
- **RAG leakage**: mitigated by §7.4 (same `canAccess` as the UI; `ai_indexed`; app-level gating).
- **Injection via content** (a malicious page "instructing" the AI): content is data, never an
  instruction (§2, §7.1); the system prompt reinforces this.
- **Unsafe HTML** in rendering: always sanitize; `body_json` is the source, `body_html` is a
  controlled derivation.
- **Large files/tables**: `body_json` in `jsonb` with a size limit; tree pagination.
- **Editing conflicts**: honest optimistic 409 (§6); real-time is for the future.

---
_§12 resolved (2026-07-22). Next step: start **Phase 1** (schema for the N wikis + CRUD + TipTap
editor + person/group/org permissions), tested before moving on. Public access lands in Phase 2
and pgvector RAG + the knowledge agent in Phase 3._

## Implementation progress

> Checklist derived from §11. Mark `[x]` **only** for what is implemented **and** passing the
> quality gates (sqlc/go build/vet/test + tsc/build + i18n). One small step per iteration.

### Phase 1 — Foundation and read/write
- [x] Migration `00090_wiki.sql` (spaces, pages, versions, space_grants, page_grants) + `sqlc generate` + `go build` OK
- [x] sqlc queries (`vis` CTE via `WikiSpaceRole`/`WikiPageRole`, CRUD spaces/pages, grants, versions) in `db/queries/queries_wiki.sql` — 26 generated methods, `go build` OK
- [x] `internal/wiki` service — access (`spaceRole`/`pageRole`/`authorize`), tree (`Tree`/`buildTree`), `body_text`/`body_html` derivation safe by construction (`render.go`), optimistic versioning (`UpdatePage` → 409 `ErrVersionConflict` + snapshot); `go build`/`vet` OK
- [x] Service tests (`internal/wiki/wiki_test.go`): `derive` (escaping/anti-script/unsafe URL), `slugify`, `safeURL`, `validateGrant`, `authorize`, `buildTree` — `go test ./internal/wiki/` OK
- [x] Thin handlers `internal/httpapi/wiki_handlers.go` + routes in `server.go` (CRUD for N wikis, pages, grants, tree, versions, move); `writeWikiErr` maps sentinels→`error_code`; `go build`/`vet`/`test` OK
- [x] Backend app gating: `toggleableApps += "wiki"` (`apps_handlers.go`); `go build`/`vet` OK
- [x] Frontend app gating: `App.tsx` (View union + VIEWS + mobile-order + `allApps` + `IcBook` + `WikiView` switch) + i18n `webmail.apps.wiki` (pt-BR + en); `tsc`/`build` OK
- [x] Install TipTap (`frontend`) — `@tiptap/react`, `@tiptap/pm`, `@tiptap/starter-kit`, `@tiptap/extension-link`, `@tiptap/extension-placeholder` (v2.27); `tsc`/`build` OK
- [x] `WikiView.tsx` — list of N wikis + create/delete space, page tree (`WikiTree`), reader (sanitized `body_html`), create page, publish/unpublish, temporary text editor with optimistic 409; `backend.ts` `wikiApi` + CSS `.wiki-*`
- [x] `WikiEditor.tsx` — TipTap editor (StarterKit H1–H3/bold/italic/code/lists/quote/code block/hr + Link + Placeholder), i18n toolbar, emits `body_json` (`getJSON`); integrated into `WikiView` (replaced the textarea). **No native dialogs in the UI** — create wiki via `Drawer` (sidecar), inline new page, delete with a 2-step `ConfirmButton` (per user feedback; see memory). `tsc`/`build` OK
- [x] Permissions UI by person/group/org — `WikiShareDrawer` (sidecar, managers only): lists grants, adds/removes access for a person (colleagues), a team (`/api/my-groups`) or the org, role view/edit/manage; controls gated by `spaceRole` (share/delete=manager, new page=editor). `tsc`/`build` OK
- [x] i18n `wiki.*` + `server.wiki.*` (pt-BR + en, 1718 keys at parity, validated)
- [x] `tsc --noEmit` + `npm run build` OK ; **deployed**: `docker compose up -d --build api worker` → migration `00090_wiki.sql` applied (goose v90), `wiki_spaces`/`wiki_page_grants` exist, api on `:8000`, `GET /api/wiki/spaces` → 401 (route live and protected), worker OK

> **Phase 1 COMPLETE and tested (live deploy).** Backend (service+handlers+routes+gating),
> frontend (WikiView+WikiEditor TipTap+WikiShareDrawer, no native dialogs), i18n pt-BR/en at
> parity (1754 keys). Next: **Phase 2**.

### Phase 2 — History, collaboration and public
- [x] Versions: diff + restore — backend `GetVersion`/`RestoreVersion` (reuses `UpdatePage`: versions+snapshots+optimistic) + routes `GET/POST .../versions/{version}[/restore]`; frontend `WikiHistoryDrawer` (sidecar) with line-based LCS diff and restore (editor only). `go build/vet/test` + `tsc`/`build` OK
- [x] Optimistic 409 lock (`wiki.version_conflict`) in the UI — `WikiView` save and `WikiHistoryDrawer` restore detect `ApiError.code` and show `wiki.conflict` + reload
- [x] Comments — migration `00091_wiki_comments.sql` (applied, table exists), sqlc, service `internal/wiki/comments.go` (read=viewer, comment=commenter, delete=author|manager), handlers+routes (`GET/POST .../comments`, `DELETE /wiki/comments/{id}`), frontend `WikiComments` (list+form, delete own/manager, no native dialogs), i18n parity. Deploy OK, route 401 guarded
- [x] Attachments (Drive/MinIO reuse) — migration `00092_wiki_attachments.sql` (applied), sqlc, service `internal/wiki/attachments.go` (attach/remove=editor, list/download=viewer; only attaches files the user owns; download checks page access), handlers+routes (`GET/POST .../attachments`, `DELETE .../attachments/{attId}`, `GET .../download`), frontend `WikiAttachments` (search in Drive by name + attach/remove/download). Deploy OK, route 401 guarded
- [x] `@person` mentions and `[[page]]` links — TipTap `@tiptap/extension-mention` (2 instances: `@`=person via `/api/colleagues`, `[`=`pageLink` via the space's tree) with a vanilla popup (`WikiSuggest.ts`, no tippy); `render.go` handles `mention`/`pageLink` nodes (escaped HTML `wiki-mention`/`wiki-pagelink` + `data-page-id`, and `body_text` includes `@label`/title) with tests; `go build/vet/test` + `tsc`/`build` OK; deployed OK
- [x] Public read-only access — queries `SetWikiSpacePublic`/`GetWikiSpaceByPublicSlug`; service `internal/wiki/public.go` (`SetPublicAccess` manager: generates a random/regenerable `public_slug`, toggles it; `PublicSpace`/`PublicPage` anonymous, `published` only, tenant resolved from the slug); authenticated handler (toggle) + **anonymous** `GET /api/public/wiki/{slug}[/{pageSlug}]` (`publicRateLimit` group, outside auth); frontend: toggle+link+copy+regenerate in `WikiShareDrawer`, public page `src/pages/PublicWiki.tsx` on routes `/w/:slug[/:pageSlug]` (outside the shell), serves only `body_html`. Tested: anonymous route → 404 (not 401), toggle → 401 guarded. Deploy OK

> **Phase 2 COMPLETE and tested (live deploy).** History (diff/restore) + 409, comments,
> attachments (Drive), `@` mentions/`[[page]]` links, and public read-only access `/w/<slug>`.
> i18n pt-BR/en at parity (1773 keys). Next: **Phase 3 (RAG + agent)** — step 1 STOPS and asks
> for the embeddings provider (dimension D) before creating `wiki_chunks`.

### Block 2 — Improvements (user request, 2026-07-22)
> Continuous loop. Order by dependency; one step per iteration, gates always.
- [x] **B1 Design + deep links** — all the `btn*` (a class that didn't exist) swapped for the
      house patterns: primary `add` (with CSS scoped to `.wiki`/drawer body), neutral `chipbtn`,
      icon `ib`; drawer footers with `<div class="grow">` like the rest. Routes
      `/mail/wiki/:spaceId/:pageId` — the selection lives in the URL (deep links; `WikiView`
      receives `sub/sub2/onNav` from `App`). `tsc`/`build` OK
- [x] **B2 BlockNote editor** — `@blocknote/core|react|mantine` 0.52 (MPL; installed with
      `--legacy-peer-deps` + peers `@mantine/core|hooks` and `@tiptap/pm@3`; TipTap itself
      removed). `WikiEditor` rewritten: uncontrolled BlockNote, pt/en locale, theme via
      `data-theme`, default slash menu + **3 AI items** (continue/improve/summarize) calling the
      new `POST /api/ai/wiki-write` (session+rate-limit; `Assistant.WikiWrite` responds with
      Markdown → `tryParseMarkdownToBlocks`; the page content goes in as DATA). `body_json` =
      array of BlockNote blocks; `render_blocknote.go` (safe HTML/text: grouped lists, checklist,
      table, code, image, styles, whitelisted links) with tests; the legacy ProseMirror format
      still renders and opens in the editor via `body_text`. **Deferred, noted:** `@`
      mentions/`[[page]]` links in BlockNote (custom inline content; to be reimplemented as its
      own step). go+tsc+build gates OK; deploy OK (route 401 guarded)
- [x] **B3 Page-level permissions** — service `page_grants.go` (list/set/remove, managing requires
      manager ON the page; grants add to the space, never reduce it), routes
      `GET/PUT/DELETE /wiki/pages/{id}/grants[/{grantId}]`; UI: `WikiGrantsEditor` extracted
      (shared space↔page, DRY), `WikiPageShareDrawer` (sidecar) with a hint, share button in the
      page header (manager only). go+tsc+build+i18n gates OK; deploy OK (401 guarded)
- [x] **B4 Wiki settings + protected delete** — `WikiSettingsDrawer` (sidecar, gear icon in the
      sidebar for managers): name/icon/description/visibility/`ai_indexed`; **danger zone** only
      for the space owner OR an org admin (owner/admin/internal_admin), deleting requires TYPING
      THE WIKI'S NAME (input compared, no native dialog); the delete button removed from the
      sidebar. Backend: `DeleteSpace` now requires owner∥admin (a granted manager role is no
      longer enough; correct 404/403). go+tsc+build+i18n gates OK; deploy OK. Note: `.vite` cache
      cleared after BlockNote (the dev server needs a restart)
- [x] **B5 Customizable home** — migration `00094_wiki_home.sql` (applied; 1 row per tenant),
      service `home.go` (derives the same way as pages), routes `GET /api/wiki/home`
      (authenticated) and `PUT` with `requirePerm(PermOrgSettings)`; frontend `WikiHome` on the
      home screen (renders `body_html`; admins get a pencil→inline BlockNote editor; shows the
      default text when there's no content). **Mantine fix**: v9 requires React 19 → downgraded
      to `@mantine/core@8.3.18` (React 18 peer, accepted by BlockNote); `.vite` cache cleared (dev
      server needs a restart). go+tsc+build+i18n gates OK; deploy OK (401 guarded, table exists)
- [x] **B6 Assistant → redirect + realtime** — the `wiki.create_page`/`wiki.edit_page` tools
      return a `link` (`/mail/wiki/<space>/<page>`) in `Result.Data` (the model cites it) and
      publish a **`wiki_page`** event on the Redis hub for the session's user; `WikiView`
      subscribes (`useChatSocket`): **created → navigates to the page; edited → refreshes the open
      one** (invalidate tree+page). Bonus: the AI now writes `body_json` in **BlockNote** format
      (opens natively in the editor). go+tsc+build gates OK; deploy OK
- [x] **B7 Page templates** — migration `00095` (`is_template` + partial index, applied), queries
      `SetWikiPageTemplate`/`ListWikiTemplates`, service (mark=editor, list=viewer), routes
      `POST /wiki/pages/{id}/template` and `GET /wiki/spaces/{id}/templates`; frontend: a
      "Make template/Template ✓" chip in the page actions, and the new-page form gets a
      "Blank page / Template: X" select that **copies the chosen template's body_json**.
      go+tsc+build+i18n gates OK; deploy OK
- [x] **B8 Export to PDF** — printer button in the page actions (`window.print`, which is the
      OS print dialog, not a data prompt) + `@media print` that hides the shell/rail/sidebar/
      actions/comments/attachments and prints only the clean content → the browser's "Save as
      PDF". `tsc`/`build`/i18n OK (no backend)
- [x] **B9 Hybrid FTS** — `SearchWikiChunks` gains
      `AND (@use_fts=false OR search_tsv @@ websearch_to_tsquery('simple', @query_text))` (cheap
      GIN pre-filter before the kNN); `SearchChunks` does 2 passes: hybrid first, and if it
      returns 0 it retries semantic-only (guaranteed recall). sqlc+build+test OK; deploy OK
- [x] **B10 Real-time (CRDT)** — **user decision: a Yjs relay in Go, no new container.** Backend:
      `wiki_collab.go` — WS route `/api/wiki/pages/{id}/collab/ws` (session +
      `canAccess`≥editor; viewers can't even join), in-process rooms per page with a
      writer-goroutine per peer, opaque binary relay (the server doesn't understand Yjs; clients
      answer each other's sync steps), 1st frame `{"type":"hello","peers":N}` for the seeding
      rule; multi-instance scaling deferred until needed (commented). Frontend:
      `WikiCollabProvider.ts` (classic y-websocket protocol over `@y/y`+`@y/protocols` v14 RC —
      the ones BlockNote 0.52 uses; sync+awareness+reconnect) and `WikiEditor` with
      `collaboration:{provider, fragment: doc.get("document-store"), user}` — **peers===0 seeds
      from body_json; peers>0 starts empty and syncs** (anti-duplication); cursors with a stable
      name/color via awareness (`me` query). Autosave keeps saving `body_json` via `UpdatePage`
      (RAG/versions/render untouched). go+tsc+build gates OK; deploy OK (WS 401 guarded)
- [x] **`@` mentions and `[[page]]` links in BlockNote** (B2 loose end closed) —
      `createReactInlineContentSpec` ×2 (`mention`/`pageLink` with `{id,label}` props), custom
      schema, 2 extra `SuggestionMenuController`s (`@`=colleagues via `/api/colleagues`,
      `[`=space pages, excluding the page itself) inserted via `insertInlineContent`;
      `render_blocknote.go` handles both (escaped HTML `wiki-mention`/`wiki-pagelink`+
      `data-page-id`, and `body_text` with `@label`/title for RAG) with tests including
      anti-`<script>`. go+tsc+build gates OK; deploy OK

- [x] **Basic template for a new page** (post-block request) — a page created without choosing a
      template starts with an i18n "Overview / Details / Notes" skeleton (BlockNote blocks via
      `basicTemplate(t)` in `WikiView`); choosing a space template still copies it.
      `tsc`/`build`/i18n OK (frontend only)

- [x] **AI writes with editor components** (post-test feedback) — Go converter
      `wiki.MarkdownToBlocks` (headings, bullet lists, numbered lists, checklists, quotes, fenced
      code, bold/italic/inline code, links) with tests; the `wiki.create_page`/`wiki.edit_page`
      tools document Markdown and the content becomes native BlockNote blocks. **Global
      notification**: when the AI creates/edits a page and the user is NOT in the wiki, a toast
      appears with an "Open" action (inside the wiki it keeps navigating/refreshing).
      Diagnostic note: "tool not available" happened because EXISTING conversations keep the
      agent they started with (routing is per-conversation, §3.2) — it works in a new
      conversation or with the "Knowledge base" agent selected in the picker

- [x] **Fix "this action isn't available" in `wiki.edit_page`** — a trace showed the model
      calling `wiki.edit.page` (dots) instead of `wiki.edit_page`; the engine gained
      `canonicalToolName` (compares ignoring `[._-]`, ONLY against the agent's own list — fixes
      spelling, never widens permissions) with a test. Routing tuned: the Documents agent's
      purpose now explicitly excludes wiki pages; the Knowledge one mentions "pages". Deploy OK

- [x] **Fix ROOT CAUSE of "editing loses formatting"** — Go serializes `[]byte` as BASE64:
      `body_json` reached the frontend as a string, `Array.isArray` failed, and the editor fell
      back to plain text (this was the primary cause from the start; StrictMode/hello were
      aggravating factors). sqlc override `wiki_pages/versions/home/comments.body_json →
      encoding/json.RawMessage` (house pattern, like `contacts.custom_fields`) → raw JSON in the
      response; defense in the editor for old cached responses (string/base64 parsing). Deploy OK

### Block 4 — Kanban (user request, 2026-07-22)
- [x] **Flat cards** — no box-shadow, no border (including the left accent bar), subtle hover;
      shadows removed on the page (kb-views)
- [x] **Card color** — migration `00097` (`cards.color`, applied), `UpdateCard` + handler; picker
      of 8 soft tints in the TaskModal (+ "no color"); a colored card forces a dark text tint
      (contrast guaranteed in both light and dark)
- [x] **Share card to chat** — `ShareCardChatDrawer`: channels (most recent first by
      `last_read_at`, searchable) and people (opens a DM via `chatOpenDM`); sends a message with
      an internal markdown link to the card; the button only appears when the Chat app is enabled
      (`useDisabledApps`). i18n pt-BR/en (1857 at parity)

- [x] **Kanban extras (post-feedback)** — visible hover on all cards (brightness filter, works
      over tints and in both themes); TaskModal footer fixed (it was nested outside the
      `.drawer>footer` scope: Save accent, Delete/Archive inline); i18n key `crm.linkContact`
      added; **per-user board reordering** — draggable tabs, order in `settings.kanban.boardOrder`
      (persisted on the account; new boards land at the end)

- [x] **Kanban board permissions** — migration `00098` (`boards.owner_user_id` +
      `board_grants` person/team/org with viewer|editor|manager, applied). Rules in SQL (wiki
      pattern): owner=manager; **legacy board (no owner, no grants) = whole org edits**
      (backwards compatibility); **assigned card = automatic editor access** (requested).
      `ListBoards` filtered per user; handler gates (view=viewer; cards/columns=editor via
      `requireCardEditor`; rename/delete/archive/prefix/grants=manager). New board: owner=creator
      + default org editor grant (removable). Frontend: `BoardShareDrawer` (reuses
      `WikiGrantsEditor`) with a note about auto-access, share button + management for managers
      only, `/access` route for the role. Deploy OK (401 guarded)

> **BLOCK 2 COMPLETE (2026-07-22).** B1–B10 + mentions: aligned design, BlockNote+AI, page-level
> permissions, protected delete, customizable home, assistant redirect+realtime, templates, PDF,
> hybrid FTS, and CRDT collaboration via a Go relay (no new infra).

### Block 3 — Attachments/images/comments (user request, 2026-07-22)
- [x] **Integrated Drive picker** — `DrivePickerDrawer` (browse folders with breadcrumbs, search,
      upload to the current folder, select) Google-style; used for wiki attachments (replaces the
      search-by-name) and reusable
- [x] **Same picker in chat** — "Attach from Drive" button in the channel composer toolbar;
      attaches by `file_id` (sending already supported `attachments[]` — zero backend work)
- [x] **Comments with an editor** — migration `00096` (body_json/body_html on `wiki_comments`,
      applied); `AddComment` derives text+HTML through the same safe path as pages;
      `CommentEditor` (mini BlockNote, no side/slash menu) in place of the textarea; rendered via
      `body_html` (old ones fall back to text)
- [x] **New page creation with a template gallery** (post-feedback revision) — the inline form
      is gone; creating a page opens a Drawer with a title + gallery: Blank, Knowledge base,
      Meeting minutes, Project brief, Step-by-step guide (CSS thumbnails representing the
      structure) + the space's own templates; the chosen content is saved into body_json (not
      just cosmetic). **Root-cause fix for "template disappears while editing"**: the relay's
      hello arrived before the editor registered its callback → the seed never ran; the provider
      now buffers and replays the hello. Comment editor with the slash menu active (the
      placeholder promises a '/')
- [x] **Definitive fix for "editing opens blank"** — root cause: the `useState` initializer runs
      twice under StrictMode and the ORPHANED provider connected in the constructor, occupying
      the room (the real one received peers=1 and never seeded). The provider no longer connects
      in the constructor (only in `useEffect` via idempotent `ensureConnected()`) — the orphan
      never opens a WS; a 2s seed fallback covers ghost connections. Editor embedded in the page
      (no border, transparent background). — (previous StrictMode fix) — in dev React
      mounts→unmounts→mounts and the cleanup was destroying the shared provider (the WS never
      reopened → seed never ran); `ensureConnected()` revives the provider on mount +
      **timeout-based seed fallback** (1.5s with no hello → seed anyway; editing alone never
      opens empty). Page editor with window height (`min-height: calc(100vh-240px)`). Comments
      with a **visible chat-style toolbar** (B/I/S/code/list/quote via `toggleStyles`/
      `updateBlock`)
- [x] **Page images → Drive + warning** — BlockNote's `uploadFile`: uploads to Drive + a public
      link to the file (`inline` URL reachable by any reader, including the public route); pasting
      a URL still works via the native Embed tab; **warning** on the public-access toggle
      (`wiki.public.imagesWarn`) that embedded links are public

### Phase 3 — RAG (pgvector) + knowledge agent
- [x] Embeddings provider **decided by the user (2026-07-22): OpenAI `text-embedding-3-small`,
      D=1536** (OpenAI-compatible endpoint via env; see memory `wiki-embeddings-provider`)
- [x] Migration `00093_wiki_chunks.sql` (`CREATE EXTENSION vector` +
      `wiki_chunks(embedding vector(1536))` + cosine ivfflat) **applied and verified** (`vector`
      extension active, table exists); Postgres image switched to `pgvector/pgvector:pg16`
      (16.x volume preserved); sqlc with a `vector→string` override (`[..]` literal + `::vector`
      cast)
- [x] Idempotent (re)embedding worker — `internal/wiki/rag.go` (`EmbedPending`: clears stale
      entries, ~2000-char chunking with overlap+cap, batch of 32, upsert by
      `(page_id,chunk_index)` + trim) + client `internal/ai/embeddings.go` (OpenAI-compatible) +
      30s ticker in `cmd/worker`; chunking/literal tests OK. **Active when `EMBEDDINGS_API_KEY`
      is set in the environment** (compose already passes `EMBEDDINGS_*`; no key = RAG disabled,
      fail-safe)
- [x] Filtered cosine kNN search — query `SearchWikiChunks` (tenant + `vis` + `ai_indexed` +
      `published`, cosine distance) + `Service.SearchChunks` (embeds the question); returns
      title/slug/excerpt (citation data). The citation surface shown to the user comes with the
      `wiki.search` tool (next step)
- [x] `AgentKnowledge` agent + 4 `wiki.*` tools — `tools_wiki.go` (read `wiki.search`/`wiki.read`
      without confirmation; write `wiki.create_page`/`wiki.edit_page` with `Writes+Confirm` §6.3);
      adapters `httpapi/assistant_wiki.go` revalidate `canAccess` in the service (create/edit =
      `editor`, optimistic, draft by default); retrieved content is DATA (the agent's
      instructions reinforce anti-injection); the "general ⊇ builtin" invariant holds (the 4
      tools are also in `AgentGeneral`); without `EMBEDDINGS_API_KEY`, `wiki.search` returns a
      clear error and the rest still works. `go test ./...` 11/11 OK
- [x] Complete wiring — `Deps.Wiki*` + `BuildRegistry` + `buildAssistant` + `Server.wikiEmb` +
      `AppTools["wiki"]` + `AppAgent["wiki"]` (disabling the app cuts tools+agent) + i18n
      `assistant.agent.knowledge` (pt-BR+en, 1774 keys at parity)

> **Phase 3 COMPLETE and tested live (2026-07-22).** With `EMBEDDINGS_API_KEY` set: the worker
> logged `wiki embed: 1 page(s) indexed` and wrote a real `vector(1536)` chunk into pgvector
> (verified in Postgres). **Wiki Phases 1–3 complete.**
