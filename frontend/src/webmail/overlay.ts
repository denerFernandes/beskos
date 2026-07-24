import { useSyncExternalStore } from "react";

// Contador global de SIDECARS/overlays abertos (23/07/2026): a bolha do assistente esconde-se
// enquanto houver um aberto (sobrepunha os rodapés dos drawers). O Drawer regista-se sozinho.
let open = 0;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

export function overlayOpened(): () => void {
  open++;
  emit();
  let done = false;
  return () => { if (!done) { done = true; open--; emit(); } };
}

export function useOverlayOpen(): boolean {
  return useSyncExternalStore(
    (l) => { listeners.add(l); return () => listeners.delete(l); },
    () => open > 0,
  );
}
