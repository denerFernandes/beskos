// Toda chamada à manager-api passa por aqui (lei 3.4). Erros voltam com error_code estável;
// os componentes traduzem via strings.ts.

export class ApiError extends Error {
  code: string;
  status: number;
  constructor(code: string, status: number) {
    super(code);
    this.code = code;
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      ...init,
    });
  } catch {
    throw new ApiError("network", 0);
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError((data as { error_code?: string }).error_code ?? "manager.failed", res.status);
  }
  return data as T;
}

export type ManagerUser = { id: string; email: string; name: string; role: "admin" | "viewer" };

export type LoginResult =
  | { totp_setup: true; secret: string; otpauth_uri: string }
  | { user: ManagerUser };

export type Overview = {
  tenants: number;
  tenants_active: number;
  users: number;
  users_active_7d: number;
  storage_bytes: number;
  domains: number;
};

export type TenantRow = {
  id: string;
  nome: string;
  plano: string;
  status: string;
  created_at: string;
  users: number;
  domains: number;
  storage_bytes: number;
};

export type TenantUser = {
  id: string;
  email: string;
  name: string;
  role: string;
  created_at: string;
  mail_seen_at: string | null;
  quota_bytes: number;
  mfa_enabled: boolean;
  storage_bytes: number;
};

export type TenantDetail = {
  tenant: TenantRow & { disabled_apps: string[]; currency?: string };
  users: TenantUser[];
};

export type DailyUsage = {
  tenant_id: string;
  day: string;
  users: number;
  users_active: number;
  storage_bytes: number;
  files: number;
  ai_tokens: number;
  meet_minutes: number;
  recordings: number;
  recordings_bytes: number;
  wiki_pages: number;
  cards: number;
  mail_sent: number;
};

export type UsageSeriesPoint = {
  day: string;
  users: number;
  users_active: number;
  storage_bytes: number;
  ai_tokens: number;
  meet_minutes: number;
  recordings: number;
  mail_sent: number;
};

export type Alert = {
  severity: "warn" | "critical";
  kind: string;
  tenant_id: string;
  tenant: string;
  detail: string;
};

export type TopAIUser = { email: string; name: string; tenant: string; tokens: number };

export type Deliverability = {
  id: string;
  nome: string;
  sent_30d: number;
  bounces_30d: number;
  complaints_30d: number;
};

export type GrowthRow = {
  month: string;
  tenants_new: number;
  users_new: number;
  tenants_total: number;
  users_total: number;
};

export type Operator = {
  id: string;
  email: string;
  name: string;
  role: string;
  totp_confirmed: boolean;
  active: boolean;
  last_login_at: string | null;
  created_at: string;
};

export type CostModel = {
  currency: string;
  fixed_month_cents: number;
  storage_gb_month_cents: number;
  ai_per_mtok_cents: number;
  email_per_1k_cents: number;
};

export type TenantFinance = {
  id: string;
  nome: string;
  plano: string;
  status: string;
  users: number;
  mrr_cents: number;
  cost_cents: number;
  margin_cents: number;
  storage_bytes: number;
  ai_tokens_30d: number;
};

export type Finance = {
  summary: {
    currency: string;
    mrr_cents: number;
    arr_cents: number;
    arpu_cents: number;
    fixed_cents: number;
    servers_cents: number;
    other_fixed_cents: number;
    variable_cents: number;
    margin_cents: number;
    paid_users: number;
  };
  tenants: TenantFinance[];
};

export type ManagerServer = {
  id: string;
  name: string;
  provider: string;
  monthly_cents: number;
  notes: string;
  active: boolean;
  created_at: string;
};

export type ServerInput = {
  id?: string;
  name: string;
  provider: string;
  monthly_cents: number;
  notes: string;
};

