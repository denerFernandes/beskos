import { useState, useRef, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { MdEditor } from "./MdEditor";
import { renderRich } from "./richtext";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, contactsApi, colleaguesApi, type Contact } from "./backend";
import { StatCards, ActionBar, useContactActions } from "./CrmBits";
import { useDisabledApps } from "./AdminView";
import { ContactDrawer } from "./ContactDrawer";
import { Spinner } from "./Spinner";
import { toast } from "./toast";
import { initials, hueFor, shortTime, faviconOf } from "./util";
import { useWebmail } from "./auth";
import { IcArrowLeft, IcPencil, IcGlobe, IcPlus, IcMail, IcChevron } from "../panel/icons";
import type { EmailSummary, EmailAddress } from "../jmap/types";

type Tab = "pessoas" | "notas" | "tarefas" | "emails";

// Ficha do contato: dashboard + abas. Empresa mostra as pessoas dela; pessoa não tem essa aba.
// Editar só aparece para o dono — e a API recusa os outros de qualquer forma.
export function ContactPage({ id, onBack, onOpenContact }: {
  id: string; onBack: () => void; onOpenContact: (id: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const [tab, setTab] = useState<Tab | null>(null);
  const [editing, setEditing] = useState(false);
  const [merging, setMerging] = useState(false);
  const [transferring, setTransferring] = useState(false);
  // Kanban ligado? A associação de tarefas (cards) ao contacto só faz sentido com a app ligada —
  // senão a aba e o contador de tarefas nem aparecem.
  const kanbanOn = !useDisabledApps().includes("kanban");
  const q = useQuery({ queryKey: ["contact", id], queryFn: () => contactsApi.get(id), retry: false });
  const notes = useQuery({ queryKey: ["contact-notes", id], queryFn: () => contactsApi.notes(id), retry: false });
  const tasks = useQuery({ queryKey: ["contact-tasks", id], queryFn: () => contactsApi.tasks(id), retry: false, enabled: kanbanOn });
  const actions = useContactActions(q.data?.contact ?? {});

  if (q.isLoading) return <div className="crm-page"><div className="loading">…</div></div>;
  if (q.isError || !q.data) return <div className="crm-page"><div className="empty">{(q.error as Error)?.message}</div></div>;
  const { contact: c, people, is_owner } = q.data;
  const isCompany = c.kind === "company";
  const site = c.domain ? (c.domain.startsWith("http") ? c.domain : `https://${c.domain}`) : null;
  const last = notes.data?.[0];
  // Aba de e-mails só faz sentido quando há endereço para cruzar com a caixa (from:/to:).
  const tabs: Tab[] = [
    ...(isCompany ? (["pessoas"] as Tab[]) : []),
    "notas",
    ...(kanbanOn ? (["tarefas"] as Tab[]) : []),
    ...(c.email ? (["emails"] as Tab[]) : []),
  ];
  // Abre na primeira aba do tipo: empresa começa nas pessoas, pessoa começa nas notas.
  const active: Tab = tab && tabs.includes(tab) ? tab : tabs[0];

  return (
    <div className="crm-page">
      <header className="crm-head">
        <button className="ib" onClick={onBack} title={t("crm.back")}><IcArrowLeft size={16} /></button>
        <ContactAvatar c={c} isOwner={is_owner} isCompany={isCompany} onChanged={() => void q.refetch()} />
        <div className="crm-id">
          <h2>{c.name}</h2>
          <span>
            {isCompany ? c.domain : [c.title, c.company_name ?? c.company].filter(Boolean).join(" · ")}
          </span>
        </div>
        <div className="grow" />
        {is_owner && !isCompany && <button className="chipbtn" onClick={() => setMerging(true)}>{t("crm.merge")}</button>}
        {is_owner && <button className="chipbtn" onClick={() => setTransferring(true)}>{t("crm.transfer")}</button>}
        {is_owner
          ? <button className="chipbtn" onClick={() => setEditing(true)}><IcPencil size={15} /> {t("contacts.edit")}</button>
          : <span className="sla" title={t("contacts.notOwnerHint")}>{t("contacts.notOwner")}</span>}
      </header>

      <ActionBar actions={[
        ...actions,
        ...(site ? [{ icon: <IcGlobe size={15} />, label: t("crm.site"), href: site }] : []),
      ]} />

      <StatCards items={[
        ...(kanbanOn ? [{ label: t("crm.stTasks"), value: (tasks.data ?? []).length }] : []),
        ...(isCompany ? [{ label: t("crm.stPeople"), value: people.length }] : []),
        { label: t("crm.stLastNote"), value: last ? shortTime(last.created_at, i18n.language) : "—", hint: last?.author_name },
      ]} />

      {c.custom_fields && Object.keys(c.custom_fields).length > 0 && (
        <div className="crm-custom">
          {Object.entries(c.custom_fields).map(([k, v]) => (
            <div className="ccf" key={k}><span className="ccf-k">{k}</span><span className="ccf-v">{v}</span></div>
          ))}
        </div>
      )}

      <nav className="crm-tabs">
        {tabs.map((x) => (
          <button key={x} className={active === x ? "on" : ""} onClick={() => setTab(x)}>{t(`crm.tab_${x}`)}</button>
        ))}
      </nav>

      <div className="crm-body">
        {active === "pessoas" && isCompany && <People company={c} people={people} onOpen={onOpenContact} />}
        {active === "notas" && <Notes id={id} locale={i18n.language} contact={c} />}
        {active === "tarefas" && <Tasks id={id} contactName={c.name} />}
        {active === "emails" && <EmailHistory email={c.email} locale={i18n.language} />}
      </div>

      {editing && <ContactDrawer contact={c} onClose={() => setEditing(false)} onDeleted={onBack} />}
      {merging && <MergeModal keep={c} onClose={() => setMerging(false)} onMerged={() => { setMerging(false); void q.refetch(); }} />}
      {transferring && <TransferModal contact={c} onClose={() => setTransferring(false)} onDone={onBack} />}
    </div>
  );
}

// Transfere a posse do contato para um colega do mesmo tenant (só o dono atual).
function TransferModal({ contact, onClose, onDone }: { contact: Contact; onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation();
  const people = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const [uid, setUid] = useState("");
  const transfer = useMutation({
    mutationFn: () => contactsApi.transfer(contact.id, uid),
    onSuccess: () => { toast(t("crm.transferred")); onDone(); }, onError: (e) => toast((e as Error).message),
  });
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk modal-sm" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>{t("crm.transferTitle")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="tk-body">
          <p className="desc">{t("crm.transferHint")}</p>
          <select value={uid} onChange={(e) => setUid(e.target.value)}>
            <option value="">—</option>
            {(people.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name || p.email}</option>)}
          </select>
        </div>
        <footer>
          <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          <button className="add" disabled={!uid || transfer.isPending} onClick={() => transfer.mutate()}>{t("crm.transfer")}</button>
        </footer>
      </div>
    </div>
  );
}

// Avatar do contato: mostra a foto (via /thumb) ou as iniciais; o dono clica para trocar a foto
// (envia a imagem por /api/files e liga ao contato — reusa o storage).
function ContactAvatar({ c, isOwner, isCompany, onChanged }: { c: Contact; isOwner: boolean; isCompany: boolean; onChanged: () => void }) {
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement>(null);
  // Logo por domínio: favicon do próprio site da empresa (sem provedor externo). Falha → monograma.
  const [logoFail, setLogoFail] = useState(false);
  const showLogo = isCompany && !c.avatar_file_id && !!c.domain && !logoFail;
  const up = useMutation({
    mutationFn: async (file: File) => { const f = await backend.uploadFile(file); return contactsApi.setAvatar(c.id, f.id); },
    onSuccess: onChanged, onError: (e) => toast((e as Error).message),
  });
  return (
    <span className={"crm-av" + (isCompany ? " sq" : "") + (isOwner ? " editable" : "")} style={{ background: hueFor(c.domain || c.email || c.name) }}
      onClick={() => { if (isOwner) inputRef.current?.click(); }} title={isOwner ? t("crm.changePhoto") : undefined}>
      {c.avatar_file_id ? <img src={backend.fileThumbUrl(c.avatar_file_id)} alt="" />
        : showLogo ? <img src={faviconOf(c.domain)} alt="" referrerPolicy="no-referrer" onError={() => setLogoFail(true)} />
        : initials(c.name)}
      {isOwner && <input ref={inputRef} type="file" accept="image/*" hidden onChange={(e) => { const file = e.target.files?.[0]; if (file) up.mutate(file); e.target.value = ""; }} />}
    </span>
  );
}

// Fusão de duplicados: escolhe outro contato do mesmo tipo para fundir NESTE (o escolhido é apagado).
function MergeModal({ keep, onClose, onMerged }: { keep: Contact; onClose: () => void; onMerged: () => void }) {
  const { t } = useTranslation();
  const [term, setTerm] = useState("");
  const all = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });
  const cands = (all.data ?? []).filter((x) => x.id !== keep.id && x.kind === keep.kind &&
    (!term || x.name.toLowerCase().includes(term.toLowerCase()) || (x.email || "").toLowerCase().includes(term.toLowerCase())));
  const merge = useMutation({
    mutationFn: (mergeId: string) => contactsApi.merge(keep.id, mergeId),
    onSuccess: () => { toast(t("crm.merged")); onMerged(); }, onError: (e) => toast((e as Error).message),
  });
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk modal-sm" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>{t("crm.mergeInto", { name: keep.name })}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="tk-body">
          <input autoFocus className="pnl-input" placeholder={t("crm.mergeSearch")} value={term} onChange={(e) => setTerm(e.target.value)} />
          <p className="desc">{t("crm.mergeHint")}</p>
          <div className="merge-list">
            {cands.slice(0, 30).map((x) => (
              <button key={x.id} className="merge-row" disabled={merge.isPending} onClick={() => merge.mutate(x.id)}>
                <span className="av" style={{ background: hueFor(x.email || x.name) }}>{initials(x.name)}</span>
                <span className="grow"><b>{x.name}</b>{x.email && <span className="desc"> {x.email}</span>}</span>
              </button>
            ))}
            {cands.length === 0 && <p className="desc">{t("crm.noDuplicates")}</p>}
          </div>
        </div>
      </div>
    </div>
  );
}

