import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router-dom";
import type { WebmailSession } from "./auth";
import type { Mailbox, EmailAttachment, EmailSummary, UploadedBlob } from "../jmap/types";
import { MailReader } from "./MailReader";
import { Compose } from "./Compose";
import { ScheduledModal } from "./ScheduledModal";
import { AIDrawer } from "./AIDrawer";
import { AttachmentViewer } from "./AttachmentViewer";
import { RulesView } from "./RulesView";
import { EventModal } from "./EventModal";
import { ExtractTasksModal } from "./ExtractTasksModal";
import { toast } from "./toast";
import { backend, contactsApi, labelKeyword } from "./backend";
import { displayName, shortTime } from "./util";
import { Avatar } from "./Avatar";
import { IcPencil, IcFolderPlus, IcSparkle, IcStar, IcRefresh, IcTrash, IcFolder, IcMail, IcChevron, IcX, IcClock, IcPaperclip } from "../panel/icons";
import { MailAdvancedSearch } from "./MailAdvancedSearch";
import { LabelChips } from "./MailLabels";

const ROLE_ORDER = ["inbox", "sent", "drafts", "archive", "junk", "trash"];

type ComposeInit = { to?: string; cc?: string; subject?: string; body?: string; bodyHtml?: string; draftId?: string; attachFiles?: File[]; attachments?: UploadedBlob[] };

