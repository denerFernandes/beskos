// Client tipado da NOSSA API (control plane) para as features de e-mail: IA, Kanban, calendário,
// marcadores. Usa o cookie de sessão de webmail (POST /api/webmail/login). Same-origin via proxy.
// O e-mail em si continua JMAP direto no Stalwart (ver jmap/).

import i18n from "../i18n";
import { apiUrl, authHeaders, mediaUrl, notifyUnauthorized } from "../apiBase";

// Traduz o erro do backend pela CHAVE estável (error_code) via i18n (server.<code>). Nunca
// mostra o texto cru do backend na UI (lei §4.6).
export function translateError(code: string | undefined, serverMsg: string | undefined, status: number): string {
  if (code) return i18n.t(`server.${code}`, { defaultValue: serverMsg || i18n.t("errors.http", { status }) });
  return serverMsg || i18n.t("errors.http", { status });
}

// ApiError carrega o STATUS HTTP além da mensagem já traduzida. Sem isto, quem apanha o erro só
// tinha o texto traduzido para classificar (ex.: distinguir "senha errada" de "backend fora do ar"),
// e fazer regex a uma string que muda com o idioma é frágil — um 401 traduzido não contém "401".
export class ApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

async function req<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(apiUrl(path), {
    ...opts,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...authHeaders(), ...(opts.headers || {}) },
  });
  if (!res.ok) {
    if (res.status === 401) notifyUnauthorized(path); // sessão morta → volta ao login (global)
    const body = await res.json().catch(() => ({}));
    throw new ApiError(res.status, translateError(body.error_code, body.error, res.status), body.error_code);
  }
  return res.json() as Promise<T>;
}

// ---- Tipos ----
export interface Task { title: string; due: string; assignee: string }
export interface Column { id: string; name: string; color: string; position: number; wip_limit?: number }
export interface BoardAssignee { card_id: string; user_id: string; name: string }
export interface BoardDep { card_id: string; depends_on_id: string; dep_title: string; dep_column: string }
export interface Automation { id: string; board_id: string; trigger_column_id: string; action: string; action_value: string }
export interface Card {
  id: string; column_id: string; title: string; description: string;
  label: string; due: string; priority: string; source_email_id: string; position: number;
  contact_id: string | null; start_date?: string;
  assignee_id: string | null;
  // assignee_name: nome do utilizador responsável — ou o texto legado de quem nunca foi utilizador
  // (migração 00026). `assignee` só sobrevive nesse caso; a UI escreve sempre assignee_id.
  assignee: string; assignee_name: string | null; comment_count?: number; color?: string;
  number: number; card_key?: string; // "QUA-12" — prefixo do quadro + número; derivado, não gravado
}

export interface CardComment {
  id: string; card_id: string; author_id: string; author_name: string; author_email: string;
  parent_id: string | null; body: string; mentions: string[]; created_at: string;
}
export interface Board { id: string; name: string; prefix?: string; owner_user_id?: string | null }
export interface BoardGrant { id: string; board_id: string; subject_type: "user" | "group" | "org"; subject_id: string | null; role: string; created_at: string }
export interface BoardDetail { board: Board; columns: { column: Column; cards: Card[] }[]; assignees?: BoardAssignee[]; deps?: BoardDep[] }
export interface Label { id: string; name: string; color: string }
// Um label aplica-se ao e-mail como keyword JMAP derivada do seu id (estável a renomeações).
// IMAP/JMAP keyword = átomo sem espaços; por isso o id sem hífens, com prefixo próprio.
export const labelKeyword = (id: string) => "mailinlabel-" + id.replace(/-/g, "");
export interface CalEvent {
  id: string; title: string; description: string; location: string;
  color: string; starts_at: string; ends_at: string; source_email_id: string;
  tags: string[]; meeting_room: string; remind_minutes?: number; all_day?: boolean; recur?: string; recur_until?: string | null;
  invited?: boolean; my_rsvp?: string; calendar_id?: string | null; booking_id?: string | null; kind?: string;
  attendees?: { name: string; email: string; avatar?: string; status?: string }[];
}
export interface TimeOffConflict { id: string; title: string; starts_at: string; ends_at: string; own: boolean }
export interface BookingWindow { weekday: number; start: number; end: number }
export interface BookingCfg { enabled: boolean; title: string; duration_min: number; phone_field: string; company_field: string; windows: BookingWindow[]; url: string }
export interface Calendar { id: string; name: string; color: string; owner_user_id: string; created_at: string; }
export interface Tag { id: string; name: string; color: string }
export interface EventInvite { user_id: string; status: string; name: string; email: string }
export interface EventInput { title: string; starts_at: string; ends_at: string; description?: string; location?: string; color?: string; tags?: string[]; create_meet?: boolean; force?: boolean; remind_minutes?: number; all_day?: boolean; recur?: string; recur_until?: string; invitee_ids?: string[]; external_emails?: string[]; calendar_id?: string }
export interface EventDraft { title: string; starts_at: string; ends_at: string; tags: string[] }
export interface Slot { starts_at: string; ends_at: string }
// Erro de conflito de horário (backend responde 409 com os eventos que colidem).
export class ConflictError extends Error { conflicts: CalEvent[]; constructor(c: CalEvent[]) { super("conflict"); this.conflicts = c; } }

