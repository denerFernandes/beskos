// Formatação via Intl (lei 4.5) — locale fixo pt-BR (ferramenta interna).

const LOCALE = "pt-BR";

const num = new Intl.NumberFormat(LOCALE);
const dateShort = new Intl.DateTimeFormat(LOCALE, { dateStyle: "short" });
const dateTime = new Intl.DateTimeFormat(LOCALE, { dateStyle: "short", timeStyle: "short" });

export function formatNumber(n: number): string {
  return num.format(n);
}

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  return dateShort.format(new Date(iso));
}

export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  return dateTime.format(new Date(iso));
}

// Cêntimos → moeda via Intl (lei 4.5).
export function formatMoney(cents: number, currency: string): string {
  return new Intl.NumberFormat(LOCALE, { style: "currency", currency }).format(cents / 100);
}

// Bytes → unidade legível (base 1024). Intl não tem "bytes" adaptativo; escolhemos a unidade e
// formatamos o número com Intl.
export function formatBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = bytes;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${new Intl.NumberFormat(LOCALE, { maximumFractionDigits: v >= 100 ? 0 : 1 }).format(v)} ${units[i]}`;
}
