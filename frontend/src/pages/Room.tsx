import "../webmail/webmail.css";
import { useEffect, useRef, useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useMutation } from "@tanstack/react-query";
import { useWebmail } from "../webmail/auth";
import { WebmailLogin } from "../webmail/WebmailLogin";
import { MeetRoom } from "../webmail/MeetRoom";
import { applyBrandColor, cachedBrand } from "../webmail/branding";
import { closeSelfWindow } from "../webmail/nativeWindow";
import { ToastHost } from "../webmail/toast";
import { backend, type MeetSession } from "../webmail/backend";

// Rota /room/:room — janela dedicada da sala: SÓ a chamada, sem rail de apps nem topbar.
// É para cá que o webmail e o chat abrem uma nova janela ao entrar numa reunião.
export function RoomPage() {
  const { room } = useParams<{ room: string }>();
  const [params] = useSearchParams();
  const audioOnly = params.get("audio") === "1";
  const { session } = useWebmail();
  const { t } = useTranslation();
  const [call, setCall] = useState<MeetSession | null>(null);
  const joined = useRef(false);
  // Janela dedicada da sala: aplica a cor da marca da organização (do cache) — o Meet segue o
  // branding como o resto da app.
  useEffect(() => { applyBrandColor(cachedBrand()?.color ?? ""); }, []);

  const join = useMutation({
    mutationFn: () => backend.meetJoin(room),
    onSuccess: setCall,
  });

  useEffect(() => {
    if (session && room && !joined.current) { joined.current = true; join.mutate(); }
  }, [session, room, join]);

  // Sala trancada ("organizador entra primeiro") ou cheia (limite do plano): tenta de novo sozinho
  // até abrir/liberar uma vaga. Sem isto o convidado teria de recarregar para descobrir que já pode.
  useEffect(() => {
    if (!call?.locked && !call?.full) return;
    const id = setInterval(() => join.mutate(), 5000);
    return () => clearInterval(id);
  }, [call?.locked, call?.full, join]);

  if (!session) return <WebmailLogin />;

  return (
    <div className="wm room-only">
      {call?.locked
        ? <div className="room-wait lobby">
            <b>{t("meet.waitingHostTitle")}</b>
            <span>{t("meet.waitingHost", { title: call.title ?? "" })}</span>
          </div>
        : call?.full
          ? <div className="room-wait lobby">
              <b>{t("meet.fullTitle")}</b>
              <span>{t("meet.fullBody", { limit: call.limit ?? 0 })}</span>
            </div>
          : call
            ? <MeetRoom session={call} audioOnly={audioOnly} onLeave={() => closeSelfWindow()} />
            : <div className="room-wait">{join.isError ? (join.error as Error).message : t("meet.joining")}</div>}
      <ToastHost />
    </div>
  );
}
