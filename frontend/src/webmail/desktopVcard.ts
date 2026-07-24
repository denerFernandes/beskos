import { isTauri } from "./tauriNotify";
import { contactsApi } from "./backend";

// Abrir um .vcf com o Beskos no desktop (duplo-clique / "Abrir com"): o Rust lê o ficheiro e
// enfileira o TEXTO (o webview não tem acesso ao disco). Aqui drenamos essa fila e importamos os
// contactos para a agenda do utilizador — reutilizando o mesmo import de vCard/CSV da web.
//
// Import dinâmico do @tauri-apps/api: fora do Tauri nunca é carregado (isTauri() é false), por isso
// o build web não depende dele em runtime.

export type VcardImportResult = { imported: number; skipped: number };

// initVcardOpen liga o dreno (uma vez, ao montar a shell autenticada) e o listener do evento
// "vcard-pending" (aberturas com a app já a correr). Devolve um cleanup que remove o listener.
export function initVcardOpen(
  onDone: (r: VcardImportResult) => void,
  onError: (msg: string) => void,
): () => void {
  if (!isTauri()) return () => {};
  let disposed = false;

  const drain = async () => {
    try {
      const { invoke } = await import("@tauri-apps/api/core");
      const texts = await invoke<string[]>("take_pending_vcards");
      for (const text of texts) {
        if (!text.trim()) continue;
        const file = new File([text], "contact.vcf", { type: "text/vcard" });
        try {
          onDone(await contactsApi.importContacts(file));
        } catch (e) {
          onError((e as Error).message);
        }
      }
    } catch {
      /* fora do Tauri ou comando indisponível */
    }
  };

  void drain(); // arranque a frio: o .vcf que abriu a app

  let unlisten: (() => void) | undefined;
  void (async () => {
    try {
      const { listen } = await import("@tauri-apps/api/event");
      const un = await listen("vcard-pending", () => { if (!disposed) void drain(); });
      if (disposed) un(); else unlisten = un;
    } catch {
      /* sem eventos: o dreno de arranque já cobriu o caso mais comum */
    }
  })();

  return () => { disposed = true; unlisten?.(); };
}
