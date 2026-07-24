// Client JMAP de baixo nível. Agnóstico de ambiente: no browser usa baseUrl "" (mesma origem
// via reverse proxy — DIVERGENCES §D7); nos testes aponta direto para o Stalwart.
//
// Auth (H11 / brokering): em produção o browser NÃO manda a credencial da caixa. Fala com o NOSSO
// proxy autenticado pela sessão — cookie same-origin (web) ou Bearer do token de sessão (app nativo)
// — e o proxy injeta a Basic da caixa server-to-server. Os testes de integração ainda usam Basic
// direto contra o Stalwart (opção `basic`).

import { notifyUnauthorized } from "../apiBase";
import type { JmapSession } from "./types";

const MAIL = "urn:ietf:params:jmap:mail";
const CORE = "urn:ietf:params:jmap:core";
const SUBMISSION = "urn:ietf:params:jmap:submission";

export type MethodCall = [string, Record<string, unknown>, string];

export class JmapError extends Error {}

export class JmapClient {
  private baseUrl: string;
  private authHeader: string;
  private sessionAuth: boolean; // true = auth pela NOSSA sessão (web/nativo); 401 → volta ao login

  // Produção: passe { bearer } (app nativo) ou nada (web usa o cookie). Testes: { basic: base64(email:senha) }.
  constructor(baseUrl = "", opts: { basic?: string; bearer?: string } = {}) {
    this.baseUrl = baseUrl;
    this.authHeader = opts.basic ? "Basic " + opts.basic : opts.bearer ? "Bearer " + opts.bearer : "";
    this.sessionAuth = !opts.basic; // Basic = testes de integração; não mexe no ecrã de login
  }

  // guard401: quando o proxy devolve 401 a sessão morreu (expirou, MFA forçado, senha trocada) — o
  // JMAP é o caminho principal do e-mail, por isso PRECISA de acionar o logout global igual às
  // chamadas /api/* (senão o app nativo abre numa app vazia sem voltar ao login). 24/07/2026.
  private guard401(status: number): void {
    if (status === 401 && this.sessionAuth) notifyUnauthorized("/jmap");
  }

  static encodeAuth(email: string, password: string): string {
    return btoa(`${email}:${password}`);
  }

  // h monta os headers: Authorization só quando há credencial explícita (Basic teste / Bearer nativo);
  // na web fica vazio e o cookie de sessão (same-origin) autentica no proxy.
  private h(extra: Record<string, string> = {}): Record<string, string> {
    return this.authHeader ? { Authorization: this.authHeader, ...extra } : extra;
  }

  async session(): Promise<JmapSession> {
    const res = await fetch(`${this.baseUrl}/jmap/session`, {
      headers: this.h(),
      credentials: "include",
    });
    if (!res.ok) { this.guard401(res.status); throw new JmapError(`sessão JMAP: HTTP ${res.status}`); }
    const s = await res.json();
    const accountId = s.primaryAccounts?.[MAIL];
    if (!accountId) throw new JmapError("conta de email não encontrada na sessão");
    return {
      accountId,
      identityId: s.primaryAccounts?.[SUBMISSION],
      capabilities: s.capabilities ?? {},
      downloadUrlTemplate: s.downloadUrl ?? "",
    };
  }

  // request executa um lote de chamadas JMAP e devolve os payloads na ordem enviada.
  async request(using: string[], calls: MethodCall[]): Promise<Record<string, unknown>[]> {
    const res = await fetch(`${this.baseUrl}/jmap/`, {
      method: "POST",
      headers: this.h({ "Content-Type": "application/json" }),
      credentials: "include",
      body: JSON.stringify({ using: [CORE, ...using], methodCalls: calls }),
    });
    if (!res.ok) { this.guard401(res.status); throw new JmapError(`JMAP: HTTP ${res.status}`); }
    const body = await res.json();
    if (!Array.isArray(body.methodResponses)) throw new JmapError("resposta JMAP inválida");
    return body.methodResponses.map((mr: [string, Record<string, unknown>, string]) => {
      if (mr[0] === "error") throw new JmapError(`método JMAP: ${JSON.stringify(mr[1])}`);
      return mr[1];
    });
  }

