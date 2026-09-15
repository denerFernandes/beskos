import i18n from "./i18n";

// Absolute base for the API/JMAP. Empty on the web (same origin: the Vite/Caddy proxy handles
// /api and /jmap). In the packaged app (Tauri desktop/mobile) there is NO proxy — VITE_API_BASE is
// set with the server's URL (e.g. https://app.mailin.com) at build time, and all calls become absolute.
// (import.meta as any) avoids depending on the vite/client types in tsc; at runtime Vite injects the env.
export const API_BASE = String((import.meta as { env?: Record<string, string> }).env?.VITE_API_BASE ?? "").replace(/\/$/, "");

// Prefixes a path in our API/JMAP with the base (no-op on the web).
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
const nativeApp = API_BASE !== ""; // the packaged build (Tauri) sets VITE_API_BASE
export function getSessionToken(): string {
  if (memToken) return memToken;
  if (!nativeApp) return "";
  try { return localStorage.getItem(TOKEN_KEY) ?? ""; } catch { return ""; }
}
export function setSessionToken(token: string): void {
  memToken = token;
  if (!nativeApp) { try { localStorage.removeItem(TOKEN_KEY); } catch { /* private mode */ } return; }
  try { if (token) localStorage.setItem(TOKEN_KEY, token); else localStorage.removeItem(TOKEN_KEY); } catch { /* private mode */ }
}
export function authHeaders(): Record<string, string> {
  const t = getSessionToken();
  const h: Record<string, string> = {};
  // Web (same origin, cookie): X-Mailin-Client is the anti-CSRF token the backend requires on
  // mutations (a third-party site can't set this header without hitting CORS). In the native app
  // (cross-site) we authenticate via Authorization Bearer — which already satisfies csrfGuard —
  // and sending a custom header would only make the CORS preflight fail; so we do NOT send it natively.
  if (!nativeApp) h["X-Mailin-Client"] = "1";
  if (t) h.Authorization = `Bearer ${t}`;
  // The PAGE's language: the AI generates content (briefing, summaries…) in this language, not a
  // fixed one. Read from i18next; allowed in CORS (X-Locale). Chat is separate (it mirrors the
  // language the person types in).
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

// triggerDownload downloads a URL (already carrying the token, via mediaUrl) without navigating
// the window out of the app -- in Tauri's webview a location.assign would break the SPA. Uses a
// temporary <a download>; the backend sends Content-Disposition: attachment, so the webview
// treats it as a download.
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

