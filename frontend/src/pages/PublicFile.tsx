import "../webmail/webmail.css";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router-dom";
import { apiUrl } from "../apiBase";

// Página pública: reusa os estilos do webmail (variáveis de tema, editor em tela cheia) embrulhando
// em .wm — mas SEM sessão nem shell, porque quem abre um link público não tem conta.
export function PublicFilePage() {
  return <div className="wm"><PublicFileView /></div>;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
declare global { interface Window { DocsAPI?: any } }

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

interface Brand { name?: string; color?: string; logo?: string }
interface Session {
  kind: "office" | "file";
  name: string;
  allow_download: boolean;
  canWrite?: boolean;
  apiJs?: string;
  brand?: Brand;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  config?: any;
}

// PublicBar é a barra de topo do visualizador público: marca da organização (logo + nome) e o modo
// (ver/editar), a dizer a quem abre — anónimo, de fora — de quem é o ficheiro. É o "cabeçalho" que
// falta a uma sessão sem conta.
function PublicBar({ brand, name, canWrite }: { brand?: Brand; name: string; canWrite?: boolean }) {
  const { t } = useTranslation();
  return (
    <header className="pub-bar" style={brand?.color ? { borderColor: brand.color } : undefined}>
      <div className="pub-brand">
        {brand?.logo ? <img src={brand.logo} alt="" className="pub-logo" /> : <span className="pub-logo-txt">{(brand?.name || "Beskos").slice(0, 1)}</span>}
        <span className="pub-brand-name">{brand?.name || "Beskos"}</span>
      </div>
      <span className="pub-file-name">{name}</span>
      <span className="pub-mode">{canWrite ? t("editor.editing") : t("editor.viewing")}</span>
    </header>
  );
}

// PublicFilePage abre um ficheiro partilhado por link PÚBLICO (sem login). Para office, monta o
// OnlyOffice em sessão anónima; para outros tipos, mostra descarga (se permitida). Pede senha quando
// o link a exige. Vive fora do webmail: quem abre não tem conta.
function PublicFileView() {
  const { t, i18n } = useTranslation();
  const { token } = useParams<{ token: string }>();
  const mount = useRef<HTMLDivElement>(null);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const editor = useRef<any>(null);

  const [session, setSession] = useState<Session | null>(null);
  const [needPassword, setNeedPassword] = useState(false);
  const [password, setPassword] = useState("");
  const [pw, setPw] = useState("");        // senha confirmada (só muda ao submeter)
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  // Nome de convidado aleatório e traduzido, estável durante a sessão (uma pessoa, um nome).
  const guest = useMemo(() => `${t("public.guest")} ${1000 + Math.floor(Math.random() * 9000)}`, [t]);

  const theme = () => (document.documentElement.getAttribute("data-theme")
    ?? (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"));

  useEffect(() => {
    if (!token) return;
    let cancelled = false;
    setLoading(true); setError("");
    const q = new URLSearchParams({ guest, lang: i18n.language || "pt-BR", theme: theme() });
    if (pw) q.set("pw", pw);
    fetch(apiUrl(`/api/public/files/${token}/session?${q.toString()}`))
      .then(async (res) => {
        if (cancelled) return;
        if (res.status === 401) { setNeedPassword(true); setLoading(false); if (pw) setError(t("public.wrongPassword")); return; }
        if (!res.ok) { setError(t("public.unavailable")); setLoading(false); return; }
        const data = (await res.json()) as Session;
        setNeedPassword(false); setSession(data); setLoading(false);
      })
      .catch(() => { if (!cancelled) { setError(t("public.unavailable")); setLoading(false); } });
    return () => { cancelled = true; };
  }, [token, pw, guest, i18n.language, t]);

  useEffect(() => {
    if (!session || session.kind !== "office" || !mount.current) return;
    let cancelled = false;
    loadScript(session.apiJs!).then(() => {
      if (cancelled || !window.DocsAPI || !mount.current) return;
      editor.current = new window.DocsAPI.DocEditor(mount.current.id, session.config);
    }).catch(() => setError(t("public.unavailable")));
    return () => {
      cancelled = true;
      try { editor.current?.destroyEditor?.(); } catch { /* noop */ }
      editor.current = null;
    };
  }, [session, t]);

  if (needPassword) {
    return (
      <div className="pub-gate">
        <div className="pub-card">
          <h2>{t("public.passwordTitle")}</h2>
          <p>{t("public.passwordHint")}</p>
          <input type="password" autoFocus value={password} placeholder={t("files.linkPassword")}
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && password) setPw(password); }} />
          {error && <div className="pub-err">{error}</div>}
          <button className="add" disabled={!password} onClick={() => setPw(password)}>{t("public.open")}</button>
        </div>
      </div>
    );
  }

  if (loading) return <div className="pub-gate"><div className="pub-msg">{t("editor.loading")}</div></div>;
  if (error && !session) return <div className="pub-gate"><div className="pub-msg">{error}</div></div>;

  // Ficheiro não-office: descarga (se permitida) ou aviso de que não há pré-visualização.
  if (session?.kind === "file") {
    const dl = apiUrl(`/api/public/files/${token}/download${pw ? `?pw=${encodeURIComponent(pw)}` : ""}`);
    return (
      <div className="pub-gate">
        <div className="pub-card">
          <h2>{session.name}</h2>
          {session.allow_download
            ? <a className="add" href={dl}>{t("public.download")}</a>
            : <p>{t("public.noPreview")}</p>}
        </div>
      </div>
    );
  }

  // Office: barra de marca no topo + OnlyOffice a ocupar o resto.
  return (
    <div className="editor-full">
      <PublicBar brand={session?.brand} name={session?.name ?? ""} canWrite={session?.canWrite} />
      <div className="editor-stage">
        <div id="oo-editor" ref={mount} className="editor-frame" />
      </div>
    </div>
  );
}
