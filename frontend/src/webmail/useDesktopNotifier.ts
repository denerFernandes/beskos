import { useEffect, useRef } from "react";
import type { MailService } from "../jmap/mail";
import type { Notif } from "./backend";
import { showDesktopNotification, isDomain, type NotifDomain } from "./desktopNotify";
import { notifText } from "./notifText";
import i18n from "../i18n";

// Mapeia o `kind` de uma notificação in-app para o domínio de notificação desktop. Sem domínio não
// há aviso do SO — por isso o calendário (convites, RSVP, reservas) e o "estão à porta" do Meet
// também entram aqui.
function domainOfKind(kind: string): NotifDomain | null {
  if (kind.startsWith("chat")) return "chat";
  if (kind.startsWith("meet")) return "meet"; // meeting_invite e meet_knock
  if (kind.startsWith("event") || kind === "booking_new") return "cal";
  if (kind.startsWith("file")) return "files";
  if (kind.startsWith("task")) return "tasks";
  return null;
}

// Dispara notificações desktop a partir de duas fontes:
//  1. E-mail: o push JMAP não traz o conteúdo, então ao mudar de estado buscamos a mensagem mais
//     recente da caixa de entrada e comparamos com a última vista (só notifica o que é novo).
//  2. Demais domínios (chat menção, reunião, ficheiro, task): diferença da lista de notificações
//     in-app — cada linha nova e não-lida vira uma notificação do SO.
// Em ambos os casos, a primeira leitura só regista o baseline (não notifica o que já existia).
export function useDesktopNotifier(opts: {
  service: MailService;
  inboxId: string | null;
  notifItems: Notif[] | undefined;
}): (() => void) {
  const { service, inboxId, notifItems } = opts;

  // --- E-mail ---
  const lastEmailId = useRef<string | null>(null);
  const emailReady = useRef(false);
  const checkEmail = useRef<() => void>(() => {});
  checkEmail.current = async () => {
    if (!inboxId) return;
    try {
      const page = await service.listEmails(inboxId, 0, 1);
      const top = page.items[0];
      if (!top) return;
      if (!emailReady.current) { lastEmailId.current = top.id; emailReady.current = true; return; }
      if (top.id !== lastEmailId.current) {
        lastEmailId.current = top.id;
        const seen = top.keywords?.$seen;
        if (!seen) {
          const from = top.from?.[0]?.name || top.from?.[0]?.email || "";
          showDesktopNotification("email", {
            title: from || top.subject || "Novo e-mail",
            body: from ? (top.subject || "") : top.preview,
            tag: `mail-${top.id}`,
            // Abre a MENSAGEM, não só a caixa: /mail/mail/<pasta>/<id> é a rota do leitor.
            link: `/mail/mail/inbox/${top.id}`,
          });
        }
      }
    } catch { /* rede: ignora, o polling recupera */ }
  };

  // --- Notificações in-app (chat/meet/files/tasks) ---
  const seenNotif = useRef<Set<string>>(new Set());
  const notifReady = useRef(false);
  useEffect(() => {
    if (!notifItems) return;
    if (!notifReady.current) {
      notifItems.forEach((n) => seenNotif.current.add(n.id));
      notifReady.current = true;
      return;
    }
    for (const n of notifItems) {
      if (seenNotif.current.has(n.id)) continue;
      seenNotif.current.add(n.id);
      if (n.read_at) continue;
      const dom = domainOfKind(n.kind);
      if (!dom || !isDomain(dom)) continue;
      const txt = notifText(n, i18n.t.bind(i18n), i18n.language);
      // `n.link` aponta para o elemento (a conversa, o ficheiro, o evento) — o mesmo destino que o
      // sino usa. Clicar na notificação do SO abre exatamente o que abre clicar no sino.
      showDesktopNotification(dom, {
        title: txt.title, body: txt.body, tag: `notif-${n.id}`, link: n.link,
      });
    }
  }, [notifItems]);

  // Devolve o gatilho de e-mail para o App chamar dentro do callback do push JMAP.
  return () => checkEmail.current();
}
