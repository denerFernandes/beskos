/* Service worker do Beskos: recebe Web Push e mostra a notificação mesmo com todas as abas
   fechadas. Mantido mínimo de propósito — só push e clique. */

// Ativa de imediato (sem esperar as abas antigas fecharem) e assume o controlo — assim
// registration.showNotification() fica utilizável logo após o primeiro registo.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));

self.addEventListener("push", (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch { data = {}; }
  const title = data.title || "Beskos";
  const options = {
    body: data.body || "",
    icon: "/favicon.svg",
    badge: "/favicon.svg",
    tag: (data.kind || "mailin") + ":" + (data.link || ""),
    data: { link: data.link || "/mail" },
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const link = (event.notification.data && event.notification.data.link) || "/mail";
  event.waitUntil((async () => {
    const all = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    // Já há uma aba do Beskos aberta? Foca-a e manda-a navegar, em vez de abrir outra.
    for (const c of all) {
      if (c.url.includes(self.location.origin)) {
        await c.focus();
        c.postMessage({ type: "notif-click", link });
        return;
      }
    }
    await self.clients.openWindow(link);
  })());
});
