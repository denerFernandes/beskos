import { useEffect, useRef, useState } from "react";
import { MFAGate } from "./MFAGate";
import { useOverlayOpen } from "./overlay";
import { ProfileView } from "./ProfileView";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { WebmailSession } from "./auth";
import { backend, settingsApi } from "./backend";
import { useJmapPush } from "./useJmapPush";
import { useDesktopNotifier } from "./useDesktopNotifier";
import { onNotificationClick } from "./webpush";
import { onDesktopNotifClick, setNotifNavigate } from "./desktopNotify";
import { applyBrandColor, cacheBrand, BESKOS_MARK } from "./branding";
import { initials } from "./util";
import { isTauri } from "./tauriNotify";
import { initVcardOpen } from "./desktopVcard";
import { setTrayAccount } from "./nativeWindow";
import { ToastHost, toast } from "./toast";
import { Onboarding } from "./Onboarding";
import { MailView } from "./MailView";
import { KanbanView } from "./KanbanView";
import { CalendarView } from "./CalendarView";
import { TodayView } from "./TodayView";
import { FilesView } from "./FilesView";
import { ContactsView } from "./ContactsView";
import { MeetView } from "./MeetView";
import { WikiView } from "./WikiView";
import { useChatSocket } from "./useChatSocket";
import { toastAction } from "./toast";
import { ChatView } from "./ChatView";
import { AdminView, useAdminEnabled, useAssistantEnabled, useDisabledApps } from "./AdminView";
import { SettingsView } from "./SettingsView";
import { NotificationsBell } from "./NotificationsBell";
import { AssistantBubble } from "./assistant/AssistantBubble";
import { IcSun, IcMail, IcColumns, IcCalendar, IcFolder, IcUsers, IcSearch, IcMoon, IcLogout, IcVideo, IcChat, IcShield, IcGear, IcChevron, IcBook } from "../panel/icons";

type View = "today" | "mail" | "chat" | "kanban" | "cal" | "files" | "contacts" | "meet" | "wiki" | "admin" | "settings" | "profile";
// Áreas com o que buscar. "today" e "admin" ficam de fora: não têm lista para filtrar.
const SEARCHABLE: View[] = ["mail", "chat", "kanban", "cal", "files", "contacts", "meet"];
const VIEWS: View[] = ["today", "mail", "chat", "kanban", "cal", "files", "contacts", "meet", "wiki", "admin", "settings", "profile"];