// Pessoas da empresa: cada uma é um contato próprio — clicar abre a ficha dela.
function People({ company, people, onOpen }: { company: Contact; people: Contact[]; onOpen: (id: string) => void }) {
  const { t } = useTranslation();
  const [adding, setAdding] = useState(false);
  return (
    <>
      <div className="cd-tab-head">
        <button className="chipbtn" onClick={() => setAdding(true)}><IcPlus size={14} /> {t("contacts.addPerson")}</button>
      </div>
      {people.length === 0 ? <p className="cd-empty">{t("crm.noPeople")}</p> : (
        <div className="cts-grid">
          {people.map((p) => (
            <div className="ctc" key={p.id} onClick={() => onOpen(p.id)}>
              <div className="av" style={{ background: hueFor(p.email || p.name) }}>{initials(p.name)}</div>
              <div className="info">
                <div className="nm">{p.name}</div>
                {p.title && <div className="em" style={{ fontFamily: "inherit" }}>{p.title}</div>}
                {p.email && <div className="em">{p.email}</div>}
              </div>
            </div>
          ))}
        </div>
      )}
      {/* contact=null → é criação; `initial` já liga a pessoa a esta empresa e herda os grupos dela
          (senão a pessoa nova nascia invisível para quem vê a empresa). */}
      {adding && <ContactDrawer contact={null} onClose={() => setAdding(false)} onOpen={onOpen}
        initial={{ kind: "person", company_id: company.id, group_ids: company.group_ids ?? [] }} />}
    </>
  );
}

