import { useRef, useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, isOfficeFile, viewerKind, type StorageFile, type Folder } from "./backend";
import { triggerDownload } from "../apiBase";
import { openInWindow } from "./nativeWindow";
import { FileTypeIcon, fileCategory } from "./filetypes";
import { Avatar } from "./Avatar";
import { ViewerModal } from "./ViewerModal";
import { toast, toastAction } from "./toast";
import {
  IcUpload, IcFolder, IcFolderPlus, IcGrid, IcList, IcTrash, IcPencil, IcShare, IcDownload, IcUsers, IcPencilBox, IcPlus, IcRefresh, IcCopy, IcStar,
} from "../panel/icons";
import { ShareModal } from "./ShareModal";
import { VersionsModal } from "./VersionsModal";
import { useUserSettings, useSection } from "./useSettings";
import { syncApi, syncSupported } from "./syncClient";

type TypeF = "" | "doc" | "pdf" | "sheet" | "image" | "video" | "audio" | "code" | "archive";
type PeopleF = "" | "me" | "shared" | "with-me";
type ModF = "" | "today" | "7d" | "30d" | "year";

// Drive: pastas/subpastas, ícones por tipo, grade/lista, arrastar para mover, compartilhar,
// filtros (tipo/pessoas/modificado), coluna de proprietário e indicador de armazenamento.
export function FilesView({ search, folderParam, onFolder }: {
  search: string;
  /** Pasta aberta, vinda da URL (/mail/files/<id>) — não há estado local a duplicá-la. */
  folderParam?: string;
  onFolder: (id: string | null) => void;
}) {
  const { t, i18n } = useTranslation();
  const nav = useNavigate();
  const qc = useQueryClient();
  // A pasta atual VIVE na URL: recarregar, partilhar o link ou usar o botão "voltar" do browser
  // continuam na mesma pasta. `setFolderId` navega em vez de guardar estado.
  // "trash" é o único segmento reservado (os ids de pasta são UUID, nunca colidem).
  const trashOpen = folderParam === "trash";
  const folderId = trashOpen ? null : (folderParam ?? null);
  const setFolderId = onFolder;
  const setTrashOpen = (open: boolean) => onFolder(open ? "trash" : null);
  const [view, setView] = useState<"grid" | "list">("list");
  // Vista por defeito das Definições (settings.files.view): aplica uma vez ao carregar; depois o
  // utilizador controla via os botões da barra.
  const settings = useUserSettings();
  const filesCfg = useSection("files");
  const changeView = (v: "grid" | "list") => { setView(v); filesCfg.save({ view: v }); };
  // Pastas sincronizadas com o computador: destacadas na lista (só existe no app de desktop).
  const [syncedIds, setSyncedIds] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (!syncSupported()) return;
    let alive = true;
    const tick = () => {
      syncApi.status()
        .then((st) => { if (alive) setSyncedIds(new Set(st.pairs.map((p) => p.folder_id))); })
        .catch(() => {});
    };
    tick();
    const id = setInterval(tick, 3000);
    return () => { alive = false; clearInterval(id); };
  }, []);
  const addSyncFolder = async () => {
    try {
      const pair = await syncApi.addFolder();
      if (pair) toast(t("files.syncStarted", { name: pair.name }));
    } catch (e) { toast((e as Error).message); }
  };
  const viewApplied = useRef(false);
  useEffect(() => {
    if (!viewApplied.current && settings.files?.view) { viewApplied.current = true; setView(settings.files.view); }
  }, [settings.files?.view]);
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const [rename, setRename] = useState<{ kind: "file" | "folder"; id: string; name: string } | null>(null);
  const [share, setShare] = useState<StorageFile | null>(null);
  const [versionsOf, setVersionsOf] = useState<StorageFile | null>(null);
  const [viewing, setViewing] = useState<StorageFile | null>(null);
  const [newFolder, setNewFolder] = useState(false);
  const [folderName, setFolderName] = useState("");
  const [newMenu, setNewMenu] = useState(false);
  const [typeF, setTypeF] = useState<TypeF>("");
  const [peopleF, setPeopleF] = useState<PeopleF>("");
  const [modF, setModF] = useState<ModF>("");
  const input = useRef<HTMLInputElement>(null);
  const dragId = useRef<string | null>(null);
  const dragKind = useRef<"file" | "folder">("file"); // o que está a ser arrastado (move ficheiro vs pasta)

  const listing = useQuery({ queryKey: ["files", folderId], queryFn: () => backend.files(folderId ?? undefined), retry: false });
  // "Partilhado comigo": ficheiros que outras pessoas partilharam (fonte à parte, vista plana).
  const sharedWithMe = useQuery({ queryKey: ["files-shared"], queryFn: () => backend.sharedWithMe(), enabled: peopleF === "with-me" || folderId === null, retry: false, refetchInterval: 30000, refetchOnWindowFocus: true });
  // Busca global (recursiva): quando há termo, o backend procura em TODAS as pastas — não só a aberta.
  const searchTerm = search.trim();
  const searching = searchTerm.length > 0 && peopleF !== "with-me";
  const globalSearch = useQuery({ queryKey: ["files-search", searchTerm], queryFn: () => backend.searchFiles(searchTerm), enabled: searching, retry: false });
  const invalidate = () => { qc.invalidateQueries({ queryKey: ["files"] }); qc.invalidateQueries({ queryKey: ["files-shared"] }); };

  const [uploads, setUploads] = useState<{ name: string; pct: number }[]>([]); // barra de progresso por ficheiro
  const [osDrag, setOsDrag] = useState(false); // ficheiro do SO a ser arrastado sobre a área
  // Upload múltiplo, sequencial, com progresso. Falha de um não derruba os outros (toast por erro).
  async function uploadMany(files: File[]) {
    if (!files.length) return;
    setUploads(files.map((f) => ({ name: f.name, pct: 0 })));
    let ok = 0;
    for (let i = 0; i < files.length; i++) {
      try {
        await backend.uploadFileProgress(files[i], folderId ?? undefined, (p) =>
          setUploads((u) => u.map((x, j) => (j === i ? { ...x, pct: p } : x))));
        ok++;
      } catch (e) { toast((e as Error).message); }
      setUploads((u) => u.map((x, j) => (j === i ? { ...x, pct: 1 } : x)));
    }
    setUploads([]);
    invalidate();
    if (ok > 0) toast(ok === 1 ? t("files.uploaded") : t("files.uploadedN", { count: ok }));
  }
  const mkFolder = useMutation({ mutationFn: () => backend.createFolder(folderName, folderId ?? undefined), onSuccess: () => { setNewFolder(false); setFolderName(""); invalidate(); toast(t("files.folderCreated")); }, onError: (e) => toast((e as Error).message) });
  const move = useMutation({ mutationFn: ({ id, to }: { id: string; to: string | null }) => backend.moveFile(id, to), onSuccess: () => { invalidate(); toast(t("files.moved")); } });
  const moveFolder = useMutation({ mutationFn: ({ id, to }: { id: string; to: string | null }) => backend.moveFolder(id, to), onSuccess: () => { invalidate(); toast(t("files.moved")); }, onError: (e) => toast((e as Error).message) });
  const del = useMutation({ mutationFn: (id: string) => backend.deleteFile(id), onSuccess: () => { invalidate(); toast(t("files.movedToTrash")); } });
  // Duplicar: insere "(cópia)" antes da extensão (o backend reusa o blob — sem recopiar bytes).
  const copyName = (name: string) => { const dot = name.lastIndexOf("."); const suf = " " + t("files.copySuffix"); return dot > 0 ? name.slice(0, dot) + suf + name.slice(dot) : name + suf; };
  const dup = useMutation({ mutationFn: (f: StorageFile) => backend.copyFile(f.id, copyName(f.nome)), onSuccess: () => { invalidate(); toast(t("files.duplicated")); }, onError: (e) => toast((e as Error).message) });
  const star = useMutation({ mutationFn: ({ id, v }: { id: string; v: boolean }) => backend.starFile(id, v), onSuccess: () => invalidate(), onError: (e) => toast((e as Error).message) });
  const [sort, setSort] = useState<"name" | "date" | "size">("name");
  // Seleção múltipla + ação em lote (lixeira com desfazer). Limpa ao trocar de pasta.
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const toggleSel = (id: string) => setSelected((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n; });
  useEffect(() => { setSelected(new Set()); }, [folderId, trashOpen]);
  const batchTrash = useMutation({
    mutationFn: (ids: string[]) => backend.batchFiles(ids, "trash"),
    onSuccess: (r, ids) => { invalidate(); const arr = [...ids]; setSelected(new Set()); toastAction(t("files.batchTrashed", { count: r.done }), t("common.undo"), () => backend.batchFiles(arr, "restore").then(() => invalidate())); },
    onError: (e) => toast((e as Error).message),
  });
  const delFolder = useMutation({ mutationFn: (id: string) => backend.deleteFolder(id), onSuccess: () => invalidate() });
  // Lixeira: soft delete restaurável. A vista abre num painel próprio (setTrashOpen).
  const trash = useQuery({ queryKey: ["files-trash"], queryFn: () => backend.trashFiles(), enabled: trashOpen, retry: false });
  const invalidateTrash = () => { qc.invalidateQueries({ queryKey: ["files-trash"] }); qc.invalidateQueries({ queryKey: ["files"] }); };
  const restore = useMutation({ mutationFn: (id: string) => backend.restoreFile(id), onSuccess: () => { invalidateTrash(); toast(t("files.restored")); }, onError: (e) => toast((e as Error).message) });
  const purge = useMutation({ mutationFn: (id: string) => backend.purgeFile(id), onSuccess: () => { invalidateTrash(); toast(t("files.deletedForever")); }, onError: (e) => toast((e as Error).message) });
  const emptyTrash = useMutation({ mutationFn: () => backend.emptyTrash(), onSuccess: () => { invalidateTrash(); toast(t("files.trashEmptied")); }, onError: (e) => toast((e as Error).message) });
  const createOffice = useMutation({
    mutationFn: ({ kind, name }: { kind: "document" | "spreadsheet" | "presentation"; name: string }) =>
      backend.createOffice(kind, name, folderId ?? undefined),
    onSuccess: (f) => { setNewMenu(false); invalidate(); openEditor(f.id); },
    onError: (e) => toast((e as Error).message),
  });

  const fmtSize = (n: number) => n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`;
  const fmtDate = (iso: string) => new Date(iso).toLocaleDateString(i18n.language, { day: "2-digit", month: "short", year: "numeric" });
  const data = listing.data;

  const owner = data?.owner;
  const sharedSet = new Set((searching ? globalSearch.data?.shared : data?.shared) ?? []);
  const isShared = (id: string) => sharedSet.has(id);
  const filtering = typeF !== "" || peopleF !== "" || modF !== "";
  // Coluna "Local": na busca global o ficheiro pode estar em qualquer pasta, então não afirma a atual.
  const localName = searching ? t("files.searchAll") : data?.breadcrumb?.length ? data.breadcrumb[data.breadcrumb.length - 1].name : t("files.myFiles");

  function withinMod(iso: string): boolean {
    if (!modF) return true;
    const days = { today: 1, "7d": 7, "30d": 30, year: 365 }[modF];
    return (Date.now() - new Date(iso).getTime()) <= days * 86400_000;
  }
  const withMe = peopleF === "with-me";
  // Partilhados COMIGO aparecem na vista principal (raiz, sem filtro/busca) numa secção própria —
  // senão um ficheiro que alguém me partilha "não aparece no meu Drive" (24/07/2026).
  const sharedWithMeIds = new Set((sharedWithMe.data?.files ?? []).map((f) => f.id));
  const rootDefault = folderId === null && peopleF === "" && !searching && typeF === "" && modF === "";
  // Em "partilhado comigo" a fonte é outra (ficheiros de terceiros); ao buscar, a fonte é o resultado
  // GLOBAL do backend (todas as pastas); senão, os ficheiros da pasta aberta. O nome já vem filtrado
  // pelo servidor na busca, então aqui só aplicam-se os filtros de tipo/pessoa/data.
  const source = withMe ? (sharedWithMe.data?.files ?? []) : searching ? (globalSearch.data?.files ?? []) : (data?.files ?? []);
  const sharedExtra = rootDefault ? (sharedWithMe.data?.files ?? []) : [];
  const files = source.filter((f) =>
    (typeF === "" || fileCategory(f.nome) === typeF) &&
    (withMe || peopleF === "" || (peopleF === "shared" ? isShared(f.id) : !isShared(f.id))) &&
    withinMod(f.created_at))
    // Favoritos primeiro; depois pela coluna escolhida (nome/data/tamanho).
    .sort((a, b) => (Number(!!b.starred) - Number(!!a.starred)) ||
      (sort === "size" ? b.tamanho - a.tamanho
        : sort === "date" ? b.created_at.localeCompare(a.created_at)
          : a.nome.localeCompare(b.nome, undefined, { numeric: true })));
  // Ao buscar, as pastas vêm do backend (nome casado em qualquer nível); senão, as da pasta atual.
  const folders = searching ? (globalSearch.data?.folders ?? []) : (filtering ? [] : (data?.folders ?? []));

  function onDropTo(target: string | null) {
    const id = dragId.current;
    if (id && dragKind.current === "folder") {
      // Pasta em cima de si mesma não faz nada; o backend também barra ciclo/descendente.
      if (id !== target) moveFolder.mutate({ id, to: target });
    } else if (id) {
      move.mutate({ id, to: target });
    }
    dragId.current = null; setDropTarget(null);
  }
  // Editor Office: no app nativo (Tauri) abre numa janela dedicada; na web navega na própria SPA.
  function openEditor(id: string) {
    if (!openInWindow(`editor-${id}`, `/mail/files/edit/${id}`, "Beskos — Editor")) nav(`/mail/files/edit/${id}`);
  }
  // Office abre no editor; imagem/pdf/áudio/vídeo abrem no visualizador; os demais fazem download.
  function openFile(f: StorageFile) {
    if (isOfficeFile(f.nome)) openEditor(f.id);
    else if (viewerKind(f.nome)) setViewing(f);
    else triggerDownload(backend.downloadFileUrl(f.id), f.nome);
  }

  const ownerCell = (id: string) => (
    (withMe || sharedWithMeIds.has(id)) ? (
      <span className="owner-cell" title={t("files.sharedWithMe")}>
        <span className="shared-badge" title={t("files.sharedWithMe")}><IcUsers size={13} /></span>
        <span className="ot">{t("files.sharedWithMe")}</span>
      </span>
    ) : (
      <span className="owner-cell" title={owner?.email}>
        <Avatar name={owner?.name} email={owner?.email} size={22} className="mini-av" />
        <span className="ot">{t("files.youShort")}</span>
        {isShared(id) && <span className="shared-badge" title={t("files.sharedBadge")}><IcUsers size={13} /></span>}
      </span>
    )
  );

  const folderActions = (f: Folder) => (
    <span className="row-acts">
      <a title={t("files.downloadZip")} href={backend.folderZipUrl(f.id)} onClick={(e) => e.stopPropagation()}><button><IcDownload size={15} /></button></a>
      <button title={t("contacts.rename")} onClick={(e) => { e.stopPropagation(); setRename({ kind: "folder", id: f.id, name: f.name }); }}><IcPencil size={15} /></button>
      <button title={t("files.delete")} onClick={(e) => { e.stopPropagation(); delFolder.mutate(f.id); }}><IcTrash size={15} /></button>
    </span>
  );
  const fileActions = (f: StorageFile) => (
    <span className="row-acts">
      <button className={"star-btn" + (f.starred ? " on" : "")} title={t("files.star")} onClick={(e) => { e.stopPropagation(); star.mutate({ id: f.id, v: !f.starred }); }}><IcStar size={15} filled={f.starred} /></button>
      {isOfficeFile(f.nome) && <button title={t("editor.open")} onClick={(e) => { e.stopPropagation(); openEditor(f.id); }}><IcPencilBox size={15} /></button>}
      {isOfficeFile(f.nome) && <button title={t("files.versions")} onClick={(e) => { e.stopPropagation(); setVersionsOf(f); }}><IcRefresh size={15} /></button>}
      <button title={t("files.share")} onClick={(e) => { e.stopPropagation(); setShare(f); }}><IcShare size={15} /></button>
      <a title={t("files.download")} href={backend.downloadFileUrl(f.id)} onClick={(e) => e.stopPropagation()}><button><IcDownload size={15} /></button></a>
      <button title={t("files.duplicate")} onClick={(e) => { e.stopPropagation(); dup.mutate(f); }}><IcCopy size={15} /></button>
      <button title={t("contacts.rename")} onClick={(e) => { e.stopPropagation(); setRename({ kind: "file", id: f.id, name: f.nome }); }}><IcPencil size={15} /></button>
      <button title={t("files.delete")} onClick={(e) => { e.stopPropagation(); del.mutate(f.id); }}><IcTrash size={15} /></button>
    </span>
  );

  const chip = (label: string, value: string, set: (v: any) => void, opts: { v: string; label: string }[]) => (
    <div className={"filter-chip" + (value ? " on" : "")}>
      <select value={value} onChange={(e) => set(e.target.value)}>
        <option value="">{label}</option>
        {opts.map((o) => <option key={o.v} value={o.v}>{o.label}</option>)}
      </select>
    </div>
  );

  if (trashOpen) {
    const items = trash.data?.files ?? [];
    return (
      <div className="dv">
        <div className="dv-top">
          <div className="crumb">
            {/* Mesmo formato do caminho normal ("Arquivos › demo"): botões de texto, sem ícones —
                com o ícone dentro do botão o rótulo quebrava para a linha de baixo. */}
            <button onClick={() => setTrashOpen(false)}>{t("files.title")}</button>
            <span className="sep">›</span>
            <button className="cur">{t("files.trash")}</button>
          </div>
          <div className="dv-actions">
            {items.length > 0 && <button className="chipbtn danger" onClick={() => emptyTrash.mutate()}><IcTrash size={15} /> {t("files.emptyTrash")}</button>}
          </div>
        </div>
        <div className="dv-body">
          {trash.isLoading ? <div className="loading">…</div> : items.length === 0 ? (
            <div className="empty">{t("files.trashEmpty")}</div>
          ) : (
            <div className="dv-list">
              <div className="dv-head">
                <span className="col-name">{t("files.colName")}</span>
                <span className="col-owner" />
                <span className="col-loc" />
                <span className="col-mod">{t("files.colDeleted")}</span>
                <span className="col-size">{t("files.colSize")}</span>
                <span className="col-acts" />
              </div>
              {items.map((f) => (
                <div className="dvrow" key={f.id}>
                  <span className="col-name"><span className="ic"><FileTypeIcon name={f.nome} size={20} /></span><span className="fn">{f.nome}</span></span>
                  <span className="col-owner" />
                  <span className="col-loc" />
                  <span className="col-mod meta">{f.deleted_at ? fmtDate(f.deleted_at) : "—"}</span>
                  <span className="col-size meta">{fmtSize(f.tamanho)}</span>
                  <span className="col-acts">
                    <span className="row-acts">
                      <button title={t("files.restore")} onClick={() => restore.mutate(f.id)}><IcRefresh size={15} /></button>
                      <button title={t("files.deleteForever")} onClick={() => purge.mutate(f.id)}><IcTrash size={15} /></button>
                    </span>
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="dv">
      <div className="dv-top">
        <div className="crumb">
          <button className={dropTarget === "root" ? "drop" : (folderId === null ? "cur" : "")}
            onClick={() => setFolderId(null)}
            onDragOver={(e) => { e.preventDefault(); setDropTarget("root"); }} onDragLeave={() => setDropTarget(null)} onDrop={() => onDropTo(null)}>
            {t("files.title")}
          </button>
          {(data?.breadcrumb ?? []).map((b, i, arr) => (
            <span key={b.id} style={{ display: "flex", alignItems: "center", gap: 5 }}>
              <span className="sep">›</span>
              <button className={i === arr.length - 1 ? "cur" : (dropTarget === b.id ? "drop" : "")}
                onClick={() => setFolderId(b.id)}
                onDragOver={(e) => { e.preventDefault(); setDropTarget(b.id); }} onDragLeave={() => setDropTarget(null)} onDrop={() => onDropTo(b.id)}>{b.name}</button>
            </span>
          ))}
        </div>
        <div className="dv-actions">
          {data?.quota && <StorageBar used={data.quota.used} max={data.quota.max} fmt={fmtSize} label={t("files.storageUsed", { used: fmtSize(data.quota.used), max: fmtSize(data.quota.max) })} />}
          <div className="filter-chip">
            <select value={sort} onChange={(e) => setSort(e.target.value as "name" | "date" | "size")} title={t("files.sortBy")}>
              <option value="name">{t("files.sortName")}</option>
              <option value="date">{t("files.sortDate")}</option>
              <option value="size">{t("files.sortSize")}</option>
            </select>
          </div>
          <div className="viewtoggle">
            <button className={view === "list" ? "on" : ""} title={t("files.list")} onClick={() => changeView("list")}><IcList size={16} /></button>
            <button className={view === "grid" ? "on" : ""} title={t("files.grid")} onClick={() => changeView("grid")}><IcGrid size={16} /></button>
          </div>
          <div className="new-wrap">
            <button className="chipbtn primary" onClick={() => setNewMenu((v) => !v)}><IcPlus size={15} /> {t("files.new")}</button>
            {newMenu && (
              <>
                <div className="menu-backdrop" onClick={() => setNewMenu(false)} />
                <div className="new-menu">
                  <button onClick={() => createOffice.mutate({ kind: "document", name: t("files.untitledDoc") })}><span className="ic"><FileTypeIcon name="a.docx" size={18} /></span>{t("files.newDoc")}</button>
                  <button onClick={() => createOffice.mutate({ kind: "spreadsheet", name: t("files.untitledSheet") })}><span className="ic"><FileTypeIcon name="a.xlsx" size={18} /></span>{t("files.newSheet")}</button>
                  <button onClick={() => createOffice.mutate({ kind: "presentation", name: t("files.untitledSlide") })}><span className="ic"><FileTypeIcon name="a.pptx" size={18} /></span>{t("files.newSlide")}</button>
                </div>
              </>
            )}
          </div>
          <button className="chipbtn" onClick={() => setNewFolder(true)}><IcFolderPlus size={15} /> {t("files.newFolder")}</button>
          {syncSupported() && (
            <button className="chipbtn" title={t("files.syncFolderHint")} onClick={() => void addSyncFolder()}>
              <IcRefresh size={15} /> {t("files.syncFolder")}
            </button>
          )}
          <button className="chipbtn" onClick={() => input.current?.click()}><IcUpload size={15} /> {t("files.upload")}</button>
          <button className="chipbtn" title={t("files.trash")} onClick={() => setTrashOpen(true)}><IcTrash size={15} /> {t("files.trash")}</button>
          <input ref={input} type="file" hidden multiple onChange={(e) => { const fs = e.target.files; if (fs?.length) uploadMany(Array.from(fs)); e.target.value = ""; }} />
        </div>
      </div>

      <div className="dv-filters">
        {chip(t("files.filterType"), typeF, setTypeF, [
          { v: "doc", label: t("files.typeDoc") }, { v: "pdf", label: "PDF" }, { v: "sheet", label: t("files.typeSheet") },
          { v: "image", label: t("files.typeImage") }, { v: "video", label: t("files.typeVideo") },
          { v: "audio", label: t("files.typeAudio") }, { v: "code", label: t("files.typeCode") }, { v: "archive", label: t("files.typeArchive") },
        ])}
        {chip(t("files.filterPeople"), peopleF, setPeopleF, [
          { v: "me", label: t("files.ownedByMe") }, { v: "shared", label: t("files.sharedByMe") },
          { v: "with-me", label: t("files.sharedWithMe") },
        ])}
        {chip(t("files.filterModified"), modF, setModF, [
          { v: "today", label: t("files.modToday") }, { v: "7d", label: t("files.mod7d") },
          { v: "30d", label: t("files.mod30d") }, { v: "year", label: t("files.modYear") },
        ])}
        {filtering && <button className="filter-clear" onClick={() => { setTypeF(""); setPeopleF(""); setModF(""); }}>{t("files.clearFilters")}</button>}
      </div>

      <div className={"dv-body" + (osDrag ? " os-drag" : "")}
        onDragOver={(e) => { if (e.dataTransfer.types.includes("Files")) { e.preventDefault(); setOsDrag(true); } }}
        onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setOsDrag(false); }}
        onDrop={(e) => { const fs = e.dataTransfer.files; if (fs?.length) { e.preventDefault(); setOsDrag(false); uploadMany(Array.from(fs)); } }}>
        {osDrag && <div className="dv-dropzone"><IcUpload size={30} /><span>{t("files.dropHere")}</span></div>}
        {uploads.length > 0 && (
          <div className="upload-progress">
            {uploads.map((u, i) => (
              <div className="up-item" key={i}>
                <span className="up-name">{u.name}</span>
                <span className="up-bar"><span className="up-fill" style={{ width: `${Math.round(u.pct * 100)}%` }} /></span>
              </div>
            ))}
          </div>
        )}
        {selected.size > 0 && (
          <div className="batch-bar">
            <span className="bb-count">{t("files.selected", { count: selected.size })}</span>
            <button className="chipbtn danger" onClick={() => batchTrash.mutate([...selected])} disabled={batchTrash.isPending}><IcTrash size={14} /> {t("files.delete")}</button>
            <button className="chipbtn" onClick={() => setSelected(new Set())}>{t("files.clearSel")}</button>
          </div>
        )}
        {listing.isLoading ? <div className="loading">…</div> : (
          <>
            {newFolder && (
              <div className="share-add" style={{ maxWidth: 360 }}>
                <input autoFocus className="pnl-input" style={{ border: "1px solid var(--accent)", borderRadius: 9, padding: "8px 11px", background: "var(--paper)", color: "var(--ink)" }}
                  value={folderName} placeholder={t("files.folderName")} onChange={(e) => setFolderName(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter" && folderName) mkFolder.mutate(); if (e.key === "Escape") setNewFolder(false); }} />
                <button className="btn-send send" onClick={() => mkFolder.mutate()} disabled={!folderName}>{t("contacts.save")}</button>
              </div>
            )}

            {folders.length === 0 && files.length === 0 && sharedExtra.length === 0 && !newFolder ? (
              <div className="empty">{filtering ? t("files.noMatch") : t("files.empty")}</div>
            ) : view === "grid" ? (
              <>
                {folders.length > 0 && <div className="dv-sec">{t("files.folders")}</div>}
                <div className="dv-grid">
                  {folders.map((f) => (
                    <div className={"dvc" + (dropTarget === f.id ? " drop" : "") + (syncedIds.has(f.id) ? " synced" : "")} key={f.id} draggable onClick={() => setFolderId(f.id)}
                      onDragStart={(e) => { dragId.current = f.id; dragKind.current = "folder"; e.currentTarget.classList.add("dragging"); }}
                      onDragEnd={(e) => e.currentTarget.classList.remove("dragging")}
                      onDragOver={(e) => { e.preventDefault(); setDropTarget(f.id); }} onDragLeave={() => setDropTarget(null)} onDrop={() => onDropTo(f.id)}>
                      <span className="ic"><IcFolder size={26} /></span>
                      <div className="fn">{f.name}
                        {syncedIds.has(f.id) && <span className="sync-badge" title={t("files.syncedFolder")}><IcRefresh size={11} /></span>}
                      </div>
                      {folderActions(f)}
                    </div>
                  ))}
                </div>
                {files.length > 0 && <div className="dv-sec">{t("files.filesLabel")}</div>}
                <div className="dv-grid">
                  {files.map((f) => (
                    <div className="dvc" key={f.id} draggable
                      onDragStart={(e) => { dragId.current = f.id; dragKind.current = "file"; e.currentTarget.classList.add("dragging"); }}
                      onDragEnd={(e) => e.currentTarget.classList.remove("dragging")}
                      onClick={() => openFile(f)}>
                      <span className="ic">
                        <Thumb file={f} size={26} />
                        {f.starred && <span className="star-badge" title={t("files.star")}><IcStar size={12} filled /></span>}
                        {(isShared(f.id) || sharedWithMeIds.has(f.id)) && <span className="shared-badge fl" title={sharedWithMeIds.has(f.id) ? t("files.sharedWithMe") : t("files.sharedBadge")}><IcUsers size={12} /></span>}
                      </span>
                      <div className="fn">{f.nome}</div>
                      <div className="meta">{fmtSize(f.tamanho)}{f.versao > 1 ? ` · v${f.versao}` : ""}</div>
                      {fileActions(f)}
                    </div>
                  ))}
                </div>
                {sharedExtra.length > 0 && (
                  <>
                    <div className="dv-sec">{t("files.sharedWithMe")}</div>
                    <div className="dv-grid">
                      {sharedExtra.map((f) => (
                        <div className="dvc" key={"sh" + f.id} onClick={() => openFile(f)}>
                          <span className="ic">
                            <Thumb file={f} size={26} />
                            <span className="shared-badge fl" title={t("files.sharedWithMe")}><IcUsers size={12} /></span>
                          </span>
                          <div className="fn">{f.nome}</div>
                          <div className="meta">{fmtSize(f.tamanho)}{f.versao > 1 ? ` · v${f.versao}` : ""}</div>
                          <span className="row-acts">
                            <a title={t("files.download")} href={backend.downloadFileUrl(f.id)} onClick={(e) => e.stopPropagation()}><button><IcDownload size={15} /></button></a>
                          </span>
                        </div>
                      ))}
                    </div>
                  </>
                )}
              </>
            ) : (
              <div className="dv-list">
                <div className="dv-head">
                  <span className="col-name">{t("files.colName")}</span>
                  <span className="col-owner">{t("files.colOwner")}</span>
                  <span className="col-loc">{t("files.colLocation")}</span>
                  <span className="col-mod">{t("files.colModified")}</span>
                  <span className="col-size">{t("files.colSize")}</span>
                  <span className="col-acts" />
                </div>
                {/* Rótulos de grupo: deixam explícito que pastas e ficheiros são irmãos na MESMA
                    pasta, e não uma árvore com os ficheiros dentro da última pasta listada. */}
                {folders.length > 0 && files.length > 0 && <div className="dv-group">{t("files.folders")}</div>}
                {folders.map((f) => (
                  <div className={"dvrow" + (dropTarget === f.id ? " drop" : "") + (syncedIds.has(f.id) ? " synced" : "")} key={f.id} draggable onClick={() => setFolderId(f.id)}
                    onDragStart={(e) => { dragId.current = f.id; dragKind.current = "folder"; e.currentTarget.classList.add("dragging"); }}
                    onDragEnd={(e) => e.currentTarget.classList.remove("dragging")}
                    onDragOver={(e) => { e.preventDefault(); setDropTarget(f.id); }} onDragLeave={() => setDropTarget(null)} onDrop={() => onDropTo(f.id)}>
                    <span className="col-name">
                      {/* Ocupa o lugar da checkbox das linhas de ficheiro, para os nomes alinharem. */}
                      <span className="row-sel-gap" aria-hidden="true" />
                      <span className="ic"><IcFolder size={20} /></span><span className="fn">{f.name}</span>
                      {syncedIds.has(f.id) && <span className="sync-badge" title={t("files.syncedFolder")}><IcRefresh size={11} /></span>}
                    </span>
                    <span className="col-owner">{ownerCell("")}</span>
                    <span className="col-loc meta">{localName}</span>
                    <span className="col-mod meta">{fmtDate(f.created_at)}</span>
                    <span className="col-size meta">—</span>
                    <span className="col-acts">{folderActions(f)}</span>
                  </div>
                ))}
                {folders.length > 0 && files.length > 0 && <div className="dv-group">{t("files.sectionFiles")}</div>}
                {files.map((f) => (
                  <div className={"dvrow" + (selected.has(f.id) ? " sel" : "")} key={f.id} draggable
                    onDragStart={(e) => { dragId.current = f.id; dragKind.current = "file"; e.currentTarget.classList.add("dragging"); }}
                    onDragEnd={(e) => e.currentTarget.classList.remove("dragging")}
                    onClick={() => openFile(f)}>
                    <span className="col-name">
                      <input type="checkbox" className="row-sel" checked={selected.has(f.id)} onClick={(e) => e.stopPropagation()} onChange={() => toggleSel(f.id)} title={t("files.select")} />
                      <span className="ic"><FileTypeIcon name={f.nome} size={20} /></span><span className="fn">{f.nome}</span>
                    </span>
                    <span className="col-owner">{ownerCell(f.id)}</span>
                    <span className="col-loc meta">{localName}</span>
                    <span className="col-mod meta">{fmtDate(f.created_at)}</span>
                    <span className="col-size meta">{fmtSize(f.tamanho)}</span>
                    <span className="col-acts">{fileActions(f)}</span>
                  </div>
                ))}
                {sharedExtra.length > 0 && (
                  <>
                    <div className="dv-group">{t("files.sharedWithMe")}</div>
                    {sharedExtra.map((f) => (
                      <div className="dvrow" key={"sh" + f.id} onClick={() => openFile(f)}>
                        <span className="col-name">
                          <span className="row-sel-gap" aria-hidden="true" />
                          <span className="ic"><FileTypeIcon name={f.nome} size={20} /></span><span className="fn">{f.nome}</span>
                        </span>
                        <span className="col-owner">{ownerCell(f.id)}</span>
                        <span className="col-loc meta">{localName}</span>
                        <span className="col-mod meta">{fmtDate(f.created_at)}</span>
                        <span className="col-size meta">{fmtSize(f.tamanho)}</span>
                        <span className="col-acts">
                          <span className="row-acts">
                            <a title={t("files.download")} href={backend.downloadFileUrl(f.id)} onClick={(e) => e.stopPropagation()}><button><IcDownload size={15} /></button></a>
                          </span>
                        </span>
                      </div>
                    ))}
                  </>
                )}
              </div>
            )}
          </>
        )}
      </div>

      {rename && <RenameModal item={rename} onClose={() => setRename(null)} onDone={() => { setRename(null); invalidate(); }} />}
      {share && <ShareModal file={share} onClose={() => setShare(null)} />}
      {versionsOf && <VersionsModal file={versionsOf} onClose={() => setVersionsOf(null)} />}
      {viewing && <ViewerModal file={viewing} onClose={() => setViewing(null)} />}
    </div>
  );
}

// Thumb: miniatura real para imagens (endpoint /thumb); cai no ícone por tipo se não for imagem
// ou se a miniatura falhar (ex.: webp/svg que a stdlib não decodifica → 415).
function Thumb({ file, size }: { file: StorageFile; size: number }) {
  const [failed, setFailed] = useState(false);
  if (fileCategory(file.nome) === "image" && !failed) {
    return <img className="fthumb" src={backend.fileThumbUrl(file.id)} alt="" loading="lazy" onError={() => setFailed(true)} />;
  }
  return <FileTypeIcon name={file.nome} size={size} />;
}

function StorageBar({ used, max, label }: { used: number; max: number; fmt: (n: number) => string; label: string }) {
  const pct = max > 0 ? Math.min(100, Math.round((used / max) * 100)) : 0;
  return (
    <div className="storage-bar" title={label}>
      <div className="sb-track"><div className="sb-fill" style={{ width: `${pct}%` }} /></div>
      <span className="sb-label">{label}</span>
    </div>
  );
}

function RenameModal({ item, onClose, onDone }: { item: { kind: "file" | "folder"; id: string; name: string }; onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState(item.name);
  const m = useMutation({
    mutationFn: () => item.kind === "folder" ? backend.renameFolder(item.id, name) : backend.renameFile(item.id, name),
    onSuccess: () => { onDone(); toast(t("files.renamed")); }, onError: (e) => toast((e as Error).message),
  });
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>{t("contacts.rename")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body"><input autoFocus className="prompt" style={{ minHeight: "auto" }} value={name} onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && name) m.mutate(); }} /></div>
        <footer><button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          <button className="add" disabled={!name || m.isPending} onClick={() => m.mutate()}>{t("contacts.save")}</button></footer>
      </div>
    </div>
  );
}