export const backend = {
  // Sessão do usuário final na nossa API (valida a credencial no Stalwart). Com 2º fator, devolve
  // mfa_required + challenge (sem token) — o 2º passo é mfaVerify.
  login: (email: string, password: string) =>
    req<{ email?: string; name?: string; token?: string; mfa_required?: boolean; challenge?: string; methods?: string[] }>("/api/webmail/login", {
      method: "POST", body: JSON.stringify({ email, password }),
    }),
  // 2º passo do login: troca o desafio + código (TOTP/SMS/recuperação) por uma sessão.
  mfaVerify: (challenge: string, code: string) =>
    req<{ token: string }>("/api/auth/mfa/verify", {
      method: "POST", body: JSON.stringify({ challenge, code }),
    }),
  logout: () => req<{ status: string }>("/api/webmail/logout", { method: "POST" }).catch(() => {}),

  // IA
  summarize: (subject: string, body: string) =>
    req<{ summary: string }>("/api/ai/summarize", { method: "POST", body: JSON.stringify({ subject, body }) }),
  draft: (original: string, instruction: string, tone: string) =>
    req<{ draft: string }>("/api/ai/draft", { method: "POST", body: JSON.stringify({ original, instruction, tone }) }),
  planTask: (idea: string) =>
    req<{ title: string; description: string }>("/api/ai/task", { method: "POST", body: JSON.stringify({ idea }) }),
  extractTasks: (subject: string, body: string) =>
    req<{ tasks: Task[] }>("/api/ai/extract-tasks", { method: "POST", body: JSON.stringify({ subject, body }) }),
  briefing: (input: { date: string; events: string[]; tasks: string[]; emails: string[]; period?: string; tomorrow?: string[] }) =>
    req<{ briefing: string }>("/api/ai/briefing", { method: "POST", body: JSON.stringify(input) }),

  announcements: (q = "", page = 1) => req<{ items: Announcement[]; total: number; page: number; can_manage: boolean }>(`/api/announcements?q=${encodeURIComponent(q)}&page=${page}`),
  announceCreate: (title: string, body: string, published: boolean) => req<{ id: string }>("/api/announcements", { method: "POST", body: JSON.stringify({ title, body, published }) }),
  announceSetPublished: (id: string, published: boolean) => req<{ status: string }>(`/api/announcements/${id}`, { method: "PATCH", body: JSON.stringify({ published }) }),
  announceDelete: (id: string) => req<{ status: string }>(`/api/announcements/${id}`, { method: "DELETE" }),

  chatMembersSearch: (id: string, q = "", page = 1) => req<{ members: ChatMember[]; total: number; page: number }>(`/api/chat/channels/${id}/members/search?q=${encodeURIComponent(q)}&page=${page}`),
  chatCanvas: (id: string) => req<{ canvas: string; updated_at: string | null }>(`/api/chat/channels/${id}/canvas`),
  chatSaveCanvas: (id: string, canvas: string) => req<{ status: string }>(`/api/chat/channels/${id}/canvas`, { method: "PUT", body: JSON.stringify({ canvas }) }),
  chatPins: (id: string) => req<{ pins: ChatPin[] }>(`/api/chat/channels/${id}/pins`),
  chatPin: (msgId: string, pinned: boolean) => req<{ status: string }>(`/api/chat/messages/${msgId}/pin`, { method: "POST", body: JSON.stringify({ pinned }) }),
  chatBookmarks: (id: string) => req<{ bookmarks: ChatBookmark[] }>(`/api/chat/channels/${id}/bookmarks`),
  chatAddBookmark: (id: string, b: { kind: "link" | "file"; title?: string; url?: string; file_id?: string; file_name?: string; file_size?: number }) =>
    req<{ id: string }>(`/api/chat/channels/${id}/bookmarks`, { method: "POST", body: JSON.stringify(b) }),
  chatDeleteBookmark: (id: string, bid: string) => req<{ status: string }>(`/api/chat/channels/${id}/bookmarks/${bid}`, { method: "DELETE" }),
  bookmarkDownloadUrl: (id: string, bid: string) => mediaUrl(`/api/chat/channels/${id}/bookmarks/${bid}/download`),
  chatSetNotifPref: (id: string, pref: "all" | "mentions" | "none") => req<{ pref: string }>(`/api/chat/channels/${id}/notif`, { method: "POST", body: JSON.stringify({ pref }) }),
  chatSearchInChannel: (id: string, q: string) => req<{ results: ChatPin[] }>(`/api/chat/channels/${id}/search?q=${encodeURIComponent(q)}`),

  // Kanban
  boards: () => req<Board[]>("/api/boards"),
  boardAccess: (id: string) => req<{ role: number }>(`/api/boards/${id}/access`),
  boardGrants: (id: string) => req<BoardGrant[]>(`/api/boards/${id}/grants`),
  setBoardGrant: (id: string, g: { subject_type: string; subject_id?: string | null; role: string }) =>
    req<BoardGrant>(`/api/boards/${id}/grants`, { method: "PUT", body: JSON.stringify(g) }),
  removeBoardGrant: (id: string, grantId: string) => req<{ status: string }>(`/api/boards/${id}/grants/${grantId}`, { method: "DELETE" }),
  board: (id: string) => req<BoardDetail>(`/api/boards/${id}`),
  createBoard: (name: string) => req<Board>("/api/boards", { method: "POST", body: JSON.stringify({ name }) }),
  updateBoard: (id: string, name: string) => req<Board>(`/api/boards/${id}`, { method: "PATCH", body: JSON.stringify({ name }) }),
  deleteBoard: (id: string) => req<{ status: string }>(`/api/boards/${id}`, { method: "DELETE" }),
  // Busca de tarefas por chave (QUA-12), número ou título — só nos quadros que a pessoa vê.
  searchCards: (q: string) => req<{ cards: CardHit[] }>(`/api/cards/search?q=${encodeURIComponent(q)}`),
  createCard: (boardId: string, c: Partial<Card> & { column_id: string; title: string }) =>
    req<Card>(`/api/boards/${boardId}/cards`, { method: "POST", body: JSON.stringify(c) }),
  moveCard: (cardId: string, columnId: string, position: number) =>
    req<Card>(`/api/cards/${cardId}/move`, { method: "PATCH", body: JSON.stringify({ column_id: columnId, position }) }),
  // Reordena (e move para) uma coluna: card_ids é a ordem final. Persiste a posição vertical do drag.
  reorderColumn: (columnId: string, cardIds: string[]) =>
    req<{ status: string }>(`/api/columns/${columnId}/reorder`, { method: "POST", body: JSON.stringify({ card_ids: cardIds }) }),
  reorderColumns: (boardId: string, columnIds: string[]) =>
    req<{ status: string }>(`/api/boards/${boardId}/columns/reorder`, { method: "POST", body: JSON.stringify({ column_ids: columnIds }) }),
  deleteCard: (cardId: string) => req<{ status: string }>(`/api/cards/${cardId}`, { method: "DELETE" }),
  archiveCard: (cardId: string, archived: boolean) => req<Card>(`/api/cards/${cardId}/archive`, { method: "POST", body: JSON.stringify({ archived }) }),
  archiveBoard: (boardId: string, archived: boolean) => req<Board>(`/api/boards/${boardId}/archive`, { method: "POST", body: JSON.stringify({ archived }) }),
  automations: (boardId: string) => req<Automation[]>(`/api/boards/${boardId}/automations`),
  createAutomation: (boardId: string, a: { trigger_column_id: string; action: string; action_value: string }) => req<Automation>(`/api/boards/${boardId}/automations`, { method: "POST", body: JSON.stringify(a) }),
  deleteAutomation: (id: string) => req<{ status: string }>(`/api/automations/${id}`, { method: "DELETE" }),
  // contact_id opcional de propósito: omitir mantém o contato ligado; "" desliga.
  updateCard: (cardId: string, c: Pick<Card, "title" | "description" | "label" | "due"> & { assignee_id?: string; contact_id?: string; priority?: string; start_date?: string; color?: string }) =>
    req<Card>(`/api/cards/${cardId}`, { method: "PATCH", body: JSON.stringify(c) }),
  addCardAssignee: (cardId: string, userId: string) => req<{ status: string }>(`/api/cards/${cardId}/assignees`, { method: "POST", body: JSON.stringify({ user_id: userId }) }),
  removeCardAssignee: (cardId: string, userId: string) => req<{ status: string }>(`/api/cards/${cardId}/assignees/${userId}`, { method: "DELETE" }),
  addCardDep: (cardId: string, dependsOnId: string) => req<{ status: string }>(`/api/cards/${cardId}/deps`, { method: "POST", body: JSON.stringify({ depends_on_id: dependsOnId }) }),
  removeCardDep: (cardId: string, depId: string) => req<{ status: string }>(`/api/cards/${cardId}/deps/${depId}`, { method: "DELETE" }),
  setBoardPrefix: (boardId: string, prefix: string) =>
    req<Board>(`/api/boards/${boardId}/prefix`, { method: "PATCH", body: JSON.stringify({ prefix }) }),
  createColumn: (boardId: string, name: string, color: string) =>
    req<Column>(`/api/boards/${boardId}/columns`, { method: "POST", body: JSON.stringify({ name, color }) }),
  updateColumn: (columnId: string, name: string, color: string, wipLimit = 0) =>
    req<Column>(`/api/columns/${columnId}`, { method: "PATCH", body: JSON.stringify({ name, color, wip_limit: wipLimit }) }),
  deleteColumn: (columnId: string) => req<{ status: string }>(`/api/columns/${columnId}`, { method: "DELETE" }),

  // Marcadores
  labels: () => req<Label[]>("/api/labels"),
  createLabel: (name: string, color: string) =>
    req<Label>("/api/labels", { method: "POST", body: JSON.stringify({ name, color }) }),
  deleteLabel: (id: string) => req<{ status: string }>(`/api/labels/${id}`, { method: "DELETE" }),

  // Calendário
  events: (from: string, to: string) =>
    req<CalEvent[]>(`/api/events?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
  // Cria evento; se houver conflito de horário e não for forçado, lança ConflictError com os choques.
  createEvent: async (e: EventInput): Promise<CalEvent> => {
    const res = await fetch(apiUrl("/api/events"), { method: "POST", credentials: "include", headers: { "Content-Type": "application/json", ...authHeaders() }, body: JSON.stringify(e) });
    if (res.status === 409) { const b = await res.json(); throw new ConflictError(b.conflicts ?? []); }
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  // Editar evento; como no create, conflito de horário (sem force) lança ConflictError.
  updateEvent: async (id: string, e: EventInput): Promise<CalEvent> => {
    const res = await fetch(apiUrl(`/api/events/${id}`), { method: "PATCH", credentials: "include", headers: { "Content-Type": "application/json", ...authHeaders() }, body: JSON.stringify(e) });
    if (res.status === 409) { const b = await res.json(); throw new ConflictError(b.conflicts ?? []); }
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  deleteEvent: (id: string) => req<{ status: string }>(`/api/events/${id}`, { method: "DELETE" }),
  restoreEvent: (id: string) => req<CalEvent>(`/api/events/${id}/restore`, { method: "POST" }),
  duplicateEvent: (id: string, title?: string) => req<CalEvent>(`/api/events/${id}/duplicate`, { method: "POST", body: JSON.stringify({ title: title ?? "" }) }),
  addMeetToEvent: (id: string) => req<{ meeting_room: string }>(`/api/events/${id}/meet`, { method: "POST" }),
  // Booking: página pública de agendamento (estilo Calendly) do próprio utilizador.
  bookingPage: () => req<BookingCfg>("/api/booking/page"),
  timeOff: (v: { kind: string; start: string; end: string; start_min?: number; end_min?: number; auto_reply: boolean; reply_subject: string; reply_body: string; on_conflict?: string }) =>
    req<{ status: string; auto_reply?: string; conflicts?: TimeOffConflict[] }>("/api/timeoff", { method: "POST", body: JSON.stringify(v) }),
  bookingSave: (cfg: Omit<BookingCfg, "url">) => req<BookingCfg>("/api/booking/page", { method: "PUT", body: JSON.stringify(cfg) }),
  calendars: () => req<Calendar[]>("/api/calendars"),
  createCalendar: (name: string, color: string) => req<Calendar>("/api/calendars", { method: "POST", body: JSON.stringify({ name, color }) }),
  updateCalendar: (id: string, name: string, color: string) => req<Calendar>(`/api/calendars/${id}`, { method: "PATCH", body: JSON.stringify({ name, color }) }),
  deleteCalendar: (id: string) => req<{ status: string }>(`/api/calendars/${id}`, { method: "DELETE" }),
  freeBusy: (userId: string, from: string, to: string) => req<{ starts_at: string; ends_at: string }[]>(`/api/events/free-busy?user=${userId}&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
  eventExternals: (id: string) => req<string[]>(`/api/events/${id}/externals`),
  eventInvites: (id: string) => req<EventInvite[]>(`/api/events/${id}/invites`),
  rsvpEvent: (id: string, status: string) => req<{ status: string }>(`/api/events/${id}/rsvp`, { method: "POST", body: JSON.stringify({ status }) }),
  tagsList: () => req<Tag[]>("/api/tags"),
  tagUpsert: (name: string, color: string) => req<Tag>("/api/tags", { method: "POST", body: JSON.stringify({ name, color }) }),
  tagDelete: (id: string) => req<{ status: string }>(`/api/tags/${id}`, { method: "DELETE" }),
  // Envia o fuso do navegador para a IA interpretar "amanhã 10h" no horário do próprio utilizador.
  aiParseEvent: (text: string) => req<EventDraft>("/api/ai/calendar/parse", { method: "POST", body: JSON.stringify({ text, tz: Intl.DateTimeFormat().resolvedOptions().timeZone }) }),
  aiSuggestSlots: (duration_min: number, from: string, to: string) => req<{ slots: Slot[] }>("/api/ai/calendar/suggest", { method: "POST", body: JSON.stringify({ duration_min, from, to }) }),
  // Exportar/importar a agenda em iCalendar (.ics).
  eventsExportUrl: () => mediaUrl("/api/events/export"),
  importEvents: async (file: File): Promise<{ imported: number }> => {
    const fd = new FormData();
    fd.append("file", file);
    const res = await fetch(apiUrl("/api/events/import"), { method: "POST", credentials: "include", headers: authHeaders(), body: fd });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  aiAgenda: (from: string, to: string) => req<{ agenda: string }>("/api/ai/calendar/agenda", { method: "POST", body: JSON.stringify({ from, to }) }),
  aiWikiWrite: (instruction: string, context: string) => req<{ content: string }>("/api/ai/wiki-write", { method: "POST", body: JSON.stringify({ instruction, context }) }),

  // Arquivos (Drive): pastas, mover, compartilhar
  files: (folderId?: string) => req<FilesListing>(`/api/files${folderId ? `?folder=${folderId}` : ""}`),
  // Busca global (recursiva) por nome, em todas as pastas — não só a aberta.
  searchFiles: (q: string) => req<FilesSearch>(`/api/files/search?q=${encodeURIComponent(q)}`),
  uploadFile: async (file: File, folderId?: string): Promise<StorageFile> => {
    const fd = new FormData();
    fd.append("file", file);
    if (folderId) fd.append("folder_id", folderId);
    const res = await fetch(apiUrl("/api/files"), { method: "POST", credentials: "include", headers: authHeaders(), body: fd });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  // Upload com progresso (via XHR — o fetch não expõe o progresso de envio). onProgress: 0..1.
  uploadFileProgress: (file: File, folderId: string | undefined, onProgress: (pct: number) => void): Promise<StorageFile> =>
    new Promise((resolve, reject) => {
      const fd = new FormData();
      fd.append("file", file);
      if (folderId) fd.append("folder_id", folderId);
      const xhr = new XMLHttpRequest();
      xhr.open("POST", apiUrl("/api/files"));
      xhr.withCredentials = true;
      for (const [k, v] of Object.entries(authHeaders())) xhr.setRequestHeader(k, v);
      xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded / e.total); };
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) { try { resolve(JSON.parse(xhr.responseText)); } catch { reject(new Error("bad response")); } }
        else { let b: { error_code?: string; error?: string } = {}; try { b = JSON.parse(xhr.responseText); } catch { /* corpo não-JSON */ } reject(new Error(translateError(b.error_code, b.error, xhr.status))); }
      };
      xhr.onerror = () => reject(new Error(translateError(undefined, undefined, 0)));
      xhr.send(fd);
    }),
  downloadFileUrl: (id: string) => mediaUrl(`/api/files/${id}/download`),
  deleteFile: (id: string) => req<{ status: string }>(`/api/files/${id}`, { method: "DELETE" }),
  trashFiles: () => req<FilesSearch>("/api/files/trash"),
  fileVersions: (id: string) => req<{ current_version: number; versions: FileVersion[] }>(`/api/files/${id}/versions`),
  restoreVersion: (id: string, versionId: string) => req<StorageFile>(`/api/files/${id}/versions/${versionId}/restore`, { method: "POST" }),
  versionDownloadUrl: (id: string, versionId: string) => mediaUrl(`/api/files/${id}/versions/${versionId}/download`),
  restoreFile: (id: string) => req<StorageFile>(`/api/files/${id}/restore`, { method: "POST" }),
  purgeFile: (id: string) => req<{ status: string }>(`/api/files/${id}/permanent`, { method: "DELETE" }),
  emptyTrash: () => req<{ status: string; count: number }>("/api/files/trash", { method: "DELETE" }),
  renameFile: (id: string, name: string) => req<unknown>(`/api/files/${id}`, { method: "PATCH", body: JSON.stringify({ name }) }),
  moveFile: (id: string, folderId: string | null) => req<unknown>(`/api/files/${id}`, { method: "PATCH", body: JSON.stringify({ folder_id: folderId ?? "" }) }),
  copyFile: (id: string, name?: string) => req<StorageFile>(`/api/files/${id}/copy`, { method: "POST", body: JSON.stringify({ name: name ?? "" }) }),
  starFile: (id: string, starred: boolean) => req<StorageFile>(`/api/files/${id}/star`, { method: "POST", body: JSON.stringify({ starred }) }),
  fileThumbUrl: (id: string) => mediaUrl(`/api/files/${id}/thumb`),
  folderZipUrl: (id: string) => mediaUrl(`/api/folders/${id}/zip`),
  batchFiles: (ids: string[], action: "trash" | "restore" | "move", folderId?: string) => req<{ done: number }>("/api/files/batch", { method: "POST", body: JSON.stringify({ ids, action, folder_id: folderId ?? "" }) }),
  createFolder: (name: string, parentId?: string) => req<Folder>("/api/folders", { method: "POST", body: JSON.stringify({ name, parent_id: parentId ?? "" }) }),
  renameFolder: (id: string, name: string) => req<Folder>(`/api/folders/${id}`, { method: "PATCH", body: JSON.stringify({ name }) }),
  moveFolder: (id: string, parentId: string | null) => req<Folder>(`/api/folders/${id}/move`, { method: "POST", body: JSON.stringify({ parent_id: parentId ?? "" }) }),
  deleteFolder: (id: string) => req<{ status: string }>(`/api/folders/${id}`, { method: "DELETE" }),
  shares: (id: string) => req<Share[]>(`/api/files/${id}/shares`),
  shareFile: (id: string, principal: string, nivel: string) => req<Share>(`/api/files/${id}/share`, { method: "POST", body: JSON.stringify({ principal, nivel }) }),
  removeShare: (fileId: string, shareId: string) => req<{ status: string }>(`/api/files/${fileId}/shares/${shareId}`, { method: "DELETE" }),
  // Link público de download (anexos grandes). Devolve o token; a URL absoluta monta-se no cliente.
  fileLink: (id: string, opts?: { expires_days?: number; password?: string; permission?: "view" | "edit"; allow_download?: boolean }) =>
    req<FileLinkInfo>(`/api/files/${id}/link`, { method: "POST", body: JSON.stringify(opts ?? {}) }),
  getFileLink: (id: string) => req<FileLinkInfo>(`/api/files/${id}/link`),
  revokeFileLink: (id: string) => req<{ status: string }>(`/api/files/${id}/link`, { method: "DELETE" }),
  shareRecipients: (id: string, emails: string[]) => req<{ shared: number }>(`/api/files/${id}/share-recipients`, { method: "POST", body: JSON.stringify({ emails }) }),
  // Equipas do utilizador (para partilhar um ficheiro com o grupo inteiro).
  myGroups: () => req<{ id: string; name: string }[]>("/api/my-groups"),
  shareWithGroup: (id: string, groupId: string, nivel: string) => req<{ shared: number }>(`/api/files/${id}/share-group`, { method: "POST", body: JSON.stringify({ group_id: groupId, nivel }) }),
  sharedWithMe: () => req<{ files: StorageFile[] }>("/api/files-shared"),
  fileLimits: () => req<{ max_upload_bytes: number }>("/api/files/limits"),
  editSession: (id: string, lang: string, theme: string) => req<EditSession>(`/api/files/${id}/edit-session?lang=${encodeURIComponent(lang)}&theme=${encodeURIComponent(theme)}`, { method: "POST" }),
  // Config do plugin de IA do OnlyOffice (proxy do DeepSeek): URL base + "API key" (token do
  // utilizador) + modelo, para colar uma vez em Plugins → AI. A chave do DeepSeek fica no servidor.
  aiEditorKey: () => req<{ base_url: string; api_key: string; model: string }>("/api/ai/editor-key"),
  createOffice: (kind: "document" | "spreadsheet" | "presentation", name: string, folderId?: string) =>
    req<StorageFile>("/api/files/office", { method: "POST", body: JSON.stringify({ kind, name, folder_id: folderId ?? "" }) }),
  meetJoin: (room?: string, access?: string) => req<MeetSession>("/api/meet/join", { method: "POST", body: JSON.stringify({ room: room ?? "", access: access ?? "" }) }),
  meetLobby: (meetingId: string) => req<{ pending: LobbyEntry[] }>(`/api/meet/meetings/${meetingId}/lobby`),
  meetAdmit: (id: string) => req<{ status: string }>(`/api/meet/lobby/${id}/admit`, { method: "POST" }),
  meetDeny: (id: string) => req<{ status: string }>(`/api/meet/lobby/${id}/deny`, { method: "POST" }),
  publicMeetInfo: (room: string) => req<{ title: string; access: string; status: string; org?: string; brand?: { color?: string; logo?: string; logo_white_dark?: boolean } }>(`/api/public/meet/${room}`),
  publicMeetJoin: (room: string, name: string) => req<GuestJoin>(`/api/public/meet/${room}/join`, { method: "POST", body: JSON.stringify({ name }) }),
  publicLobbyPoll: (id: string) => req<GuestJoin & { status: string }>(`/api/public/meet/lobby/${id}`),
  meetList: () => req<{ meetings: Meeting[] }>("/api/meet/meetings"),
  meetListPage: (bucket: "upcoming" | "past", page: number, per = 6) =>
    req<{ meetings: Meeting[]; total: number }>(`/api/meet/meetings?bucket=${bucket}&page=${page}&per=${per}`),
  meetCreate: (body: CreateMeetingBody) => req<Meeting>("/api/meet/meetings", { method: "POST", body: JSON.stringify(body) }),
  meetRSVP: (id: string, status: RsvpStatus) => req<{ rsvp: string }>(`/api/meet/meetings/${id}/rsvp`, { method: "POST", body: JSON.stringify({ status }) }),
  meetCancel: (id: string) => req<{ status: string }>(`/api/meet/meetings/${id}/cancel`, { method: "POST" }),
  meetUpdate: (id: string, body: { title: string; scheduled_start: string; scheduled_end: string }) => req<Meeting>(`/api/meet/meetings/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
  // Controles/acesso da sala — o backend só aceita do dono da reunião.
  meetControls: (id: string, c: MeetControls) => req<MeetControls>(`/api/meet/meetings/${id}/controls`, { method: "PATCH", body: JSON.stringify(c) }),
  meetParticipants: (id: string) => req<MeetParticipant[]>(`/api/meet/meetings/${id}/participants`),
  meetKick: (id: string, identity: string) => req<{ status: string }>(`/api/meet/meetings/${id}/kick`, { method: "POST", body: JSON.stringify({ identity }) }),
  meetMute: (id: string, identity: string) => req<{ status: string }>(`/api/meet/meetings/${id}/mute`, { method: "POST", body: JSON.stringify({ identity }) }),
  meetCohosts: (id: string) => req<{ cohosts: string[] }>(`/api/meet/meetings/${id}/cohosts`),
  meetAddCohost: (id: string, userId: string) => req<{ status: string }>(`/api/meet/meetings/${id}/cohosts`, { method: "POST", body: JSON.stringify({ user_id: userId }) }),
  meetRemoveCohost: (id: string, userId: string) => req<{ status: string }>(`/api/meet/meetings/${id}/cohosts/${userId}`, { method: "DELETE" }),
  meetBreakouts: (id: string) => req<{ breakouts: BreakoutRoom[] }>(`/api/meet/meetings/${id}/breakouts`),
  meetCreateBreakouts: (id: string, count: number, autoAssign: boolean) => req<{ breakouts: BreakoutRoom[] }>(`/api/meet/meetings/${id}/breakouts`, { method: "POST", body: JSON.stringify({ count, auto_assign: autoAssign }) }),
  meetAssignBreakout: (id: string, bid: string, userId: string) => req<{ status: string }>(`/api/meet/meetings/${id}/breakouts/${bid}/assign`, { method: "POST", body: JSON.stringify({ user_id: userId }) }),
  meetCloseBreakouts: (id: string) => req<{ status: string }>(`/api/meet/meetings/${id}/breakouts`, { method: "DELETE" }),
  meetBreakoutToken: (id: string, bid: string) => req<BreakoutSession>(`/api/meet/meetings/${id}/breakouts/${bid}/token`),
  meetSendNotes: (id: string) => req<{ sent: number; failed: number }>(`/api/meet/meetings/${id}/notes/email`, { method: "POST" }),
  meetAccess: (id: string, access: string) => req<{ access: string }>(`/api/meet/meetings/${id}/access`, { method: "PATCH", body: JSON.stringify({ access }) }),
  meetNotes: (id: string) => req<{ body: string; title: string; updated_at?: string }>(`/api/meet/meetings/${id}/notes`),
  meetSaveNotes: (id: string, body: string) => req<{ body: string; updated_at: string }>(`/api/meet/meetings/${id}/notes`, { method: "PUT", body: JSON.stringify({ body }) }),
  meetRecordStart: (room: string) => req<{ recording_id: string; egress_id: string }>("/api/meet/record/start", { method: "POST", body: JSON.stringify({ room }) }),
  meetRecordStop: (room: string) => req<{ status: string }>("/api/meet/record/stop", { method: "POST", body: JSON.stringify({ room }) }),
  meetRecordings: (page = 1, per = 6) => req<{ recordings: Recording[]; total: number }>(`/api/meet/recordings?page=${page}&per=${per}`),
  recordingPlayUrl: (id: string) => mediaUrl(`/api/meet/recordings/${id}/play`),
  meetRecordingDelete: (id: string) => req<{ status: string }>(`/api/meet/recordings/${id}`, { method: "DELETE" }),
  // Apresentação do Meet (estilo BBB). O PDF é servido por URL pública com token (membros e convidados).
  presentationGet: (room: string) => req<Presentation & { active: boolean }>(`/api/meet/presentation?room=${encodeURIComponent(room)}`),
  presentationStartDrive: (room: string, fileId: string) => req<Presentation>("/api/meet/presentation/drive", { method: "POST", body: JSON.stringify({ room, file_id: fileId }) }),
  presentationStartUpload: async (room: string, file: File): Promise<Presentation> => {
    const fd = new FormData();
    fd.append("room", room);
    fd.append("file", file);
    const res = await fetch(apiUrl("/api/meet/presentation/upload"), { method: "POST", credentials: "include", headers: authHeaders(), body: fd });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  presentationSetSlide: (id: string, slide: number) => req<{ status: string }>(`/api/meet/presentation/${id}/slide`, { method: "POST", body: JSON.stringify({ slide }) }),
  // keepalive: o encerrar acontece muitas vezes ao FECHAR a janela/aba — sem isto o browser aborta
  // o pedido a meio e a apresentação ficava ativa no servidor.
  presentationStop: (id: string) => req<{ status: string }>(`/api/meet/presentation/${id}/stop`, { method: "POST", keepalive: true }),
  presentationPdfUrl: (id: string, token: string) => apiUrl(`/api/public/meet/presentation/${id}/pdf?token=${encodeURIComponent(token)}`),
  setMyStatus: (emoji: string, text: string) => req<{ emoji: string; text: string }>("/api/me/status", { method: "PUT", body: JSON.stringify({ emoji, text }) }),
  userStatus: (email: string) => req<{ emoji: string; text: string }>(`/api/user-status?email=${encodeURIComponent(email)}`),
  me: () => req<{ id?: string; presence?: string; email?: string; name?: string; role?: string; mfa_setup_required?: boolean; mfa_required_method?: string; avatar?: string }>("/api/me"),
  myProfile: () => req<{ email: string; name: string; job_title: string; phone: string; avatar: string }>("/api/me/profile"),
  updateMyProfile: (p: { name: string; job_title: string; phone: string; avatar: string }) =>
    req<{ status: string }>("/api/me/profile", { method: "PUT", body: JSON.stringify(p) }),
  changeMyPassword: (current: string, newPw: string) =>
    req<{ status: string }>("/api/me/password", { method: "POST", body: JSON.stringify({ current, "new": newPw }) }),
  setPresence: (presence: "active" | "away" | "dnd") => req<{ presence: string }>("/api/me/presence", { method: "PUT", body: JSON.stringify({ presence }) }),
  // Snooze: regista a hora de despertar; o worker devolve a mensagem à inbox (o cliente já a moveu p/ Snoozed).
  snoozeEmail: (emailId: string, wakeAt: string) => req<{ status: string }>("/api/mail/snooze", { method: "POST", body: JSON.stringify({ email_id: emailId, wake_at: wakeAt }) }),
  // Outbox server-side: agenda o envio real (undo-send / agendado). O worker submete via JMAP.
  outboxCreate: (b: { email_id: string; identity_id: string; from: string; rcpts: string[]; subject: string; send_at: string }) =>
    req<{ id: string }>("/api/mail/outbox", { method: "POST", body: JSON.stringify(b) }),
  outboxList: () => req<OutboxItem[]>("/api/mail/outbox"),
  outboxCancel: (id: string) => req<{ status: string }>(`/api/mail/outbox/${id}`, { method: "DELETE" }),
  // Templates de resposta (canned responses) do dono.
  templatesList: () => req<MailTemplate[]>("/api/mail/templates"),
  templateCreate: (b: { name: string; subject: string; body_html: string }) => req<MailTemplate>("/api/mail/templates", { method: "POST", body: JSON.stringify(b) }),
  templateUpdate: (id: string, b: { name: string; subject: string; body_html: string }) => req<MailTemplate>(`/api/mail/templates/${id}`, { method: "PATCH", body: JSON.stringify(b) }),
  templateDelete: (id: string) => req<{ status: string }>(`/api/mail/templates/${id}`, { method: "DELETE" }),
  brandingGet: () => req<Branding>("/api/branding"),
  brandingUpdate: (b: { color: string; logo: string; logo_white_dark?: boolean }) => req<{ status: string }>("/api/branding", { method: "PATCH", body: JSON.stringify(b) }),
  brandingResolve: (email: string) => req<Branding>(`/api/public/branding?email=${encodeURIComponent(email)}`),
  notifications: () => req<{ items: Notif[]; unread: number }>("/api/notifications"),
  notificationRead: (id: string) => req<{ status: string }>(`/api/notifications/${id}/read`, { method: "POST" }),
  notificationsReadAll: () => req<{ status: string }>("/api/notifications/read-all", { method: "POST" }),
  notificationsReadKind: (kind: string) => req<{ status: string }>("/api/notifications/read-kind", { method: "POST", body: JSON.stringify({ kind }) }),

  pushVapid: () => req<{ enabled: boolean; publicKey?: string }>("/api/push/vapid"),
  pushSubscribe: (sub: unknown) => req<{ status: string }>("/api/push/subscribe", { method: "POST", body: JSON.stringify(sub) }),
  pushUnsubscribe: (endpoint: string) => req<{ status: string }>("/api/push/unsubscribe", { method: "POST", body: JSON.stringify({ endpoint }) }),

  // Chat (estilo Slack)
  chatChannels: () => req<{ channels: ChatChannel[]; discover: ChatChannel[] }>("/api/chat/channels"),
  chatSchedule: (channelId: string, body: string, sendAt: string) => req<{ id: string }>(`/api/chat/channels/${channelId}/schedule`, { method: "POST", body: JSON.stringify({ body, send_at: sendAt }) }),
  chatScheduled: () => req<{ id: string; channel_id: string; channel_name: string; body: string; send_at: string }[]>("/api/chat/scheduled"),
  chatCancelScheduled: (id: string) => req<{ status: string }>(`/api/chat/scheduled/${id}`, { method: "DELETE" }),
  chatRemind: (messageId: string, remindAt: string, note?: string) => req<{ id: string }>(`/api/chat/messages/${messageId}/remind`, { method: "POST", body: JSON.stringify({ remind_at: remindAt, note: note ?? "" }) }),
  chatEmojis: () => req<{ id: string; name: string; file_id: string }[]>("/api/chat/emojis"),
  chatCreateEmoji: (name: string, fileId: string) => req<{ id: string }>("/api/chat/emojis", { method: "POST", body: JSON.stringify({ name, file_id: fileId }) }),
  chatDeleteEmoji: (id: string) => req<{ status: string }>(`/api/chat/emojis/${id}`, { method: "DELETE" }),
  chatCreateChannel: (opts: { name: string; topic: string; kind: "public" | "private"; listed: boolean; add_policy: "open" | "restricted" }) =>
    req<ChatChannel>("/api/chat/channels", { method: "POST", body: JSON.stringify(opts) }),
  chatUpdateChannel: (id: string, opts: { name?: string; topic?: string; listed?: boolean; add_policy?: "open" | "restricted" }) =>
    req<{ status: string }>(`/api/chat/channels/${id}`, { method: "PATCH", body: JSON.stringify(opts) }),
  chatAddMember: (id: string, email: string) => req<{ status: string }>(`/api/chat/channels/${id}/members`, { method: "POST", body: JSON.stringify({ email }) }),
  chatRemoveMember: (id: string, userId: string) => req<{ status: string }>(`/api/chat/channels/${id}/members/${userId}`, { method: "DELETE" }),
  chatSetMemberRole: (id: string, userId: string, role: "manager" | "member") => req<{ status: string }>(`/api/chat/channels/${id}/members/${userId}`, { method: "PATCH", body: JSON.stringify({ role }) }),
  chatJoin: (id: string) => req<{ status: string }>(`/api/chat/channels/${id}/join`, { method: "POST" }),
  chatOpenDM: (email: string) => req<ChatChannel>("/api/chat/dm", { method: "POST", body: JSON.stringify({ email }) }),
  chatOpenGroupDM: (emails: string[]) => req<ChatChannel>("/api/chat/group-dm", { method: "POST", body: JSON.stringify({ emails }) }),
  chatMembers: (id: string) => req<ChatMember[]>(`/api/chat/channels/${id}/members`),
  chatWebhooks: (id: string) => req<ChatWebhook[]>(`/api/chat/channels/${id}/webhooks`),
  chatCreateWebhook: (id: string, name: string) => req<ChatWebhook>(`/api/chat/channels/${id}/webhooks`, { method: "POST", body: JSON.stringify({ name }) }),
  chatDeleteWebhook: (id: string) => req<{ status: string }>(`/api/chat/webhooks/${id}`, { method: "DELETE" }),
  chatMessages: (id: string, before?: string) =>
    req<{ messages: ChatMessage[]; has_more: boolean }>(`/api/chat/channels/${id}/messages${before ? `?before=${encodeURIComponent(before)}` : ""}`),
  chatMessagesAround: (id: string, msgId: string) =>
    req<{ messages: ChatMessage[]; has_more: boolean }>(`/api/chat/channels/${id}/messages?around=${encodeURIComponent(msgId)}`),
  chatReplies: (msgId: string) => req<{ replies: ChatMessage[] }>(`/api/chat/messages/${msgId}/replies`),
  chatSend: (id: string, body: string, parent_id = "", mentions: string[] = [], attachments: ChatAttachment[] = []) =>
    req<ChatMessage>(`/api/chat/channels/${id}/messages`, { method: "POST", body: JSON.stringify({ body, parent_id, mentions, attachments }) }),
  chatAttachmentUrl: (fileId: string, inline = false) => mediaUrl(`/api/chat/files/${fileId}/download${inline ? "?inline=1" : ""}`),
  chatPresence: () => req<{ online: string[]; presence?: Record<string, string> }>("/api/chat/presence"),
  chatSearch: (q: string) => req<{ results: ChatSearchHit[] }>(`/api/chat/search?q=${encodeURIComponent(q)}`),
  chatEdit: (msgId: string, body: string) => req<{ id: string; body: string; edited_at: string }>(`/api/chat/messages/${msgId}`, { method: "PATCH", body: JSON.stringify({ body }) }),
  chatDelete: (msgId: string) => req<{ status: string }>(`/api/chat/messages/${msgId}`, { method: "DELETE" }),
  // Arquivar/restaurar um canal (dono do canal ou admin da organização). Arquivado sai das listas e
  // fica só de leitura; o histórico fica intacto — ao contrário do apagar.
  chatArchiveChannel: (id: string, archived: boolean) =>
    req<{ id: string; archived: boolean }>(`/api/chat/channels/${id}/archive`, { method: "POST", body: JSON.stringify({ archived }) }),
  chatArchivedChannels: () => req<{ channels: ArchivedChannel[] }>("/api/chat/channels/archived"),
  chatDeleteChannel: (id: string) => req<{ status: string }>(`/api/chat/channels/${id}`, { method: "DELETE" }),
  chatMarkUnread: (msgId: string) => req<{ status: string }>(`/api/chat/messages/${msgId}/unread`, { method: "POST" }),
  chatForward: (msgId: string, channelId: string) => req<ChatMessage>(`/api/chat/messages/${msgId}/forward`, { method: "POST", body: JSON.stringify({ channel_id: channelId }) }),
  chatSave: (msgId: string) => req<{ status: string }>(`/api/chat/messages/${msgId}/save`, { method: "POST" }),
  chatUnsave: (msgId: string) => req<{ status: string }>(`/api/chat/messages/${msgId}/save`, { method: "DELETE" }),
  chatSaved: () => req<{ saved: ChatSavedItem[] }>("/api/chat/saved"),
  chatUnfurl: (url: string) => req<{ unfurl: UnfurlData | null }>(`/api/chat/unfurl?url=${encodeURIComponent(url)}`),
  chatReact: (msgId: string, emoji: string, remove = false) => req<{ status: string }>(`/api/chat/messages/${msgId}/react`, { method: "POST", body: JSON.stringify({ emoji, remove }) }),
  chatMarkRead: (id: string) => req<{ status: string }>(`/api/chat/channels/${id}/read`, { method: "POST" }),
};

export interface Branding { name: string; color: string; logo: string; logo_white_dark?: boolean }
export interface Announcement { id: string; title: string; body: string; author: string; published: boolean; created_at: string }
export interface Notif { id: string; kind: string; title: string; body: string; link: string; read_at: string | null; created_at: string; args?: Record<string, string> }

export interface ChatChannel { id: string; kind: "public" | "private" | "dm"; name: string; topic: string; is_general?: boolean; unread?: number; last_read_at?: string; other_id?: string; listed?: boolean; add_policy?: "open" | "restricted"; my_role?: string; notif_pref?: "all" | "mentions" | "none" }
// Tarefa devolvida pela busca (/task no chat). card_key vem do servidor como "PREFIXO-Nº".
export interface CardHit { id: string; board_id: string; title: string; label: string; due: string; priority: string; card_key: string; board_name: string; column_name: string; assignee_name: string }
export interface ArchivedChannel { id: string; kind: string; name: string; topic: string; is_general?: boolean; archived_at: string }
export interface ChatSearchHit { id: string; channel_id: string; channel_name: string; channel_kind: string; author: string; body: string; created_at: string }
export interface ChatReaction { emoji: string; count: number; mine: boolean; users: string[] }
export interface ChatAttachment { file_id: string; name: string; size: number }
export interface ChatMessage { id: string; channel_id: string; user_id: string; parent_id: string; body: string; author: string; author_email: string; deleted: boolean; edited_at: string; created_at: string; reply_count: number; reactions: ChatReaction[]; attachments: ChatAttachment[]; saved?: boolean; bot_name?: string }
export interface ChatWebhook { id: string; channel_id: string; token: string; name: string; created_at: string }
export interface OutboxItem { id: string; email_id: string; from_email: string; rcpts: string[]; subject: string; send_at: string; status: string; created_at: string }
export interface MailTemplate { id: string; name: string; subject: string; body_html: string; created_at: string; updated_at: string }
export interface ChatSavedItem { id: string; channel_id: string; channel_name: string; channel_kind: string; author: string; body: string; created_at: string; saved_at: string }
export interface UnfurlData { url: string; title: string; description: string; image: string; site: string }
export interface ChatMember { user_id: string; email: string; name: string; role: string; status_emoji?: string; status_text?: string; last_read_at?: string }
export interface ChatPin { id: string; body: string; author: string; created_at: string; pinned_at: string }
export interface ChatBookmark { id: string; kind: "link" | "file"; title: string; url: string; file_name: string; file_size: number; created_at: string }
export interface Recording {
  id: string; room_name: string; status: "recording" | "ready" | "failed"; started_by: string;
  started_at: string; ended_at: string | null; duration_seconds: number | null; size_bytes: number | null;
}
// Layout de composição da gravação (RoomComposite do LiveKit). Espelha a lista aceite no backend.
export type RecordLayout = "grid" | "speaker" | "single-speaker";

export interface MeetControls { allow_mic: boolean; allow_cam: boolean; allow_share: boolean; allow_chat: boolean; lock_until_host: boolean; locked: boolean }
export interface MeetParticipant { identity: string; name: string; is_host: boolean; is_cohost?: boolean; can_manage?: boolean }
export interface BreakoutRoom { id: string; idx: number; name: string; room_name: string; members: string[] }
export interface BreakoutSession { url: string; token: string; room: string; name: string; breakout_id: string }
// locked=true: "organizador entra primeiro" e ele ainda não chegou — não vem token nenhum.
export interface MeetSession { url?: string; token?: string; room: string; identity?: string; name?: string; meeting_id?: string; title?: string; is_host?: boolean; is_owner?: boolean; access?: string; controls?: MeetControls; locked?: boolean; full?: boolean; limit?: number }
export interface Presentation { id: string; name: string; current_slide: number; token: string; by: string }
export interface LobbyEntry { id: string; name: string; at: string }
export interface GuestJoin { mode: "joined" | "lobby"; url?: string; token?: string; room?: string; identity?: string; name?: string; lobby_id?: string }
export type RsvpStatus = "accepted" | "declined" | "tentative" | "pending";
export interface MeetParticipant { email: string; role: string; rsvp: RsvpStatus; notified: boolean; joined: boolean }
export interface Meeting {
  id: string; title: string; room: string; status: "scheduled" | "live" | "ended" | "canceled";
  scheduled_start: string | null; scheduled_end: string | null;
  actual_start: string | null; actual_end: string | null; duration_seconds: number | null;
  my_rsvp: RsvpStatus | ""; is_owner: boolean; participants: MeetParticipant[];
}
export interface CreateMeetingBody { title: string; scheduled_start?: string; scheduled_end?: string; participants: string[]; add_to_calendar: boolean; access?: string }

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export interface EditSession { apiJs: string; config: Record<string, any>; canWrite: boolean; name: string }

// Extensões de office abertas no editor (OnlyOffice). Mantém em sincronia com officeExts no backend.
// txt fica no visualizador de texto (leve), não no editor pesado.
const OFFICE_EXTS = new Set(["docx", "doc", "odt", "rtf", "xlsx", "xls", "ods", "csv", "pptx", "ppt", "odp"]);
export function isOfficeFile(name: string): boolean {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  return OFFICE_EXTS.has(ext);
}

// Tipo visualizável no navegador (visualizador embutido) — senão faz download.
export type ViewerKind = "image" | "pdf" | "audio" | "video" | "text";
const VIEWER: Record<string, ViewerKind> = {
  png: "image", jpg: "image", jpeg: "image", gif: "image", webp: "image", svg: "image", bmp: "image", heic: "image", avif: "image",
  pdf: "pdf",
  mp3: "audio", wav: "audio", flac: "audio", ogg: "audio", m4a: "audio", aac: "audio",
  mp4: "video", mov: "video", webm: "video", mkv: "video", avi: "video", m4v: "video",
  // texto/código
  txt: "text", json: "text", xml: "text", md: "text", markdown: "text", log: "text",
  yml: "text", yaml: "text", ini: "text", toml: "text", env: "text",
  js: "text", ts: "text", tsx: "text", jsx: "text", go: "text", py: "text", rb: "text",
  java: "text", c: "text", cpp: "text", h: "text", cs: "text", php: "text", rs: "text",
  css: "text", scss: "text", html: "text", htm: "text", sh: "text", sql: "text",
};
export function viewerKind(name: string): ViewerKind | null {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  return VIEWER[ext] ?? null;
}
// Lê o conteúdo textual de um arquivo (para o visualizador de texto).
export async function fetchText(id: string): Promise<string> {
  const res = await fetch(inlineUrl(id), { credentials: "include", headers: authHeaders() });
  if (!res.ok) throw new Error(translateError(undefined, undefined, res.status));
  return res.text();
}
export function inlineUrl(id: string): string { return mediaUrl(`/api/files/${id}/download?inline=1`); }

export interface Folder { id: string; name: string; parent_id: { Valid: boolean; Bytes?: string } | null; created_at: string }
export interface Breadcrumb { id: string; name: string }
export interface Quota { used: number; max: number }
export interface Owner { name: string; email: string }
export interface FilesListing {
  folders: Folder[]; files: StorageFile[]; breadcrumb: Breadcrumb[];
  shared: string[]; owner: Owner; quota: Quota;
}
export interface FilesSearch { folders: Folder[]; files: StorageFile[]; shared: string[]; owner: Owner }
export interface FileVersion { id: string; versao: number; tamanho: number; created_at: string; author: string }
export interface Share { id: string; principal: string; nivel: string }

// Estado do link público de um ficheiro. token vazio = sem link.
export interface FileLinkInfo {
  token: string;
  has_password?: boolean;
  expires_at?: string;
  permission?: "view" | "edit";
  allow_download?: boolean;
}

export interface StorageFile {
  id: string; nome: string; hash: string; tamanho: number; versao: number; created_at: string;
  deleted_at?: string | null; // preenchido = na lixeira
  starred?: boolean;
}

export type ContactKind = "company" | "person";
export interface Contact {
  id: string; kind: ContactKind; name: string; email: string; phone: string; company: string;
  domain: string; notes: string; tags: string[]; group_ids: string[]; created_at: string;
  title: string; mobile: string; is_whatsapp: boolean; allow_phone: boolean; allow_email: boolean;
  company_id: string | null; company_name?: string | null; owner_user_id: string;
  extra_emails: string[]; extra_phones: string[]; // e-mails/telefones secundários
  custom_fields?: Record<string, string>; // campos livres (rótulo → valor)
  avatar_file_id?: string | null;          // foto: aponta para um storage_files (ver /thumb)
}

export interface CrmNote { id: string; body: string; created_at: string; author_name: string; author_email: string }

export interface CrmTask { id: string; title: string; description: string; assignee: string; due: string; column_id: string; board_id: string; board_name: string; column_name: string }
export interface NamedRef { id: string; name: string }
export interface ContactGroup { id: string; name: string; color: string }
// O que o formulário envia: `id`, `created_at`, `owner_user_id` e `company_name` são do servidor.
export type ContactInput = Omit<Contact, "id" | "created_at" | "owner_user_id" | "company_name">;

// Contatos: empresa ou pessoa, numa lista só. Ver é regido pelo GRUPO (partilhado com equipas);
// editar é só do dono. Ambas as regras vivem no backend — isto aqui é só transporte.
export const contactsApi = {
  list: () => req<Contact[]>("/api/contacts"),
  get: (id: string) => req<{ contact: Contact; group_ids: string[]; people: Contact[]; is_owner: boolean }>(`/api/contacts/${id}`),
  create: (c: ContactInput) => req<Contact>("/api/contacts", { method: "POST", body: JSON.stringify(c) }),
  // Grava destinatários de um envio como contactos, para os sugerir da próxima vez. Idempotente.
  seen: (recipients: string[]) => req<{ status: string }>("/api/contacts/seen", { method: "POST", body: JSON.stringify({ recipients }) }),
  update: (id: string, c: ContactInput) => req<Contact>(`/api/contacts/${id}`, { method: "PATCH", body: JSON.stringify(c) }),
  remove: (id: string) => req<{ status: string }>(`/api/contacts/${id}`, { method: "DELETE" }),
  editNote: (contactId: string, noteId: string, body: string) => req<CrmNote>(`/api/contacts/${contactId}/notes/${noteId}`, { method: "PATCH", body: JSON.stringify({ body }) }),
  transfer: (id: string, userId: string) => req<Contact>(`/api/contacts/${id}/transfer`, { method: "POST", body: JSON.stringify({ user_id: userId }) }),
  merge: (keepId: string, mergeId: string) => req<Contact>(`/api/contacts/${keepId}/merge`, { method: "POST", body: JSON.stringify({ merge_id: mergeId }) }),
  enrich: () => req<{ linked: number }>("/api/contacts/enrich", { method: "POST" }),
  setAvatar: (id: string, fileId: string) => req<Contact>(`/api/contacts/${id}/avatar`, { method: "POST", body: JSON.stringify({ file_id: fileId }) }),
  notes: (id: string) => req<CrmNote[]>(`/api/contacts/${id}/notes`),
  addNote: (id: string, body: string) => req<CrmNote>(`/api/contacts/${id}/notes`, { method: "POST", body: JSON.stringify({ body }) }),
  tasks: (id: string) => req<CrmTask[]>(`/api/contacts/${id}/tasks`),
  groups: () => req<ContactGroup[]>("/api/contact-groups"),
  // E-mails de um grupo (lista de distribuição): escrever ao grupo inteiro de uma vez.
  groupEmails: (id: string) => req<{ name: string; email: string }[]>(`/api/contact-groups/${id}/emails`),
  createGroup: (name: string, color: string) => req<ContactGroup>("/api/contact-groups", { method: "POST", body: JSON.stringify({ name, color }) }),
  updateGroup: (id: string, name: string, color: string) => req<ContactGroup>(`/api/contact-groups/${id}`, { method: "PATCH", body: JSON.stringify({ name, color }) }),
  deleteGroup: (id: string) => req<{ status: string }>(`/api/contact-groups/${id}`, { method: "DELETE" }),
  // Com quem o grupo está partilhado: equipas E pessoas.
  shares: (id: string) => req<{ teams: NamedRef[]; users: NamedRef[] }>(`/api/contact-groups/${id}/shares`),
  setShares: (id: string, userGroupIds: string[], userIds: string[]) =>
    req<{ teams: NamedRef[]; users: NamedRef[] }>(`/api/contact-groups/${id}/shares`, { method: "PUT", body: JSON.stringify({ user_group_ids: userGroupIds, user_ids: userIds }) }),
  userGroups: () => req<NamedRef[]>("/api/groups"),
  // Equipas do tenant para o picker de partilha (não exige permissão de admin).
  shareTeams: () => req<NamedRef[]>("/api/contact-share-teams"),
  // Exportar/importar agenda em vCard/CSV (formatos universais).
  exportUrl: (format: "vcard" | "csv") => mediaUrl(`/api/contacts/export?format=${format}`),
  // Um contacto como ficheiro .vcf, para anexar a um e-mail ("enviar contacto"). Devolve o File já
  // nomeado a partir do Content-Disposition do servidor.
  vcardFile: async (id: string): Promise<File> => {
    const res = await fetch(apiUrl(`/api/contacts/${id}/vcard`), { credentials: "include", headers: authHeaders() });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    const cd = res.headers.get("Content-Disposition") || "";
    const m = /filename="?([^"]+)"?/.exec(cd);
    const name = m?.[1] || "contact.vcf";
    return new File([await res.blob()], name, { type: "text/vcard" });
  },
  importContacts: async (file: File): Promise<{ imported: number; skipped: number }> => {
    const fd = new FormData();
    fd.append("file", file);
    const res = await fetch(apiUrl("/api/contacts/import"), { method: "POST", credentials: "include", headers: authHeaders(), body: fd });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
};



export interface Colleague { id: string; email: string; name: string; avatar?: string }

// Colegas do tenant — para escolher quem faz uma tarefa.
export interface Gif { id: string; file_id: string; name: string; size: number; created_at: string }

// GIFs: a "biblioteca" é só uma marca sobre ficheiros do Drive — nada sai para serviços de fora.
export const gifsApi = {
  list: () => req<Gif[]>("/api/gifs"),
  add: (file_id: string, name: string) => req<Gif>("/api/gifs", { method: "POST", body: JSON.stringify({ file_id, name }) }),
  remove: (id: string) => req<{ status: string }>(`/api/gifs/${id}`, { method: "DELETE" }),
};

// Preferências do utilizador, por app. Cada secção grava-se isolada (merge no servidor).
export interface EmailSettings { signature?: string; signatureOn?: boolean; aiSummary?: boolean }
// Preferências gerais que seguem o utilizador entre aparelhos (web, desktop, mobile). O localStorage
// continua a ser o cache instantâneo pré-login; ao carregar, o servidor ganha.
export interface GeneralSettings { theme?: "light" | "dark"; language?: string; startView?: string }
export interface ChatSettings { enterSends?: boolean; sounds?: boolean }
export interface CalendarSettings {
  weekStart?: "monday" | "sunday";
  workDays?: number[]; // dias úteis (0=domingo … 6=sábado); ausente = todos
  defaultView?: "month" | "week" | "day" | "list";
  defaultDuration?: number; // minutos
  defaultReminder?: number; // minutos antes; 0 = sem lembrete
}
export interface FilesSettings { view?: "grid" | "list"; linkExpiryDays?: number }
export interface MeetSettings { enterMuted?: boolean; camOff?: boolean }
export interface KanbanSettings { defaultBoard?: string; defaultView?: "kanban" | "list" | "calendar"; boardOrder?: string[] }
export interface UserSettings {
  email?: EmailSettings;
  general?: GeneralSettings;
  chat?: ChatSettings;
  calendar?: CalendarSettings;
  files?: FilesSettings;
  meet?: MeetSettings;
  kanban?: KanbanSettings;
}

export const settingsApi = {
  get: () => req<UserSettings>("/api/settings"),
  // patch é {app: {...}} — grava só a secção enviada, sem tocar nas outras.
  patch: (patch: Partial<UserSettings>) => req<{ status: string }>("/api/settings", { method: "PATCH", body: JSON.stringify(patch) }),
};

// Dispositivos (tokens para o daemon de sync). O token cru só volta na criação — nunca mais.
export interface DeviceToken {
  id: string; name: string; token_prefix: string; scope: string;
  last_used_at: string | null; expires_at: string | null; revoked_at: string | null; created_at: string;
}
export const devicesApi = {
  list: () => req<{ devices: DeviceToken[] }>("/api/devices"),
  create: (name: string, expiresInDays = 0) =>
    req<{ id: string; name: string; token: string; prefix: string }>("/api/devices", { method: "POST", body: JSON.stringify({ name, expires_in_days: expiresInDays }) }),
  revoke: (id: string) => req<{ status: string }>(`/api/devices/${id}`, { method: "DELETE" }),
};

export const colleaguesApi = {
  list: () => req<Colleague[]>("/api/colleagues"),
};

export const commentsApi = {
  list: (cardId: string) => req<CardComment[]>(`/api/cards/${cardId}/comments`),
  add: (cardId: string, body: string, parentId?: string) =>
    req<CardComment>(`/api/cards/${cardId}/comments`, { method: "POST", body: JSON.stringify({ body, parent_id: parentId ?? "" }) }),
  remove: (cardId: string, id: string) =>
    req<{ status: string }>(`/api/cards/${cardId}/comments/${id}`, { method: "DELETE" }),
  edit: (cardId: string, id: string, body: string) =>
    req<CardComment>(`/api/cards/${cardId}/comments/${id}`, { method: "PATCH", body: JSON.stringify({ body }) }),
};

export interface CardActivity { id: string; kind: string; detail: string; created_at: string; actor_name: string }
export const cardActivityApi = {
  list: (cardId: string) => req<CardActivity[]>(`/api/cards/${cardId}/activity`),
};

export interface CardAttachment { id: string; file_id: string; nome: string; tamanho: number; created_at: string }
export const cardAttachmentsApi = {
  list: (cardId: string) => req<CardAttachment[]>(`/api/cards/${cardId}/attachments`),
  attach: (cardId: string, fileId: string) => req<CardAttachment[]>(`/api/cards/${cardId}/attachments`, { method: "POST", body: JSON.stringify({ file_id: fileId }) }),
  detach: (cardId: string, attId: string) => req<{ status: string }>(`/api/cards/${cardId}/attachments/${attId}`, { method: "DELETE" }),
  downloadUrl: (cardId: string, attId: string) => mediaUrl(`/api/cards/${cardId}/attachments/${attId}/download`),
};

// ---- Wiki / base de conhecimento ----
export interface WikiSpace {
  id: string; tenant_id: string; key: string; name: string; description: string; icon: string;
  owner_user_id: string; visibility: "restricted" | "org" | "public"; public_slug: string | null;
  ai_indexed: boolean; created_at: string; updated_at: string;
}
export interface WikiPage {
  id: string; tenant_id: string; space_id: string; parent_id: string | null; title: string; slug: string;
  body_json: unknown; body_text: string; body_html: string; position: number;
  status: "draft" | "published"; author_user_id: string; version: number; is_template?: boolean;
  created_at: string; updated_at: string;
}
export interface WikiTreeNode {
  id: string; parent_id: string | null; title: string; slug: string;
  position: number; status: "draft" | "published"; children: WikiTreeNode[];
}
export interface WikiPageGrant {
  id: string; page_id: string; subject_type: "user" | "group" | "org"; subject_id: string | null; role: string; created_at: string;
}
export interface WikiGrant {
  id: string; space_id: string; subject_type: "user" | "group" | "org"; subject_id: string | null; role: string; created_at: string;
}
export interface WikiVersion {
  id: string; page_id: string; version: number; title: string; body_text: string;
  author_user_id: string | null; created_at: string;
}
export interface WikiVersionFull extends WikiVersion { body_json: unknown }
export interface WikiHome { tenant_id: string; body_json: unknown; body_text: string; body_html: string; updated_by: string | null; updated_at: string }
export interface PublicWikiSpace { space: { name: string; icon: string; description: string }; pages: WikiTreeNode[] }
export interface PublicWikiPage { title: string; slug: string; body_html: string; updated_at: string }
export interface WikiAttachment { id: string; file_id: string; nome: string; tamanho: number; created_at: string }
export interface WikiComment {
  id: string; page_id: string; author_user_id: string; body: string; body_html?: string; created_at: string;
  author_name: string; author_email: string;
}
// role: rank efetivo do utilizador (1 viewer .. 4 manager) devolvido junto do espaço/página.
export interface WikiSpaceInput { key?: string; name: string; description?: string; icon?: string; visibility?: string; ai_indexed?: boolean }
export interface WikiPageInput { parent_id?: string | null; title: string; body_json?: unknown; status?: string; expected_version?: number }

export const wikiApi = {
  listSpaces: () => req<WikiSpace[]>("/api/wiki/spaces"),
  createSpace: (in_: WikiSpaceInput) => req<WikiSpace>("/api/wiki/spaces", { method: "POST", body: JSON.stringify(in_) }),
  getSpace: (id: string) => req<{ space: WikiSpace; role: number }>(`/api/wiki/spaces/${id}`),
  updateSpace: (id: string, in_: WikiSpaceInput) => req<WikiSpace>(`/api/wiki/spaces/${id}`, { method: "PATCH", body: JSON.stringify(in_) }),
  deleteSpace: (id: string) => req<{ status: string }>(`/api/wiki/spaces/${id}`, { method: "DELETE" }),
  tree: (spaceId: string) => req<WikiTreeNode[]>(`/api/wiki/spaces/${spaceId}/tree`),
  listGrants: (spaceId: string) => req<WikiGrant[]>(`/api/wiki/spaces/${spaceId}/grants`),
  setGrant: (spaceId: string, g: { subject_type: string; subject_id?: string | null; role: string }) =>
    req<WikiGrant>(`/api/wiki/spaces/${spaceId}/grants`, { method: "PUT", body: JSON.stringify(g) }),
  removeGrant: (spaceId: string, grantId: string) => req<{ status: string }>(`/api/wiki/spaces/${spaceId}/grants/${grantId}`, { method: "DELETE" }),
  setPublic: (spaceId: string, enabled: boolean, regenerate = false) => req<WikiSpace>(`/api/wiki/spaces/${spaceId}/public`, { method: "POST", body: JSON.stringify({ enabled, regenerate }) }),
  templates: (spaceId: string) => req<{ id: string; title: string }[]>(`/api/wiki/spaces/${spaceId}/templates`),
  setTemplate: (pageId: string, is_template: boolean) => req<WikiPage>(`/api/wiki/pages/${pageId}/template`, { method: "POST", body: JSON.stringify({ is_template }) }),
  home: () => req<WikiHome>("/api/wiki/home"),
  setHome: (body_json: unknown) => req<WikiHome>("/api/wiki/home", { method: "PUT", body: JSON.stringify({ body_json }) }),
  pageGrants: (pageId: string) => req<WikiPageGrant[]>(`/api/wiki/pages/${pageId}/grants`),
  setPageGrant: (pageId: string, g: { subject_type: string; subject_id?: string | null; role: string }) =>
    req<WikiPageGrant>(`/api/wiki/pages/${pageId}/grants`, { method: "PUT", body: JSON.stringify(g) }),
  removePageGrant: (pageId: string, grantId: string) => req<{ status: string }>(`/api/wiki/pages/${pageId}/grants/${grantId}`, { method: "DELETE" }),
  createPage: (spaceId: string, in_: WikiPageInput) => req<WikiPage>(`/api/wiki/spaces/${spaceId}/pages`, { method: "POST", body: JSON.stringify(in_) }),
  getPage: (id: string) => req<{ page: WikiPage; role: number }>(`/api/wiki/pages/${id}`),
  updatePage: (id: string, in_: WikiPageInput) => req<WikiPage>(`/api/wiki/pages/${id}`, { method: "PATCH", body: JSON.stringify(in_) }),
  deletePage: (id: string) => req<{ status: string }>(`/api/wiki/pages/${id}`, { method: "DELETE" }),
  movePage: (id: string, parent_id: string | null, position: number) =>
    req<{ status: string }>(`/api/wiki/pages/${id}/move`, { method: "POST", body: JSON.stringify({ parent_id, position }) }),
  versions: (id: string) => req<WikiVersion[]>(`/api/wiki/pages/${id}/versions`),
  getVersion: (id: string, version: number) => req<WikiVersionFull>(`/api/wiki/pages/${id}/versions/${version}`),
  restoreVersion: (id: string, version: number, expectedVersion: number) =>
    req<WikiPage>(`/api/wiki/pages/${id}/versions/${version}/restore`, { method: "POST", body: JSON.stringify({ expected_version: expectedVersion }) }),
  comments: (id: string) => req<WikiComment[]>(`/api/wiki/pages/${id}/comments`),
  addComment: (id: string, body_json: unknown) => req<{ id: string }>(`/api/wiki/pages/${id}/comments`, { method: "POST", body: JSON.stringify({ body_json }) }),
  deleteComment: (id: string) => req<{ status: string }>(`/api/wiki/comments/${id}`, { method: "DELETE" }),
  attachments: (id: string) => req<WikiAttachment[]>(`/api/wiki/pages/${id}/attachments`),
  attachFile: (id: string, fileId: string) => req<WikiAttachment[]>(`/api/wiki/pages/${id}/attachments`, { method: "POST", body: JSON.stringify({ file_id: fileId }) }),
  detachAttachment: (id: string, attId: string) => req<{ status: string }>(`/api/wiki/pages/${id}/attachments/${attId}`, { method: "DELETE" }),
  attachmentDownloadUrl: (id: string, attId: string) => mediaUrl(`/api/wiki/pages/${id}/attachments/${attId}/download`),
};

export const publicWikiApi = {
  space: (slug: string) => req<PublicWikiSpace>(`/api/public/wiki/${encodeURIComponent(slug)}`),
  page: (slug: string, pageSlug: string) => req<PublicWikiPage>(`/api/public/wiki/${encodeURIComponent(slug)}/${encodeURIComponent(pageSlug)}`),
};

export interface ChecklistItem { id: string; text: string; done: boolean; position: number }
export const checklistApi = {
  list: (cardId: string) => req<ChecklistItem[]>(`/api/cards/${cardId}/checklist`),
  add: (cardId: string, text: string) => req<ChecklistItem>(`/api/cards/${cardId}/checklist`, { method: "POST", body: JSON.stringify({ text }) }),
  toggle: (cardId: string, id: string, done: boolean) => req<ChecklistItem>(`/api/cards/${cardId}/checklist/${id}`, { method: "PATCH", body: JSON.stringify({ done }) }),
  remove: (cardId: string, id: string) => req<{ status: string }>(`/api/cards/${cardId}/checklist/${id}`, { method: "DELETE" }),
};
