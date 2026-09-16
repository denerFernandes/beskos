// Applying the organization's brand at runtime: the primary colour overrides the
// --accent variables (and derivatives) via a <style> tag injected with !important, so it wins
// over the theme rules. The logo is used directly in the components (app top bar and login).

/** Beskos mark: shown when the customer has no logo of their own. */
export const BESKOS_MARK = "/beskos-mark.svg";

// Colour palette offered to the admin (name key + hex). "provide a few colours" — no free-form
// colour picker, to guarantee contrast and coherence. `name` is an i18n key under
// `branding.colors.*` (law 4.1) — consumers must run it through `t()` before display.
export const BRAND_COLORS: { id: string; name: string; hex: string }[] = [
  // The muted family comes first: these four are built for the warm paper the product now runs on,
  // and terracotta is the accent the product ships with. The saturated entries below them are the
  // original list, kept as they were -- their hex is what a tenant has stored, so editing a value
  // would silently rebrand whoever picked it.
  { id: "terracotta", name: "branding.colors.terracotta", hex: "#B4785F" },
  { id: "sage", name: "branding.colors.sage", hex: "#7C8C6A" },
  { id: "slateblue", name: "branding.colors.slateblue", hex: "#6B8299" },
  { id: "mauve", name: "branding.colors.mauve", hex: "#96768A" },
  { id: "teal", name: "branding.colors.teal", hex: "#116466" },
  { id: "blue", name: "branding.colors.blue", hex: "#2563EB" },
  { id: "indigo", name: "branding.colors.indigo", hex: "#4F46E5" },
  { id: "violet", name: "branding.colors.violet", hex: "#7C3AED" },
  { id: "green", name: "branding.colors.green", hex: "#059669" },
  { id: "red", name: "branding.colors.red", hex: "#DC2626" },
  { id: "orange", name: "branding.colors.orange", hex: "#EA580C" },
  { id: "pink", name: "branding.colors.pink", hex: "#DB2777" },
  { id: "slate", name: "branding.colors.slate", hex: "#475569" },
  { id: "black", name: "branding.colors.black", hex: "#111111" },
];

const STYLE_ID = "mailin-brand";

// applyBrandColor overrides the accent colour across the whole app. An empty colour removes the
// override and returns to the theme's own accent. THEME AWARE: on the dark ground the colour is
// LIGHTENED (mixed with white), the same thing the theme does to the default accent
// (#B4785F -> #D2A28C). Without it, a dark brand (Black #111, say) went invisible against the dark
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
  // Dark theme: selection/active state uses WHITE — the selection pill is a subtle white veil
  // and the active text/icon turns near-white; the "functional" accent (buttons/knobs) is the
  // brand colour lightened so it does not disappear into the dark background. --accent-solid is
  // the button FILL: follows the brand colour, lightened just a little (stays vivid, not pastel)
  // and legible against white text.
  const dark =
    `--accent:color-mix(in srgb, ${color} 45%, #fff)!important;` +
    `--accent-solid:color-mix(in srgb, ${color} 85%, #fff)!important;` +
    `--accent-ink:#ECEAE5!important;` +
    `--accent-tint:rgba(255,255,255,.09)!important;`;
  // Same cascade as the theme (webmail.css): media query as the base, explicit data-theme wins.
  const scope = ".wm,.pnl,.sg";
  el.textContent =
    `${scope}{${light}}` +
    `@media (prefers-color-scheme:dark){${scope}{${dark}}}` +
    `:root[data-theme="dark"] ${scope}{${dark}}` +
    `:root[data-theme="light"] ${scope}{${light}}`;
}

// Local cache of the brand (so login shows it immediately on return, without waiting on the network).
const LS_BRAND = "mailin.brand";
export function cacheBrand(b: { name: string; color: string; logo: string; logo_white_dark?: boolean }): void {
  try { localStorage.setItem(LS_BRAND, JSON.stringify(b)); } catch { /* private mode */ }
}
export function cachedBrand(): { name: string; color: string; logo: string; logo_white_dark?: boolean } | null {
  try { const r = localStorage.getItem(LS_BRAND); return r ? JSON.parse(r) : null; } catch { return null; }
}
