import "../webmail/webmail.css";
import "./guest.css";
import { useState, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { LiveKitRoom, VideoConference } from "@livekit/components-react";
import "@livekit/components-styles";
import { backend, type GuestJoin } from "../webmail/backend";
import { MeetEffects } from "../webmail/MeetEffects";
import { PresentationLayer } from "../webmail/MeetPresentation";
import { applyBrandColor } from "../webmail/branding";
import { ToastHost, toast } from "../webmail/toast";
import { IcVideo } from "../panel/icons";

// Página pública de entrada em reunião (convidado externo, SEM login). Sala pública entra direto;
// sala por aprovação fica aguardando o anfitrião admitir. Veste a marca da organização, deteta o
// idioma do browser (i18next LanguageDetector) e deixa trocá-lo; assina com "Powered by Beskos".
export function GuestPage() {
  const { t, i18n } = useTranslation();
  const { room } = useParams<{ room: string }>();
  const [name, setName] = useState("");
  const [session, setSession] = useState<GuestJoin | null>(null);
  const [waiting, setWaiting] = useState<string | null>(null); // lobby_id
  const [denied, setDenied] = useState(false);
  const [busy, setBusy] = useState(false);
  const poll = useRef<number | null>(null);

  const info = useQuery({ queryKey: ["public-meet", room], queryFn: () => backend.publicMeetInfo(room!), retry: false, enabled: !!room });
  // Marca da organização (cor) aplicada à página; volta ao default ao sair.
  useEffect(() => { if (info.data?.brand?.color) applyBrandColor(info.data.brand.color); return () => applyBrandColor(""); }, [info.data?.brand?.color]);

  async function join() {
    if (!name.trim() || !room) return;
    setBusy(true);
    try {
      const r = await backend.publicMeetJoin(room, name.trim());
      if (r.mode === "joined") setSession(r);
      else if (r.mode === "lobby" && r.lobby_id) setWaiting(r.lobby_id);
    } catch (e) { toast((e as Error).message); }
    setBusy(false);
  }

  // Aguardando aprovação: consulta o lobby até ser admitido (ou negado).
  useEffect(() => {
    if (!waiting) return;
    const tick = async () => {
      try {
        const r = await backend.publicLobbyPoll(waiting);
        if (r.status === "admitted" && r.token) { setSession(r); setWaiting(null); }
        else if (r.status === "denied") { setDenied(true); setWaiting(null); }
      } catch { /* segue tentando */ }
    };
    poll.current = window.setInterval(tick, 3000);
    return () => { if (poll.current) clearInterval(poll.current); };
  }, [waiting]);

  if (session?.token && session.url) {
    return (
      <div className="wm">
        <div className="meet-call" style={{ position: "fixed", inset: 0 }}>
          <LiveKitRoom serverUrl={session.url} token={session.token} connect video audio
            onDisconnected={() => setSession(null)} data-lk-theme="default" style={{ height: "100%" }}>
            <VideoConference />
            <MeetEffects />
            <PresentationLayer room={room!} viewerOnly />
          </LiveKitRoom>
        </div>
        <ToastHost />
      </div>
    );
  }

  const brand = info.data?.brand;
  const title = info.data?.title || t("meet.title");
  return (
    <div className="wm">
      <div className="gp">
        <div className="gp-card">
          <LangSwitch />
          {brand?.logo
            ? <img className={"gp-logo" + (brand.logo_white_dark ? " white-dark" : "")} src={brand.logo} alt={info.data?.org || ""} />
            : <span className="gp-badge"><IcVideo size={26} /></span>}
          {info.data?.org && <div className="gp-org">{info.data.org}</div>}
          <h1 className="gp-title">{title}</h1>

          {denied ? (
            <p className="gp-msg">{t("meet.denied")}</p>
          ) : waiting ? (
            <div className="gp-waiting">
              <span className="gp-spin" />
              <p className="gp-msg">{t("meet.waitingHost", { title })}</p>
            </div>
          ) : (
            <>
              <p className="gp-sub">{info.data?.access === "approval" ? t("meet.approvalHint") : t("meet.publicHint")}</p>
              <input className="gp-input" autoFocus value={name} placeholder={t("meet.yourName")}
                onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") join(); }} />
              <button className="gp-join" disabled={!name.trim() || busy} onClick={join}>
                {busy ? "…" : <><IcVideo size={17} /> {t("meet.enter")}</>}
              </button>
            </>
          )}
          <a className="gp-powered" href="https://beskos.com" target="_blank" rel="noreferrer">
            Powered by <b>Beskos</b>
          </a>
        </div>
      </div>
      <ToastHost />
    </div>
  );

  function LangSwitch() {
    const cur = i18n.language.startsWith("pt") ? "pt-BR" : "en";
    return (
      <div className="gp-lang">
        {([["pt-BR", "PT"], ["en", "EN"]] as const).map(([lng, label]) => (
          <button key={lng} className={cur === lng ? "on" : ""} onClick={() => i18n.changeLanguage(lng)}>{label}</button>
        ))}
      </div>
    );
  }
}
