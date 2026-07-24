import { useEffect, useRef } from "react";
import { API_BASE, getSessionToken } from "../apiBase";

export interface ChatSocketEvent { type: string; channel_id: string; payload: Record<string, unknown> }

// URL do WebSocket do chat. Na web usa a mesma origem (proxy same-origin). No app nativo (Tauri) a
// origem é tauri://localhost — sem backend ali — por isso deriva o host/proto de API_BASE. O WS não
// pode mandar Authorization, então o token da sessão vai na query (?access_token=); o backend
// aceita-o só nesta rota (ver requireWebmailSession).
function chatWsUrl(): string {
  const base = API_BASE || location.origin; // API_BASE vazio na web
  const u = new URL(base);
  u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
  u.pathname = "/api/chat/ws";
  const t = getSessionToken();
  if (t) u.searchParams.set("access_token", t);
  return u.toString();
}

// Conexão WebSocket do chat (hub Redis no backend). Reconecta com backoff. Envia só "digitando";
// o resto trafega por REST + eventos de push.
export function useChatSocket(onEvent: (ev: ChatSocketEvent) => void): { sendTyping: (channelId: string) => void } {
  const cb = useRef(onEvent);
  cb.current = onEvent;
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    let closed = false;
    let retry = 1000;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const connect = () => {
      if (closed) return;
      // new WebSocket pode lançar de forma síncrona (URL inválida ou bloqueio de CSP). Isolar num
      // try/catch impede que um erro aqui derrube toda a app (tela em branco no Tauri).
      let sock: WebSocket;
      try {
        sock = new WebSocket(chatWsUrl());
      } catch {
        if (!closed) { timer = setTimeout(connect, retry); retry = Math.min(retry * 2, 15000); }
        return;
      }
      wsRef.current = sock;
      sock.onopen = () => { retry = 1000; };
      sock.onmessage = (e) => { try { cb.current(JSON.parse(e.data)); } catch { /* ignora frame inválido */ } };
      sock.onerror = () => sock.close();
      sock.onclose = () => {
        wsRef.current = null;
        if (!closed) { timer = setTimeout(connect, retry); retry = Math.min(retry * 2, 15000); }
      };
    };
    connect();
    return () => { closed = true; if (timer) clearTimeout(timer); wsRef.current?.close(); };
  }, []);

  const sendTyping = (channelId: string) => {
    const s = wsRef.current;
    if (s && s.readyState === WebSocket.OPEN) s.send(JSON.stringify({ type: "typing", channel_id: channelId }));
  };
  return { sendTyping };
}
