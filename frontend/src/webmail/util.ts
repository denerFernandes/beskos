import type { EmailAddress } from "../jmap/types";

const HUES = ["#B44D3A", "#116466", "#8257C7", "#2E7D5B", "#C0692F", "#3B6FB0", "#A23E6E", "#5B6470"];

export function displayName(addrs: EmailAddress[] | null): string {
  const a = addrs?.[0];
  if (!a) return "—";
  return a.name || a.email;
}

export function initials(name: string): string {
  const parts = name.replace(/["']/g, "").trim().split(/[\s@.]+/).filter(Boolean);
  return ((parts[0]?.[0] ?? "?") + (parts[1]?.[0] ?? "")).toUpperCase();
}

// Logo de empresa a partir do domínio: favicon do próprio site (sem provedor externo/broker de dados).
// Sanitiza o domínio para evitar injeção de caminho/esquema; devolve "" se inválido.
export function faviconOf(domain: string): string {
  const d = domain.trim().toLowerCase().replace(/^https?:\/\//, "").replace(/\/.*$/, "");
  if (!/^[a-z0-9.-]+\.[a-z]{2,}$/.test(d)) return "";
  return `https://${d}/favicon.ico`;
}

export function hueFor(key: string): string {
  let h = 0;
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0;
  return HUES[h % HUES.length];
}

// Formata a data relativa/curta conforme o locale (lei §4.5 — Intl, não à mão).
// Data completa por extenso, com fuso — é o que se quer no "mostrar detalhes" e ao imprimir.
// Ex.: "quinta-feira, 16 de julho de 2026 às 07:49 (GMT-3)".
export function fullDateTime(iso: string, locale: string): string {
  return new Date(iso).toLocaleString(locale, {
    weekday: "long", day: "2-digit", month: "long", year: "numeric",
    hour: "2-digit", minute: "2-digit", timeZoneName: "short",
  });
}

// "há 1 hora", "há 3 dias" — o relativo que aparece ao lado da hora, como no Gmail.
export function relativeTime(iso: string, locale: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const min = Math.round(diff / 60000);
  if (Math.abs(min) < 60) return rtf.format(-min, "minute");
  const hr = Math.round(min / 60);
  if (Math.abs(hr) < 24) return rtf.format(-hr, "hour");
  return rtf.format(-Math.round(hr / 24), "day");
}

export function shortTime(iso: string, locale: string): string {
  const d = new Date(iso);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return d.toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" });
  return d.toLocaleDateString(locale, { day: "2-digit", month: "short" });
}
