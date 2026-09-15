// Cliente da nossa API para o assistente agêntico. Vive à parte do `backend.ts` por duas razões:
// o `backend.ts` já passa das 600 linhas (lei 1.4), e o chat não é JSON — é um fio SSE, que precisa
// de um caminho próprio. Os componentes continuam sem fazer `fetch` cru (lei 3.4).

import { apiUrl, authHeaders } from "../../apiBase";
import { translateError } from "../backend";

// Um cartão de ação: o registo da chamada real, não uma frase a dizer que fez (§3.3 do desenho).
export interface ToolStep {
  tool: string;
  args: unknown;
  summary?: string;
  // error_code é uma CHAVE de i18n; nunca se mostra texto cru do servidor (lei 4.6).
  error_code?: string;
  // choices são respostas rápidas (botões) que a ferramenta ofereceu — ex.: qual contacto "Bob".
  // Clicar num envia value como mensagem. Poupa ao utilizador escrever a resposta.
  choices?: { label: string; value: string }[];
}

export interface AssistantRun {
  id: string;
  agent: string;
  answer: string;
  steps: ToolStep[];
  stopped?: string;
}

export interface ConversationSummary {
  id: string;
  agent: string;
  title: string;
  updated_at: string;
}

export interface AgentProfile {
  key: string;
  // name só existe nos perfis do cliente: o nome de um agente de origem é texto de interface e vem
  // do i18n pela chave (lei 4.1).
  name?: string;
  purpose?: string;
  instructions?: string;
  builtin: boolean;
  enabled: boolean;
  tools: string[];
}

// O que se envia ao criar ou alterar um perfil do cliente.
export interface AgentDraft {
  name: string;
  purpose: string;
  instructions: string;
  tools: string[];
  // duplicate_of parte de um agente de ORIGEM. A cópia é independente: não recebe as nossas
  // atualizações, e isso é dito a quem duplica (§3.2).
  duplicate_of?: string;
}

export interface StoredMessage {
  id: string;
  role: "user" | "assistant";
  // agent identifies who answered. Empty on user messages, and on assistant messages recorded
  // before this field existed.
  agent?: string;
  content: string;
  steps: ToolStep[];
  stopped?: string;
  created_at: string;
}

// Estado da quota da janela de 6h (§9.2).
export interface QuotaStatus {
  limit: number;
  used: number;
  remaining: number;
  renews_at?: string;
}

// Uma chamada de ferramenta registada (§6.4). O registo da chamada real, legível pelo utilizador.
export interface AuditEntry {
  id: string;
  agent: string;
  tool: string;
  args: Record<string, unknown>;
  result?: string;
  error?: string;
  duration_ms: number;
  created_at: string;
}

// Um facto guardado na memória (§8). Texto legível, não um vetor opaco: o utilizador tem de
// conseguir ler o que a app sabe sobre si (§8.1).
export interface MemoryFact {
  id: string;
  fact: string;
  category: string;
  created_at: string;
  last_used_at?: string;
}

// O que a extração guardou e recusou num turno (§6.5). As recusas trazem uma chave de i18n.
export interface MemoryEvent {
  saved: { fact: string; category: string }[];
  refused: { fact: string; reason: string }[];
}

// Uma tarefa em segundo plano (§3.6). Sobrevive a recarregar a página e a reiniciar o servidor:
// o estado vive na base, não neste separador.
export interface AssistantTask {
  id: string;
  conversation_id: string;
  agent: string;
  message: string;
  // queued | running | awaiting_confirmation | done | failed | cancelled
  status: string;
  steps: ToolStep[];
  answer?: string;
  error_code?: string;
  stopped?: string;
  created_at: string;
  updated_at: string;
}

// Uma ação de tarefa à espera de decisão. Igual a uma confirmação síncrona, mas durável.
export interface PendingAction {
  id: string;
  task_id: string;
  tool: string;
  args: Record<string, unknown>;
}

// Uma ação que espera aprovação humana (§6.3). `args` são os argumentos EXATOS que vão correr —
// o painel mostra-os tal e qual, não uma versão resumida.
export interface ConfirmRequest {
  id: string;
  tool: string;
  args: Record<string, unknown>;
}

// Eventos do fio. `conversation` chega primeiro para que uma conversa nova tenha id logo no início:
// quem feche o painel a meio não perde o fio que já está gravado no servidor.
export type ChatEvent =
  | { kind: "conversation"; conversation_id: string; title: string; agent: string; pinned_agent: string }
  | { kind: "delta"; text: string }
  | { kind: "step"; step: ToolStep }
  | { kind: "confirm"; confirm: ConfirmRequest }
  | { kind: "memory"; saved: MemoryEvent["saved"]; refused: MemoryEvent["refused"] }
  | { kind: "done"; run: AssistantRun }
  | { kind: "error"; error_code: string };

async function req<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(apiUrl(path), {
    ...opts,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...authHeaders(), ...(opts.headers || {}) },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(translateError(body.error_code, body.error, res.status));
  }
  return res.json() as Promise<T>;
}

