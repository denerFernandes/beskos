import { useState, useRef, useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { LiveKitRoom, VideoConference, PreJoin, useRoomContext, type LocalUserChoices } from "@livekit/components-react";
import "@livekit/components-styles";
import { MeetEffects } from "./MeetEffects";
import { MeetReactions } from "./MeetReactions";
import { PresentationLayer } from "./MeetPresentation";
import { ControlBarSlot } from "./MeetBarSlot";
import { NotesPanel } from "./MeetNotes";
import { Avatar } from "./Avatar";
import { useUserSettings } from "./useSettings";
import { backend, type MeetSession, type Recording, type MeetControls, type BreakoutSession } from "./backend";
import { toast } from "./toast";
import { openInWindow } from "./nativeWindow";
import { isTauri, onTauriEvent } from "./tauriNotify";
import { publicUrl } from "../apiBase";
import { IcUsers, IcLink, IcGear, IcX, IcPencilBox, IcLogout, IcMicOff, IcStar } from "../panel/icons";
import { Switch } from "./Switch";

// openRoom abre a sala numa janela dedicada (/room/:room). O nome/label é por sala, então clicar de
// novo foca a janela existente em vez de abrir outra. Na web usa window.open; no app nativo (Tauri)
// o window.open não cria janela — é preciso a API nativa WebviewWindow.
export function openRoom(room: string, audio = false) {
  const path = `/room/${encodeURIComponent(room)}${audio ? "?audio=1" : ""}`;
  if (openInWindow(`room-${room}`, path, "Beskos — Sala")) return; // app nativo: janela dedicada
  const w = Math.min(1280, screen.availWidth), h = Math.min(800, screen.availHeight);
  const feats = `popup=yes,width=${w},height=${h},left=${Math.max(0, (screen.availWidth - w) / 2)},top=${Math.max(0, (screen.availHeight - h) / 2)}`;
  const win = window.open(path, `mailin-room-${room}`, feats);
  win?.focus();
}

type Panel = "notes" | "host" | null;

// A chamada (janela dedicada /room/:room). Layout no padrão do Meet: os controles de mídia ficam
// na barra do LiveKit (centro-inferior) e os nossos botões numa barra inferior-direita; os painéis
// abrem ancorados na lateral direita, um de cada vez (por isso nada mais se sobrepõe).
export function MeetRoom({ session, audioOnly, onLeave }: {
  session: MeetSession; audioOnly?: boolean; onLeave: () => void;
}) {
  const { t } = useTranslation();
  const settings = useUserSettings(); // defaults de mic/câmara (settings.meet)
  const [panel, setPanel] = useState<Panel>(null);
  const toggle = (p: Panel) => setPanel((cur) => (cur === p ? null : p));
  const isHost = !!session.is_host && !!session.meeting_id;
  // Chat desligado pelo host: esconde a UI de chat do LiveKit para não-hosts (o data channel segue
  // aberto para apresentação/laser — a permissão nunca é cortada). Aplicado à entrada.
  const chatOff = session.controls?.allow_chat === false && !isHost;

  // Pré-join: tela de conferência de câmera/microfone antes de conectar. O usuário escolhe os
  // dispositivos, vê o preview e o nível do mic, e só entra ao confirmar (padrão Meet). Enquanto
  // `choices` é null, não conectamos à sala — nada de mídia enviada até o "Entrar".
  const [choices, setChoices] = useState<LocalUserChoices | null>(null);

  // Breakout: quando `bk` está setado, conectamos à sala do breakout em vez da principal (mesma
  // reunião, sala LiveKit diferente). Trocar de sala remonta o LiveKitRoom (via key); o guard
  // `switching` impede que o onDisconnected da remontagem feche a janela.
  const [bk, setBk] = useState<BreakoutSession | null>(null);
  const switching = useRef(false);
  const enterBreakout = useCallback((b: BreakoutSession) => { switching.current = true; setBk(b); }, []);
  const returnToMain = useCallback(() => { switching.current = true; setBk(null); }, []);
  const active = bk ?? { url: session.url, token: session.token, room: session.room };

  if (!choices) {
    return (
      <div className="meet-prejoin" data-lk-theme="default">
        <PreJoin
          defaults={{
            username: session.name ?? "",
            // Respeita "entrar com câmara/micro desligados" das Definições (settings.meet).
            videoEnabled: !audioOnly && settings.meet?.camOff !== true,
            audioEnabled: settings.meet?.enterMuted !== true,
          }}
          camLabel={t("meet.prejoin.cam")}
          micLabel={t("meet.prejoin.mic")}
          userLabel={t("meet.prejoin.name")}
          joinLabel={t("meet.prejoin.join")}
          onSubmit={setChoices}
          persistUserChoices={false}
        />
      </div>
    );
  }

  return (
    <div className={"meet-call" + (chatOff ? " chat-off" : "")}>
      <LiveKitRoom key={active.room} serverUrl={active.url} token={active.token} connect
        video={choices.videoEnabled} audio={choices.audioEnabled}
        options={{
          videoCaptureDefaults: { deviceId: choices.videoDeviceId || undefined },
          audioCaptureDefaults: { deviceId: choices.audioDeviceId || undefined },
        }}
        onDisconnected={() => { if (switching.current) { switching.current = false; return; } onLeave(); }}
        data-lk-theme="default" style={{ height: "100%" }}>
        <VideoConference />
        <LeaveOnWindowClose />

        {/* Barra de breakout (topo): entrar na tua sala / voltar à principal; some quando não há salas. */}
        {session.meeting_id && session.identity && (
          <BreakoutBar meetingId={session.meeting_id} myId={session.identity} inBreakout={bk}
            onJoin={enterBreakout} onReturn={returnToMain} />
        )}

        {/* Pedidos de entrada (aprovação): cartão no canto superior esquerdo, longe dos painéis. */}
        {isHost && session.access === "approval" && <HostLobby meetingId={session.meeting_id!} />}

        {/* Os nossos botões entram DENTRO da barra do LiveKit: uma só fila, um só estilo. */}
        <ControlBarSlot>
          <MeetEffects />
          {session.meeting_id && (
            <ActionBtn label={t("meet.notes")} on={panel === "notes"} onClick={() => toggle("notes")}>
              <IcPencilBox size={19} />
            </ActionBtn>
          )}
          {isHost && (
            <ActionBtn label={t("meet.hostControls")} on={panel === "host"} onClick={() => toggle("host")}>
              <IcGear size={19} />
            </ActionBtn>
          )}
          <MeetReactions />
          <PresentationLayer room={session.room} />
          <PipButton />
          <RecordButton room={session.room} />
          <CopyLinkButton room={session.room} />
        </ControlBarSlot>

        {panel && (
          <aside className="meet-side">
            <header>
              <b>{panel === "notes" ? t("meet.notes") : t("meet.hostControls")}</b>
              <div className="grow" />
              <button className="ib" title={t("common.close")} onClick={() => setPanel(null)}><IcX size={15} /></button>
            </header>
            <div className="ms-body">
              {panel === "notes" && session.meeting_id && <NotesPanel meetingId={session.meeting_id} title={session.title || session.room} isHost={session.is_host} />}
              {panel === "host" && isHost && (
                <HostPanel meetingId={session.meeting_id!} initial={session.controls} initialAccess={session.access ?? "public"} isOwner={!!session.is_owner} />
              )}
            </div>
          </aside>
        )}
      </LiveKitRoom>
    </div>
  );
}

// Fechar a janela = sair da reunião. No app de desktop nenhum dos dois caminhos corria o cleanup do
// React, e a pessoa ficava na sala com a câmara congelada para os outros:
//
//  (a) Janela PRINCIPAL: fechar não fecha a app — esconde-a no tray e o webview segue ligado. O lado
//      nativo avisa ("window-hidden").
//  (b) Janela DEDICADA da sala (é assim que o Meet abre no desktop, ver openRoom): fechá-la destrói
//      o webview de repente, sem despedida ao servidor. Travamos o fecho, saímos da sala e só então
//      destruímos a janela — com teto de tempo, para nunca prender a janela do utilizador.
//
// Em ambos os casos o onDisconnected trata do resto (fecha a vista e encerra a apresentação).
function LeaveOnWindowClose() {
  const room = useRoomContext();
  useEffect(() => {
    const offHidden = onTauriEvent("window-hidden", () => { void room.disconnect(); });
    let offClose: (() => void) | null = null;
    let gone = false;
    if (isTauri()) {
      void (async () => {
        try {
          const { getCurrentWindow } = await import("@tauri-apps/api/window");
          const win = getCurrentWindow();
          if (win.label === "main") return; // a principal é o caso (a)
          // Registar um onCloseRequested faz o Tauri travar o fecho nativo e passar a decisão ao
          // JS: a própria API destrói a janela quando este handler terminar sem preventDefault.
          // Logo, nada de preventDefault aqui — só atrasamos o fecho o tempo de sair da sala (com
          // teto, para nunca prender a janela) e deixamos a API fechá-la. Isto exige
          // `core:window:allow-destroy` nas capabilities; sem ela o fecho era negado e a janela
          // ficava presa (nenhum clique a fechava).
          const un = await win.onCloseRequested(async () => {
            await Promise.race([room.disconnect().catch(() => {}), new Promise((r) => setTimeout(r, 1500))]);
          });
          if (gone) un(); else offClose = un;
        } catch { /* API indisponível: fica só o caminho (a) */ }
      })();
    }
    return () => { offHidden(); gone = true; offClose?.(); };
  }, [room]);
  return null;
}

// Botão redondo da barra de ações (mesma linguagem visual dos controles do LiveKit).
function ActionBtn({ label, on, onClick, danger, children }: {
  label: string; on?: boolean; onClick: () => void; danger?: boolean; children: React.ReactNode;
}) {
  return (
    <button className={"meet-abtn" + (on ? " on" : "") + (danger ? " danger" : "")} title={label} aria-label={label} onClick={onClick}>
      {children}
    </button>
  );
}

// Controles do organizador (conteúdo do painel). Só o dono vê — e o backend também exige o dono
// (a query filtra owner_user_id), então esconder o botão não é a única defesa.
function HostPanel({ meetingId, initial, initialAccess, isOwner }: {
  meetingId: string; initial?: MeetControls; initialAccess: string; isOwner: boolean;
}) {
  const { t } = useTranslation();
  const [c, setC] = useState<MeetControls>(initial ?? { allow_mic: true, allow_cam: true, allow_share: true, allow_chat: true, lock_until_host: false, locked: false });
  const [access, setAccess] = useState(initialAccess);

  const save = useMutation({
    mutationFn: (next: MeetControls) => backend.meetControls(meetingId, next),
    onSuccess: (r) => setC(r),
    onError: (e) => { setC(c); toast((e as Error).message); },
  });
  const setAcc = useMutation({
    mutationFn: (a: string) => backend.meetAccess(meetingId, a),
    onSuccess: (r) => { setAccess(r.access); toast(t("meet.accessSaved")); },
    onError: (e) => toast((e as Error).message),
  });
  const toggle = (k: keyof MeetControls) => { const next = { ...c, [k]: !c[k] }; setC(next); save.mutate(next); };

  const row = (k: keyof MeetControls, label: string, hint?: string) => (
    <label className="hc-row">
      <span className="hc-label">{label}{hint && <i>{hint}</i>}</span>
      <span className="switch">
        <input type="checkbox" checked={c[k]} onChange={() => toggle(k)} />
        <span className="track" />
      </span>
    </label>
  );

  // Ordem lógica (24/07/2026): 1) como se ENTRA (acesso + tranca), 2) o que se PODE fazer
  // (permissões de media), 3) quem está na sala, 4) dividir em breakout. As opções de sala/acesso
  // são do DONO; o co-host só modera as pessoas.
  return (
    <>
      {isOwner && <>
        <div className="hc-sec first">{t("meet.accessSection")}</div>
        <div className="hc-access">
          {(["public", "approval"] as const).map((a) => (
            <button key={a} className={access === a ? "on" : ""} disabled={setAcc.isPending} onClick={() => setAcc.mutate(a)}>
              <b>{t(a === "public" ? "meet.accessPublic" : "meet.accessApproval")}</b>
              <i>{t(a === "public" ? "meet.accessPublicHint" : "meet.accessApprovalHint")}</i>
            </button>
          ))}
        </div>
        {row("lock_until_host", t("meet.lockUntilHost"), t("meet.lockUntilHostHint"))}
        {row("locked", t("meet.locked"), t("meet.lockedHint"))}

        <div className="hc-sec">{t("meet.permissions")}</div>
        {row("allow_mic", t("meet.allowMic"))}
        {row("allow_cam", t("meet.allowCam"))}
        {row("allow_share", t("meet.allowShare"))}
        {row("allow_chat", t("meet.allowChat"), t("meet.allowChatHint"))}
      </>}

      <div className={"hc-sec" + (isOwner ? "" : " first")}>{t("meet.people")}</div>
      <Participants meetingId={meetingId} isOwner={isOwner} />

      <div className="hc-sec">{t("meet.breakouts")}</div>
      <BreakoutPanel meetingId={meetingId} />
    </>
  );
}

// Quem está NA SALA agora (o LiveKit é quem sabe) + expulsar. O host não aparece com botão de
// expulsar: sair da própria sala com a tranca ligada deixaria toda a gente à porta.
function Participants({ meetingId, isOwner }: { meetingId: string; isOwner: boolean }) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["meet-participants", meetingId],
    queryFn: () => backend.meetParticipants(meetingId),
    refetchInterval: 5000, retry: false,
  });
  const kick = useMutation({
    mutationFn: (identity: string) => backend.meetKick(meetingId, identity),
    onSuccess: () => { void q.refetch(); toast(t("meet.kicked")); },
    onError: (e) => toast((e as Error).message),
  });
  const mute = useMutation({
    mutationFn: (identity: string) => backend.meetMute(meetingId, identity),
    onSuccess: () => toast(t("meet.muted")),
    onError: (e) => toast((e as Error).message),
  });
  // Promover/despromover co-host — só o dono. A identidade do LiveKit é o id do utilizador.
  const promote = useMutation({
    mutationFn: (identity: string) => backend.meetAddCohost(meetingId, identity),
    onSuccess: () => { void q.refetch(); toast(t("meet.cohostAdded")); },
    onError: (e) => toast((e as Error).message),
  });
  const demote = useMutation({
    mutationFn: (identity: string) => backend.meetRemoveCohost(meetingId, identity),
    onSuccess: () => { void q.refetch(); toast(t("meet.cohostRemoved")); },
    onError: (e) => toast((e as Error).message),
  });
  const list = q.data ?? [];
  if (list.length === 0) return <p className="hc-intro">{t("meet.noOneHere")}</p>;
  return (
    <div className="hc-people">
      {list.map((p) => (
        <div className="hc-person" key={p.identity}>
          <Avatar name={p.name} userId={p.identity} size={30} />
          <span className="nm">{p.name || p.identity.slice(0, 8)}</span>
          {p.is_host && <span className="pnl-pill muted">{t("meet.host")}</span>}
          {p.is_cohost && <span className="pnl-pill">{t("meet.cohost")}</span>}
          {!p.is_host && <>
            <button className="ib" title={t("meet.muteOne")} disabled={mute.isPending} onClick={() => mute.mutate(p.identity)}><IcMicOff size={14} /></button>
            <button className="ib" title={t("meet.kick")} disabled={kick.isPending} onClick={() => kick.mutate(p.identity)}><IcLogout size={14} /></button>
            {isOwner && (p.is_cohost
              ? <button className="ib on" title={t("meet.removeCohost")} disabled={demote.isPending} onClick={() => demote.mutate(p.identity)}><IcStar size={14} filled /></button>
              : <button className="ib" title={t("meet.makeCohost")} disabled={promote.isPending} onClick={() => promote.mutate(p.identity)}><IcStar size={14} /></button>)}
          </>}
        </div>
      ))}
    </div>
  );
}

