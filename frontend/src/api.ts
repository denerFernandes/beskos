import { apiUrl, authHeaders, notifyUnauthorized } from "./apiBase";
// Client da NOSSA API Go (control plane). O webmail (Fase 2) falará JMAP direto com o
// Stalwart; aqui é só painel/admin/billing/storage. Nenhuma credencial de admin do
// Stalwart passa por aqui (brief §2/§8). Sessão via cookie HttpOnly (same-origin).

export interface Me {
  id: string;
  email: string;
  role: string;
  tenant_id: { Bytes?: string; Valid?: boolean } | string;
  mfa_setup_required?: boolean; // política da org exige 2FA e o utilizador ainda não configurou
  org_mfa_required?: boolean; // política de 2FA da organização está ligada
  mfa_required_method?: string; // any | totp | sms
  language: string; // "" = automático (browser); senão a preferência salva na conta
  permissions: string[]; // gates da UI (ver internal/rbac)
  mfa: { totp: boolean; sms: boolean };
  disabled_apps?: string[]; // apps que o admin desligou p/ a org — o rail some com elas
  assistant_enabled?: boolean; // falso se o admin desligou o assistente (ou IA não configurada) — a bolha some
}

export interface Member { id: string; email: string; name?: string; role: string; created_at: string; has_mailbox?: boolean }
export interface MemberPermissions {
  role: string; all: string[]; from_role: string[]; effective: string[];
  allow: string[]; deny: string[]; editable: string[];
}
export interface Group { id: string; name: string; channel_id: string; members: number }
export interface GroupMember { id: string; email: string; name: string; role: string }
export interface Invite { id: string; email: string; role: string; token: string; created_at: string }
export interface Plan {
  id: string; name: string; mailbox_range: string;
  max_mailboxes: number; max_storage_bytes: number; max_daily_send: number; price_cents_per_user: number;
  // Recursos do catálogo do manager (docs/manager.md §3.1) — a tabela de planos lista tudo.
  min_users: number; apps_enabled: string[]; ai_tokens_per_window: number;
  max_meet_participants: number; meet_max_minutes: number;
  recording: string; recording_retention_days: number;
  onlyoffice_edit: boolean; wiki_public_links: boolean; active: boolean; currency?: string;
}
export interface BillingInfo {
  provider: string; current_plan: Plan; status: string; plans: Plan[];
  currency: string; min_paid_users: number; seats: number;
  subscription?: { manageable: boolean; status: string; cancel_at_period_end: boolean; current_period_end: number };
}
export interface Usage {
  plan: Plan;
  usage: { mailboxes: number; storage_bytes: number; mail_sent_today: number };
  limits: { max_mailboxes: number; max_storage_bytes: number; max_daily_send: number };
}

// Idiomas suportados na UI (espelha o backend). "" = automático pelo browser.
export type Language = "" | "pt-BR" | "en";

export interface Domain {
  id: string;
  dominio: string;
  provision_state: string;
}

export interface DNSRecord {
  host: string;
  type: string;
  value: string;
  purpose: string; // mx | spf | dkim | dkim_ses | dmarc | other
}

export interface DomainDetail {
  id: string;
  dominio: string;
  provision_state: string;
  dkim_status: string;
  dns_status: string;
  dns_records: DNSRecord[];
}

import i18n from "./i18n";

// Traduz o erro pela CHAVE estável do backend (error_code — lei §4.4), com fallback
// para a mensagem do servidor e depois para um genérico. Nunca mostra HTTP cru ao usuário.
function translateError(code: string | undefined, serverMsg: string | undefined, status: number): string {
  if (code) {
    return i18n.t(`server.${code}`, {
      defaultValue: serverMsg || i18n.t("errors.http", { status }),
    });
  }
  return serverMsg || i18n.t("errors.http", { status });
}

async function req<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(apiUrl(path), {
    ...opts,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...authHeaders(), ...(opts.headers || {}) },
  });
  if (!res.ok) {
    if (res.status === 401) notifyUnauthorized(path); // sessão morta → volta ao login (global)
    const body = await res.json().catch(() => ({}));
    throw new Error(translateError(body.error_code, body.error, res.status));
  }
  return res.json() as Promise<T>;
}