export const assistantApi = {
  agents: () => req<{ items: AgentProfile[]; max_tools: string[] }>("/api/assistant/agents"),
  agentCreate: (a: AgentDraft) =>
    req<AgentProfile>("/api/assistant/agents", { method: "POST", body: JSON.stringify(a) }),
  agentUpdate: (key: string, a: AgentDraft) =>
    req<AgentProfile>(`/api/assistant/agents/${key}`, { method: "PUT", body: JSON.stringify(a) }),
  agentDelete: (key: string) =>
    req<{ status: string }>(`/api/assistant/agents/${key}`, { method: "DELETE" }),
  // Liga ou desliga um agente de ORIGEM. Controla a disponibilidade, nunca o comportamento (§3.2).
  // Ferramentas desligadas pela organização (teto fino sobre TODOS os agentes).
  tools: () => req<{ tools: string[]; disabled: string[] }>("/api/assistant/tools"),
  toolEnable: (tool: string, enabled: boolean) =>
    req<{ tool: string; enabled: boolean }>(`/api/assistant/tools/${encodeURIComponent(tool)}`, { method: "POST", body: JSON.stringify({ enabled }) }),
  agentEnable: (key: string, enabled: boolean) =>
    req<{ key: string; enabled: boolean }>(`/api/assistant/agents/${key}/enabled`, {
      method: "POST", body: JSON.stringify({ enabled }),
    }),
  usage: () => req<QuotaStatus>("/api/assistant/usage"),
  audit: () => req<{ items: AuditEntry[] }>("/api/assistant/audit"),
  memory: () => req<{ items: MemoryFact[] }>("/api/assistant/memory"),
  memoryAdd: (fact: string, category: string) =>
    req<MemoryFact>("/api/assistant/memory", { method: "POST", body: JSON.stringify({ fact, category }) }),
  memoryDelete: (id: string) =>
    req<{ status: string }>(`/api/assistant/memory/${id}`, { method: "DELETE" }),
  memoryClear: () =>
    req<{ status: string }>("/api/assistant/memory/clear", { method: "POST" }),
  tasks: () => req<{ items: AssistantTask[]; pending: PendingAction[] }>("/api/assistant/tasks"),
  taskCancel: (id: string) =>
    req<{ status: string }>(`/api/assistant/tasks/${id}/cancel`, { method: "POST" }),
  conversations: () => req<{ items: ConversationSummary[] }>("/api/assistant/conversations"),
  conversation: (id: string) =>
    req<{ id: string; agent: string; pinned_agent: string; title: string; messages: StoredMessage[] }>(
      `/api/assistant/conversations/${id}`,
    ),
  remove: (id: string) => req<{ status: string }>(`/api/assistant/conversations/${id}`, { method: "DELETE" }),

  // confirm aprova ou rejeita UMA ação. Uma por pedido: não há aprovação em bloco (§6.3).
  confirm: (id: string, approve: boolean) =>
    req<{ status: string; approved: boolean }>(`/api/assistant/confirm/${id}`, {
      method: "POST",
      body: JSON.stringify({ approve }),
    }),

  // chat abre o fio SSE. Não dá para usar EventSource: o pedido é POST e leva corpo. `signal`
  // permite cancelar — fechar o painel a meio corta a ligação, e o servidor grava na mesma o que
  // já correu.
  chat: streamChat,

  // background enfileira o pedido em vez de o correr no pedido HTTP. Devolve logo o cartão de
  // tarefa; o progresso vê-se no separador de Tarefas (§3.6).
  background: (body: { message: string; agent?: string; conversation_id?: string }) =>
    req<{ conversation_id: string; agent: string; task: AssistantTask }>("/api/assistant/chat", {
      method: "POST",
      body: JSON.stringify({ ...body, background: true }),
    }),
};

async function streamChat(
  body: { message: string; agent?: string; conversation_id?: string },
  onEvent: (ev: ChatEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(apiUrl("/api/assistant/chat"), {
    method: "POST",
    credentials: "include",
    signal,
    headers: { "Content-Type": "application/json", ...authHeaders() },
    body: JSON.stringify(body),
  });
  if (!res.ok || !res.body) {
    const err = await res.json().catch(() => ({}));
    throw new Error(translateError(err.error_code, err.error, res.status));
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    // Eventos SSE separam-se por linha em branco. O último pedaço fica no buffer até estar completo:
    // processar meio evento daria um JSON truncado a cada leitura.
    let sep = buffer.indexOf("\n\n");
    while (sep !== -1) {
      const raw = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      const ev = parseEvent(raw);
      if (ev) onEvent(ev);
      sep = buffer.indexOf("\n\n");
    }
  }
}

function parseEvent(raw: string): ChatEvent | null {
  const line = raw.split("\n").find((l) => l.startsWith("data:"));
  if (!line) return null;
  try {
    return JSON.parse(line.slice(5).trim()) as ChatEvent;
  } catch {
    return null; // um evento estragado não deita fora o fio
  }
}