  // downloadBlob baixa um anexo autenticado (Basic por header) e devolve o Blob. Necessário
  // porque <img>/<iframe>/<a download> não mandam Authorization → dariam 401 no path direto.
  async downloadBlob(accountId: string, blobId: string, name: string, type: string): Promise<Blob> {
    const url = `${this.baseUrl}/jmap/download/${encodeURIComponent(accountId)}/` +
      `${encodeURIComponent(blobId)}/${encodeURIComponent(name)}?accept=${encodeURIComponent(type)}`;
    const res = await fetch(url, { headers: this.h(), credentials: "include" });
    if (!res.ok) { this.guard401(res.status); throw new JmapError(`download: HTTP ${res.status}`); }
    return res.blob();
  }

  // streamEvents consome o EventSource do JMAP (push de StateChange, RFC 8620 §7.3). O
  // EventSource nativo não deixa mandar Authorization, então lemos o SSE via fetch streaming.
  // Chama onState() a cada evento que não seja ping. Retorna quando a conexão encerra.
  async streamEvents(onState: () => void, signal: AbortSignal): Promise<void> {
    const url = `${this.baseUrl}/jmap/eventsource?types=*&closeafter=no&ping=30`;
    const res = await fetch(url, {
      headers: this.h({ Accept: "text/event-stream" }),
      credentials: "include",
      signal,
    });
    if (!res.ok || !res.body) { this.guard401(res.status); throw new JmapError(`eventsource: HTTP ${res.status}`); }
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) return;
      buf += dec.decode(value, { stream: true });
      let idx: number;
      while ((idx = buf.indexOf("\n\n")) >= 0) {
        const frame = buf.slice(0, idx);
        buf = buf.slice(idx + 2);
        const lines = frame.split("\n");
        const ev = lines.find((l) => l.startsWith("event:"))?.slice(6).trim();
        const hasData = lines.some((l) => l.startsWith("data:"));
        if (hasData && ev !== "ping") onState();
      }
    }
  }

  // upload sobe um blob para a conta e devolve {blobId, type, size}. Path same-origin via proxy
  // (/jmap/upload/{accountId}/ — padrão do Stalwart, DIVERGENCES §D7).
  async upload(accountId: string, file: File): Promise<{ blobId: string; type: string; size: number }> {
    const res = await fetch(`${this.baseUrl}/jmap/upload/${encodeURIComponent(accountId)}/`, {
      method: "POST",
      headers: this.h({ "Content-Type": file.type || "application/octet-stream" }),
      credentials: "include",
      body: file,
    });
    if (!res.ok) { this.guard401(res.status); throw new JmapError(`upload: HTTP ${res.status}`); }
    const b = await res.json();
    if (!b.blobId) throw new JmapError("upload sem blobId");
    return { blobId: b.blobId, type: b.type ?? "", size: b.size ?? file.size };
  }

  // downloadUrl monta a URL de download de um blob (anexo) usando o path do template da sessão,
  // reescrito para a origem atual (não o serverHostname — DIVERGENCES §D7).
  downloadUrl(session: JmapSession, accountId: string, blobId: string, name: string, type: string): string {
    const path = new URL(session.downloadUrlTemplate, "http://x").pathname; // pega só o path
    return (
      this.baseUrl +
      path
        .replace("{accountId}", encodeURIComponent(accountId))
        .replace("{blobId}", encodeURIComponent(blobId))
        .replace("{name}", encodeURIComponent(name)) +
      `?accept=${encodeURIComponent(type)}`
    );
  }
}

export const USING_MAIL = [MAIL];
export const USING_SEND = [MAIL, SUBMISSION];
