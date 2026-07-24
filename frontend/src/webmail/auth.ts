import { useCallback, useMemo, useSyncExternalStore } from "react";
import { JmapClient } from "../jmap/client";
import { MailService } from "../jmap/mail";
import { API_BASE, getSessionToken, setSessionToken, setUnauthorizedHandler } from "../apiBase";
import { backend } from "./backend";

// Auth do webmail (H11 / brokering): a credencial da caixa NÃO vive mais no browser. Login autentica
// na nossa API (sessão: cookie HttpOnly na web, token opaco no app nativo) e o backend guarda a
// credencial JMAP cifrada; o proxy a injeta server-to-server. Aqui só persistimos dados NÃO-secretos
// (e-mail + accountId) para reidratar a UI entre visitas — nada de senha.
const KEY = "mailin.wm.auth";

interface Stored {
  email: string;
  accountId: string;
}

function read(): Stored | null {
  const raw = localStorage.getItem(KEY);
  return raw ? (JSON.parse(raw) as Stored) : null;
}

// Store mínimo para o useSyncExternalStore reagir a login/logout.
const listeners = new Set<() => void>();
function emit() { listeners.forEach((l) => l()); }
function subscribe(l: () => void) { listeners.add(l); return () => listeners.delete(l); }

// Sessão inválida em QUALQUER chamada → limpa o estado local; o useSyncExternalStore re-renderiza
// para o ecrã de login. (Registado no módulo: corre uma vez, sem depender de componente montado.)
setUnauthorizedHandler(() => {
  if (!localStorage.getItem(KEY)) return; // já deslogado — evita loops
  setSessionToken("");
  localStorage.removeItem(KEY);
  emit();
});

export interface WebmailSession {
  email: string;
  service: MailService;
  logout: () => void;
}

export function useWebmail(): { session: WebmailSession | null; login: typeof login } {
  const stored = useSyncExternalStore(subscribe, () => localStorage.getItem(KEY));

  const logout = useCallback(() => {
    void backend.logout();
    setSessionToken("");
    localStorage.removeItem(KEY);
    emit();
  }, []);

  // Memoiza por conta: o MailService (e o JmapClient) mantêm identidade estável entre renders,
  // essencial para o push SSE (useJmapPush) não reconectar a cada render. O JMAP autentica-se pela
  // sessão: cookie same-origin na web, Bearer do token no app nativo (getSessionToken).
  const session = useMemo<WebmailSession | null>(() => {
    if (!stored) return null;
    const parsed = JSON.parse(stored) as Stored;
    return {
      email: parsed.email,
      service: new MailService(new JmapClient(API_BASE, { bearer: getSessionToken() }), parsed.accountId),
      logout,
    };
  }, [stored, logout]);

  return { session, login };
}

// MfaChallenge: quando o utilizador tem 2º fator, o login devolve isto em vez de uma sessão. A UI
// pede o código e chama verifyMfa para completar.
export interface MfaChallenge { challenge: string; methods: string[] }

// establishSession termina o login com o token emitido: guarda-o (memória/localStorage), valida a
// sessão JMAP pelo nosso proxy e persiste a conta. Partilhado pelos caminhos sem MFA e pós-MFA.
async function establishSession(email: string, token: string): Promise<void> {
  setSessionToken(token); // memória (web) / localStorage (app nativo) — token opaco, não a senha
  const client = new JmapClient(API_BASE, { bearer: getSessionToken() });
  const session = await client.session(); // autenticado pela sessão; o proxy injeta a Basic da caixa
  const stored: Stored = { email, accountId: session.accountId };
  localStorage.setItem(KEY, JSON.stringify(stored));
  emit();
}

// login abre a sessão na nossa API (que valida a credencial no Stalwart e guarda a cred JMAP cifrada
// server-side). Se a conta tem 2º fator, NÃO há sessão ainda: devolve o desafio para a UI pedir o
// código (senão o token vinha vazio e a sessão JMAP dava 401 — bug 24/07/2026). Sem MFA, entra logo.
export async function login(email: string, password: string): Promise<MfaChallenge | null> {
  const resp = await backend.login(email, password); // 401 → lança e não persiste
  if (resp.mfa_required) return { challenge: resp.challenge ?? "", methods: resp.methods ?? [] };
  await establishSession(email, resp.token ?? "");
  return null;
}

// verifyMfa troca o desafio + código pelo token e completa o login.
export async function verifyMfa(email: string, challenge: string, code: string): Promise<void> {
  const resp = await backend.mfaVerify(challenge, code);
  await establishSession(email, resp.token);
}

export { read as readWebmailAuth };
