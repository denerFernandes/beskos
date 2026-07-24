import { backend } from "./backend";

// Web Push no cliente: regista o service worker, subscreve no serviço de push do browser com a
// chave pública VAPID do servidor e envia a subscrição ao backend. Best-effort — se o servidor não
// tiver push configurado (VAPID desligado), não faz nada e as notificações com aba aberta bastam.

export function pushSupported(): boolean {
  return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

// A chave VAPID vem em base64url; o PushManager quer um Uint8Array.
function urlB64ToUint8Array(base64: string): Uint8Array {
  const padding = "=".repeat((4 - (base64.length % 4)) % 4);
  const b64 = (base64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(b64);
  const buf = new ArrayBuffer(raw.length);
  const out = new Uint8Array(buf);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

async function registerSW(): Promise<ServiceWorkerRegistration | null> {
  if (!pushSupported()) return null;
  try { return await navigator.serviceWorker.register("/sw.js"); }
  catch { return null; }
}

// Subscreve (idempotente): reaproveita a subscrição existente do browser se já houver uma. Só
// prossegue se o servidor tiver VAPID ligado. Devolve true se ficou subscrito.
export async function enableWebPush(): Promise<boolean> {
  if (!pushSupported() || Notification.permission !== "granted") return false;
  let vapid: { enabled: boolean; publicKey?: string };
  try { vapid = await backend.pushVapid(); } catch { return false; }
  if (!vapid.enabled || !vapid.publicKey) return false;

  const reg = await registerSW();
  if (!reg) return false;
  try {
    const existing = await reg.pushManager.getSubscription();
    const sub = existing ?? await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlB64ToUint8Array(vapid.publicKey) as BufferSource,
    });
    await backend.pushSubscribe(sub.toJSON());
    return true;
  } catch { return false; }
}

// Cancela a subscrição neste browser (e avisa o servidor para apagar a linha).
export async function disableWebPush(): Promise<void> {
  if (!pushSupported()) return;
  try {
    const reg = await navigator.serviceWorker.getRegistration();
    const sub = await reg?.pushManager.getSubscription();
    if (sub) {
      await backend.pushUnsubscribe(sub.endpoint).catch(() => { /* segue mesmo assim */ });
      await sub.unsubscribe();
    }
  } catch { /* nada a fazer */ }
}

// Encaminha o clique numa notificação (vindo do service worker) para a navegação do app.
export function onNotificationClick(navigate: (link: string) => void): () => void {
  if (!("serviceWorker" in navigator)) return () => {};
  const handler = (e: MessageEvent) => {
    if (e.data?.type === "notif-click" && e.data.link) navigate(e.data.link);
  };
  navigator.serviceWorker.addEventListener("message", handler);
  return () => navigator.serviceWorker.removeEventListener("message", handler);
}