// Shell do app de e-mail: barra superior + rail de apps + view ativa. A view ativa fica na URL
// (/mail/<view>), então links são navegáveis e recarregáveis. "Hoje" é o padrão.
export function App({ session }: { session: WebmailSession }) {
  const { t, i18n } = useTranslation();
  const nav = useNavigate();
  // 2º segmento da URL: sala (Meet) ou seção (Admin). Todo item de menu fica na URL —
  // links são compartilháveis e o refresh não perde o lugar.
  const { view: viewParam, sub, sub2 } = useParams<{ view: string; sub: string; sub2: string }>();
  const view: View = VIEWS.includes(viewParam as View) ? (viewParam as View) : "today";
  const setView = (v: View) => nav(v === "today" ? "/mail" : `/mail/${v}`);
  const [search, setSearch] = useState("");
  const [advOpen, setAdvOpen] = useState(false); // painel de filtros avançados do e-mail (Gmail-style)
  // No app de desktop, dá a conta ao menu do tray: o tray é construído em Rust e não conhece a
  // sessão. Ao sair limpa (o cleanup manda `null`) — deixar lá o email de quem saiu seria mentira.
  useEffect(() => {
    if (!isTauri()) return;
    void setTrayAccount(session.email);
    return () => { void setTrayAccount(null); };
  }, [session.email]);
  const [mobSearch, setMobSearch] = useState(false); // no telemóvel a busca abre numa caixa abaixo
  const searchRef = useRef<HTMLInputElement>(null);
  useEffect(() => { if (mobSearch) setTimeout(() => searchRef.current?.focus(), 0); }, [mobSearch]);
  useEffect(() => { setMobSearch(false); }, [view]);
  // Preenche a busca de cima E foca o input (ex.: o botão "in:canal" do chat). Move o cursor ao fim.
  const fillSearch = (v: string) => {
    setSearch(v);
    setTimeout(() => { const el = searchRef.current; if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); } }, 0);
  };

  // Tema com estado (para o ícone refletir e alternar) + persistência entre sessões.
  const [theme, setTheme] = useState<"light" | "dark">(currentTheme);
  useEffect(() => { document.documentElement.setAttribute("data-theme", theme); }, [theme]);
  const toggleTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    try { localStorage.setItem("mailin.theme", next); } catch { /* modo privado */ }
    // Segue o utilizador entre aparelhos (fire-and-forget; o localStorage já garante o local).
    settingsApi.patch({ general: { theme: next } }).catch(() => {});
  };

  // Preferências gerais vindas do servidor: ao autenticar, o servidor manda (o localStorage foi só
  // o cache instantâneo até aqui). Reconcilia tema e idioma se divergirem do que está aplicado.
  const settingsQ = useQuery({ queryKey: ["settings"], queryFn: () => settingsApi.get(), retry: false });
  useEffect(() => {
    const g = settingsQ.data?.general;
    if (!g) return;
    if ((g.theme === "light" || g.theme === "dark") && g.theme !== theme) {
      setTheme(g.theme);
      try { localStorage.setItem("mailin.theme", g.theme); } catch { /* modo privado */ }
    }
    if (g.language && g.language !== i18n.language) {
      void i18n.changeLanguage(g.language);
      try { localStorage.setItem("mailin.lang", g.language); } catch { /* modo privado */ }
    }
    // Só reage a novos dados do servidor; theme/i18n fora das deps de propósito (evita re-loop).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsQ.data]);

  // Vista inicial (settings.general.startView): ao abrir a app na raiz (/mail, sem vista explícita na
  // URL), vai para a vista preferida. Uma única vez por carregamento — não pisa a navegação seguinte.
  const startApplied = useRef(false);
  useEffect(() => {
    if (startApplied.current) return;
    const sv = settingsQ.data?.general?.startView;
    if (sv === undefined) return; // aguarda as definições carregarem
    startApplied.current = true;
    if (sv && sv !== "today" && viewParam === undefined && VIEWS.includes(sv as View)) setView(sv as View);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsQ.data, viewParam]);

  // Trocar de área limpa a busca: um termo deixado para trás filtraria a área nova em silêncio —
  // a pessoa veria uma lista vazia sem perceber porquê.
  useEffect(() => { setSearch(""); }, [view]);

  // Clicar numa notificação desktop navega para o ITEM que a originou. Três origens, o mesmo
  // destino: service worker (web push), app nativo (evento do Rust) e o construtor Notification
  // (fallback web, cujo clique cai nesta aba).
  useEffect(() => onNotificationClick((link) => nav(link)), [nav]);
  useEffect(() => onDesktopNotifClick((link) => nav(link)), [nav]);
  useEffect(() => {
    setNotifNavigate((link) => nav(link));
    return () => setNotifNavigate(null);
  }, [nav]);
  const qc = useQueryClient();
  // Desktop: abrir um .vcf com o Beskos importa o contacto para a agenda. Só liga com sessão (esta
  // shell só monta autenticada), por isso o import nunca corre deslogado.
  useEffect(() => {
    return initVcardOpen(
      (r) => {
        qc.invalidateQueries({ queryKey: ["contacts"] });
        toast(r.imported > 0 ? t("contacts.vcardImported", { count: r.imported }) : t("contacts.vcardNoneNew"));
        if (r.imported > 0) nav("/mail/contacts");
      },
      (msg) => toast(msg),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const canAdmin = useAdminEnabled();
  // Primeiro acesso de quem se registou: define a senha da caixa, veste a casa (logo e cor) e chama
  // a equipa. Só aparece a quem pode fazê-lo (o dono) e enquanto a flag não estiver nas definições.
  const [obDone, setObDone] = useState(false);
  const needsOnboarding = !obDone && settingsQ.isSuccess && settingsQ.data?.general?.onboarded !== true && canAdmin;
  // A IA escreveu na wiki (evento wiki_page do hub): se NÃO estás na wiki, oferece "Abrir" num toast;
  // dentro da wiki o WikiView trata (navega para a página criada / refresca a aberta).
  useChatSocket((ev) => {
    if (ev.type !== "wiki_page" || view === "wiki") return;
    const p = ev.payload as { title?: string; link?: string };
    if (p.link) toastAction(t("wiki.aiWrotePage", { title: p.title ?? "" }), t("wiki.open"), () => nav(p.link!));
  });

  const disabledApps = useDisabledApps();
  const assistantEnabled = useAssistantEnabled();
  // A app atual foi desligada pela organização (ou por link direto): sai para a primeira app ainda
  // ligada (o Correio já não é garantido — também se pode desligar).
  useEffect(() => {
    if (!disabledApps.includes(view)) return;
    const order: View[] = ["today", "mail", "chat", "kanban", "cal", "files", "contacts", "meet", "wiki"];
    setView(order.find((v) => !disabledApps.includes(v)) ?? "settings");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [disabledApps, view]);

  // Fontes das badges: e-mail não lido (JMAP) + notificações não lidas por tipo (reunião, arquivo).
  const boxesQ = useQuery({ queryKey: ["mailboxes"], queryFn: () => session.service.mailboxes(), refetchInterval: 30000 });
  // refetchIntervalInBackground: TanStack Query stops an interval once the tab is hidden, and the
  // OS notification only fires WHILE the tab is hidden (see desktopNotify). The two rules cancelled
  // each other out: nothing was discovered during the only window in which anything would be shown,
  // and by the time the tab came back and the query refetched, the notification was suppressed for
  // being redundant. Desktop alerts for chat, tasks and the calendar were, on the web, near enough
  // dead. The alert sound rode on the same query and arrived just as late.
  const notifQ = useQuery({
    queryKey: ["notifications"], queryFn: () => backend.notifications(),
    refetchInterval: 30000, refetchIntervalInBackground: true, retry: false,
  });
  const chatChansQ = useQuery({
    queryKey: ["chat-channels"], queryFn: () => backend.chatChannels(),
    refetchInterval: 30000, refetchIntervalInBackground: true, retry: false,
  });
  const items = notifQ.data?.items ?? [];

  // Marca da organização (cor + logo). Aplica a cor globalmente e guarda em cache para o login.
  const brandingQ = useQuery({ queryKey: ["branding"], queryFn: () => backend.brandingGet(), retry: false, staleTime: 300000 });
  const brand = brandingQ.data;
  useEffect(() => {
    applyBrandColor(brand?.color ?? "");
    if (brand) cacheBrand(brand);
  }, [brand?.color, brand?.logo, brand?.name]);

  // Notificações desktop (aba aberta): e-mail via push JMAP, demais via diff das notificações.
  const inboxId = boxesQ.data?.find((b) => b.role === "inbox")?.id ?? null;
  const notifyEmail = useDesktopNotifier({
    service: session.service, inboxId, notifItems: notifQ.data?.items,
  });

  // Push JMAP global (fica ativo em qualquer aba): mantém a badge de e-mail viva mesmo fora do Mail
  // e alimenta a notificação desktop de novo e-mail.
  useJmapPush(session.service, () => {
    void qc.invalidateQueries({ queryKey: ["emails"] });
    void qc.invalidateQueries({ queryKey: ["mailboxes"] });
    notifyEmail();
  });
  const unreadOf = (kind: string) => items.filter((n) => n.kind === kind && !n.read_at).length;
  const chatUnread = (chatChansQ.data?.channels ?? []).reduce((s, c) => s + (c.unread ?? 0), 0);
  const badges: Partial<Record<View, number>> = {
    mail: boxesQ.data?.find((b) => b.role === "inbox")?.unreadEmails ?? 0,
    chat: chatUnread,
    meet: unreadOf("meeting_invite"),
    files: unreadOf("file_share"),
  };

  // Ao entrar na área do Meet, marca os convites como lidos (a vista mostra-os à cabeça). Ficheiros
  // NÃO: estar no Drive não significa que viste o ficheiro que te partilharam (pode estar numa
  // secção que não abriste) — a notificação fica no sino até a leres (24/07/2026, badge de partilha).
  useEffect(() => {
    if (view === "meet" && unreadOf("meeting_invite") > 0) {
      backend.notificationsReadKind("meeting_invite").then(() => qc.invalidateQueries({ queryKey: ["notifications"] })).catch(() => {});
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, items.length]);

  // Apps desligadas pela organização somem do rail para todos (mail é núcleo; admin é gated à parte).
  const off = new Set(disabledApps);
  const allApps: { id: View; label: string; icon: JSX.Element }[] = [
    { id: "today", label: t("webmail.apps.today"), icon: <IcSun size={20} /> },
    { id: "mail", label: t("webmail.apps.mail"), icon: <IcMail size={20} /> },
    { id: "chat", label: t("webmail.apps.chat"), icon: <IcChat size={20} /> },
    { id: "kanban", label: t("webmail.apps.board"), icon: <IcColumns size={20} /> },
    { id: "cal", label: t("webmail.apps.calendar"), icon: <IcCalendar size={20} /> },
    { id: "files", label: t("webmail.apps.files"), icon: <IcFolder size={20} /> },
    { id: "contacts", label: t("webmail.apps.contacts"), icon: <IcUsers size={20} /> },
    { id: "meet", label: t("webmail.apps.meet"), icon: <IcVideo size={20} /> },
    { id: "wiki", label: t("webmail.apps.wiki"), icon: <IcBook size={20} /> },
  ];
  const apps = allApps.filter((a) => !off.has(a.id));
  if (canAdmin) apps.push({ id: "admin", label: t("webmail.apps.admin"), icon: <IcShield size={20} /> });

  // Política da organização (admin/security): MFA obrigatório e ainda sem 2º fator → gate
  // bloqueante em cima de tudo até concluir o enrollment (partilha o cache "me-presence").
  const overlayUp = useOverlayOpen();
  const meGateQ = useQuery({
    queryKey: ["me-presence"], queryFn: () => backend.me(), retry: false,
    // A política de MFA pode ligar-se a qualquer momento: revalida ao focar a janela e a cada 60s,
    // para o gate aparecer sem exigir refresh manual.
    refetchOnWindowFocus: true, refetchInterval: 60_000,
  });
  // Sessão inválida/expirada detetada na validação de arranque (e nos refetch de 60s / foco): limpa
  // o estado local e volta ao login, mesmo que o caminho global do wrapper não tenha disparado.
  const gateErr = meGateQ.error as { status?: number } | null;
  useEffect(() => {
    if (gateErr && (gateErr.status === 401 || gateErr.status === 403)) session.logout();
  }, [gateErr, session]);
  if (meGateQ.data?.mfa_setup_required === true) {
    return <MFAGate method={meGateQ.data.mfa_required_method} email={session.email} onLogout={session.logout} />;
  }

  // Dentro do .wm de propósito: as variáveis do design system (cores, bordas, tema) estão
  // definidas nesse escopo. Fora dele, o cartão saía sem fundo nem contraste.
  if (needsOnboarding) {
    return <div className="wm"><Onboarding email={session.email} onDone={() => setObDone(true)} /></div>;
  }

  return (
    <div className="wm">
      <div className="shell">
        <header className="topbar">
          <div className="brand">
            {/* Sem logótipo do cliente, mostra a marca Beskos (antes era a inicial em texto). */}
            <img className={"brand-logo" + (brand?.logo && brand?.logo_white_dark ? " white-dark" : "")} src={brand?.logo || BESKOS_MARK} alt={brand?.name || "Beskos"} />
            <b>{brand?.name || "Beskos"}</b>
          </div>
          {SEARCHABLE.includes(view) && (
            <button className="ib search-icon" title={t("webmail.clearSearch")} onClick={() => setMobSearch((v) => !v)}><IcSearch size={18} /></button>
          )}
          {SEARCHABLE.includes(view) && (
            <label className={"search" + (mobSearch ? " mob-open" : "")}>
              <IcSearch size={16} />
              <input ref={searchRef} placeholder={t(`webmail.searchIn.${view}`)} value={search}
                onChange={(e) => setSearch(e.target.value)} />
              {search && <button className="clear" title={t("webmail.clearSearch")} onClick={() => setSearch("")}>×</button>}
              {view === "mail" && <button className="adv-toggle" title={t("adv.title")} onClick={() => setAdvOpen((v) => !v)}><IcChevron size={16} /></button>}
            </label>
          )}
          <div className="grow" />
          <NotificationsBell />
          <button className={"ib" + (view === "settings" ? " on" : "")} title={t("settings.title")} onClick={() => nav("/mail/settings")}><IcGear size={18} /></button>
          <button className="ib" title={t("webmail.toggleTheme")} onClick={toggleTheme}>{theme === "dark" ? <IcSun size={18} /> : <IcMoon size={18} />}</button>
          <StatusControl email={session.email} onProfile={() => nav("/mail/profile")} onLogout={session.logout} />
        </header>

        <nav className="apprail">
          {apps.map((a) => {
            const n = badges[a.id] ?? 0;
            return (
              <button key={a.id} className={"app" + (view === a.id ? " on" : "")}
                data-tip={a.label} data-tip-side="right" aria-label={a.label} onClick={() => setView(a.id)}>
                {a.icon}
                {n > 0 && <span className="app-badge">{n > 99 ? "99+" : n}</span>}
              </button>
            );
          })}
          {/* Assistente de IA: item no FUNDO do menu (rail), não uma bolha a pairar sobre o conteúdo.
              Fica montado mesmo escondido (a conversa sobrevive à navegação). */}
          <AssistantBubble hidden={view === "settings" || !assistantEnabled || overlayUp} />
        </nav>

        <div className="stage">
          {view === "today" && <TodayView session={session} onNavigate={setView} />}
          {view === "mail" && <MailView session={session} search={search} sub={sub} sub2={sub2} advOpen={advOpen} onAdvClose={() => setAdvOpen(false)} />}
          {view === "chat" && <ChatView search={search} onSearch={fillSearch} session={session} channelParam={sub} onChannel={(id) => nav(`/mail/chat/${id}`)} />}
          {view === "kanban" && <KanbanView search={search} boardParam={sub} cardParam={sub2} onBoard={(id) => nav(`/mail/kanban/${id}`)} onCard={(bid, cid) => nav(cid ? `/mail/kanban/${bid}/${cid}` : `/mail/kanban/${bid}`)} />}
          {view === "cal" && <CalendarView search={search} eventParam={sub} />}
          {/* "file" e "m" são segmentos reservados: /mail/files/file/<id> abre o ficheiro e
              /mail/meet/m/<id> a ficha da reunião (destinos das notificações). Os ids são UUID,
              nunca colidem com estes nomes. */}
          {view === "files" && <FilesView search={search} folderParam={sub === "file" ? undefined : sub} fileParam={sub === "file" ? sub2 : undefined} onFolder={(id) => nav(id ? `/mail/files/${id}` : "/mail/files")} />}
          {view === "contacts" && <ContactsView search={search} sub={sub} sub2={sub2} />}
          {view === "meet" && <MeetView roomParam={sub === "m" ? undefined : sub} meetingParam={sub === "m" ? sub2 : undefined} />}
          {view === "wiki" && <WikiView sub={sub} sub2={sub2} onNav={(s, p) => nav(p ? `/mail/wiki/${s}/${p}` : s ? `/mail/wiki/${s}` : "/mail/wiki")} />}
          {/* sec vazio = voltar à raiz (lista de secções no telemóvel); senão entra na secção. */}
          {view === "admin" && canAdmin && (
            <AdminView section={sub} detail={sub2}
              onSection={(sec) => nav(sec ? `/mail/admin/${sec}` : "/mail/admin")}
              /* O detalhe (ex.: um domínio) vive na URL: o link é partilhável e o refresh não perde o sítio. */
              onDetail={(sec, id) => nav(id ? `/mail/admin/${sec}/${id}` : `/mail/admin/${sec}`)} />
          )}
          {view === "profile" && <ProfileView section={sub} onSection={(sec) => nav(sec ? `/mail/profile/${sec}` : "/mail/profile")} />}
          {view === "settings" && <SettingsView section={sub} onSection={(sec) => nav(sec ? `/mail/settings/${sec}` : "/mail/settings")} />}
        </div>
      </div>
      <ToastHost />
    </div>
  );
}

// Status do utilizador (emoji + texto), estilo Slack. Clicar no avatar abre o editor com presets.
const STATUS_PRESETS = [
  { emoji: "💬", key: "available" }, { emoji: "📅", key: "meeting" }, { emoji: "🎧", key: "focus" },
  { emoji: "🍽️", key: "lunch" }, { emoji: "🏠", key: "remote" }, { emoji: "🌴", key: "vacation" }, { emoji: "🤒", key: "sick" },
];
function StatusControl({ email, onProfile, onLogout }: { email: string; onProfile: () => void; onLogout: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const q = useQuery({ queryKey: ["my-status"], queryFn: () => backend.userStatus(email), retry: false });
  const meQ = useQuery({ queryKey: ["me-presence"], queryFn: () => backend.me(), retry: false });
  const presence = meQ.data?.presence ?? "active";
  const setPresence = async (p: "active" | "away" | "dnd") => { try { await backend.setPresence(p); qc.invalidateQueries({ queryKey: ["me-presence"] }); } catch { /* silencioso */ } };
  const [emoji, setEmoji] = useState("");
  const [text, setText] = useState("");
  useEffect(() => { if (q.data) { setEmoji(q.data.emoji); setText(q.data.text); } }, [q.data]);
  const save = async (e: string, tx: string) => {
    setOpen(false);
    try { await backend.setMyStatus(e, tx); qc.invalidateQueries({ queryKey: ["my-status"] }); } catch { /* silencioso */ }
  };
  return (
    <div className="status-wrap">
      <button className="me" title={q.data?.text || email} onClick={() => setOpen((v) => !v)}>
        {meQ.data?.avatar
          ? <img src={meQ.data.avatar} alt="" style={{ width: "100%", height: "100%", borderRadius: "50%", objectFit: "cover" }} />
          : initials(email)}
        {q.data?.emoji && <span className="me-status">{q.data.emoji}</span>}
        <span className={"me-presence " + presence} />
      </button>
      {open && (
        <div className="status-pop" onMouseLeave={() => setOpen(false)}>
          <div className="status-id">
            {meQ.data?.avatar
              ? <img className="status-id-av" src={meQ.data.avatar} alt="" />
              : <span className="status-id-av status-id-ini">{initials(email)}</span>}
            <div className="status-id-tx">
              <b>{meQ.data?.name || email.split("@")[0]}</b>
              <span>{email}</span>
            </div>
          </div>
          <div className="presence-row">
            {(["active", "away", "dnd"] as const).map((p) => (
              <button key={p} className={"presence-opt " + p + (presence === p ? " on" : "")} onClick={() => void setPresence(p)}>
                <span className={"pres-dot " + p} /> {t("presence." + p)}
              </button>
            ))}
          </div>
          <div className="status-in">
            <input className="status-emoji" value={emoji} onChange={(e) => setEmoji(e.target.value)} placeholder="😀" maxLength={4} />
            <input className="status-text" value={text} onChange={(e) => setText(e.target.value)} placeholder={t("status.placeholder")} maxLength={100} onKeyDown={(e) => { if (e.key === "Enter") void save(emoji, text); }} />
          </div>
          <div className="status-presets">
            {STATUS_PRESETS.map((p) => (
              <button key={p.key} onClick={() => void save(p.emoji, t("status." + p.key))}>
                <span className="emo">{p.emoji}</span>{t("status." + p.key)}
              </button>
            ))}
          </div>
          <div className="status-actions">
            {(q.data?.emoji || q.data?.text) && <button className="chipbtn" onClick={() => void save("", "")}>{t("status.clear")}</button>}
            <div className="grow" />
            <button className="add" onClick={() => void save(emoji, text)}>{t("status.save")}</button>
          </div>
          <div className="status-menu">
            <button className="status-mi" onClick={() => { setOpen(false); onProfile(); }}>
              <IcUsers size={16} /> {t("profile.menu")}
            </button>
            <button className="status-mi danger" onClick={onLogout}>
              <IcLogout size={16} /> {t("webmail.logout")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// Tema inicial: o guardado (se houver), senão o atributo já no <html>, senão a preferência do SO.
function currentTheme(): "light" | "dark" {
  try {
    const saved = localStorage.getItem("mailin.theme");
    if (saved === "light" || saved === "dark") return saved;
  } catch { /* modo privado */ }
  const attr = document.documentElement.getAttribute("data-theme");
  if (attr === "light" || attr === "dark") return attr;
  return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}