export type Plan = {
  key: string;
  name: string;
  description: string;
  price_cents_per_user: number;
  min_users: number;
  max_users: number;
  apps_enabled: string[];
  max_storage_gb: number;
  ai_tokens_per_window: number;
  meet_max_participants: number;
  meet_max_minutes: number;
  recording: "none" | "offline" | "offline_priority";
  recording_retention_days: number;
  max_daily_send: number;
  onlyoffice_edit: boolean;
  wiki_public_links: boolean;
  active: boolean;
  sort: number;
  // Preço por moeda (centavos): {"USD":1900,"BRL":9900}. price_cents_per_user é o da moeda base.
  prices?: Record<string, number>;
  stripe_product_id?: string;
  stripe_price_id?: string;
  created_at: string;
  updated_at: string;
};

export type PlanInput = Omit<Plan, "created_at" | "updated_at" | "active">;

export const api = {
  login: (email: string, password: string, code?: string) =>
    request<LoginResult>("/api/manager/login", {
      method: "POST",
      body: JSON.stringify({ email, password, code: code ?? "" }),
    }),
  me: () => request<{ user: ManagerUser }>("/api/manager/me"),
  logout: () => request<{ status: string }>("/api/manager/logout", { method: "POST", body: "{}" }),
  overview: () => request<Overview>("/api/manager/overview"),
  tenants: () => request<TenantRow[]>("/api/manager/tenants"),
  tenant: (id: string) => request<TenantDetail>(`/api/manager/tenants/${id}`),
  tenantUsage: (id: string, days = 14) =>
    request<DailyUsage[]>(`/api/manager/tenants/${id}/usage?days=${days}`),
  usageSeries: (days = 30) =>
    request<UsageSeriesPoint[]>(`/api/manager/usage/series?days=${days}`),
  growth: () => request<GrowthRow[]>("/api/manager/reports/growth"),
  alerts: () => request<{ alerts: Alert[]; top_ai_users: TopAIUser[] }>("/api/manager/alerts"),
  deliverability: () => request<Deliverability[]>("/api/manager/deliverability"),
  operators: () => request<Operator[]>("/api/manager/operators"),
  createOperator: (o: { email: string; name: string; password: string; role: string }) =>
    request<ManagerUser>("/api/manager/operators", { method: "POST", body: JSON.stringify(o) }),
  setOperatorActive: (id: string, active: boolean) =>
    request<{ id: string; active: boolean }>(`/api/manager/operators/${id}/active`, {
      method: "POST",
      body: JSON.stringify({ active }),
    }),
  servers: () => request<ManagerServer[]>("/api/manager/servers"),
  saveServer: (srv: ServerInput) =>
    request<ManagerServer>("/api/manager/servers", { method: "PUT", body: JSON.stringify(srv) }),
  setServerActive: (id: string, active: boolean) =>
    request<{ id: string; active: boolean }>(`/api/manager/servers/${id}/active`, {
      method: "POST",
      body: JSON.stringify({ active }),
    }),
  deleteServer: (id: string) =>
    request<{ status: string }>(`/api/manager/servers/${id}`, { method: "DELETE" }),
  finance: () => request<Finance>("/api/manager/finance"),
  costs: () => request<CostModel>("/api/manager/settings/costs"),
  saveCosts: (cm: CostModel) =>
    request<CostModel>("/api/manager/settings/costs", { method: "PUT", body: JSON.stringify(cm) }),
  plans: () => request<Plan[]>("/api/manager/plans"),
  savePlan: (plan: PlanInput) =>
    request<{ plan: Plan; stripe_sync?: "ok" | "failed" }>("/api/manager/plans", { method: "PUT", body: JSON.stringify(plan) }),
  setPlanActive: (key: string, active: boolean) =>
    request<{ key: string; active: boolean }>(`/api/manager/plans/${key}/active`, {
      method: "POST",
      body: JSON.stringify({ active }),
    }),
  setTenantCurrency: (tenantId: string, currency: string) =>
    request<{ currency: string }>(`/api/manager/tenants/${tenantId}/currency`, {
      method: "POST", body: JSON.stringify({ currency }),
    }),
  setTenantPlan: (tenantId: string, plan: string) =>
    request<{ tenant_id: string; plan: string }>(`/api/manager/tenants/${tenantId}/plan`, {
      method: "POST",
      body: JSON.stringify({ plan }),
    }),
};
