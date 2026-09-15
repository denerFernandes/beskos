import i18n from "./i18n";

// Base absoluta da API/JMAP. Vazia na web (mesma origem: o proxy do Vite/Caddy trata /api e /jmap).
// No app empacotado (Tauri desktop/mobile) NÃO há proxy — define-se VITE_API_BASE com a URL do
// servidor (ex.: https://app.mailin.com) no build, e todas as chamadas passam a ser absolutas.
// (import.meta as any) evita depender dos tipos vite/client no tsc; em runtime o Vite injeta env.
export const API_BASE = String((import.meta as { env?: Record<string, string> }).env?.VITE_API_BASE ?? "").replace(/\/$/, "");

// Prefixa um caminho da nossa API/JMAP com a base (no-op na web).
export function apiUrl(path: string): string {
  return API_BASE + path;
}

// PUBLIC origin of the web app -- for SHAREABLE links (meeting invite, link in an email, file
// share, task link). In the native app the local origin is tauri://localhost, useless to third
// parties; use VITE_PUBLIC_BASE (the public web URL) or, failing that, API_BASE (in production
// Caddy serves the SPA and the API from the same origin). On the web it is just the origin itself.
// NEVER attach the session token here: a shared link must not carry the credentials of whoever
// copied it.
export const PUBLIC_BASE = String((import.meta as { env?: Record<string, string> }).env?.VITE_PUBLIC_BASE ?? "").replace(/\/$/, "");
export function publicUrl(path: string): string {
  const base = PUBLIC_BASE || API_BASE || (typeof location !== "undefined" ? location.origin : "");
  return base + path;
}

// Backend session token. On the web the HttpOnly cookie is enough, but the native app (Tauri) is
// cross-site and never receives the cookie, so it keeps the login token and sends it as
// `Authorization: Bearer`. The backend accepts either the header or the cookie.
const TOKEN_KEY = "mailin.session";
// On the web (same-origin) the HttpOnly cookie already keeps the session alive -- keeping a copy
// of the token in localStorage would just give an XSS one more secret to steal. So on the web the
// token stays in memory only; only the native app (cross-site, no cookie) persists it, to resend
// it via Authorization.
let memToken = "";
const nativeApp = API_BASE !== ""; // build empacotado (Tauri) define VITE_API_BASE
export function getSessionToken(): string {
  if (memToken) return memToken;
  if (!nativeApp) return "";
  try { return localStorage.getItem(TOKEN_KEY) ?? ""; } catch { return ""; }
}
export function setSessionToken(token: string): void {
  memToken = token;
  if (!nativeApp) { try { localStorage.removeItem(TOKEN_KEY); } catch { /* modo privado */ } return; }
  try { if (token) localStorage.setItem(TOKEN_KEY, token); else localStorage.removeItem(TOKEN_KEY); } catch { /* modo privado */ }
}
export function authHeaders(): Record<string, string> {
  const t = getSessionToken();
  const h: Record<string, string> = {};
  // Web (mesma origem, cookie): o X-Mailin-Client é o token anti-CSRF que o backend exige em
  // mutações (um site terceiro não consegue setar este header sem cair no CORS). No app nativo
  // (cross-site) autenticamos por Authorization Bearer — que já satisfaz o csrfGuard — e enviar um
  // header custom só faria o preflight CORS falhar; por isso NÃO o enviamos no nativo.
  if (!nativeApp) h["X-Mailin-Client"] = "1";
  if (t) h.Authorization = `Bearer ${t}`;
  // Idioma da PÁGINA: a IA gera conteúdo (briefing, resumos…) neste idioma, não num fixo. Lê-se do
  // i18next; permitido no CORS (X-Locale). O chat é à parte (espelha o idioma que a pessoa escreve).
  const lang = i18n.language;
  if (lang) h["X-Locale"] = lang;
  return h;
}

// mediaUrl builds an absolute API URL for use in <img>/<video>/<iframe>/<a> or a download. Since
// those requests send no header (and no cross-site cookie in the native app), the session token
// rides in the query string (?access_token=) -- the backend accepts it (see
// requireWebmailSession). On the web (no stored token) only the same-origin cookie is used, so the
// token never appears in the URL.
export function mediaUrl(path: string): string {
  const url = apiUrl(path);
  const t = getSessionToken();
  if (!t) return url;
  return url + (url.includes("?") ? "&" : "?") + "access_token=" + encodeURIComponent(t);
}

// triggerDownload baixa uma URL (já com token, via mediaUrl) sem navegar a janela para fora do app —
// no webview do Tauri um location.assign quebraria a SPA. Usa um <a download> temporário; o backend
// envia Content-Disposition: attachment, então o webview trata como transferência.
export function triggerDownload(url: string, filename?: string): void {
  const a = document.createElement("a");
  a.href = url;
  if (filename) a.download = filename;
  a.rel = "noopener";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

// ---- Invalid session (401) -- handled GLOBALLY ----
// When the session dies on the server (it expired, an admin turned on the MFA policy and killed
// all sessions, the password changed on another device...), any protected call returns 401. The
// registered handler (auth.ts) clears local state and the app falls back to the login screen --
// without this it would keep navigating an empty app. Login/logout endpoints do not trigger it
// (their 401 means "wrong password").
let unauthorizedHandler: (() => void) | null = null;
export function setUnauthorizedHandler(h: () => void): void { unauthorizedHandler = h; }
export function notifyUnauthorized(path: string): void {
  if (path.includes("/login") || path.includes("/logout") || path.includes("/auth/") || path.includes("/mfa/verify")) return;
  unauthorizedHandler?.();
}

