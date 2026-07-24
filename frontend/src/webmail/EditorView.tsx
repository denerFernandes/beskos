import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { backend } from "./backend";
import { toast } from "./toast";
import { isDedicatedWindow, closeSelfWindow } from "./nativeWindow";
import { triggerDownload } from "../apiBase";
import { IcX, IcPencil, IcDownload } from "../panel/icons";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
declare global { interface Window { DocsAPI?: any } }

// Carrega o api.js do Document Server uma vez (cacheia a Promise por URL).
const loaded = new Map<string, Promise<void>>();
function loadScript(src: string): Promise<void> {
  if (!loaded.has(src)) {
    loaded.set(src, new Promise((resolve, reject) => {
      const el = document.createElement("script");
      el.src = src; el.async = true;
      el.onload = () => resolve();
      el.onerror = () => reject(new Error("editor_script_failed"));
      document.body.appendChild(el);
    }));
  }
  return loaded.get(src)!;
}

// Editor de office (OnlyOffice Document Server) em tela cheia. Busca o config assinado, carrega
// o api.js e instancia o DocEditor. Voltar retorna ao Drive.
export function EditorView() {
  const { t, i18n } = useTranslation();
  const nav = useNavigate();
  const { id } = useParams<{ id: string }>();
  const mount = useRef<HTMLDivElement>(null);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const editor = useRef<any>(null);

  const themeNow = () => {
    const attr = document.documentElement.getAttribute("data-theme");
    if (attr) return attr;
    return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  };
  const session = useQuery({
    queryKey: ["edit-session", id],
    queryFn: () => backend.editSession(id!, i18n.language || "pt-BR", themeNow()),
    retry: false,
    enabled: !!id,
    refetchOnWindowFocus: false,
  });

  useEffect(() => {
    if (!session.data || !mount.current) return;
    let cancelled = false;
    loadScript(session.data.apiJs).then(() => {
      if (cancelled || !window.DocsAPI || !mount.current) return;
      editor.current = new window.DocsAPI.DocEditor(mount.current.id, session.data!.config);
    }).catch(() => { /* erro mostrado abaixo pela query em caso de falha de config */ });
    return () => {
      cancelled = true;
      try { editor.current?.destroyEditor?.(); } catch { /* noop */ }
      editor.current = null;
    };
  }, [session.data]);

  return (
    <div className="editor-full">
      <header className="editor-bar">
        {/* Mesmo padrão do visualizador (ViewerBar): título ao CENTRO; ações à DIREITA, com Fechar
            por último. Baixar → Fechar. */}
        {session.data && (
          <div className="eb-center">
            <EditorTitle id={id!} name={session.data.name} canWrite={session.data.canWrite} />
            <span className="editor-mode">{session.data.canWrite ? t("editor.editing") : t("editor.viewing")}</span>
          </div>
        )}
        <div className="eb-actions">
          {session.data && (
            <button className="chipbtn" onClick={() => triggerDownload(backend.downloadFileUrl(id!), session.data!.name)}>
              <IcDownload size={15} /> {t("files.download")}
            </button>
          )}
          <button className="chipbtn" onClick={() => { if (isDedicatedWindow()) closeSelfWindow(); else nav("/mail/files"); }}><IcX size={15} /> {t("editor.close")}</button>
        </div>
      </header>
      <div className="editor-stage">
        {session.isLoading && <div className="editor-msg">{t("editor.loading")}</div>}
        {session.isError && <div className="editor-msg">{(session.error as Error).message}</div>}
        <div id="oo-editor" ref={mount} className="editor-frame" />
      </div>
    </div>
  );
}

// Título editável no topo do editor (renomeia o arquivo; estilo Google Docs). Só o dono/editor
// pode renomear. O nome novo vale ao reabrir; o título interno do OnlyOffice não muda em tempo real.
function EditorTitle({ id, name, canWrite }: { id: string; name: string; canWrite: boolean }) {
  const { t } = useTranslation();
  const [cur, setCur] = useState(name); // nome exibido (atualiza ao renomear)
  const [editing, setEditing] = useState(false);
  const [val, setVal] = useState(name);
  const m = useMutation({
    mutationFn: (n: string) => backend.renameFile(id, n),
    onSuccess: (_r, n) => { setCur(n); setEditing(false); toast(t("files.renamed")); },
    onError: (e) => { setVal(cur); setEditing(false); toast((e as Error).message); },
  });
  const commit = () => { const n = val.trim(); if (n && n !== cur) m.mutate(n); else { setVal(cur); setEditing(false); } };

  if (!canWrite) return <span className="editor-title">{cur}</span>;
  return editing ? (
    <input className="editor-title-input" autoFocus value={val}
      onChange={(e) => setVal(e.target.value)} onBlur={commit}
      onKeyDown={(e) => { if (e.key === "Enter") commit(); if (e.key === "Escape") { setVal(cur); setEditing(false); } }} />
  ) : (
    <button className="editor-title editable" title={t("contacts.rename")} onClick={() => { setVal(cur); setEditing(true); }}>
      {cur} <IcPencil size={13} />
    </button>
  );
}
