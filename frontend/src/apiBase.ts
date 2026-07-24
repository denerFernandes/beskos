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

// Origem PÚBLICA da app web — para links PARTILHÁVEIS (convite de reunião, link em e-mail, partilha
// de ficheiro, link de tarefa). No app nativo a origem local é tauri://localhost, inútil para
// terceiros; usa-se VITE_PUBLIC_BASE (URL pública da web) ou, na falta, API_BASE (em produção o
// Caddy serve SPA e API na mesma origem). Na web fica a própria origem. NUNCA anexar aqui o token
// de sessão: um link partilhado não pode transportar credenciais de quem o copiou.
export const PUBLIC_BASE = String((import.meta as { env?: Record<string, string> }).env?.VITE_PUBLIC_BASE ?? "").replace(/\/$/, "");
export function publicUrl(path: string): string {
  const base = PUBLIC_BASE || API_BASE || (typeof location !== "undefined" ? location.origin : "");
  return base + path;
}

// Token de sessão do backend. Na web o cookie (HttpOnly) chega; mas o app nativo (Tauri) é
// cross-site e não recebe o cookie, por isso guardamos o token do login e enviamo-lo em
// `Authorization: Bearer`. O backend aceita header OU cookie.
const TOKEN_KEY = "mailin.session";
// Na web (same-origin) o cookie HttpOnly já mantém a sessão — manter uma cópia do token em
// localStorage só dá a um XSS um segredo a mais para roubar. Por isso, na web o token fica só em
// memória; só o app nativo (cross-site, sem cookie) o persiste para reenviar em Authorization.
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

// mediaUrl monta uma URL absoluta da API para usar em <img>/<video>/<iframe>/<a> ou download. Como
// essas requisições NÃO enviam header (nem cookie cross-site no app nativo), o token de sessão vai
// na query (?access_token=) — o backend aceita-o (ver requireWebmailSession). Na web (sem token
// guardado) fica só o cookie same-origin, sem expor o token na URL.
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

// ---- Sessão inválida (401) — tratamento GLOBAL (23/07/2026) ----
// Quando a sessão morre no servidor (expirou, admin ligou a política de MFA e matou as sessões,
// senha trocada noutra sessão…), qualquer chamada protegida devolve 401. O handler registado
// (auth.ts) limpa o estado local e a app volta ao ecrã de login — sem isto ficava-se a navegar
// numa app vazia. Endpoints de login/logout não disparam (o 401 deles é "senha errada").
let unauthorizedHandler: (() => void) | null = null;
export function setUnauthorizedHandler(h: () => void): void { unauthorizedHandler = h; }
export function notifyUnauthorized(path: string): void {
  if (path.includes("/login") || path.includes("/logout") || path.includes("/auth/") || path.includes("/mfa/verify")) return;
  unauthorizedHandler?.();
}

