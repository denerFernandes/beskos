import * as Y from "@y/y";
import * as encoding from "lib0/encoding";
import * as decoding from "lib0/decoding";
import * as syncProtocol from "@y/protocols/sync";
import * as awarenessProtocol from "@y/protocols/awareness";

import { API_BASE, getSessionToken } from "../apiBase";

// Provider Yjs da wiki (B10): fala com o RELAY em Go (/api/wiki/pages/:id/collab/ws) usando o
// protocolo y-websocket clássico — varuint de tipo (0=sync, 1=awareness) + payload. O servidor só
// retransmite; são os clientes que respondem aos sync steps uns dos outros. O 1º frame do servidor é
// texto {"type":"hello","peers":N}: peers===0 → quem entrou semeia o doc do body_json; peers>0 →
// começa vazio e sincroniza dos pares (semear duas vezes duplicaria o conteúdo).

const MSG_SYNC = 0;
const MSG_AWARENESS = 1;

export class WikiCollabProvider {
  readonly doc: Y.Doc;
  readonly awareness: awarenessProtocol.Awareness;
  // O hello pode chegar ANTES de o editor registar o callback (o WS liga no construtor): guarda-se
  // o valor e dispara-se ao registar — sem isto o seed do template perdia a corrida e a página
  // abria vazia no editor.
  private helloPeers: number | null = null;
  private helloCb?: (peers: number) => void;
  /** Regista o callback do hello; se o hello já chegou, dispara imediatamente. */
  set onHello(cb: ((peers: number) => void) | undefined) {
    this.helloCb = cb;
    if (cb && this.helloPeers !== null) cb(this.helloPeers);
  }

  private ws: WebSocket | null = null;
  private closed = false;
  private retry = 1000;
  private readonly url: string;

  constructor(pageId: string, doc: Y.Doc) {
    this.doc = doc;
    this.awareness = new awarenessProtocol.Awareness(doc);
    const base = API_BASE || location.origin;
    const u = new URL(base);
    u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
    u.pathname = `/api/wiki/pages/${pageId}/collab/ws`;
    const t = getSessionToken();
    if (t) u.searchParams.set("access_token", t);
    this.url = u.toString();

    this.doc.on("update", this.handleDocUpdate);
    this.awareness.on("update", this.handleAwarenessUpdate);
    // NÃO conecta aqui: em dev o StrictMode invoca o initializer do estado DUAS vezes e o provider
    // órfão ocuparia a sala (o real receberia peers=1 e nunca semearia). Liga-se no useEffect
    // via ensureConnected() — só a instância adotada pelo React abre o WS.
  }

  private started = false;
  private destroyTimer: ReturnType<typeof setTimeout> | null = null;
  /** Abre (ou reabre) a ligação. Chamado no mount do editor; idempotente e cancela um destroy adiado. */
  ensureConnected() {
    if (this.destroyTimer) { clearTimeout(this.destroyTimer); this.destroyTimer = null; }
    if (this.started && !this.closed) return;
    this.started = true;
    this.closed = false;
    this.retry = 1000;
    if (!this.ws) this.connect();
  }

  /** Destroy ADIADO (150ms): o StrictMode monta→desmonta→monta em dev; fechar o WS no cleanup do
   *  mount falso abria-e-fechava um socket por cada edição (EPIPE no proxy). O remount cancela. */
  scheduleDestroy() {
    if (this.destroyTimer) clearTimeout(this.destroyTimer);
    this.destroyTimer = setTimeout(() => this.destroy(), 150);
  }

  destroy() {
    this.closed = true;
    this.doc.off("update", this.handleDocUpdate);
    this.awareness.off("update", this.handleAwarenessUpdate);
    awarenessProtocol.removeAwarenessStates(this.awareness, [this.doc.clientID], "destroy");
    this.ws?.close();
    this.ws = null;
  }

  private connect() {
    if (this.closed) return;
    let sock: WebSocket;
    try {
      sock = new WebSocket(this.url);
    } catch {
      this.scheduleReconnect();
      return;
    }
    sock.binaryType = "arraybuffer";
    this.ws = sock;
    sock.onopen = () => {
      this.retry = 1000;
      // SyncStep1: pede o estado aos pares (via relay).
      const enc = encoding.createEncoder();
      encoding.writeVarUint(enc, MSG_SYNC);
      syncProtocol.writeSyncStep1(enc, this.doc);
      sock.send(encoding.toUint8Array(enc));
      // Anuncia o próprio estado de awareness (cursor/utilizador).
      const aw = encoding.createEncoder();
      encoding.writeVarUint(aw, MSG_AWARENESS);
      encoding.writeVarUint8Array(aw, awarenessProtocol.encodeAwarenessUpdate(this.awareness, [this.doc.clientID]));
      sock.send(encoding.toUint8Array(aw));
    };
    sock.onmessage = (e) => {
      if (typeof e.data === "string") {
        try {
          const hello = JSON.parse(e.data) as { type?: string; peers?: number };
          if (hello.type === "hello") {
            this.helloPeers = hello.peers ?? 0;
            this.helloCb?.(this.helloPeers);
          }
        } catch { /* frame de controlo inválido: ignora */ }
        return;
      }
      const dec = decoding.createDecoder(new Uint8Array(e.data as ArrayBuffer));
      const kind = decoding.readVarUint(dec);
      if (kind === MSG_SYNC) {
        const reply = encoding.createEncoder();
        encoding.writeVarUint(reply, MSG_SYNC);
        syncProtocol.readSyncMessage(dec, reply, this.doc, this);
        if (encoding.length(reply) > 1) sock.send(encoding.toUint8Array(reply));
      } else if (kind === MSG_AWARENESS) {
        awarenessProtocol.applyAwarenessUpdate(this.awareness, decoding.readVarUint8Array(dec), this);
      }
    };
    sock.onerror = () => sock.close();
    sock.onclose = () => {
      this.ws = null;
      this.scheduleReconnect();
    };
  }

  private scheduleReconnect() {
    if (this.closed) return;
    setTimeout(() => this.connect(), this.retry);
    this.retry = Math.min(this.retry * 2, 15000);
  }

  private send(data: Uint8Array) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) this.ws.send(data);
  }

  // Update local do doc → retransmite aos pares (origin === this → veio da rede, não re-emite).
  private handleDocUpdate = (update: Uint8Array, origin: unknown) => {
    if (origin === this) return;
    const enc = encoding.createEncoder();
    encoding.writeVarUint(enc, MSG_SYNC);
    syncProtocol.writeUpdate(enc, update);
    this.send(encoding.toUint8Array(enc));
  };

  private handleAwarenessUpdate = ({ added, updated, removed }: { added: number[]; updated: number[]; removed: number[] }) => {
    const changed = [...added, ...updated, ...removed];
    if (changed.length === 0) return;
    const enc = encoding.createEncoder();
    encoding.writeVarUint(enc, MSG_AWARENESS);
    encoding.writeVarUint8Array(enc, awarenessProtocol.encodeAwarenessUpdate(this.awareness, changed));
    this.send(encoding.toUint8Array(enc));
  };
}