// Histórico de e-mails do contato: cruza o endereço dele com a caixa (from:/to:) via JMAP. Paginado
// (25/página, server-side) e clicável — abre o e-mail no correio.
function EmailHistory({ email, locale }: { email: string; locale: string }) {
  const { t } = useTranslation();
  const nav = useNavigate();
  const { session } = useWebmail();
  const [page, setPage] = useState(1);
  const q = useQuery({
    queryKey: ["contact-emails", email, page],
    enabled: !!session && !!email,
    retry: false,
    queryFn: () => session!.service.searchAdvanced(
      { operator: "OR", conditions: [{ from: email }, { to: email }] }, (page - 1) * CRM_PER, CRM_PER),
  });
  const addr = (a: EmailAddress[] | null) => (a && a[0] ? (a[0].name || a[0].email) : "—");
  const items: EmailSummary[] = q.data?.items ?? [];
  const total = q.data?.total ?? 0;
  if (!session) return <p className="cd-empty">{t("crm.emailsUnavailable")}</p>;
  return (
    <div className="cd-emails">
      {q.isLoading ? <div className="loading">…</div>
        : q.isError ? <p className="cd-empty">{(q.error as Error)?.message}</p>
        : items.length === 0 ? <p className="cd-empty">{t("crm.noEmails")}</p>
        : items.map((m) => (
          <div className="cd-email cd-clickable" key={m.id} onClick={() => nav(`/mail/mail/inbox/${m.id}`)} title={t("crm.openEmail")}>
            <span className="ic"><IcMail size={15} /></span>
            <div className="grow">
              <div className="cd-email-subj">{m.subject || t("crm.noSubject")}</div>
              <div className="cd-email-meta">{addr(m.from)} → {addr(m.to)}{m.preview ? ` · ${m.preview.slice(0, 80)}` : ""}</div>
            </div>
            <span className="cd-email-date">{shortTime(m.receivedAt, locale)}</span>
          </div>
        ))}
      <CrmPager page={page} total={total} per={CRM_PER} onPage={setPage} />
    </div>
  );
}

