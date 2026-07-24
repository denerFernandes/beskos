// Notificações desktop (Web Notifications API), fase 1a: disparam enquanto há uma aba do Beskos
// aberta. A permissão é por-browser, então as preferências vivem no dispositivo (localStorage),
// não nas settings sincronizadas — activar no portátil não devia activar no telemóvel.
//
// Regra de ruído: só notifica quando a aba NÃO está visível (document.hidden). Se a pessoa está a
// olhar para o Beskos, já vê o que chega — uma notificação do SO seria redundante.

import { isTauri, tauriPermission, tauriRequestPermission, tauriShow } from "./tauriNotify";

export type NotifDomain = "email" | "chat" | "meet" | "files" | "tasks";
const DOMAINS: NotifDomain[] = ["email", "chat", "meet", "files", "tasks"];
const LS_PREFS = "mailin.notif.prefs";

export function notifSupported(): boolean {
  if (isTauri()) return true; // app nativo: usa o plugin do SO (o WKWebView não tem Web Notifications)
  return typeof window !== "undefined" && "Notification" in window;
}

export function notifPermission(): NotificationPermission {
  if (isTauri()) return tauriPermission();
  return typeof window !== "undefined" && "Notification" in window ? Notification.permission : "denied";
}

// Pede permissão. Tem de ser chamado a partir de um gesto do utilizador (clique).
export async function requestNotifPermission(): Promise<NotificationPermission> {
  if (isTauri()) return tauriRequestPermission();
  if (!("Notification" in window)) return "denied";
  try { return await Notification.requestPermission(); }
  catch { return "denied"; }
}

export function notifPrefs(): Record<NotifDomain, boolean> {
  const def = { email: true, chat: true, meet: true, files: true, tasks: true };
  try {
    const raw = localStorage.getItem(LS_PREFS);
    if (!raw) return def;
    return { ...def, ...JSON.parse(raw) };
  } catch { return def; }
}

export function setNotifPref(domain: NotifDomain, on: boolean): void {
  try { localStorage.setItem(LS_PREFS, JSON.stringify({ ...notifPrefs(), [domain]: on })); }
  catch { /* modo privado: fica só nesta sessão */ }
}

export function isDomain(v: string): v is NotifDomain {
  return (DOMAINS as string[]).includes(v);
}

// Mostra uma notificação preferindo o SERVICE WORKER (registration.showNotification), que é o
// caminho fiável em Chrome/Edge/Firefox desktop — o construtor `new Notification()` é obsoleto e
// não aparece em vários browsers. Cai para o construtor só se não houver SW.
async function show(title: string, body?: string, tag?: string): Promise<boolean> {
  if (notifPermission() !== "granted") return false;
  if (isTauri()) return tauriShow(title, body); // notificação nativa do SO (desktop/mobile)
  const opts: NotificationOptions = { body, tag, icon: "/beskos-mark.svg", badge: "/beskos-mark.svg" };
  // Caminho preferido: service worker ATIVO. Logo após register() o SW ainda está a instalar e
  // reg.showNotification() falha em silêncio — por isso esperamos serviceWorker.ready (com timeout
  // curto para não pendurar) antes de usar o registration.
  if ("serviceWorker" in navigator) {
    try {
      if (!(await navigator.serviceWorker.getRegistration())) await navigator.serviceWorker.register("/sw.js");
      const reg = await Promise.race<ServiceWorkerRegistration | undefined>([
        navigator.serviceWorker.ready,
        new Promise((res) => setTimeout(() => res(undefined), 1500)),
      ]);
      if (reg?.showNotification) { await reg.showNotification(title, opts); return true; }
    } catch { /* cai para o construtor abaixo */ }
  }
  // Fallback: construtor Notification (menos fiável, mas suficiente em vários browsers).
  try { const n = new Notification(title, opts); n.onclick = () => { window.focus(); n.close(); }; return true; }
  catch { return false; }
}

// Notificação de TESTE — ignora a regra de visibilidade e as preferências por app. Se ainda não há
// permissão, pede-a primeiro (o clique no botão é o gesto exigido pelo browser).
export async function testNotification(title: string, body: string): Promise<boolean> {
  if (!notifSupported()) return false;
  if (notifPermission() !== "granted") {
    const p = await requestNotifPermission();
    if (p !== "granted") return false;
  }
  return show(title, body, "mailin-test");
}

// Mostra uma notificação do SO se: browser suporta, permissão concedida, o domínio está ligado e a
// aba não está visível. `tag` deduplica (uma notificação por conversa/evento em vez de empilhar).
export function showDesktopNotification(domain: NotifDomain, opts: {
  title: string; body?: string; tag?: string; onClick?: () => void;
}): void {
  if (notifPermission() !== "granted") return;
  if (!notifPrefs()[domain]) return;
  // No app nativo a janela pode estar minimizada mas "visível"; document.hidden é pouco fiável no
  // WKWebView. Na web mantemos a regra de só notificar com a aba escondida (evita ruído redundante).
  if (!isTauri() && !document.hidden) return;
  void show(opts.title, opts.body, opts.tag); // o clique é tratado no service worker (notificationclick)
}