export function MailView({ session, search, sub, sub2, advOpen = false, onAdvClose }: { session: WebmailSession; search: string; sub?: string; sub2?: string; advOpen?: boolean; onAdvClose?: () => void }) {
  const { t, i18n } = useTranslation();
  const svc = session.service;
  // O push JMAP em tempo real fica no App (global). Aqui só o refetchInterval como fallback.

  // Auto-atualiza os contadores das pastas (novos e-mails / lidos) a cada 20s e ao focar a janela.
  const boxesQ = useQuery({
    queryKey: ["mailboxes"], queryFn: () => svc.mailboxes(),
    refetchInterval: 20000, refetchOnWindowFocus: true,
  });
  // Várias janelas de escrita ao mesmo tempo (novas e rascunhos), empilhadas em baixo — como o Gmail.
  const [composers, setComposers] = useState<{ id: number; init: ComposeInit }[]>([]);
  const composerSeq = useRef(0);
  const openComposer = (init: ComposeInit) => setComposers((cs) => {
    // Abrir um rascunho já aberto não duplica — mantém a janela existente.
    if (init.draftId && cs.some((c) => c.init.draftId === init.draftId)) return cs;
    return [...cs, { id: ++composerSeq.current, init }];
  });
  const closeComposer = (id: number) => { setComposers((cs) => cs.filter((c) => c.id !== id)); void listQ.refetch(); void boxesQ.refetch(); };
  const [scheduledOpen, setScheduledOpen] = useState(false);
  const [aiOriginal, setAiOriginal] = useState<{ original: string; reply: { to?: string; cc?: string; subject?: string } } | null>(null);
  const [extract, setExtract] = useState<{ subject: string; body: string; emailId: string } | null>(null);
  const [viewAtt, setViewAtt] = useState<EmailAttachment | null>(null);
  const [meetTitle, setMeetTitle] = useState<string | null>(null);
  const [newFolder, setNewFolder] = useState<string | null>(null);
  const [kebabFolder, setKebabFolder] = useState<string | null>(null);   // pasta com o menu (⋯) aberto
  const [renameFolderId, setRenameFolderId] = useState<string | null>(null);
  const [subfolderOf, setSubfolderOf] = useState<string | null>(null); // criar subpasta dentro desta
  const [folderInput, setFolderInput] = useState("");
  const [params, setParams] = useSearchParams();

  // Largura da coluna da lista: redimensionável pelo utilizador (pega na borda), com MÍNIMO
  // garantido e persistida por dispositivo (localStorage — depende do ecrã, não da conta).
  const LIST_W_KEY = "mailin.wm.listWidth";
  const LIST_W_MIN = 340, LIST_W_MAX = 680;
  const [listW, setListW] = useState<number>(() => {
    const v = Number(localStorage.getItem(LIST_W_KEY));
    return v >= LIST_W_MIN && v <= LIST_W_MAX ? v : 420;
  });
  const [resizing, setResizing] = useState(false);
  const startResize = (e: React.PointerEvent) => {
    e.preventDefault();
    const startX = e.clientX, startW = listW;
    setResizing(true);
    const move = (ev: PointerEvent) => {
      setListW(Math.min(LIST_W_MAX, Math.max(LIST_W_MIN, startW + (ev.clientX - startX))));
    };
    const up = (ev: PointerEvent) => {
      const w = Math.min(LIST_W_MAX, Math.max(LIST_W_MIN, startW + (ev.clientX - startX)));
      localStorage.setItem(LIST_W_KEY, String(w));
      setResizing(false);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const nav = useNavigate();
  // As automações vivem DENTRO do e-mail: é onde as regras actuam. Na URL, como todo o resto.
  const rules = sub === "rules";
  // Importantes é uma VISTA (marca $flagged), não uma pasta: a mensagem continua onde está.
  const starred = sub === "starred";
  // Vista por marcador: /mail/mail/label:<id>. Como Importantes, filtra por keyword, sem mudar pasta.
  const labelId = !rules && !starred && sub?.startsWith("label:") ? sub.slice("label:".length) : null;
  const labelsQ = useQuery({ queryKey: ["labels"], queryFn: () => backend.labels() });
  const [dragId, setDragId] = useState<string | null>(null);
  const [dropOn, setDropOn] = useState<string | null>(null);
  // Seleção em lote. Set (não array): marcar/desmarcar 50 linhas tem de ser O(1).
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [confirmEmpty, setConfirmEmpty] = useState(false);
  const [bulkMove, setBulkMove] = useState(false);
  const [folderMenu, setFolderMenu] = useState(false); // gaveta de pastas no telemóvel

  // A URL carrega TUDO: /mail/mail/<pasta>/<emailId>?p=<pag>. Pasta é o role (inbox, sent…) ou o
  // id da pasta custom — assim o link é legível e copiável. O email aberto e a página idem.
  const page = Math.max(1, Number(params.get("p")) || 1);
  const selected = sub2 && !rules && !starred ? sub2 : null;

  // ?to=<email>: alguém pediu "enviar e-mail" de fora (ex.: ficha do contato). Abre a janela de
  // escrita já endereçada e limpa o parâmetro, senão o refresh reabriria.
  const to = params.get("to");
  useEffect(() => {
    if (!to) return;
    openComposer({ to });
    setParams({}, { replace: true });
  }, [to, setParams]);

  // ?vcard=<id>: "enviar contato" da ficha — abre a escrita com o cartão (.vcf) já anexado. Vai
  // buscar o ficheiro à API e limpa o parâmetro (senão o refresh reanexava).
  const vcardId = params.get("vcard");
  const vcardName = params.get("vname") || "";
  useEffect(() => {
    if (!vcardId) return;
    setParams({}, { replace: true });
    contactsApi.vcardFile(vcardId)
      .then((file) => openComposer({
        attachFiles: [file],
        subject: vcardName ? t("webmail.compose.vcardSubject", { name: vcardName }) : "",
      }))
      .catch((e) => toast((e as Error).message));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [vcardId, setParams]);

  const folders = useMemo(() => sortFolders(boxesQ.data ?? []), [boxesQ.data]);
  // A pasta da URL (`sub`) pode ser um ROLE (inbox, sent…) ou o id de uma pasta custom. Sem `sub`
  // — e sem ser regras/importantes — é a caixa de entrada.
  const folderKey = rules || starred || labelId ? null : sub ?? null;
  const activeFolder = folderKey
    ? (folders.find((f) => f.role === folderKey)?.id ?? folders.find((f) => f.id === folderKey)?.id ?? null)
    : (folders.find((f) => f.role === "inbox")?.id ?? folders[0]?.id ?? null);

  // Slug estável de uma pasta para a URL: o role quando existe (legível), senão o id.
  const folderSlug = (f?: Mailbox) => f?.role ?? f?.id ?? "inbox";
  const openFolder = (f: Mailbox) => nav(`/mail/mail/${folderSlug(f)}`);
  // Base da URL ao abrir/fechar um e-mail. Marcador tem a sua própria base; o resto mantém o
  // comportamento anterior (importantes cai na pasta activa, como antes).
  const viewBase = labelId ? `label:${labelId}` : (() => { const f = folders.find((x) => x.id === activeFolder); return f ? folderSlug(f) : "inbox"; })();
  // Abrir/fechar e-mail PRESERVA a query string (?p=2…): sem isto, clicar num e-mail da página 2
  // atirava a lista de volta à página 1 (bug reportado 23/07/2026).
  const keepParams = (path: string) => { const q = params.toString(); return q ? `${path}?${q}` : path; };
  const openEmail = (id: string) => nav(keepParams(`/mail/mail/${viewBase}/${id}`));
  const closeEmail = () => nav(keepParams(`/mail/mail/${viewBase}`));
  const goPage = (n: number) => setParams((prev) => { const q = new URLSearchParams(prev); if (n <= 1) q.delete("p"); else q.set("p", String(n)); return q; }, { replace: true });
  const position = (page - 1) * 50;

  // Busca avançada (Gmail-style): quando um filtro JMAP está ativo, ele manda sobre a busca simples.
  // O painel abre-se do chevron na barra de busca de cima (advOpen vem do App).
  const [advFilter, setAdvFilter] = useState<Record<string, unknown> | null>(null);

  // A lista recarrega sozinha (novos e-mails sem recarregar a página). Busca não fica em polling.
  // Separação por endereço (estilo Mail do Mac): um alias entrega na MESMA caixa, por isso "separar"
  // é filtrar por destinatário, não abrir uma conta nova. Sem aliases, nada disto aparece.
  const addrQ = useQuery({ queryKey: ["my-addresses"], queryFn: () => backend.myAddresses(), retry: false, staleTime: 300000 });
  const aliases = addrQ.data?.aliases ?? [];
  const [addrFilter, setAddrFilter] = useState(""); // "" = tudo o que chega à caixa
  // O filtro por endereço vira uma busca avançada limitada à pasta atual: para/cc com o endereço.
  const addrJmapFilter = useMemo(() => {
    if (!addrFilter || !activeFolder) return null;
    return {
      operator: "AND",
      conditions: [
        { inMailbox: activeFolder },
        { operator: "OR", conditions: [{ to: addrFilter }, { cc: addrFilter }] },
      ],
    } as Record<string, unknown>;
  }, [addrFilter, activeFolder]);

  const listQ = useQuery({
    queryKey: ["emails", advFilter ? `adv:${JSON.stringify(advFilter)}` : search ? `s:${search}` : starred ? "starred" : labelId ? `label:${labelId}` : activeFolder, addrFilter, page],
    queryFn: () => (advFilter ? svc.searchAdvanced(advFilter, position)
      : search ? svc.search(search, position)
      : starred ? svc.listStarred(position)
      : labelId ? svc.listByLabel(labelKeyword(labelId), position)
      : addrJmapFilter ? svc.searchAdvanced(addrJmapFilter, position)
      : svc.listEmails(activeFolder!, position)),
    enabled: advFilter ? true : search ? search.length > 1 : starred || !!labelId || !!activeFolder,
    refetchInterval: (search || advFilter) ? false : 20000, refetchOnWindowFocus: true,
  });
  const rawEmails = listQ.data?.items ?? [];
  const total = listQ.data?.total ?? 0;

  // Ordenação da lista (na página atual; o servidor já traz por data). data | remetente | assunto.
  const [sortBy, setSortBy] = useState<"date" | "from" | "subject">("date");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");
  const emails = useMemo(() => {
    const dir = sortDir === "asc" ? 1 : -1;
    return [...rawEmails].sort((a, b) => {
      if (sortBy === "from") return dir * displayName(a.from).localeCompare(displayName(b.from), undefined, { sensitivity: "base" });
      if (sortBy === "subject") return dir * (a.subject || "").localeCompare(b.subject || "", undefined, { sensitivity: "base" });
      return dir * (a.receivedAt || "").localeCompare(b.receivedAt || "");
    });
  }, [rawEmails, sortBy, sortDir]);

  // Trocar de pasta/página/busca zera a seleção: um lote marcado numa lista já não visível
  // aplicaria ações a mensagens fora da vista.
  useEffect(() => { setSel(new Set()); }, [activeFolder, page, search, starred, labelId]);

  // Navegação por teclado (estilo Gmail): j/k move o foco, o/Enter abre, e arquiva, r responde,
  // # move p/ lixeira, x (des)marca. Ignora quando se está a escrever num campo ou com o compositor.
  const [focusIdx, setFocusIdx] = useState(-1);
  useEffect(() => { setFocusIdx(-1); }, [activeFolder, page, search, starred, labelId, sortBy, sortDir]);
  const focusedId = focusIdx >= 0 && focusIdx < emails.length ? emails[focusIdx].id : null;
  useEffect(() => { if (focusedId) document.querySelector(`[data-mrow="${focusedId}"]`)?.scrollIntoView({ block: "nearest" }); }, [focusedId]);

  // Estrelar não muda de pasta: só a marca. Recarrega a lista para Importantes acompanhar.
  const toggleStar = async (id: string, on: boolean) => {
    await svc.setStarred(id, on);
    void listQ.refetch(); void boxesQ.refetch();
  };

  // --- Seleção em lote ---
  const allOnPage = emails.length > 0 && emails.every((m) => sel.has(m.id));
  const toggleOne = (id: string) => setSel((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n; });
  const toggleAll = () => setSel(allOnPage ? new Set() : new Set(emails.map((m) => m.id)));

  // "Selecionar todas as N" (as de todas as páginas). Busca os ids da pasta e mete-os no lote.
  // Só faz sentido numa pasta (não em busca/importantes, onde a query é outra).
  async function selectWholeFolder() {
    if (!activeFolder) return;
    try { setSel(new Set(await svc.allIds(activeFolder))); }
    catch (e) { toast((e as Error).message); }
  }
  const selIds = () => [...sel];

  async function bulk(fn: () => Promise<void>, msg?: string) {
    try { await fn(); setSel(new Set()); if (selected && sel.has(selected)) closeEmail();
      await listQ.refetch(); void boxesQ.refetch(); if (msg) toast(msg); }
    catch (e) { toast((e as Error).message); }
  }

  // Arrastar para uma pasta = mover de verdade (Email/set mailboxIds). É o oposto de estrelar.
  const dropInto = async (folderId: string) => {
    const id = dragId;
    setDragId(null); setDropOn(null);
    if (!id) return;
    const junkBox = folders.find((f) => f.role === "junk");
    try {
      await svc.moveTo(id, folderId, junkBox ? folderId === junkBox.id : undefined);
      if (selected === id) closeEmail();
      toast(t("webmail.moved"));
      void listQ.refetch(); void boxesQ.refetch();
    } catch (e) { toast((e as Error).message); }
  };

  const inboxId = folders.find((f) => f.role === "inbox")?.id ?? null;
  const junkId = folders.find((f) => f.role === "junk")?.id ?? null;
  const activeRole = folders.find((f) => f.id === activeFolder)?.role ?? null;
  // Pastas de SAÍDA (enviados/rascunhos): na listagem mostramos o DESTINATÁRIO ("Para: …"), não o
  // remetente — como no Gmail. Nas restantes mostramos o remetente.
  const outgoing = activeRole === "sent" || activeRole === "drafts";
  // Vistas que MISTURAM pastas (Importantes, busca, marcador): mostram um chip com a pasta de cada
  // mensagem, como no Gmail. Numa pasta normal seria redundante (todas na mesma), por isso omite-se.
  const mixedView = starred || !!labelId || !!search || !!advFilter;
  const folderChip = (m: EmailSummary) => {
    if (!mixedView || !m.mailboxIds) return null;
    const cands = Object.keys(m.mailboxIds)
      .filter((k) => m.mailboxIds![k])
      .map((id) => folders.find((f) => f.id === id))
      .filter(Boolean) as Mailbox[];
    if (!cands.length) return null;
    const f = cands.find((c) => c.role) ?? cands[0]; // prefere a pasta de sistema (inbox, sent…)
    return <span className="row-folder">{folderLabel(t, f)}</span>;
  };
  // "Não é spam" na pasta Spam; "Marcar como spam" nas demais. Sem ação em Enviados/Lixeira.
  const spamAction = (() => {
    if (search || !selected) return undefined;
    if (activeRole === "junk" && inboxId) {
      return { label: t("webmail.notSpam"), junk: false, dest: inboxId };
    }
    if (junkId && (activeRole === "inbox" || activeRole === "archive")) {
      return { label: t("webmail.markSpam"), junk: true, dest: junkId };
    }
    return undefined;
  })();
  async function runSpamAction() {
    if (!spamAction || !selected) return;
    await svc.moveTo(selected, spamAction.dest, spamAction.junk);
    closeEmail();
    await listQ.refetch();
    void boxesQ.refetch();
    toast(t(spamAction.junk ? "webmail.markedSpam" : "webmail.markedNotSpam"));
  }

  const trashId = folders.find((f) => f.role === "trash")?.id ?? null;
  // Arquivar de 1 clique: move para a pasta Arquivo (role archive). Só onde faz sentido — não em
  // Enviados/Rascunhos/Lixeira nem já dentro do Arquivo, e nunca durante uma busca.
  const archiveId = folders.find((f) => f.role === "archive")?.id ?? null;
  // Aplicável em qualquer pasta menos Arquivo/Enviados/Rascunhos/Lixeira; a pasta Arquivo é criada
  // sob demanda se ainda não existir (ensureArchiveMailbox).
  const canArchive = !search && !["archive", "sent", "drafts", "trash"].includes(activeRole ?? "");
  async function archiveSelected() {
    if (!selected) return;
    try {
      const dest = archiveId ?? (await svc.ensureArchiveMailbox());
      await svc.moveTo(selected, dest);
      closeEmail();
      toast(t("webmail.archived"));
      void listQ.refetch(); void boxesQ.refetch();
    } catch (e) { toast((e as Error).message); }
  }
  // Snooze: move a mensagem para "Snoozed" e regista a hora; o worker devolve-a à inbox. Aplicável
  // fora de Enviados/Rascunhos/Lixeira e sem busca.
  const canSnooze = !search && !["sent", "drafts", "trash"].includes(activeRole ?? "");
  async function snoozeSelected(wake: Date) {
    if (!selected) return;
    try {
      const dest = await svc.ensureSnoozedMailbox();
      await svc.moveTo(selected, dest);
      await backend.snoozeEmail(selected, wake.toISOString());
      closeEmail();
      toast(t("webmail.snoozed"));
      void listQ.refetch(); void boxesQ.refetch();
    } catch (e) { toast((e as Error).message); }
  }

  // Destinos do menu "Mover para": todas as pastas menos a atual e a lixeira (esta tem botão próprio).
  const moveTargets = folders
    .filter((f) => f.id !== activeFolder && f.role !== "trash")
    .map((f) => ({ id: f.id, label: folderLabel(t, f) }));

  async function emptyNow() {
    const box = folders.find((f) => f.id === activeFolder);
    if (!box) return;
    setConfirmEmpty(false);
    try { const n = await svc.emptyMailbox(box.id); await listQ.refetch(); void boxesQ.refetch(); toast(t("webmail.emptied", { n })); }
    catch (e) { toast((e as Error).message); }
  }

  async function afterMutate() { closeEmail(); await listQ.refetch(); void boxesQ.refetch(); }
  // Move um id específico (atalhos de teclado), fechando o leitor só se for o aberto.
  async function moveOne(id: string, dest: string | null, msg: string) {
    if (!id || !dest) return;
    try { await svc.moveTo(id, dest); if (selected === id) closeEmail(); toast(msg); void listQ.refetch(); void boxesQ.refetch(); }
    catch (e) { toast((e as Error).message); }
  }
  async function archiveOne(id: string) { await moveOne(id, archiveId ?? (await svc.ensureArchiveMailbox()), t("webmail.archived")); }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable)) return;
      if (e.metaKey || e.ctrlKey || e.altKey || composers.length || !emails.length) return;
      switch (e.key) {
        case "j": case "ArrowDown": e.preventDefault(); setFocusIdx((i) => Math.min(emails.length - 1, i + 1)); break;
        case "k": case "ArrowUp": e.preventDefault(); setFocusIdx((i) => Math.max(0, (i < 0 ? 0 : i) - 1)); break;
        case "o": case "Enter": { if (focusedId) { const m = emails[focusIdx]; openRow(focusedId, m?.keywords ? !m.keywords.$seen : false); } break; }
        case "e": if (focusedId && canArchive) void archiveOne(focusedId); break;
        case "#": if (focusedId && trashId) void moveOne(focusedId, trashId, t("webmail.movedTrash")); break;
        case "r": if (focusedId) openEmail(focusedId); break;
        case "x": if (focusedId) toggleOne(focusedId); break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [emails, focusIdx, focusedId, composers.length, canArchive, trashId, archiveId, selected]);
  async function moveSelected(mailboxId: string) { if (selected) { await svc.moveTo(selected, mailboxId); await afterMutate(); toast(t("webmail.moved")); } }
  async function trashSelected() { if (selected && trashId) { await svc.moveTo(selected, trashId); await afterMutate(); toast(t("webmail.movedTrash")); } }
  async function markUnread() { if (selected) { await svc.markRead(selected, false); await afterMutate(); } }

  async function createFolder() {
    const name = (newFolder ?? "").trim();
    if (!name) { setNewFolder(null); return; }
    try { await svc.createMailbox(name); void boxesQ.refetch(); toast(t("webmail.folderCreated")); }
    catch (e) { toast((e as Error).message); }
    setNewFolder(null);
  }

  async function renameFolder(id: string) {
    const name = folderInput.trim();
    if (name) {
      try { await svc.renameMailbox(id, name); void boxesQ.refetch(); toast(t("webmail.folderRenamed")); }
      catch (e) { toast((e as Error).message); }
    }
    setRenameFolderId(null); setFolderInput("");
  }
  async function createSubfolder(parentId: string) {
    const name = folderInput.trim();
    if (name) {
      try { await svc.createMailbox(name, parentId); void boxesQ.refetch(); toast(t("webmail.folderCreated")); }
      catch (e) { toast((e as Error).message); }
    }
    setSubfolderOf(null); setFolderInput("");
  }
  async function deleteFolder(id: string) {
    setKebabFolder(null);
    try {
      await svc.deleteMailbox(id);
      if (activeFolder === id) nav("/mail/mail");
      void boxesQ.refetch();
      toast(t("webmail.folderDeleted"));
    } catch (e) {
      const m = (e as Error).message;
      toast(m === "notEmpty" ? t("webmail.folderNotEmpty") : m === "hasChild" ? t("webmail.folderHasChild") : t("webmail.folderDeleteFailed"));
    }
  }

  // Abre um rascunho no compositor (pasta Rascunhos): carrega o email e preenche o Compose.
  async function openDraft(id: string) {
    const m = await svc.getEmail(id);
    const part = m.textBody?.[0]?.partId;
    const body = part ? m.bodyValues[part]?.value ?? "" : m.preview;
    // Preserva o HTML do rascunho (formatação, imagens inline) em vez de recarregar só o texto.
    const htmlMeta = m.htmlBody?.[0];
    const bodyHtml = htmlMeta?.type === "text/html" ? m.bodyValues[htmlMeta.partId]?.value : undefined;
    // Restaura os anexos do rascunho (blobs já no servidor). Exclui imagens inline (têm cid — vivem
    // no HTML do corpo, não são anexos-ficheiro): reanexá-las duplicaria a imagem.
    const attachments: UploadedBlob[] = (m.attachments ?? [])
      .filter((a) => !a.cid)
      .map((a) => ({ blobId: a.blobId, type: a.type, name: a.name || "anexo", size: a.size }));
    openComposer({
      to: (m.to ?? []).map((a) => a.email).join(", "),
      subject: m.subject, body, bodyHtml: bodyHtml || undefined, draftId: id,
      attachments: attachments.length ? attachments : undefined,
    });
  }

  function openRow(id: string, unread: boolean) {
    if (activeRole === "drafts" && !search) { void openDraft(id); return; }
    openEmail(id);
    // Ao abrir uma não lida: marca lida e atualiza a lista E os contadores das pastas.
    if (unread) void svc.markRead(id, true).then(() => { void listQ.refetch(); void boxesQ.refetch(); });
  }

  return (
    <div className={"mailgrid" + (rules ? " rules-mode" : "") + (selected ? " reading" : "")}
      style={{ "--listw": listW + "px" } as React.CSSProperties}>
      {folderMenu && <div className="mgrid-backdrop" onClick={() => setFolderMenu(false)} />}
      <aside className={"mrail" + (folderMenu ? " open" : "")} onClick={() => setFolderMenu(false)}>
        <button className="compose" onClick={() => { if (rules) nav("/mail/mail"); openComposer({}); }}><IcPencil size={16} /> <span>{t("webmail.compose.new")}</span></button>
        {aliases.length > 0 && (
          // Só aparece quando há aliases: sem eles, uma lista de "caixas" com uma entrada só seria ruído.
          <div className="mail-accounts">
            <div className="ma-title">{t("webmail.accounts")}</div>
            <button className={"ma-btn" + (addrFilter === "" ? " on" : "")} onClick={() => setAddrFilter("")}>
              {t("webmail.accountsAll")}
            </button>
            {[addrQ.data?.primary ?? "", ...aliases].filter(Boolean).map((a) => (
              <button key={a} className={"ma-btn" + (addrFilter === a ? " on" : "")} onClick={() => setAddrFilter(a)} title={a}>
                {a}
              </button>
            ))}
          </div>
        )}
        <div className="nav">
          {layoutFolders(boxesQ.data ?? []).map(({ f, depth }) => (
            <div key={f.id} className="folder-row" style={{ marginLeft: depth * 12 }}>
              {renameFolderId === f.id ? (
                <input className="newfolder" autoFocus value={folderInput} onChange={(e) => setFolderInput(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") void renameFolder(f.id); if (e.key === "Escape") { setRenameFolderId(null); setFolderInput(""); } }}
                  onBlur={() => void renameFolder(f.id)} />
              ) : (
                <button className={"fbtn " + (f.id === activeFolder && !search && !starred ? "on" : "") + (dropOn === f.id ? " drop" : "")}
                  // Clicar numa pasta SAI das Automações: elas são uma página do e-mail, não uma camada.
                  onClick={() => openFolder(f)}
                  onDragOver={(e) => { if (dragId) { e.preventDefault(); setDropOn(f.id); } }}
                  onDragLeave={() => setDropOn((v) => (v === f.id ? null : v))}
                  onDrop={(e) => { e.preventDefault(); void dropInto(f.id); }}>
                  <span>{folderLabel(t, f)}</span>
                  {f.unreadEmails > 0 && <span className="c">{f.unreadEmails}</span>}
                </button>
              )}
              {/* Só pastas do utilizador (sem role de sistema) podem ser renomeadas/apagadas/aninhadas. */}
              {!f.role && renameFolderId !== f.id && (
                <div className="folder-menuwrap">
                  <button className="folder-kebab" title={t("webmail.folderActions")} onClick={() => setKebabFolder((v) => (v === f.id ? null : f.id))}>⋯</button>
                  {kebabFolder === f.id && (
                    <div className="move-menu" onMouseLeave={() => setKebabFolder(null)}>
                      <button onClick={() => { setRenameFolderId(f.id); setFolderInput(f.name); setKebabFolder(null); }}>{t("webmail.renameFolder")}</button>
                      <button onClick={() => { setSubfolderOf(f.id); setFolderInput(""); setKebabFolder(null); }}>{t("webmail.newSubfolder")}</button>
                      <button className="danger-item" onClick={() => void deleteFolder(f.id)}>{t("webmail.deleteFolder")}</button>
                    </div>
                  )}
                </div>
              )}
              {subfolderOf === f.id && (
                <input className="newfolder subfolder" autoFocus value={folderInput} placeholder={t("webmail.folderName")} onChange={(e) => setFolderInput(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") void createSubfolder(f.id); if (e.key === "Escape") { setSubfolderOf(null); setFolderInput(""); } }}
                  onBlur={() => void createSubfolder(f.id)} />
              )}
            </div>
          ))}
          {/* Importantes fica JUNTO das pastas: é para onde se navega, não uma ação. A marca de
              $flagged é que decide o conteúdo, mas para quem usa é mais um lugar. */}
          <button className={"star-row" + (starred ? " on" : "")}
            onClick={() => nav(starred ? "/mail/mail" : "/mail/mail/starred")}>
            <IcStar size={15} filled={starred} />
            <span>{t("webmail.starred")}</span>
          </button>

          {/* Marcadores (labels): cada um é uma vista por keyword. O catálogo gere-se no reader. */}
          {(labelsQ.data ?? []).map((l) => (
            <button key={l.id} className={"label-row" + (labelId === l.id ? " on" : "")}
              onClick={() => nav(labelId === l.id ? "/mail/mail" : `/mail/mail/label:${l.id}`)}>
              <span className="label-dot" style={{ background: l.color }} />
              <span>{l.name}</span>
            </button>
          ))}

          {newFolder !== null
            ? <input className="newfolder" autoFocus value={newFolder} placeholder={t("webmail.folderName")}
                onChange={(e) => setNewFolder(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") void createFolder(); if (e.key === "Escape") setNewFolder(null); }}
                onBlur={() => void createFolder()} />
            : <button className="addfolder" onClick={() => setNewFolder("")}><IcFolderPlus size={15} /> <span>{t("webmail.newFolder")}</span></button>}
        </div>
        <button className="addfolder rules-entry" onClick={() => setScheduledOpen(true)}>
          <IcClock size={15} /> <span>{t("webmail.scheduled.title")}</span>
        </button>
        <button className={"addfolder rules-entry" + (rules ? " on" : "")}
          onClick={() => nav(rules ? "/mail/mail" : "/mail/mail/rules")}>
          <IcSparkle size={15} /> <span>{t("rules.title")}</span>
        </button>
      </aside>

      {rules ? <RulesView session={session} /> : (<>
      <section className="list">
        <div className={"list-resizer" + (resizing ? " dragging" : "")} onPointerDown={startResize}
          role="separator" aria-orientation="vertical" aria-label="resize" />
        <div className="mobtop">
          <button className="ib" onClick={() => setFolderMenu(true)} aria-label="menu"><IcFolder size={18} /></button>
          <b>{search ? t("webmail.searchResults") : starred ? t("webmail.starred") : folderLabel(t, folders.find((f) => f.id === activeFolder))}</b>
        </div>
        {/* Barra de ações: seleciona-tudo, ações em lote, esvaziar (Lixo/Spam) e paginação — como
            o Gmail. Só mostra o que faz sentido no contexto. */}
        <div className="ltoolbar">
          {!search && !starred && (
            <label className="selall" title={t("webmail.selectAll")}>
              <input type="checkbox" checked={allOnPage} ref={(el) => { if (el) el.indeterminate = sel.size > 0 && !allOnPage; }} onChange={toggleAll} />
            </label>
          )}
          <button className="ib" title={t("webmail.refresh")} onClick={() => void listQ.refetch()}><IcRefresh size={16} /></button>
          {advFilter && <button className="chipbtn" onClick={() => { setAdvFilter(null); goPage(1); }}><IcX size={13} /> {t("adv.clear")}</button>}
          {sel.size === 0 && emails.length > 0 && (
            <div className="filter-chip lsort">
              <select value={`${sortBy}:${sortDir}`} title={t("webmail.sortBy")}
                onChange={(e) => { const [b, d] = e.target.value.split(":"); setSortBy(b as "date" | "from" | "subject"); setSortDir(d as "asc" | "desc"); }}>
                <option value="date:desc">{t("webmail.sortNewest")}</option>
                <option value="date:asc">{t("webmail.sortOldest")}</option>
                <option value="from:asc">{t("webmail.sortSender")}</option>
                <option value="subject:asc">{t("webmail.sortSubject")}</option>
              </select>
            </div>
          )}

          {sel.size > 0 ? (
            <div className="bulkbar">
              <span className="n">{t("webmail.nSelected", { n: sel.size })}</span>
              <button className="ib" title={t("webmail.markRead")} onClick={() => void bulk(() => svc.markReadMany(selIds(), true))}><IcMail size={15} /></button>
              <button className="ib" title={t("webmail.star")} onClick={() => void bulk(() => svc.setStarredMany(selIds(), true))}><IcStar size={15} /></button>
              {activeRole === "trash" || activeRole === "junk"
                ? <button className="ib" title={t("webmail.deleteForever")} onClick={() => void bulk(() => svc.deleteMany(selIds()), t("webmail.deleted"))}><IcTrash size={15} /></button>
                : trashId && <button className="ib" title={t("webmail.moveTrash")} onClick={() => void bulk(() => svc.moveMany(selIds(), trashId), t("webmail.movedTrash"))}><IcTrash size={15} /></button>}
              <div className="movewrap">
                <button className={"ib" + (bulkMove ? " on" : "")} title={t("webmail.moveTo")} onClick={() => setBulkMove((v) => !v)}><IcFolder size={15} /></button>
                {bulkMove && (
                  <div className="movemenu" onMouseLeave={() => setBulkMove(false)}>
                    {moveTargets.map((tg) => (
                      <button key={tg.id} onClick={() => { setBulkMove(false); void bulk(() => svc.moveMany(selIds(), tg.id), t("webmail.moved")); }}>{tg.label}</button>
                    ))}
                  </div>
                )}
              </div>
            </div>
          ) : (
            <>
              <h2 className="ltitle">{search ? t("webmail.searchResults") : starred ? t("webmail.starred") : folderLabel(t, folders.find((f) => f.id === activeFolder))}</h2>
              {/* Esvaziar: só no Lixo e no Spam, e só se houver algo. Apaga DE VEZ. */}
              {(activeRole === "trash" || activeRole === "junk") && total > 0 && (
                <button className="chipbtn danger" onClick={() => setConfirmEmpty(true)}>
                  <IcTrash size={14} /> {t("webmail.emptyFolder2")}
                </button>
              )}
            </>
          )}

          <div className="grow" />
          {sel.size === 0 && !starred && total > 50 && (
            <div className="pager">
              <span className="rng">{t("webmail.pageRange", { from: position + 1, to: Math.min(position + emails.length, total), total })}</span>
              <button className="ib" title={t("webmail.prevPage")} disabled={page <= 1} onClick={() => goPage(page - 1)}><IcChevron size={16} className="left" /></button>
              <button className="ib" title={t("webmail.nextPage")} disabled={position + 50 >= total} onClick={() => goPage(page + 1)}><IcChevron size={16} className="right" /></button>
            </div>
          )}
        </div>

        {/* Gmail-style: a página toda está marcada, mas há mais — oferece marcar TODAS. */}
        {allOnPage && !search && !starred && total > emails.length && sel.size <= emails.length && (
          <div className="selall-banner">
            {t("webmail.pageAllSelected", { n: emails.length })}{" "}
            <button onClick={() => void selectWholeFolder()}>{t("webmail.selectWhole", { total })}</button>
          </div>
        )}
        {sel.size > emails.length && (
          <div className="selall-banner on">
            {t("webmail.wholeSelected", { n: sel.size })}{" "}
            <button onClick={() => setSel(new Set())}>{t("webmail.clearSelection")}</button>
          </div>
        )}

        {listQ.isLoading ? <div className="loading">…</div>
          : emails.length === 0 ? <div className="empty">{t("webmail.emptyFolder")}</div>
          : emails.map((m) => {
            // Em Enviados/Rascunhos a "cara" da linha é o destinatário; no resto, o remetente.
            const rcpts = m.to ?? [];
            const primary = outgoing ? rcpts[0] : m.from?.[0];
            const name = outgoing
              ? (rcpts.length === 0 ? t("webmail.list.toNobody")
                : rcpts.length === 1 ? t("webmail.list.to", { who: primary?.name || primary?.email })
                : t("webmail.list.toMany", { who: primary?.name || primary?.email, n: rcpts.length - 1 }))
              : displayName(m.from);
            const unread = m.keywords ? !m.keywords.$seen : false;
            const picked = sel.has(m.id);
            return (
              <article key={m.id} draggable
                className={`row ${unread ? "unread" : ""} ${m.id === selected ? "on" : ""} ${m.id === focusedId ? "kbfocus" : ""} ${dragId === m.id ? "dragging" : ""} ${picked ? "picked" : ""}`}
                data-mrow={m.id}
                onClick={() => openRow(m.id, unread)}
                onDragStart={() => setDragId(m.id)}
                onDragEnd={() => { setDragId(null); setDropOn(null); }}>
                <label className="rowsel" onClick={(e) => e.stopPropagation()}>
                  <input type="checkbox" checked={picked} onChange={() => toggleOne(m.id)} />
                </label>
                <Avatar name={outgoing ? (primary?.name || primary?.email) : name} email={primary?.email} size={34} />
                <div className="rowmid">
                  <div className="topl">{unread && <span className="udot" />}<span className="w">{name}</span>
                    {m.threadCount && <span className="row-thread">{m.threadCount}</span>}</div>
                  <div className="s">{m.subject || t("webmail.noSubject")}</div>
                  <div className="sn">{folderChip(m)}{m.preview}<LabelChips keywords={m.keywords} /></div>
                </div>
                <div className="rowr">
                  <div className="tm">{m.hasAttachment && <span className="row-clip"><IcPaperclip size={12} /></span>}{shortTime(m.receivedAt, i18n.language)}</div>
                  <button className={"star" + (m.keywords?.$flagged ? " on" : "")}
                    title={t(m.keywords?.$flagged ? "webmail.unstar" : "webmail.star")}
                    onClick={(e) => { e.stopPropagation(); void toggleStar(m.id, !m.keywords?.$flagged); }}>
                    <IcStar size={14} filled={!!m.keywords?.$flagged} />
                  </button>
                </div>
              </article>
            );
          })}
      </section>

      <section className="reader">
        {selected && (
          <div className="mobtop">
            <button className="ib" onClick={closeEmail} aria-label="back"><IcChevron size={18} className="left" /></button>
            <b>{t("webmail.apps.mail")}</b>
          </div>
        )}
        <MailReader service={svc} emailId={selected} locale={i18n.language} me={session.email}
          isSpam={activeRole === "junk"}
          onReply={(draft) => openComposer(draft)}
          onDraftAI={(original, reply) => setAiOriginal({ original, reply })}
          onExtractTasks={(subject, body) => setExtract({ subject, body, emailId: selected ?? "" })}
          onSchedule={(subject) => setMeetTitle(subject)}
          spamAction={spamAction ? { label: spamAction.label, junk: spamAction.junk, run: () => void runSpamAction() } : undefined}
          moveTargets={moveTargets}
          onMove={(id) => void moveSelected(id)}
          onArchive={canArchive ? () => void archiveSelected() : undefined}
          onSnooze={canSnooze ? (wake) => void snoozeSelected(wake) : undefined}
          onTrash={() => void trashSelected()}
          onMarkUnread={() => void markUnread()}
          onOpenAttachment={(a) => setViewAtt(a)} />
      </section>
      </>)}

      {composers.map((c, i) => (
        <Compose key={c.id} service={svc} from={session.email} initial={c.init} dockIndex={i}
          onClose={() => closeComposer(c.id)} />
      ))}
      {scheduledOpen && <ScheduledModal onClose={() => setScheduledOpen(false)} />}
      {aiOriginal !== null && (
        <AIDrawer original={aiOriginal.original} onClose={() => setAiOriginal(null)}
          onUse={(text) => { const r = aiOriginal.reply; setAiOriginal(null); openComposer({ to: r.to, cc: r.cc, subject: r.subject, body: text }); }} />
      )}
      {extract && <ExtractTasksModal subject={extract.subject} body={extract.body} sourceEmailId={extract.emailId} onClose={() => setExtract(null)} />}
      {confirmEmpty && (
        <div className="tk-bg open" onMouseDown={() => setConfirmEmpty(false)}>
          <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 380 }}>
            <header><span>{t("webmail.emptyFolder2")}</span><button className="ib" title={t("common.close")} onClick={() => setConfirmEmpty(false)}><IcX size={15} /></button></header>
            <div className="body"><p className="desc">{t("webmail.emptyConfirm", { n: total })}</p>
              <div className="drawer-actions">
                <button className="chipbtn" onClick={() => setConfirmEmpty(false)}>{t("common.cancel")}</button>
                <button className="add danger" onClick={() => void emptyNow()}>{t("webmail.emptyFolder2")}</button>
              </div>
            </div>
          </div>
        </div>
      )}

      {advOpen && (
        <MailAdvancedSearch folders={folders} folderLabel={(f) => folderLabel(t, f)}
          onClose={() => onAdvClose?.()}
          onSearch={(filter) => { setAdvFilter(filter); onAdvClose?.(); goPage(1); }} />
      )}
      {viewAtt && <AttachmentViewer service={svc} attachment={viewAtt} onClose={() => setViewAtt(null)} />}
      {meetTitle !== null && (
        <EventModal day={new Date()} prefillTitle={t("webmail.ai.meetingFrom") + (meetTitle ? ` — ${meetTitle}` : "")}
          onClose={() => setMeetTitle(null)} onSaved={() => { setMeetTitle(null); toast(t("cal.created")); }} />
      )}
    </div>
  );
}

function sortFolders(boxes: Mailbox[]): Mailbox[] {
  return [...boxes].sort((a, b) => {
    const ia = ROLE_ORDER.indexOf(a.role ?? ""), ib = ROLE_ORDER.indexOf(b.role ?? "");
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib) || a.name.localeCompare(b.name);
  });
}

// layoutFolders devolve a ordem de exibição com a profundidade de aninhamento: pastas de sistema
// (role) primeiro na ordem canónica; depois as do utilizador em árvore por parentId (DFS por nome).
function layoutFolders(boxes: Mailbox[]): { f: Mailbox; depth: number }[] {
  const custom = boxes.filter((b) => !b.role);
  const customIds = new Set(custom.map((c) => c.id));
  const sys = boxes.filter((b) => b.role).sort((a, b) => {
    const ia = ROLE_ORDER.indexOf(a.role ?? ""), ib = ROLE_ORDER.indexOf(b.role ?? "");
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib);
  });
  const byParent = new Map<string | null, Mailbox[]>();
  for (const b of custom) {
    const p = b.parentId && customIds.has(b.parentId) ? b.parentId : null; // pai não-custom → raiz
    const arr = byParent.get(p) ?? [];
    arr.push(b);
    byParent.set(p, arr);
  }
  for (const arr of byParent.values()) arr.sort((a, b) => a.name.localeCompare(b.name));
  const out: { f: Mailbox; depth: number }[] = sys.map((f) => ({ f, depth: 0 }));
  const walk = (parent: string | null, depth: number) => {
    for (const c of byParent.get(parent) ?? []) { out.push({ f: c, depth }); walk(c.id, depth + 1); }
  };
  walk(null, 0);
  return out;
}

function folderLabel(t: (k: string, o?: Record<string, unknown>) => string, f?: Mailbox): string {
  if (!f) return "";
  return f.role ? t(`webmail.folders.${f.role}`, { defaultValue: f.name }) : f.name;
}