function Notes({ id, locale, contact }: { id: string; locale: string; contact: Contact }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [body, setBody] = useState("");
  const [editId, setEditId] = useState<string | null>(null);
  const [editBody, setEditBody] = useState("");
  const meQ = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false, staleTime: Infinity });
  const myEmail = (meQ.data?.email ?? "").toLowerCase();
  const notes = useQuery({ queryKey: ["contact-notes", id], queryFn: () => contactsApi.notes(id), retry: false });
  const refreshNotes = () => qc.invalidateQueries({ queryKey: ["contact-notes", id] });
  const add = useMutation({
    mutationFn: () => contactsApi.addNote(id, body),
    onSuccess: () => { setBody(""); refreshNotes(); },
    onError: (e) => toast((e as Error).message),
  });
  const editNote = useMutation({
    mutationFn: () => contactsApi.editNote(id, editId!, editBody),
    onSuccess: () => { setEditId(null); refreshNotes(); }, onError: (e) => toast((e as Error).message),
  });
  const empty = (h: string) => !h.replace(/<[^>]*>/g, "").replace(/&nbsp;/gi, " ").trim();
  return (
    <div className="cd-notas">
      <div className="cd-newnote">
        <MdEditor value={body} onChange={setBody} placeholder={t("crm.notePh")} />
        <button className="chipbtn" disabled={empty(body) || add.isPending} onClick={() => add.mutate()}>
          {add.isPending ? <Spinner size={13} /> : t("crm.addNote")}
        </button>
      </div>
      {contact.notes && <p className="cd-notes">{contact.notes}</p>}
      {notes.isLoading ? <div className="loading">…</div>
        : (notes.data ?? []).length === 0 ? <p className="cd-empty">{t("crm.noNotes")}</p>
        : (notes.data ?? []).map((n) => (
          <div className="cd-note" key={n.id}>
            <div className="cd-note-h"><b>{n.author_name || n.author_email}</b><span>{shortTime(n.created_at, locale)}</span>
              {n.author_email.toLowerCase() === myEmail && editId !== n.id && (
                <button className="ib sm" title={t("chat.edit")} onClick={() => { setEditId(n.id); setEditBody(n.body); }}><IcPencil size={12} /></button>
              )}
            </div>
            {editId === n.id ? (
              <div className="cd-note-edit">
                <MdEditor value={editBody} onChange={setEditBody} placeholder={t("crm.notePh")} />
                <div className="cmt-edit-acts">
                  <button className="chipbtn" onClick={() => setEditId(null)}>{t("common.cancel")}</button>
                  <button className="add" disabled={empty(editBody) || editNote.isPending} onClick={() => editNote.mutate()}>{t("contacts.save")}</button>
                </div>
              </div>
            ) : <div className="cd-note-b">{renderRich(n.body)}</div>}
          </div>
        ))}
    </div>
  );
}

const CRM_PER = 25;

// Paginador compacto (‹ x / y ›) partilhado pelas abas Tarefas e E-mails.
function CrmPager({ page, total, per, onPage }: { page: number; total: number; per: number; onPage: (p: number) => void }) {
  const { t } = useTranslation();
  const pages = Math.ceil(total / per);
  if (pages <= 1) return null;
  return (
    <div className="crm-pager">
      <button className="ib" disabled={page <= 1} title={t("common.prevPage")} onClick={() => onPage(page - 1)}><IcChevron size={16} className="left" /></button>
      <span>{page} / {pages}</span>
      <button className="ib" disabled={page >= pages} title={t("common.nextPage")} onClick={() => onPage(page + 1)}><IcChevron size={16} className="right" /></button>
    </div>
  );
}

