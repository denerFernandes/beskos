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
import { displayName } from "./util";
import { IcFolder, IcChevron } from "../panel/icons";
import { MailAdvancedSearch } from "./MailAdvancedSearch";
import { sortFolders, folderLabel } from "./mailFolders";
import { useResizableListWidth } from "./useResizableListWidth";
import { useMailKeyboardNav } from "./useMailKeyboardNav";
import { MailSidebar } from "./MailSidebar";
import { MailListToolbar } from "./MailListToolbar";
import { MailSelectionBanners } from "./MailSelectionBanners";
import { MailListRow } from "./MailListRow";
import { MailEmptyFolderModal } from "./MailEmptyFolderModal";

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
  const { listW, resizing, startResize } = useResizableListWidth();
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
  // Opening/closing an email PRESERVES the query string (?p=2…): without this, clicking an email
  // on page 2 threw the list back to page 1.
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
  const [addrOpen, setAddrOpen] = useState(false);   // menu de caixas aberto
  // "Só por ler": o contador da pasta conta o que está na CAIXA, não o que cabe na 1.ª página — uma
  // não-lida antiga fica lá em baixo e o número parece inventado. Isto mostra-as, e só elas.
  const [unreadOnly, setUnreadOnly] = useState(false);
  // O filtro por endereço vira uma busca avançada limitada à pasta atual: para/cc com o endereço.
  const addrJmapFilter = useMemo(() => {
    if ((!addrFilter && !unreadOnly) || !activeFolder) return null;
    const conditions: Record<string, unknown>[] = [{ inMailbox: activeFolder }];
    if (addrFilter) conditions.push({ operator: "OR", conditions: [{ to: addrFilter }, { cc: addrFilter }] });
    if (unreadOnly) conditions.push({ notKeyword: "$seen" });
    return { operator: "AND", conditions } as Record<string, unknown>;
  }, [addrFilter, unreadOnly, activeFolder]);

  const listQ = useQuery({
    queryKey: ["emails", advFilter ? `adv:${JSON.stringify(advFilter)}` : search ? `s:${search}` : starred ? "starred" : labelId ? `label:${labelId}` : activeFolder, addrFilter, unreadOnly, page],
    queryFn: () => (advFilter ? svc.searchAdvanced(advFilter, position)
      : search ? svc.search(search, position)
      : starred ? svc.listStarred(position)
      : labelId ? svc.listByLabel(labelKeyword(labelId), position)
      // Sem colapso quando se pedem as não lidas: colapsada, a conversa mostra a mensagem mais
      // recente e o filtro deixava de encontrar a resposta antiga por ler.
      : addrJmapFilter ? svc.searchAdvanced(addrJmapFilter, position, 50, !unreadOnly)
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

  // The server counter can diverge from the actual content: a folder has shown "1 unread" with
  // nothing unread in it. When "unread only" mode runs, the query total is the REAL count — we
  // store it, and from then on it drives that folder's badge.
  const [realUnread, setRealUnread] = useState<Record<string, number>>({});
  useEffect(() => {
    if (!unreadOnly || !activeFolder || listQ.data === undefined || addrFilter) return;
    setRealUnread((v) => (v[activeFolder] === listQ.data!.total ? v : { ...v, [activeFolder]: listQ.data!.total }));
  }, [unreadOnly, activeFolder, addrFilter, listQ.data]);
  // unreadOf devolve o número a mostrar: a contagem real quando a temos, senão a do servidor.
  const unreadOf = (f: Mailbox) => realUnread[f.id] ?? f.unreadEmails;

  // Não-lidas que o filtro por endereço esconde: o contador da pasta conta a CAIXA toda, por isso
  // com um endereço escolhido pode dizer "1" sem que a lista mostre nenhuma por ler. Só se calcula
  // na 1.ª página — nas seguintes o que se vê não representa a pasta e o aviso seria enganador.
  const hiddenUnread = useMemo(() => {
    if (!addrFilter || page > 1 || search || starred || labelId) return 0;
    const fo = folders.find((f) => f.id === activeFolder);
    const folderUnread = fo ? (realUnread[fo.id] ?? fo.unreadEmails) : 0;
    if (folderUnread === 0) return 0;
    const visible = emails.filter((m) => !m.keywords?.$seen).length;
    return Math.max(0, folderUnread - visible);
  }, [addrFilter, page, search, starred, labelId, folders, activeFolder, emails, realUnread]);

  // Trocar de pasta/página/busca zera a seleção: um lote marcado numa lista já não visível
  // aplicaria ações a mensagens fora da vista.
  useEffect(() => { setSel(new Set()); }, [activeFolder, page, search, starred, labelId]);
  useEffect(() => { if (search || starred || labelId) setUnreadOnly(false); }, [search, starred, labelId]);

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

  useMailKeyboardNav({
    emails, focusIdx, setFocusIdx, focusedId, composersCount: composers.length, canArchive, trashId,
    archiveId, selected, openRow: (id, unread) => openRow(id, unread), archiveOne, moveOne, openEmail, toggleOne, t,
  });
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
    // A linha é uma conversa e abri-la mostra-a toda: marcar só a representante deixava as irmãs
    // por ler e a caixa a contá-las. Sem threadId (lista não colapsada) marca-se só esta.
    if (unread) {
      const m = emails.find((e) => e.id === id);
      const done = () => { void listQ.refetch(); void boxesQ.refetch(); };
      void (m?.threadId ? svc.markThreadRead(m.threadId) : svc.markRead(id, true)).then(done);
    }
  }

  const listTitle = search ? t("webmail.searchResults") : starred ? t("webmail.starred") : folderLabel(t, folders.find((f) => f.id === activeFolder));

  return (
    <div className={"mailgrid" + (rules ? " rules-mode" : "") + (selected ? " reading" : "")}
      style={{ "--listw": listW + "px" } as React.CSSProperties}>
      <MailSidebar
        folderMenu={folderMenu} setFolderMenu={setFolderMenu}
        onCompose={() => { if (rules) nav("/mail/mail"); openComposer({}); }}
        aliases={aliases} primary={addrQ.data?.primary ?? ""} addrFilter={addrFilter} setAddrFilter={setAddrFilter}
        addrOpen={addrOpen} setAddrOpen={setAddrOpen}
        boxes={boxesQ.data ?? []} activeFolder={activeFolder} search={search} starred={starred}
        dragId={dragId} dropOn={dropOn} setDropOn={setDropOn}
        onOpenFolder={openFolder} onDropInto={dropInto}
        unreadOf={unreadOf} unreadOnly={unreadOnly} setUnreadOnly={setUnreadOnly}
        renameFolderId={renameFolderId} setRenameFolderId={setRenameFolderId}
        folderInput={folderInput} setFolderInput={setFolderInput} onRenameFolder={renameFolder}
        kebabFolder={kebabFolder} setKebabFolder={setKebabFolder}
        subfolderOf={subfolderOf} setSubfolderOf={setSubfolderOf} onCreateSubfolder={createSubfolder}
        onDeleteFolder={deleteFolder}
        newFolder={newFolder} setNewFolder={setNewFolder} onCreateFolder={createFolder}
        labels={labelsQ.data ?? []} labelId={labelId}
        nav={nav} onOpenScheduled={() => setScheduledOpen(true)} rules={rules} t={t}
      />

      {rules ? <RulesView session={session} /> : (<>
      <section className="list">
        <div className={"list-resizer" + (resizing ? " dragging" : "")} onPointerDown={startResize}
          role="separator" aria-orientation="vertical" aria-label="resize" />
        <div className="mobtop">
          <button className="ib" onClick={() => setFolderMenu(true)} aria-label="menu"><IcFolder size={18} /></button>
          <b>{listTitle}</b>
        </div>
        {/* Barra de ações: seleciona-tudo, ações em lote, esvaziar (Lixo/Spam) e paginação — como
            o Gmail. Só mostra o que faz sentido no contexto. */}
        <MailListToolbar
          svc={svc} search={search} starred={starred} advFilter={advFilter} setAdvFilter={setAdvFilter}
          onRefresh={() => void listQ.refetch()} sel={sel} selIds={selIds} allOnPage={allOnPage} toggleAll={toggleAll}
          emails={emails} sortBy={sortBy} sortDir={sortDir} setSortBy={setSortBy} setSortDir={setSortDir}
          bulk={bulk} activeRole={activeRole} trashId={trashId} bulkMove={bulkMove} setBulkMove={setBulkMove}
          moveTargets={moveTargets} title={listTitle} total={total} onRequestEmpty={() => setConfirmEmpty(true)}
          page={page} position={position} goPage={goPage} t={t}
        />

        <MailSelectionBanners allOnPage={allOnPage} search={search} starred={starred} total={total} emails={emails}
          sel={sel} onSelectWholeFolder={() => void selectWholeFolder()} onClearSelection={() => setSel(new Set())} t={t} />

        {/* O contador da pasta é da CAIXA inteira; com um endereço escolhido, uma não-lida dirigida a
            outro endereço fica de fora da lista e o número parecia mentir. Diz-se o que se passa. */}
        {unreadOnly && (
          <div className="mlist-note">
            {t("webmail.unreadOnly")}{" "}
            <button onClick={() => setUnreadOnly(false)}>{t("webmail.showAllMessages")}</button>
          </div>
        )}
        {!unreadOnly && hiddenUnread > 0 && (
          <div className="mlist-note">
            {t("webmail.unreadElsewhere", { count: hiddenUnread })}{" "}
            <button onClick={() => setAddrFilter("")}>{t("webmail.accountsAll")}</button>
          </div>
        )}

        {listQ.isLoading ? <div className="loading">…</div>
          : emails.length === 0 ? <div className="empty">{t("webmail.emptyFolder")}</div>
          : emails.map((m) => (
            <MailListRow key={m.id} m={m} outgoing={outgoing} selected={selected} focusedId={focusedId}
              dragId={dragId} picked={sel.has(m.id)} locale={i18n.language} folderChip={folderChip}
              onOpen={openRow} onDragStart={setDragId} onDragEnd={() => { setDragId(null); setDropOn(null); }}
              onToggleOne={toggleOne} onToggleStar={toggleStar} t={t} />
          ))}
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
        <MailEmptyFolderModal total={total} onCancel={() => setConfirmEmpty(false)} onConfirm={() => void emptyNow()} t={t} />
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