// Estado de um registo DNS no mundo real: o que pedimos, o que está publicado e o veredito.
export interface DNSCheck {
  host: string; type: string; expected: string; purpose: string;
  found: string[]; status: "ok" | "missing" | "mismatch"; required: boolean;
}
export interface DNSCheckResult {
  records: DNSCheck[]; checked_at: string | null; cached?: boolean;
  summary: { total: number; ok: number; missing: number; mismatch: number; required_pending: number };
}

export const api = {
  me: () => req<Me>("/api/me"),
  // Verificação do DNS a pedido (informativa: quem promove o domínio é o worker).
  dnsCheck: (id: string) => req<DNSCheckResult>(`/api/domains/${id}/dns/check`),
  // Exportar a zona: descarrega por fetch (com a sessão) e guarda como ficheiro — assim o token
  // nunca vai parar a uma URL num href.
  dnsExport: async (id: string, format: "zone" | "csv", domain: string): Promise<void> => {
    const res = await fetch(apiUrl(`/api/domains/${id}/dns/export?format=${format}`), {
      credentials: "include", headers: authHeaders(),
    });
    if (!res.ok) throw new Error(translateError(undefined, undefined, res.status));
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = format === "csv" ? `${domain}-dns.csv` : `${domain}.zone`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  },
  // Apps da organização: lista alternáveis + desligadas; PUT grava as desligadas (só admin/owner).
  appsGet: () => req<{ apps: string[]; disabled: string[] }>("/api/apps"),
  appsSet: (disabled: string[]) => req<{ disabled: string[] }>("/api/apps", { method: "PUT", body: JSON.stringify({ disabled }) }),
  signup: (org_name: string, email: string, password: string) =>
    req<{ tenant_id: string; user_id: string }>("/api/auth/signup", {
      method: "POST",
      body: JSON.stringify({ org_name, email, password }),
    }),
  login: (email: string, password: string) =>
    req<{ user_id?: string; mfa_required?: boolean; challenge?: string; methods?: string[] }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),
  logout: () => req<{ status: string }>("/api/webmail/logout", { method: "POST" }),
  domains: () => req<Domain[]>("/api/domains"),
  deleteDomain: (id: string) =>
    req<{ status: string }>(`/api/domains/${id}`, { method: "DELETE" }),
  createDomain: (dominio: string) =>
    req<Domain>("/api/domains", { method: "POST", body: JSON.stringify({ dominio }) }),
  getDomain: (id: string) => req<DomainDetail>(`/api/domains/${id}`),
  createMailbox: (domainId: string, login: string, name: string, password: string) =>
    req<{ email: string }>(`/api/domains/${domainId}/mailboxes`, {
      method: "POST",
      body: JSON.stringify({ login, name, password }),
    }),
  updateLanguage: (language: Language) =>
    req<{ language: string }>("/api/me/settings", {
      method: "PATCH",
      body: JSON.stringify({ language }),
    }),

  // Membros / RBAC
  members: () => req<Member[]>("/api/members"),
  // Grupos de utilizadores (criar grupo cria o canal privado no chat).
  groups: () => req<Group[]>("/api/groups"),
  createGroup: (name: string, members: string[] = []) => req<Group>("/api/groups", { method: "POST", body: JSON.stringify({ name, members }) }),
  renameGroup: (id: string, name: string) => req<Group>(`/api/groups/${id}`, { method: "PATCH", body: JSON.stringify({ name }) }),
  deleteGroup: (id: string) => req<{ status: string }>(`/api/groups/${id}`, { method: "DELETE" }),
  groupMembers: (id: string) => req<GroupMember[]>(`/api/groups/${id}/members`),
  addGroupMember: (id: string, user_id: string) => req<{ status: string }>(`/api/groups/${id}/members`, { method: "POST", body: JSON.stringify({ user_id }) }),
  removeGroupMember: (id: string, userId: string) => req<{ status: string }>(`/api/groups/${id}/members/${userId}`, { method: "DELETE" }),
  // Grupos DO usuário — um usuário pode estar em vários.
  userGroups: (id: string) => req<{ id: string; name: string }[]>(`/api/members/${id}/groups`),
  setUserGroups: (id: string, group_ids: string[]) => req<{ id: string; name: string }[]>(`/api/members/${id}/groups`, { method: "PUT", body: JSON.stringify({ group_ids }) }),
  invites: () => req<Invite[]>("/api/invites"),
  createInvite: (email: string, role: string) =>
    req<Invite>("/api/invites", { method: "POST", body: JSON.stringify({ email, role }) }),
  revokeInvite: (id: string) => req<unknown>(`/api/invites/${id}`, { method: "DELETE" }),
  // Permissões efetivas de um membro: o que vem do papel, o que foi ajustado à mão e o que EU
  // posso mexer (não se concede o que não se tem).
  memberPermissions: (id: string) => req<MemberPermissions>(`/api/members/${id}/permissions`),
  setMemberPermissions: (id: string, allow: string[], deny: string[]) =>
    req<{ effective: string[]; allow: string[]; deny: string[] }>(`/api/members/${id}/permissions`, { method: "PUT", body: JSON.stringify({ allow, deny }) }),
  changeRole: (id: string, role: string) =>
    req<Member>(`/api/members/${id}/role`, { method: "PATCH", body: JSON.stringify({ role }) }),
  removeMember: (id: string) => req<unknown>(`/api/members/${id}`, { method: "DELETE" }),

  // MFA
  securityPolicyGet: () => req<{ mfa_required: boolean; mfa_method: string }>("/api/org/security"),
  securityPolicySet: (on: boolean, method: string) =>
    req<{ mfa_required: boolean; mfa_method: string }>("/api/org/security", { method: "PUT", body: JSON.stringify({ mfa_required: on, mfa_method: method }) }),
  totpStart: () => req<{ secret: string; otpauth_uri: string }>("/api/mfa/totp/start", { method: "POST" }),
  totpEnable: (code: string) =>
    req<{ recovery_codes: string[] }>("/api/mfa/totp/enable", { method: "POST", body: JSON.stringify({ code }) }),
  smsStart: (phone: string) =>
    req<{ challenge: string }>("/api/mfa/sms/start", { method: "POST", body: JSON.stringify({ phone }) }),
  smsEnable: (challenge: string, code: string) =>
    req<{ recovery_codes: string[] }>("/api/mfa/sms/enable", { method: "POST", body: JSON.stringify({ challenge, code }) }),
  mfaDisable: (password: string) => req<unknown>("/api/mfa/disable", { method: "POST", body: JSON.stringify({ password }) }),
  mfaVerify: (challenge: string, code: string) =>
    req<{ user_id: string }>("/api/auth/mfa/verify", { method: "POST", body: JSON.stringify({ challenge, code }) }),

  // Caixas de email (gestão pelo painel)
  mailboxes: () => req<{ id: string; email: string; name: string; role: string; created_at: string }[]>("/api/mailboxes"),
  resetMailboxPassword: (id: string, password: string) =>
    req<{ status: string }>(`/api/mailboxes/${id}/password`, { method: "POST", body: JSON.stringify({ password }) }),

  // Usuários unificados = caixa + papel. Criar provisiona os dois juntos.
  createUser: (domain_id: string, login: string, name: string, password: string, role: string, groups: string[] = []) =>
    req<{ id: string; email: string; role: string }>("/api/users", { method: "POST", body: JSON.stringify({ domain_id, login, name, password, role, quota_gb: 5, groups }) }),
  resetUserPassword: (email: string, password: string) =>
    req<{ status: string }>("/api/users/reset-password", { method: "POST", body: JSON.stringify({ email, password }) }),

  // Billing / uso
  billing: () => req<BillingInfo>("/api/billing"),
  usage: () => req<Usage>("/api/usage"),
  checkout: (plan: string) => req<{ url?: string; applied: boolean }>("/api/billing/checkout", { method: "POST", body: JSON.stringify({ plan }) }),
  billingCancel: () => req<{ status: string }>("/api/billing/cancel", { method: "POST", body: "{}" }),
  billingResume: () => req<{ status: string }>("/api/billing/resume", { method: "POST", body: "{}" }),
  billingPortal: () => req<{ url: string }>("/api/billing/portal", { method: "POST", body: "{}" }),
};

export function hasPerm(me: Me | undefined, perm: string): boolean {
  return !!me?.permissions?.includes(perm);
}
