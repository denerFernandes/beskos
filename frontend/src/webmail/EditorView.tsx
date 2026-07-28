import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { backend } from "./backend";
import { toast } from "./toast";
import { isDedicatedWindow, closeSelfWindow } from "./nativeWindow";

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

// Editor de office (OnlyOffice Document Server) em tela cheia, SEM barra nossa por cima: fechar e
// renomear vivem no cabeçalho do próprio editor, que já leva a marca Beskos. Ambos só existem
// porque registamos os eventos aqui — é `onRequestClose` que faz nascer o X, e `onRequestRename`
// que torna o título editável (ver api.js: canRequestClose/canRename derivam dos eventos).
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
      editor.current = new window.DocsAPI.DocEditor(mount.current.id, {
        ...session.data!.config,
        events: {
          // X do cabeçalho: janela dedicada fecha-se; no separador normal, volta ao Drive.
          onRequestClose: () => { if (isDedicatedWindow()) closeSelfWindow(); else nav("/mail/files"); },
          // Título do cabeçalho passa a editável; o novo nome chega em `data`.
          onRequestRename: (e: { data?: string }) => {
            const name = String(e?.data ?? "").trim();
            if (!name) return;
            backend.renameFile(id!, name)
              .then(() => toast(t("files.renamed")))
              .catch((err) => toast((err as Error).message));
          },
        },
      });
    }).catch(() => { /* erro mostrado abaixo pela query em caso de falha de config */ });
    return () => {
      cancelled = true;
      try { editor.current?.destroyEditor?.(); } catch { /* noop */ }
      editor.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.data]);

  return (
    <div className="editor-full">
      <div className="editor-stage">
        {session.isLoading && <div className="editor-msg">{t("editor.loading")}</div>}
        {session.isError && <div className="editor-msg">{(session.error as Error).message}</div>}
        <div id="oo-editor" ref={mount} className="editor-frame" />
      </div>
    </div>
  );
}
