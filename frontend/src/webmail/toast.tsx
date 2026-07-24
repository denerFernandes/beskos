import { useSyncExternalStore } from "react";

// Toast global simples (feedback de ações). Sem dependências.
// Suporta uma ação opcional (ex.: "Desfazer") com o seu próprio tempo de vida — usada pelo
// "desfazer envio", em que a janela para cancelar é maior que a de um aviso normal.
interface ToastState {
  message: string;
  action?: { label: string; onClick: () => void };
}
let state: ToastState = { message: "" };
let timer: ReturnType<typeof setTimeout> | null = null;
const subs = new Set<() => void>();
const emit = () => subs.forEach((s) => s());

function show(next: ToastState, ms: number) {
  state = next;
  emit();
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => { state = { message: "" }; emit(); }, ms);
}

export function toast(message: string) {
  show({ message }, 2600);
}

// Toast com botão de ação. Fica visível `ms` (default 5s) para dar tempo de reagir.
export function toastAction(message: string, label: string, onClick: () => void, ms = 5000) {
  show({ message, action: { label, onClick } }, ms);
}

// Fecha o toast já (ex.: depois de a ação ser usada).
export function dismissToast() {
  if (timer) clearTimeout(timer);
  state = { message: "" };
  emit();
}

export function ToastHost() {
  const s = useSyncExternalStore((cb) => { subs.add(cb); return () => subs.delete(cb); }, () => state);
  return (
    <div className={"toast" + (s.message ? " show" : "")}>
      <span>{s.message}</span>
      {s.action && (
        <button className="toast-action" onClick={() => { s.action?.onClick(); dismissToast(); }}>
          {s.action.label}
        </button>
      )}
    </div>
  );
}
