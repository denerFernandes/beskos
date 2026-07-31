import { type ChatMessage } from "./backend";
import { SHORTCODES } from "./emoji";


export const KB = (n: number) => `${Math.max(1, Math.round(n / 1024))} KB`;

// Reações rápidas: as poucas que se usam em 90% dos casos. O resto vem do seletor completo.
export const QUICK = ["👍", "❤️", "😄", "🎉", "👀", "🙏", "🔥", "✅"];

// Convenção de "chamada": mensagem-sistema que o frontend renderiza como cartão de entrada.
export const CALL_RE = /^📞 __call__ (video|audio) (\S+)$/;

export const callBody = (mode: "video" | "audio", room: string) => `📞 __call__ ${mode} ${room}`;

export function parseCall(body: string): { mode: "video" | "audio"; room: string } | null {
  const m = CALL_RE.exec(body);
  return m ? { mode: m[1] as "video" | "audio", room: m[2] } : null;
}

// applyShortcodes troca ":atalho: " ou ":) " pelo emoji, só depois do espaço final.
export function applyShortcodes(v: string): string {
  if (!v.endsWith(" ")) return v;
  const body = v.slice(0, -1);
  for (const [code, ch] of Object.entries(SHORTCODES)) {
    if (body.endsWith(code)) return body.slice(0, -code.length) + ch + " ";
  }
  return v;
}

export const SLASH_COMMANDS: { key: string; desc: string; args?: string }[] = [
  { key: "task", desc: "chat.cmdTaskDesc" },
  { key: "me", desc: "chat.cmdMeDesc", args: "chat.cmdMeArgs" },
  { key: "shrug", desc: "chat.cmdShrugDesc" },
  { key: "tableflip", desc: "chat.cmdTableflipDesc" },
  { key: "unflip", desc: "chat.cmdUnflipDesc" },
];

export function applySlash(body: string): string {
  const m = /^\/(\w+)(?:\s+([\s\S]*))?$/.exec(body);
  if (!m) return body;
  const cmd = m[1].toLowerCase();
  const rest = (m[2] || "").trim();
  switch (cmd) {
    case "shrug": return (rest ? rest + " " : "") + "¯\\_(ツ)_/¯";
    case "me": return rest ? "_" + rest + "_" : body;
    case "tableflip": return (rest ? rest + " " : "") + "(╯°□°)╯︵ ┻━┻";
    case "unflip": return (rest ? rest + " " : "") + "┬─┬ノ( º _ ºノ)";
    default: return body;
  }
}

// Slash-commands client-side (estilo Slack): transformam o texto antes do envio. Comandos
// desconhecidos seguem como texto normal. `/me` e `/shrug` são os úteis; os flips são brincadeira.
// Comandos disponíveis no composer. `menu` é o que a lista do "/" mostra; os de texto são aplicados
// no envio (applySlash), o /task abre o seletor de tarefas.
// Barra de formatação visível (por aparelho, como no Slack).
export const LS_FMT = "mailin.chat.fmtbar";

// Opções rápidas de horário (agendar envio / lembrar-me), estilo Slack.
export function quickTimes(t: (k: string) => string): { label: string; date: Date }[] {
  const now = Date.now();
  const tomorrow9 = new Date(); tomorrow9.setDate(tomorrow9.getDate() + 1); tomorrow9.setHours(9, 0, 0, 0);
  return [
    { label: t("chat.in30m"), date: new Date(now + 30 * 60000) },
    { label: t("chat.in1h"), date: new Date(now + 60 * 60000) },
    { label: t("chat.in3h"), date: new Date(now + 180 * 60000) },
    { label: t("chat.tomorrow9"), date: tomorrow9 },
  ];
}

// Aplica um evento de reação (WS) a uma mensagem, incrementalmente — sem refazer o fetch do canal.
// Idempotente por membership em `users`: o eco da própria ação (ou reentrega) não conta em dobro.
// `mine` é recalculado com o id do usuário atual, então serve para qualquer membro que receba o evento.
export function applyReaction(m: ChatMessage, emoji: string, userId: string, remove: boolean, myId: string): ChatMessage {
  const reactions = [...(m.reactions ?? [])];
  const idx = reactions.findIndex((r) => r.emoji === emoji);
  if (remove) {
    if (idx < 0 || !reactions[idx].users.includes(userId)) return m; // já removido → no-op
    const users = reactions[idx].users.filter((u) => u !== userId);
    if (users.length === 0) reactions.splice(idx, 1);
    else reactions[idx] = { ...reactions[idx], users, count: users.length, mine: users.includes(myId) };
  } else {
    if (idx < 0) {
      reactions.push({ emoji, users: [userId], count: 1, mine: userId === myId });
    } else {
      if (reactions[idx].users.includes(userId)) return m; // dedup do próprio eco
      const users = [...reactions[idx].users, userId];
      reactions[idx] = { ...reactions[idx], users, count: users.length, mine: users.includes(myId) };
    }
  }
  return { ...m, reactions };
}

