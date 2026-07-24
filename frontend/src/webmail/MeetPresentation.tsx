import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { overlayRoot } from "./MeetBarSlot";
import { useTranslation } from "react-i18next";
import { useLocalParticipant, useRoomContext } from "@livekit/components-react";
import { ConnectionState, RoomEvent } from "livekit-client";
import { backend, type Presentation, type StorageFile } from "./backend";
import { loadPdf, type PDFDocument } from "./pdfjs";
import { toast } from "./toast";
import { Spinner } from "./Spinner";
import { IcX, IcTrash, IcUpload, IcFolder, IcPencil, IcPresent, IcSearch, IcArrowLeft, IcUsers, IcPaperclip, IcChevron, IcExternal } from "../panel/icons";
import { hueFor, initials } from "./util";

// Apresentação no Meet (estilo BBB). O apresentador escolhe PDF/PPTX (upload ou Drive); todos
// renderizam o PDF localmente (pdf.js) e o slide/caneta/laser sincronizam pelo data channel do
// LiveKit. Funciona na web e no desktop (o mesmo código; o PDF vem por URL pública com token).

type Stroke = { pts: number[]; color: string; w: number }; // pts normalizados (0..1), pares x,y
type Msg =
  | { t: "hello" }
  | { t: "start"; id: string; token: string; by: string; slide: number; granted: string[] }
  | { t: "slide"; n: number }
  | { t: "stroke"; page: number; s: Stroke }
  /** Fotografia dos traços de uma página, para quem entra a meio. `reset` marca o primeiro lote:
   *  substitui o que houver; os seguintes acrescentam (um desenho longo não cabe num só pacote). */
  | { t: "strokes"; page: number; list: Stroke[]; reset: boolean }
  | { t: "clear"; page: number }
  | { t: "laser"; x: number; y: number }
  | { t: "laserOff" }
  | { t: "grant"; id: string }
  | { t: "revoke"; id: string }
  | { t: "stop" };

const COLORS = ["#e53935", "#1e88e5", "#43a047", "#fdd835", "#111"];
/** Teto por pacote ao reenviar traços. O LiveKit corta perto dos 15 KB; ficamos com folga. */
const MAX_SNAPSHOT_BYTES = 8000;
const enc = new TextEncoder();
const dec = new TextDecoder();
const DEBUG = false; // painel de diagnóstico da apresentação (ligar só para depurar)

