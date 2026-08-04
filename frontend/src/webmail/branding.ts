// Aplicação da marca da organização em tempo de execução: a cor principal sobrepõe as variáveis
// --accent (e derivadas) via um <style> injetado com !important, para ganhar às regras de tema.
// O logo é usado diretamente nos componentes (topo do app e login).

/** Marca Beskos: o que se mostra quando o cliente não tem logótipo próprio. */
export const BESKOS_MARK = "/beskos-mark.svg";

// Paleta de cores oferecida ao admin (nome + hex). "fornecer algumas cores" — sem color picker
// livre, para garantir contraste e coerência.
export const BRAND_COLORS: { id: string; name: string; hex: string }[] = [
  // Clay leads because it is the product's own accent. Teal stays on the list: it was the
  // default until this change, and dropping it would silently rebrand whoever had chosen it.
  { id: "clay", name: "Argila", hex: "#C2603A" },
  { id: "teal", name: "Teal", hex: "#116466" },
  { id: "blue", name: "Azul", hex: "#2563EB" },
  { id: "indigo", name: "Índigo", hex: "#4F46E5" },
  { id: "violet", name: "Violeta", hex: "#7C3AED" },
  { id: "green", name: "Verde", hex: "#059669" },
  { id: "red", name: "Vermelho", hex: "#DC2626" },
  { id: "orange", name: "Laranja", hex: "#EA580C" },
  { id: "pink", name: "Rosa", hex: "#DB2777" },
  { id: "slate", name: "Ardósia", hex: "#475569" },
  { id: "black", name: "Preto", hex: "#111111" },
];

const STYLE_ID = "mailin-brand";

// applyBrandColor overrides the accent colour across the whole app. An empty colour removes the
// override and returns to the theme's own accent. THEME AWARE (23/07/2026): on the dark ground the
// colour is LIGHTENED (mixed with white), the same thing the theme does to the default accent
// (#C2603A -> #DE8560). Without it, a dark brand (Black #111, say) went invisible against the dark
// background and selections lost their contrast.
export function applyBrandColor(color: string): void {
  let el = document.getElementById(STYLE_ID) as HTMLStyleElement | null;
  if (!color) { el?.remove(); return; }
  if (!el) { el = document.createElement("style"); el.id = STYLE_ID; document.head.appendChild(el); }
  const light =
    `--accent:${color}!important;` +
    `--accent-solid:${color}!important;` +
    `--accent-ink:color-mix(in srgb, ${color} 82%, #000)!important;` +
    `--accent-tint:color-mix(in srgb, ${color} 16%, transparent)!important;`;
  // Escuro: seleção/ativo em BRANCO (pedido 23/07/2026) — pill de seleção é um véu branco
  // subtil e o texto/ícone ativo fica quase-branco; o accent "funcional" (botões/knobs) é a
  // marca clareada para não desaparecer no fundo escuro. --accent-solid é o FILL dos botões:
  // segue a cor da marca, mas pouco clareado (fica vivo, não pastel) e legível com texto branco.
  const dark =
    `--accent:color-mix(in srgb, ${color} 45%, #fff)!important;` +
    `--accent-solid:color-mix(in srgb, ${color} 85%, #fff)!important;` +
    `--accent-ink:#ECEAE5!important;` +
    `--accent-tint:rgba(255,255,255,.09)!important;`;
  // Mesma cascata do tema (webmail.css): media query como base, data-theme explícito ganha.
  el.textContent =
    `.wm{${light}}` +
    `@media (prefers-color-scheme:dark){.wm{${dark}}}` +
    `:root[data-theme="dark"] .wm{${dark}}` +
    `:root[data-theme="light"] .wm{${light}}`;
}

// Cache local da marca (para o login mostrar de imediato ao voltar, sem esperar a rede).
const LS_BRAND = "mailin.brand";
export function cacheBrand(b: { name: string; color: string; logo: string; logo_white_dark?: boolean }): void {
  try { localStorage.setItem(LS_BRAND, JSON.stringify(b)); } catch { /* modo privado */ }
}
export function cachedBrand(): { name: string; color: string; logo: string; logo_white_dark?: boolean } | null {
  try { const r = localStorage.getItem(LS_BRAND); return r ? JSON.parse(r) : null; } catch { return null; }
}