function Tasks({ id, contactName }: { id: string; contactName: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const nav = useNavigate();
  const [creating, setCreating] = useState(false);
  const [page, setPage] = useState(1);
  const q = useQuery({ queryKey: ["contact-tasks", id], queryFn: () => contactsApi.tasks(id), retry: false });
  const all = q.data ?? [];
  const list = all.slice((page - 1) * CRM_PER, page * CRM_PER);
  return (
    <div className="cd-tasks">
      {/* Ação no topo, alinhada à direita (padrão pedido). */}
      <div className="cd-tab-head">
        <button className="chipbtn" onClick={() => setCreating(true)}><IcPlus size={14} /> {t("crm.newTask")}</button>
      </div>
      {q.isLoading ? <div className="loading">…</div>
        : all.length === 0 ? <p className="cd-empty">{t("crm.noTasks")}</p>
        : list.map((k) => (
          <div className="cd-task cd-clickable" key={k.id} onClick={() => nav(`/mail/kanban/${k.board_id}/${k.id}`)} title={t("crm.openTask")}>
            <div className="cd-task-t">{k.title}</div>
            <div className="cd-task-m">
              <span className="pnl-pill muted">{k.board_name}</span>
              <span className="pnl-pill">{k.column_name}</span>
              {k.due && <span className="kdue">{k.due}</span>}
              {k.assignee && <span className="av" style={{ background: hueFor(k.assignee) }}>{initials(k.assignee)}</span>}
            </div>
          </div>
        ))}
      <CrmPager page={page} total={all.length} per={CRM_PER} onPage={setPage} />
      {creating && <NewCardModal contactId={id} contactName={contactName} onClose={() => setCreating(false)}
        onCreated={() => qc.invalidateQueries({ queryKey: ["contact-tasks", id] })} />}
    </div>
  );
}

// Criar um card do Kanban já ligado a este contacto: escolhe quadro + coluna, dá título. O card
// aparece depois na aba Tarefas do contacto (e no quadro). (24/07/2026)
function NewCardModal({ contactId, contactName, onClose, onCreated }: {
  contactId: string; contactName: string; onClose: () => void; onCreated: () => void;
}) {
  const { t } = useTranslation();
  const [boardId, setBoardId] = useState("");
  const [colId, setColId] = useState("");
  const [title, setTitle] = useState("");
  const boards = useQuery({ queryKey: ["boards"], queryFn: () => backend.boards(), retry: false });
  const detail = useQuery({ queryKey: ["board", boardId], queryFn: () => backend.board(boardId), enabled: !!boardId, retry: false });
  const cols = (detail.data?.columns ?? []).map((c) => c.column);
  useEffect(() => { if (!boardId && boards.data?.length) setBoardId(boards.data[0].id); }, [boards.data, boardId]);
  useEffect(() => { setColId(cols[0]?.id ?? ""); /* eslint-disable-next-line */ }, [detail.data]);
  const create = useMutation({
    mutationFn: () => backend.createCard(boardId, { column_id: colId, title: title.trim(), contact_id: contactId }),
    onSuccess: () => { toast(t("crm.taskCreated")); onCreated(); onClose(); },
    onError: (e) => toast((e as Error).message),
  });
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 440 }}>
        <header><span>{t("crm.newTaskFor", { name: contactName })}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body meet-form">
          {boards.isLoading ? <div className="loading">…</div>
            : (boards.data ?? []).length === 0 ? <p className="cd-empty">{t("crm.noBoards")}</p>
            : (
              <>
                <label>{t("crm.board")}<select value={boardId} onChange={(e) => setBoardId(e.target.value)}>
                  {(boards.data ?? []).map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
                </select></label>
                <label>{t("crm.column")}<select value={colId} onChange={(e) => setColId(e.target.value)}>
                  {cols.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
                </select></label>
                <label>{t("crm.taskTitle")}<input autoFocus value={title} onChange={(e) => setTitle(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter" && title.trim() && colId) create.mutate(); }} placeholder={t("crm.taskTitlePh")} /></label>
              </>
            )}
        </div>
        <footer>
          <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          <button className="add" disabled={!title.trim() || !colId || create.isPending} onClick={() => create.mutate()}>{t("common.create")}</button>
        </footer>
      </div>
    </div>
  );
}