// BreakoutBar: faixa no topo da chamada. Quem foi atribuído a uma sala vê "Entrar na tua sala";
// quem já está num breakout vê "Voltar à sala principal". Se o host fechar tudo (lista vazia)
// enquanto o utilizador está num breakout, volta sozinho à principal.
function BreakoutBar({ meetingId, myId, inBreakout, onJoin, onReturn }: {
  meetingId: string; myId: string; inBreakout: BreakoutSession | null;
  onJoin: (b: BreakoutSession) => void; onReturn: () => void;
}) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["meet-breakouts", meetingId],
    queryFn: () => backend.meetBreakouts(meetingId),
    refetchInterval: 4000, retry: false,
  });
  const rooms = q.data?.breakouts ?? [];
  const mine = rooms.find((b) => b.members.includes(myId));

  // Host fechou os breakouts enquanto eu estava num → volto à principal automaticamente.
  useEffect(() => {
    if (inBreakout && rooms.length === 0 && q.isFetched) onReturn();
  }, [inBreakout, rooms.length, q.isFetched, onReturn]);

  const join = useMutation({
    mutationFn: (bid: string) => backend.meetBreakoutToken(meetingId, bid),
    onSuccess: (s) => onJoin(s),
    onError: (e) => toast((e as Error).message),
  });

  if (inBreakout) {
    return (
      <div className="breakout-bar">
        <span className="bb-label">{t("meet.inBreakout", { name: inBreakout.name })}</span>
        <button className="bb-btn" onClick={onReturn}>{t("meet.returnMain")}</button>
      </div>
    );
  }
  if (!mine) return null;
  return (
    <div className="breakout-bar invite">
      <span className="bb-label">{t("meet.breakoutInvite", { name: mine.name })}</span>
      <button className="bb-btn" disabled={join.isPending} onClick={() => join.mutate(mine.id)}>{t("meet.joinBreakout")}</button>
    </div>
  );
}