export function PresentationLayer({ room, viewerOnly }: { room: string; viewerOnly?: boolean }) {
  const { t } = useTranslation();
  const roomCtx = useRoomContext();
  const { localParticipant } = useLocalParticipant();
  const myId = localParticipant?.identity ?? "";

  // Diagnóstico temporário: conta mensagens recebidas e mostra o estado.
  const dbg = useRef({ rx: 0, tx: 0, topic: "-", lastT: "-", err: "" });
  const [, setDbgTick] = useState(0);
  useEffect(() => {
    if (!DEBUG) return;
    const i = setInterval(() => setDbgTick((v) => v + 1), 700);
    return () => clearInterval(i);
  }, []);

  const [pres, setPres] = useState<{ id: string; token: string; by: string } | null>(null);
  const [picker, setPicker] = useState(false);
  const [busy, setBusy] = useState(false);
  const [hidden, setHidden] = useState(false); // esconder localmente (não encerra; mostra as câmaras)
  const [barCollapsed, setBarCollapsed] = useState(false); // encolher a barra de controlos (aba)
  const isPresenter = !!pres && pres.by === myId;

  // Envio pelo data channel do LiveKit (tópico "pres"). publishData SÓ funciona com a sala
  // CONECTADA — antes disso o LiveKit rejeita ("PC manager is closed") e a mensagem perde-se (era
  // por isso que o hello/start nunca chegavam). Mensagens cedo demais vão para a fila e são
  // despachadas no RoomEvent.Connected. Laser vai lossy (rápido); o resto fiável.
  const pending = useRef<{ m: Msg; reliable: boolean }[]>([]);
  /** `to` limita a entrega a participantes concretos (usado para pôr quem entra agora em dia sem
   *  reenviar o estado a toda a sala). Sem `to`, vai para todos. */
  const post = useCallback((m: Msg, reliable = true, to?: string[]) => {
    const room = roomCtx;
    if (!room) { if (DEBUG) dbg.current.err = "no-room"; return; }
    if (room.state !== ConnectionState.Connected) { pending.current.push({ m, reliable }); return; }
    room.localParticipant.publishData(enc.encode(JSON.stringify(m)), { reliable, topic: "pres", destinationIdentities: to })
      .then(() => { if (DEBUG) dbg.current.tx++; })
      .catch((e) => { if (DEBUG) dbg.current.err = "tx:" + String((e as Error).message).slice(0, 24); });
  }, [roomCtx]);

  // Estado partilhado da anotação (refs para não re-renderizar a cada traço).
  const strokes = useRef<Map<number, Stroke[]>>(new Map());
  const [granted, setGranted] = useState<Set<string>>(new Set());
  const [page, setPage] = useState(1);
  const [numPages, setNumPages] = useState(0);
  const [redraw, setRedraw] = useState(0);
  const pageRef = useRef(1); pageRef.current = page;

  const docRef = useRef<PDFDocument | null>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const pdfCanvas = useRef<HTMLCanvasElement>(null);
  const annoCanvas = useRef<HTMLCanvasElement>(null);

  // Laser IMPERATIVO: mover um elemento por ref não re-renderiza o componente. Posiciona por
  // TRANSFORM (só compositor) — animar left/top forçava layout a cada pixel e "travava" o rato.
  // slideSize é atualizado quando a página renderiza (evita ler getBoundingClientRect a cada move).
  const laserRef = useRef<HTMLSpanElement>(null);
  const slideSize = useRef({ w: 0, h: 0 });
  const laserFade = useRef<ReturnType<typeof setTimeout>>();
  const showLaser = useCallback((x: number, y: number) => {
    const el = laserRef.current; if (!el || !slideSize.current.w) return;
    el.style.transform = `translate(${x * slideSize.current.w - 9}px, ${y * slideSize.current.h - 9}px)`;
    el.style.opacity = "1";
    clearTimeout(laserFade.current);
    laserFade.current = setTimeout(() => { if (laserRef.current) laserRef.current.style.opacity = "0"; }, 2500);
  }, []);
  const hideLaser = useCallback(() => { if (laserRef.current) laserRef.current.style.opacity = "0"; clearTimeout(laserFade.current); }, []);

  // --- recepção de mensagens ---
  function onMsg(m: Msg, from?: string) {
    switch (m.t) {
      case "hello":
        // Só o apresentador responde, re-emitindo o estado para quem entrou agora (inclui convidados).
        // Dirigido a quem perguntou: reemitir para a sala toda faria os outros redesenhar à toa.
        if (pres && pres.by === myId) resendRef.current(from ? [from] : undefined);
        break;
      case "start":
        setPres({ id: m.id, token: m.token, by: m.by });
        setGranted(new Set(m.granted));
        setPage(m.slide || 1);
        setHidden(false);
        break;
      case "slide": setPage(m.n); break;
      case "stroke": {
        const arr = strokes.current.get(m.page) ?? [];
        arr.push(m.s); strokes.current.set(m.page, arr);
        if (m.page === pageRef.current) setRedraw((v) => v + 1);
        break;
      }
      case "strokes": {
        const arr = m.reset ? [] : (strokes.current.get(m.page) ?? []);
        arr.push(...m.list); strokes.current.set(m.page, arr);
        if (m.page === pageRef.current) setRedraw((v) => v + 1);
        break;
      }
      case "clear": strokes.current.set(m.page, []); if (m.page === pageRef.current) setRedraw((v) => v + 1); break;
      case "laser": showLaser(m.x, m.y); break;
      case "laserOff": hideLaser(); break;
      case "grant": setGranted((g) => new Set(g).add(m.id)); break;
      case "revoke": setGranted((g) => { const n = new Set(g); n.delete(m.id); return n; }); break;
      case "stop": setPres(null); strokes.current.clear(); break;
    }
  }

  // Recebe as mensagens "pres" direto do room. onMsgRef garante o estado atual no handler (sem
  // fechar sobre um `pres`/`granted` obsoletos).
  const onMsgRef = useRef(onMsg);
  onMsgRef.current = onMsg;
  // resendState: o apresentador reemite o estado atual (usado ao entrar alguém novo).
  const resendRef = useRef<(to?: string[]) => void>(() => {});
  useEffect(() => {
    if (!roomCtx) return;
    const onData = (payload: Uint8Array, p?: { identity?: string }, _k?: unknown, topic?: string) => {
      if (DEBUG) { dbg.current.rx++; dbg.current.topic = topic ?? "(none)"; }
      if (topic && topic !== "pres") return;
      try {
        const m = JSON.parse(dec.decode(payload)) as Msg;
        if (DEBUG) dbg.current.lastT = m.t;
        onMsgRef.current(m, p?.identity);
      } catch { /* frame inválido */ }
    };
    // Estado só para quem entrou (não para a sala toda). O atraso dá tempo ao novato de montar o listener.
    const onJoin = (p?: { identity?: string }) => {
      const to = p?.identity ? [p.identity] : undefined;
      setTimeout(() => resendRef.current(to), 900);
    };
    // Sala conectou: despacha as mensagens em fila (ex.: o hello do mount, feito antes do connect).
    const onConnected = () => { const q = pending.current.splice(0); q.forEach(({ m, reliable }) => post(m, reliable)); };
    // Reconexão: volta a pedir o estado e, se somos o apresentador, reemite-o.
    const onReconnected = () => { post({ t: "hello" }); setTimeout(() => resendRef.current(), 600); };
    roomCtx.on(RoomEvent.DataReceived, onData);
    roomCtx.on(RoomEvent.ParticipantConnected, onJoin);
    roomCtx.on(RoomEvent.Connected, onConnected);
    roomCtx.on(RoomEvent.Reconnected, onReconnected);
    if (roomCtx.state === ConnectionState.Connected) onConnected(); // já conectada ao montar
    return () => {
      roomCtx.off(RoomEvent.DataReceived, onData);
      roomCtx.off(RoomEvent.ParticipantConnected, onJoin);
      roomCtx.off(RoomEvent.Connected, onConnected);
      roomCtx.off(RoomEvent.Reconnected, onReconnected);
    };
  }, [roomCtx, post]);

  resendRef.current = (to?: string[]) => {
    if (!pres || pres.by !== myId) return;
    post({ t: "start", id: pres.id, token: pres.token, by: myId, slide: pageRef.current, granted: [...granted] }, true, to);
    // ...e os traços já desenhados. Sem isto, quem entra a meio vê a apresentação em branco: as
    // anotações só existiam como eventos ao vivo, nunca como estado.
    for (const [pg, list] of strokes.current) {
      if (!list.length) continue;
      let batch: Stroke[] = [];
      let bytes = 0;
      let reset = true;
      const flush = () => {
        if (!batch.length) return;
        post({ t: "strokes", page: pg, list: batch, reset }, true, to);
        reset = false; batch = []; bytes = 0;
      };
      for (const s of list) {
        // Um pacote de dados do LiveKit é limitado (~15 KB); parte-se antes de lá chegar.
        const size = s.pts.length * 8 + 40;
        if (bytes + size > MAX_SNAPSHOT_BYTES) flush();
        batch.push(s); bytes += size;
      }
      flush();
    }
  };

  // Ao montar/reconectar, pergunta o estado (para entrar a meio da apresentação).
  useEffect(() => { post({ t: "hello" }); }, [post]);

  // Recuperação por REST só UMA vez, ao entrar (caso ninguém responda ao hello a tempo). Não pode
  // correr sempre que `pres` fica null, senão logo após "Encerrar" voltaria a buscar e reabria.
  const askedRef = useRef(false);
  useEffect(() => {
    if (askedRef.current) return;
    askedRef.current = true;
    let stop = false;
    backend.presentationGet(room).then((p) => {
      if (stop || !p.active) return;
      // by = identidade do apresentador (backend). Se formos nós (reentrámos na sala), recuperamos
      // o papel: controlos de volta e respondemos aos hellos de quem chegar.
      setPres({ id: p.id, token: p.token, by: p.by || "" }); setPage(p.current_slide || 1);
    }).catch(() => { /* sem apresentação ou convidado sem sessão: ignora */ });
    return () => { stop = true; };
  }, [room]);

  // Carrega o PDF quando a apresentação fica ativa.
  useEffect(() => {
    if (!pres) { docRef.current = null; setNumPages(0); return; }
    let stop = false;
    loadPdf(backend.presentationPdfUrl(pres.id, pres.token)).then((doc) => {
      if (stop) return;
      docRef.current = doc; setNumPages(doc.numPages); setRedraw((v) => v + 1);
      if (DEBUG) dbg.current.err = "pdf ok " + doc.numPages;
    }).catch((e) => { if (DEBUG) dbg.current.err = "pdf ERR: " + String((e as Error).message).slice(0, 40); toast(t("meet.presLoadFail")); });
    return () => { stop = true; };
  }, [pres, t]);

  // Renderiza a página atual no canvas do PDF e ajusta o canvas de anotação. Depende de `hidden`
  // porque ao esconder/mostrar os canvas são remontados (novos e em branco) — sem re-renderizar,
  // reapareceria um quadrado branco.
  useEffect(() => {
    if (hidden) return;
    const doc = docRef.current, stage = stageRef.current, pc = pdfCanvas.current, ac = annoCanvas.current;
    if (!doc || !stage || !pc || !ac) return;
    let cancelled = false;
    const dpr = window.devicePixelRatio || 1;
    doc.getPage(page).then((pg) => {
      if (cancelled) return;
      const base = pg.getViewport({ scale: 1 });
      const scale = Math.min(stage.clientWidth / base.width, stage.clientHeight / base.height) || 1;
      const vp = pg.getViewport({ scale: scale * dpr });
      const cssW = vp.width / dpr, cssH = vp.height / dpr;
      slideSize.current = { w: cssW, h: cssH }; // para o laser posicionar por transform sem medir o DOM
      for (const c of [pc, ac]) { c.width = vp.width; c.height = vp.height; c.style.width = cssW + "px"; c.style.height = cssH + "px"; }
      const ctx = pc.getContext("2d")!;
      pg.render({ canvasContext: ctx, viewport: vp });
      drawStrokes();
    });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, numPages, redraw, hidden]);

  // O slide era medido UMA vez e nunca mais. Quando o palco muda de tamanho — ao abrir a coluna das
  // câmaras, ao redimensionar a janela, ao entrar/sair de ecrã inteiro — o canvas ficava com a
  // medida antiga e transbordava. Observar é seguro: `.pres-canvas-wrap` é `flex:1`, dimensionado
  // pelo palco e não pelo conteúdo, por isso mudar o canvas não realimenta o observador.
  useEffect(() => {
    const stage = stageRef.current;
    if (!stage || hidden || !pres) return;
    const ro = new ResizeObserver(() => setRedraw((v) => v + 1));
    ro.observe(stage);
    return () => ro.disconnect();
  }, [hidden, pres]);

  // Desenha os traços da página atual no canvas de anotação.
  function drawStrokes() {
    const ac = annoCanvas.current; if (!ac) return;
    const ctx = ac.getContext("2d")!;
    ctx.clearRect(0, 0, ac.width, ac.height);
    ctx.lineCap = "round"; ctx.lineJoin = "round";
    for (const s of strokes.current.get(page) ?? []) drawStroke(ctx, s, ac.width, ac.height);
  }
  function drawStroke(ctx: CanvasRenderingContext2D, s: Stroke, w: number, h: number) {
    if (s.pts.length < 2) return;
    ctx.strokeStyle = s.color; ctx.lineWidth = s.w * w;
    ctx.beginPath(); ctx.moveTo(s.pts[0] * w, s.pts[1] * h);
    for (let i = 2; i < s.pts.length; i += 2) ctx.lineTo(s.pts[i] * w, s.pts[i + 1] * h);
    ctx.stroke();
  }

  const canDraw = isPresenter || granted.has(myId);
  const [tool, setTool] = useState<"pen" | null>(null); // null = modo laser (automático ao passar o rato)
  const [color, setColor] = useState(COLORS[0]);
  const cur = useRef<number[] | null>(null);
  const lastLaser = useRef(0);

  const relPos = (e: React.PointerEvent) => {
    const r = annoCanvas.current!.getBoundingClientRect();
    return { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height };
  };
  // Laser AUTOMÁTICO: enquanto o rato passa sobre a apresentação (e não se está a desenhar), a
  // posição do ponteiro vira laser para todos. Move-se por ref (sem re-render) e o envio é limitado.
  const sendLaser = (x: number, y: number) => {
    showLaser(x, y);
    const now = performance.now();
    if (now - lastLaser.current > 33) { lastLaser.current = now; post({ t: "laser", x, y }, false); }
  };
  const onDown = (e: React.PointerEvent) => {
    if (!canDraw || tool !== "pen") return;
    const { x, y } = relPos(e);
    cur.current = [x, y]; annoCanvas.current?.setPointerCapture(e.pointerId);
  };
  const onMove = (e: React.PointerEvent) => {
    if (!canDraw) return;
    const { x, y } = relPos(e);
    // Com a CANETA escolhida o laser nunca entra em cena. Antes, mover o rato sem carregar caía no
    // laser automático e o ponto vermelho continuava a seguir o ponteiro — parecia que a ferramenta
    // não tinha mudado.
    if (tool === "pen") {
      if (cur.current) {
        cur.current.push(x, y);
        const ac = annoCanvas.current!; drawStroke(ac.getContext("2d")!, { pts: cur.current, color, w: 0.004 }, ac.width, ac.height);
      }
      return;
    }
    sendLaser(x, y); // laser automático (só no modo laser)
  };
  const onUp = () => {
    if (cur.current && cur.current.length >= 4) {
      const s: Stroke = { pts: cur.current, color, w: 0.004 };
      const arr = strokes.current.get(page) ?? []; arr.push(s); strokes.current.set(page, arr);
      post({ t: "stroke", page, s });
    }
    cur.current = null;
  };
  const onLeave = () => { onUp(); hideLaser(); post({ t: "laserOff" }, false); };

  // --- ações do apresentador ---
  const gotoPage = (n: number) => {
    const p = Math.max(1, Math.min(numPages || 1, n));
    setPage(p); post({ t: "slide", n: p });
    if (pres) backend.presentationSetSlide(pres.id, p).catch(() => {});
  };
  const clearPage = () => { strokes.current.set(page, []); setRedraw((v) => v + 1); post({ t: "clear", page }); };
  const stop = () => { if (pres) backend.presentationStop(pres.id).catch(() => {}); post({ t: "stop" }); setPres(null); strokes.current.clear(); };
  const startWith = async (fn: () => Promise<Presentation>) => {
    setPicker(false); setBusy(true); // fecha o seletor e mostra o overlay "a preparar"
    try {
      const p = await fn();
      strokes.current.clear();
      setPres({ id: p.id, token: p.token, by: myId }); setGranted(new Set()); setPage(p.current_slide || 1); setHidden(false);
      post({ t: "start", id: p.id, token: p.token, by: myId, slide: p.current_slide || 1, granted: [] });
    } catch (e) { toast((e as Error).message); }
    setBusy(false);
  };

  // Conceder/retirar a caneta. O envio pela rede fica FORA do updater do setState — pô-lo dentro
  // fazia o React re-executar o updater (StrictMode/dev) e enviar grant+revoke, anulando a concessão
  // (bug 24/07/2026). Calcula-se o próximo estado à parte e envia-se uma só mensagem.
  const toggleGrant = (id: string) => {
    const has = granted.has(id);
    const next = new Set(granted);
    if (has) next.delete(id); else next.add(id);
    setGranted(next);
    post({ t: has ? "revoke" : "grant", id });
  };

  // Participantes remotos (para conceder anotação).
  const peers = roomCtx ? [...roomCtx.remoteParticipants.values()] : [];

  // O BOTÃO fica na barra de controlos; tudo o resto (palco, modais, avisos) é sobreposição de ecrã
  // inteiro e vai para o <body>. Se ficasse dentro da barra, bastava a barra ganhar
  // `backdrop-filter`/`transform`/`filter` para passar a ser o containing block dos `position:fixed`
  // — e o palco encolhia para dentro dela, por cima dos ícones. Já aconteceu; daí o portal.
  const overlays = (
    <>
      {DEBUG && (
        <div style={{ position: "fixed", top: 6, left: 6, zIndex: 300, background: "rgba(0,0,0,.78)", color: "#3f6", font: "11px monospace", padding: "4px 7px", borderRadius: 6, pointerEvents: "none", whiteSpace: "nowrap" }}>
          {`id:${myId.slice(0, 6)} st:${roomCtx?.state ?? "?"} rp:${roomCtx?.remoteParticipants?.size ?? "?"} tx:${dbg.current.tx} rx:${dbg.current.rx} last:${dbg.current.lastT} pres:${pres ? 1 : 0} by:${pres?.by?.slice(0, 6) || "-"} pg:${numPages} ${dbg.current.err}`}
        </div>
      )}
      {picker && <Picker onClose={() => setPicker(false)} onUpload={(f) => startWith(() => backend.presentationStartUpload(room, f))} onDrive={(id) => startWith(() => backend.presentationStartDrive(room, id))} />}

      {busy && <div className="pres-loading"><Spinner size={22} /><span>{t("meet.presConverting")}</span></div>}

      {/* Escondida (local): mostra só um botão para trazer a apresentação de volta; as câmaras voltam. */}
      {pres && hidden && (
        <button className="pres-show" onClick={() => setHidden(false)}><IcPresent size={16} /> {t("meet.presShow")}</button>
      )}

      {pres && !hidden && (
        <div className="pres-stage">
          <div className="pres-canvas-wrap" ref={stageRef}>
            <div className="pres-slide">
              <canvas ref={pdfCanvas} className="pres-pdf" />
              <canvas ref={annoCanvas} className="pres-anno"
                style={{ cursor: canDraw ? (tool === "pen" ? "crosshair" : "none") : "default", pointerEvents: canDraw ? "auto" : "none" }}
                onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerLeave={onLeave} />
              <span className="pres-laser" ref={laserRef} />
            </div>
          </div>

          {barCollapsed ? (
            <button className="pres-bar-tab" title={t("meet.presBarShow")} onClick={() => setBarCollapsed(false)}>
              <IcChevron size={16} className="chev-up" />
            </button>
          ) : (
          <div className="pres-bar">
            <button className="ib" title={t("meet.presBarHide")} onClick={() => setBarCollapsed(true)}><IcChevron size={16} /></button>
            <span className="pres-sep" />
            {isPresenter && <>
              <button className="ib" title={t("meet.presPrev")} onClick={() => gotoPage(page - 1)} disabled={page <= 1}><IcChevron size={16} className="chev-l" /></button>
              <span className="pres-count">{page} / {numPages || "…"}</span>
              <button className="ib" title={t("meet.presNext")} onClick={() => gotoPage(page + 1)} disabled={page >= numPages}><IcChevron size={16} className="chev-r" /></button>
              <span className="pres-sep" />
            </>}
            {canDraw && <>
              <button className={"ib" + (tool === "pen" ? " on" : "")} title={t("meet.presPen")}
                onClick={() => {
                  const next = tool === "pen" ? null : "pen";
                  setTool(next);
                  // Ao passar para a caneta, apaga o laser que ficou aceso — no meu ecrã e no dos outros.
                  if (next === "pen") { hideLaser(); post({ t: "laserOff" }); }
                }}><IcPencil size={15} /></button>
              {tool === "pen" && (
                <span className="pres-swatches">
                  {COLORS.map((c) => <button key={c} className={"pres-swatch" + (c === color ? " on" : "")} style={{ "--sw": c } as React.CSSProperties} onClick={() => setColor(c)} aria-label={c} />)}
                </span>
              )}
              <button className="ib" title={t("meet.presClear")} onClick={clearPage}><IcTrash size={15} /></button>
              <span className="pres-sep" />
            </>}
            {isPresenter && peers.length > 0 && <GrantMenu peers={peers} granted={granted} onToggle={toggleGrant} label={t("meet.presAllow")} />}
            {/* Esconder é LOCAL (não encerra): traz as câmaras de volta. */}
            <button className="ib" title={t("meet.presHide")} onClick={() => setHidden(true)}><IcExternal size={15} /></button>
            {isPresenter && <>
              <span className="pres-sep" />
              <button className="pres-stop" onClick={stop}><IcX size={14} /> {t("meet.presStop")}</button>
            </>}
          </div>
          )}
        </div>
      )}
    </>
  );

  return (
    <>
      {/* Convidados (viewerOnly) só veem — não podem iniciar apresentação (sem sessão na nossa API). */}
      {!viewerOnly && (
        <button className="meet-abtn" title={t("meet.present")} aria-label={t("meet.present")} onClick={() => (pres ? undefined : setPicker(true))} disabled={!!pres && !isPresenter}>
          <IcPresent size={19} />
        </button>
      )}
      {createPortal(overlays, overlayRoot())}
    </>
  );
}

