// Adaptador de notificações NATIVAS do Tauri (desktop/mobile). O WKWebView do macOS não suporta a
// Web Notifications API — por isso, no app empacotado, as notificações web não funcionam e a app
// nem aparece nas definições de notificações do SO. O plugin nativo do Tauri resolve ambos: usa o
// centro de notificações do SO e regista o bundle da app lá.
//
// Import dinâmico: o pacote `@tauri-apps/plugin-notification` só existe/funciona dentro do Tauri;
// na web nunca é carregado (isTauri() é false), então o build web não depende dele em runtime.

// isTauri deteta o runtime nativo. O Tauri 2 injeta window.isTauri; __TAURI_INTERNALS__ é o backup.
export function isTauri(): boolean {
  if (typeof window === "undefined") return false;
  const w = window as unknown as { isTauri?: boolean; __TAURI_INTERNALS__?: unknown };
  return w.isTauri === true || typeof w.__TAURI_INTERNALS__ !== "undefined";
}

// Cache síncrona da permissão: a UI (SettingsView) lê o estado de forma síncrona, mas o plugin é
// async. Inicializamos no arranque (initTauriNotify) e atualizamos após pedir permissão.
let cachedPerm: NotificationPermission = "default";
export function tauriPermission(): NotificationPermission { return cachedPerm; }

type NotifPlugin = typeof import("@tauri-apps/plugin-notification");
async function plugin(): Promise<NotifPlugin> {
  return import("@tauri-apps/plugin-notification");
}

// initTauriNotify lê a permissão atual para a cache (chamar uma vez no arranque, dentro do Tauri).
export async function initTauriNotify(): Promise<void> {
  try {
    const p = await plugin();
    cachedPerm = (await p.isPermissionGranted()) ? "granted" : "default";
  } catch { /* fora do Tauri ou plugin indisponível */ }
}

export async function tauriRequestPermission(): Promise<NotificationPermission> {
  try {
    const p = await plugin();
    if (await p.isPermissionGranted()) { cachedPerm = "granted"; return "granted"; }
    const res = await p.requestPermission(); // "granted" | "denied" | "default"
    cachedPerm = res === "granted" ? "granted" : "denied";
    return cachedPerm;
  } catch { return "denied"; }
}

// onTauriEvent escuta um evento emitido pelo lado nativo (ex.: "window-hidden", quando fechar a
// janela a esconde no tray). Na web é um no-op. Devolve a função que cancela a escuta — o registo
// é assíncrono, por isso guardamos o "cancelado" para não deixar listeners órfãos.
export function onTauriEvent(name: string, fn: () => void): () => void {
  if (!isTauri()) return () => {};
  let off: (() => void) | null = null;
  let cancelled = false;
  void (async () => {
    try {
      const { listen } = await import("@tauri-apps/api/event");
      const un = await listen(name, () => fn());
      if (cancelled) un(); else off = un;
    } catch { /* API indisponível: ignora */ }
  })();
  return () => { cancelled = true; off?.(); };
}

export async function tauriShow(title: string, body?: string): Promise<boolean> {
  try {
    const p = await plugin();
    if (!(await p.isPermissionGranted())) return false;
    p.sendNotification({ title, body });
    return true;
  } catch { return false; }
}
