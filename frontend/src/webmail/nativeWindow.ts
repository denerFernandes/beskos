import { isTauri } from "./tauriNotify";
import { toast } from "./toast";

// openInWindow abre uma rota da app numa janela NATIVA do Tauri (desktop). Devolve true se tratou
// (estamos no app nativo); false na web, onde o chamador deve seguir com o fluxo normal (navegar/
// window.open). O label é único por recurso: reabrir foca a janela existente em vez de duplicar.
//
// O asset server do Tauri não faz fallback SPA, por isso carrega index.html?wpath=<rota> e o
// main.tsx reescreve a URL para a rota real antes do router montar (mesma técnica da sala do Meet).
// isDedicatedWindow diz (de forma síncrona) se estamos numa janela dedicada aberta por openInWindow
// (sala/editor). O flag é posto no main.tsx quando a janela carrega via ?wpath=.
export function isDedicatedWindow(): boolean {
  return typeof window !== "undefined" && (window as unknown as { __mailinDedicated?: boolean }).__mailinDedicated === true;
}

// closeSelfWindow fecha a janela atual: no app nativo via API do Tauri; na web via window.close().
export function closeSelfWindow(): void {
  if (isTauri()) {
    void (async () => {
      try {
        const { getCurrentWindow } = await import("@tauri-apps/api/window");
        await getCurrentWindow().close();
      } catch { /* sem permissão/API: ignora */ }
    })();
    return;
  }
  window.close();
}

export function openInWindow(label: string, path: string, title: string): boolean {
  if (!isTauri()) return false;
  void (async () => {
    const safe = label.replace(/[^a-zA-Z0-9_-]/g, "");
    try {
      const { WebviewWindow } = await import("@tauri-apps/api/webviewWindow");
      const existing = await WebviewWindow.getByLabel(safe);
      if (existing) { await existing.setFocus(); return; }
      const url = `index.html?wpath=${encodeURIComponent(path)}`;
      const win = new WebviewWindow(safe, { url, title, width: 1280, height: 800, resizable: true, focus: true });
      win.once("tauri://error", (e) => toast(String((e as { payload?: unknown }).payload ?? "erro ao abrir a janela")));
    } catch {
      toast("Não foi possível abrir a janela.");
    }
  })();
  return true;
}

// --- Pontes para o lado nativo (só existem no app de desktop) ---

/** Diz ao menu do tray quem está autenticado. `null` ao terminar sessão. */
export async function setTrayAccount(label: string | null): Promise<void> {
  try {
    const { invoke } = await import("@tauri-apps/api/core");
    await invoke("set_account", { label });
  } catch { /* na web não existe; ignorar */ }
}