// Menu para conceder/retirar a caneta a participantes.
function GrantMenu({ peers, granted, onToggle, label }: {
  peers: { identity: string; name?: string }[]; granted: Set<string>; onToggle: (id: string) => void; label: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="pres-grant">
      <button className={"ib" + (open ? " on" : "")} title={label} onClick={() => setOpen((o) => !o)}><IcUsers size={15} /></button>
      {open && (
        <>
          <div className="pres-grant-back" onClick={() => setOpen(false)} />
          <div className="pres-grant-menu">
            <b>{label}</b>
            {peers.map((p) => {
              const on = granted.has(p.identity);
              const nm = p.name || p.identity;
              return (
                <button key={p.identity} type="button" className="pres-grant-row" onClick={() => onToggle(p.identity)}>
                  <span className="pg-av" style={{ background: hueFor(p.identity) }}>{initials(nm)}</span>
                  <span className="pg-nm">{nm}</span>
                  <span className={"pg-sw" + (on ? " on" : "")}><i /></span>
                </button>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}

// Seletor: enviar ficheiro ou escolher do Drive. O separador "Do Drive" é um mini gestor de
// ficheiros — pastas, busca e a secção "Partilhado comigo" — para não ficar inviável com muitos
// ficheiros (24/07/2026). Só apresenta PDF/PPTX/PPT/ODP.
const okPresExt = (n: string) => /\.(pdf|pptx|ppt|odp)$/i.test(n);
const fmtSz = (n: number) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);

function Picker({ onClose, onUpload, onDrive }: {
  onClose: () => void; onUpload: (f: File) => void; onDrive: (id: string) => void;
}) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"upload" | "drive">("upload");
  const [folderId, setFolderId] = useState<string | undefined>(undefined);
  const [q, setQ] = useState("");
  const [listing, setListing] = useState<{ files: StorageFile[]; folders: { id: string; name: string }[]; breadcrumb: { id: string; name: string }[] } | null>(null);
  const [results, setResults] = useState<{ files: StorageFile[]; folders: { id: string; name: string }[] } | null>(null);
  const [shared, setShared] = useState<StorageFile[]>([]);
  const searching = q.trim().length > 0;

  // Carrega a pasta atual (ou a busca) e, na raiz, os partilhados-comigo.
  useEffect(() => {
    if (tab !== "drive" || searching) return;
    backend.files(folderId).then(setListing).catch(() => setListing(null));
    if (!folderId) backend.sharedWithMe().then((r) => setShared(r.files.filter((f) => okPresExt(f.nome)))).catch(() => setShared([]));
  }, [tab, folderId, searching]);
  useEffect(() => {
    if (tab !== "drive" || !searching) return;
    const id = setTimeout(() => { backend.searchFiles(q.trim()).then(setResults).catch(() => setResults(null)); }, 250);
    return () => clearTimeout(id);
  }, [tab, q, searching]);

  const folders = searching ? (results?.folders ?? []) : (listing?.folders ?? []);
  const files = (searching ? (results?.files ?? []) : (listing?.files ?? [])).filter((f) => okPresExt(f.nome) && !f.deleted_at);
  const crumbs = listing?.breadcrumb ?? [];

  return (
    <div className="pres-modal" onMouseDown={onClose}>
      <div className="pres-picker" onMouseDown={(e) => e.stopPropagation()}>
        <header><b>{t("meet.present")}</b><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button></header>
        <div className="pres-tabs">
          <button className={tab === "upload" ? "on" : ""} onClick={() => setTab("upload")}><IcUpload size={14} /> {t("meet.presUpload")}</button>
          <button className={tab === "drive" ? "on" : ""} onClick={() => setTab("drive")}><IcFolder size={14} /> {t("meet.presDrive")}</button>
        </div>
        {tab === "upload" ? (
          <label className="pres-drop">
            <IcUpload size={22} />
            <span>{t("meet.presPickFile")}</span>
            <input type="file" accept=".pdf,.pptx,.ppt,.odp" hidden onChange={(e) => { const f = e.target.files?.[0]; if (f) onUpload(f); }} />
          </label>
        ) : (
          <div className="pres-fm">
            <label className="pres-fm-search">
              <IcSearch size={14} />
              <input value={q} placeholder={t("drivePicker.search")} onChange={(e) => setQ(e.target.value)} autoFocus />
            </label>
            {!searching && (
              <div className="pres-fm-crumbs">
                {folderId && (
                  <button className="ib" title={t("drivePicker.up")}
                    onClick={() => setFolderId(crumbs.length > 1 ? crumbs[crumbs.length - 2].id : undefined)}><IcArrowLeft size={13} /></button>
                )}
                <button className="pres-crumb" onClick={() => setFolderId(undefined)}>{t("drivePicker.root")}</button>
                {crumbs.map((c) => <span key={c.id}> / <button className="pres-crumb" onClick={() => setFolderId(c.id)}>{c.name}</button></span>)}
              </div>
            )}
            <div className="pres-fm-list">
              {folders.map((fo) => (
                <button key={fo.id} className="pres-fm-item folder" onClick={() => { setQ(""); setFolderId(fo.id); }}>
                  <IcFolder size={16} /> <span className="nm">{fo.name}</span>
                </button>
              ))}
              {files.map((f) => (
                <button key={f.id} className="pres-fm-item" onClick={() => onDrive(f.id)}>
                  <IcPaperclip size={15} /> <span className="nm">{f.nome}</span><span className="sz">{fmtSz(f.tamanho)}</span>
                </button>
              ))}
              {/* Partilhados comigo (só na raiz, sem busca) — também dá para apresentar. */}
              {!searching && !folderId && shared.length > 0 && (
                <>
                  <div className="pres-fm-sec">{t("files.sharedWithMe")}</div>
                  {shared.map((f) => (
                    <button key={"sh" + f.id} className="pres-fm-item" onClick={() => onDrive(f.id)}>
                      <IcUsers size={15} /> <span className="nm">{f.nome}</span><span className="sz">{fmtSz(f.tamanho)}</span>
                    </button>
                  ))}
                </>
              )}
              {folders.length === 0 && files.length === 0 && shared.length === 0 && (
                <div className="editor-msg">{searching ? t("drivePicker.noResults") : t("meet.presNoFiles")}</div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
