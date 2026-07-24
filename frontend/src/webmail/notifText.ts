import type { TFunction } from "i18next";
import type { Notif } from "./backend";

// Traduz uma notificação in-app para o idioma do usuário (CLAUDE.md §4.6): o backend manda `kind` +
// `args` estruturados; aqui montamos o texto por `server.notif.<kind>`. O título é sempre traduzido;
// o corpo só quando existe chave `.b` (reunião/evento/arquivo). Para chat/tarefa o corpo é conteúdo
// do usuário (comentário/preview) e cai no `defaultValue` = texto cru gravado. Linhas antigas sem
// `args` também caem no fallback title/body. Datas ISO viram formato do locale via Intl.
export function notifText(n: Notif, t: TFunction, lang: string): { title: string; body: string } {
  const args: Record<string, string> = { ...(n.args ?? {}) };
  if (args.when) args.when = fmtWhen(args.when, lang);
  if (args.status) args.status = t(`server.rsvp.${args.status}`, { defaultValue: args.status });

  // @channel/@here usa um título diferente da menção pessoal (mesmo kind no backend).
  const key = n.kind === "chat_mention" && args.at ? "chat_mention_at" : n.kind;
  const base = `server.notif.${key}`;
  // Reunião instantânea não tem horário: usa a variante sem "quando".
  const bKey = key === "meeting_invite" && !args.when ? `${base}.b0` : `${base}.b`;

  const title = t(`${base}.t`, { ...args, defaultValue: n.title });
  const body = t(bKey, { ...args, defaultValue: n.body });
  return { title, body };
}

function fmtWhen(iso: string, lang: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return new Intl.DateTimeFormat(lang, { dateStyle: "short", timeStyle: "short" }).format(d);
}