// BreakoutPanel (host): cria N salas (com auto-distribuição em rodízio dos presentes), lista as
// salas + nº de membros, e fecha tudo. (A atribuição manual existe no backend; a UI usa auto.)
function BreakoutPanel({ meetingId }: { meetingId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [count, setCount] = useState(2);
  const [auto, setAuto] = useState(true);
  const q = useQuery({ queryKey: ["meet-breakouts", meetingId], queryFn: () => backend.meetBreakouts(meetingId), retry: false });
  const rooms = q.data?.breakouts ?? [];
  const refresh = () => qc.invalidateQueries({ queryKey: ["meet-breakouts", meetingId] });
  const create = useMutation({
    mutationFn: () => backend.meetCreateBreakouts(meetingId, count, auto),
    onSuccess: () => { refresh(); toast(t("meet.breakoutsCreated")); },
    onError: (e) => toast((e as Error).message),
  });
  const close = useMutation({
    mutationFn: () => backend.meetCloseBreakouts(meetingId),
    onSuccess: () => { refresh(); toast(t("meet.breakoutsClosed")); },
    onError: (e) => toast((e as Error).message),
  });
  return (
    <div className="breakout-panel">
      {rooms.length === 0 ? (
        <div className="bp-create">
          <p className="bp-hint">{t("meet.breakoutsHint")}</p>
          <div className="bp-row">
            <span>{t("meet.breakoutCount")}</span>
            <input type="number" min={1} max={20} value={count} onChange={(e) => setCount(Math.max(1, Math.min(20, Number(e.target.value) || 1)))} />
          </div>
          <div className="bp-row">
            <span>{t("meet.breakoutAuto")}</span>
            <Switch checked={auto} onChange={setAuto} label={t("meet.breakoutAuto")} />
          </div>
          <button className="bp-btn" disabled={create.isPending} onClick={() => create.mutate()}>{t("meet.createBreakouts")}</button>
        </div>
      ) : (
        <>
          <div className="bp-rooms">
            {rooms.map((b) => (
              <div key={b.id} className="bp-room">
                <b>{b.name}</b>
                <span className="bp-count">{t("meet.breakoutMembers", { n: b.members.length })}</span>
              </div>
            ))}
          </div>
          <button className="bp-btn danger" disabled={close.isPending} onClick={() => close.mutate()}>{t("meet.closeBreakouts")}</button>
        </>
      )}
    </div>
  );
}

// Gravar (toggle) — botão redondo com ponto vermelho quando gravando. A gravação usa o pipeline de
// tracks (montagem offline em GRELHA); layouts de composição (orador) só existiriam no RoomComposite
// ao vivo, que nenhum plano usa — por isso não se oferece um seletor de layout que não teria efeito.
function RecordButton({ room }: { room: string }) {
  const { t } = useTranslation();
  const [on, setOn] = useState(false);
  const checkFailed = async (id: string) => {
    for (let i = 0; i < 5; i++) {
      await new Promise((r) => setTimeout(r, 2500));
      const rec = (await backend.meetRecordings().catch(() => null))?.recordings.find((x: Recording) => x.id === id);
      if (rec?.status === "failed") { setOn(false); toast(t("meet.recFailed")); return; }
      if (rec?.status === "ready") return;
    }
  };
  const start = useMutation({ mutationFn: () => backend.meetRecordStart(room), onSuccess: (d) => { setOn(true); toast(t("meet.recStarted")); void checkFailed(d.recording_id); }, onError: (e) => toast((e as Error).message) });
  const stop = useMutation({ mutationFn: () => backend.meetRecordStop(room), onSuccess: () => { setOn(false); toast(t("meet.recStopped")); }, onError: (e) => toast((e as Error).message) });
  const busy = start.isPending || stop.isPending;
  return (
    <button className={"meet-abtn rec" + (on ? " on" : "")} disabled={busy} title={on ? t("meet.stopRec") : t("meet.record")}
      aria-label={on ? t("meet.stopRec") : t("meet.record")} onClick={() => (on ? stop : start).mutate()}>
      <span className="rec-dot" />
    </button>
  );
}

// Picture-in-Picture: destaca o vídeo em foco (partilha de ecrã ou orador) numa janela flutuante do
// SO, para seguir a reunião ao trocar de aba/app. Feature-detected: some onde o browser não suporta
// (ex.: WKWebView do iOS). Escolhe o maior <video> a tocar dentro do palco do LiveKit.
function PipButton() {
  const { t } = useTranslation();
  const [on, setOn] = useState(false);
  const supported = typeof document !== "undefined" && (document as Document & { pictureInPictureEnabled?: boolean }).pictureInPictureEnabled;
  if (!supported) return null;

  const pickVideo = (): HTMLVideoElement | null => {
    const vids = Array.from(document.querySelectorAll<HTMLVideoElement>(".lk-room-container video, .meet-room video"))
      .filter((v) => v.readyState >= 2 && v.videoWidth > 0 && !v.disablePictureInPicture);
    // Maior área visível ≈ palco/partilha; sem candidatos, null (o clique vira no-op).
    return vids.sort((a, b) => b.videoWidth * b.videoHeight - a.videoWidth * a.videoHeight)[0] ?? null;
  };

  const toggle = async () => {
    try {
      if (document.pictureInPictureElement) { await document.exitPictureInPicture(); setOn(false); return; }
      const v = pickVideo();
      if (!v) { toast(t("meet.pipNoVideo")); return; }
      v.addEventListener("leavepictureinpicture", () => setOn(false), { once: true });
      await v.requestPictureInPicture();
      setOn(true);
    } catch { toast(t("meet.pipFailed")); }
  };

  return (
    <button className={"meet-abtn" + (on ? " on" : "")} title={on ? t("meet.pipExit") : t("meet.pip")}
      aria-label={on ? t("meet.pipExit") : t("meet.pip")} onClick={() => void toggle()}>
      <svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <rect x="2" y="4" width="20" height="16" rx="2" />
        <rect x="12" y="12" width="8" height="6" rx="1" fill="currentColor" stroke="none" />
      </svg>
    </button>
  );
}

// Copiar o link público da sala (para convidados externos).
function CopyLinkButton({ room }: { room: string }) {
  const { t } = useTranslation();
  const link = publicUrl(`/j/${room}`);
  return (
    <button className="meet-abtn" title={link} aria-label={t("meet.copyLink")}
      onClick={() => { navigator.clipboard?.writeText(link); toast(t("meet.linkCopied")); }}>
      <IcLink size={18} />
    </button>
  );
}

// Sala de espera do host: convidados pendentes (aprovação). Poll + admitir/negar.
function HostLobby({ meetingId }: { meetingId: string }) {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ["lobby", meetingId], queryFn: () => backend.meetLobby(meetingId), refetchInterval: 3000, retry: false });
  const admit = useMutation({ mutationFn: (id: string) => backend.meetAdmit(id), onSuccess: () => q.refetch() });
  const deny = useMutation({ mutationFn: (id: string) => backend.meetDeny(id), onSuccess: () => q.refetch() });
  const pending = q.data?.pending ?? [];
  if (!pending.length) return null;
  return (
    <div className="host-lobby">
      <div className="hl-title"><IcUsers size={14} /> {t("meet.lobbyTitle")}</div>
      {pending.map((k) => (
        <div className="hl-item" key={k.id}>
          <span className="grow">{k.name}</span>
          <button className="hl-admit" onClick={() => admit.mutate(k.id)}>{t("meet.admit")}</button>
          <button className="hl-deny" onClick={() => deny.mutate(k.id)}>{t("meet.deny")}</button>
        </div>
      ))}
    </div>
  );
}
