import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { settingsApi, devicesApi, backend, type EmailSettings, type DeviceToken } from "./backend";
import { useSection } from "./useSettings";
import { Spinner } from "./Spinner";
import { Switch } from "./Switch";
import { SignatureEditor } from "./SignatureEditor";
import { toast } from "./toast";
import { IcMail, IcGear, IcBell, IcKey, IcChat, IcCalendar, IcFolder, IcVideo, IcColumns, IcSparkle } from "../panel/icons";
import { MemorySection } from "./assistant/MemorySection";
import { syncApi, syncSupported, type SyncStatus } from "./syncClient";
import {
  notifSupported, notifPermission, requestNotifPermission, notifPrefs, setNotifPref, testNotification, type NotifDomain,
} from "./desktopNotify";
import { enableWebPush, disableWebPush, pushSupported } from "./webpush";
import { isTauri } from "./tauriNotify";

type Section = "general" | "email" | "chat" | "calendar" | "files" | "meet" | "kanban" | "notifications" | "devices" | "assistant";
const SECTIONS: Section[] = ["general", "email", "chat", "calendar", "files", "meet", "kanban", "notifications", "devices", "assistant"];

// Definições do utilizador — preferências pessoais, separadas do Admin (que é gestão da
// organização). Uma secção por app; por agora Geral e E-mail. A secção vem da URL.
export function SettingsView({ section, onSection }: { section?: string; onSection: (s: string) => void }) {
  const { t } = useTranslation();
  // No telemóvel a navegação é estilo iOS: sem secção na URL = lista de secções; com secção = detalhe
  // + voltar. `current` nulo significa "na lista" (raiz). No desktop o rail lateral mostra sempre
  // tudo, e a secção mostrada por omissão é a Geral.
  // A sincronização deixou de ser secção própria (vive dentro de Files); a URL antiga continua a
  // funcionar em vez de cair no vazio de quem tinha o link guardado.
  const wanted = section === "sync" ? "files" : section;
  const current: Section | null = SECTIONS.includes(wanted as Section) ? (wanted as Section) : null;
  const shown: Section = current ?? "general";

  const items: { id: Section; label: string; icon: JSX.Element }[] = [
    { id: "general", label: t("settings.general"), icon: <IcGear size={17} /> },
    { id: "email", label: t("settings.email"), icon: <IcMail size={17} /> },
    { id: "chat", label: t("settings.chat"), icon: <IcChat size={17} /> },
    { id: "calendar", label: t("settings.calendar"), icon: <IcCalendar size={17} /> },
    { id: "files", label: t("settings.files"), icon: <IcFolder size={17} /> },
    { id: "meet", label: t("settings.meet"), icon: <IcVideo size={17} /> },
    { id: "kanban", label: t("settings.kanban"), icon: <IcColumns size={17} /> },
    { id: "notifications", label: t("settings.notifications"), icon: <IcBell size={17} /> },
    { id: "devices", label: t("settings.devices"), icon: <IcKey size={17} /> },
    { id: "assistant", label: t("memory.title"), icon: <IcSparkle size={17} /> },
  ];

  return (
    <div className={"adm" + (current ? " has-section" : "")}>
      <aside className="adm-rail">
        <div className="adm-title">{t("settings.title")}</div>
        {items.map((i) => (
          <button key={i.id} className={shown === i.id ? "on" : ""} onClick={() => onSection(i.id)}>
            {i.icon} <span>{i.label}</span>
          </button>
        ))}
      </aside>
      <section className="adm-main">
        {/* Voltar à lista de secções (só telemóvel; escondido por CSS no desktop). */}
        <button className="adm-back" onClick={() => onSection("")}>‹ {t("settings.title")}</button>
        {shown === "general" && <GeneralSection />}
        {shown === "email" && <EmailSection />}
        {shown === "chat" && <ChatSection />}
        {shown === "calendar" && <CalendarSection />}
        {shown === "files" && <FilesSection />}
        {shown === "meet" && <MeetSection />}
        {shown === "kanban" && <KanbanSection />}
        {shown === "notifications" && <NotificationsSection />}
        {shown === "devices" && <DevicesSection />}
        {shown === "assistant" && <MemorySection />}
      </section>
    </div>
  );
}

// Geral: idioma, tema e vista inicial. Seguem o utilizador entre aparelhos (settings.general).
function GeneralSection() {
  const { t, i18n } = useTranslation();
  const { value, save } = useSection("general");
  const [lang, setLang] = useState(i18n.language.startsWith("en") ? "en" : "pt-BR");
  const changeLang = (l: string) => {
    setLang(l);
    void i18n.changeLanguage(l);
    try { localStorage.setItem("mailin.lang", l); } catch { /* modo privado */ }
    save({ language: l });
  };
  const changeTheme = (th: "light" | "dark") => {
    try { localStorage.setItem("mailin.theme", th); } catch { /* modo privado */ }
    document.documentElement.setAttribute("data-theme", th); // aplica já; o App reconcilia pela cache
    save({ theme: th });
  };
  const theme = value.theme ?? (document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light");
  // id da vista ⇄ chave de tradução do nav (kanban→board, cal→calendar).
  const VIEWS: { id: string; key: string }[] = [
    { id: "today", key: "today" }, { id: "mail", key: "mail" }, { id: "chat", key: "chat" },
    { id: "kanban", key: "board" }, { id: "cal", key: "calendar" }, { id: "files", key: "files" },
    { id: "contacts", key: "contacts" }, { id: "meet", key: "meet" },
  ];

  return (
    <div className="set-page">
      <h2>{t("settings.general")}</h2>
      <div className="set-card">
        <label className="set-field">
          <span>{t("settings.language")}</span>
          <select value={lang} onChange={(e) => changeLang(e.target.value)}>
            <option value="pt-BR">Português (Brasil)</option>
            <option value="en">English</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.theme")}</span>
          <select value={theme} onChange={(e) => changeTheme(e.target.value as "light" | "dark")}>
            <option value="light">{t("settings.themeLight")}</option>
            <option value="dark">{t("settings.themeDark")}</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.startView")}</span>
          <select value={value.startView ?? "today"} onChange={(e) => save({ startView: e.target.value })}>
            {VIEWS.map((v) => <option key={v.id} value={v.id}>{t(`webmail.apps.${v.key}`)}</option>)}
          </select>
        </label>
      </div>
    </div>
  );
}

// Chat: Enter envia + sons de notificação.
function ChatSection() {
  const { t } = useTranslation();
  const { value, save } = useSection("chat");
  return (
    <div className="set-page">
      <h2>{t("settings.chat")}</h2>
      <div className="set-card">
        <div className="ios-list">
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.chatEnterSends")}</span>
              <span className="ios-row-sub">{t("settings.chatEnterSendsHint")}</span>
            </div>
            <Switch checked={value.enterSends !== false} onChange={(on) => save({ enterSends: on })} label={t("settings.chatEnterSends")} />
          </div>
        </div>
      </div>
    </div>
  );
}

// Calendário: início da semana, dias de trabalho, vista inicial, duração e lembrete por defeito.
function CalendarSection() {
  const { t, i18n } = useTranslation();
  const { value, save } = useSection("calendar");
  // Dias de trabalho (0=domingo…6=sábado): fora deles o calendário fica desabilitado para criar.
  const work = value.workDays ?? [0, 1, 2, 3, 4, 5, 6];
  const first = value.weekStart === "sunday" ? 0 : 1;
  const dayName = (d: number) => new Date(2026, 2, 1 + d).toLocaleDateString(i18n.language, { weekday: "short" }); // 01/03/2026 = domingo
  const toggleDay = (d: number) => {
    const next = work.includes(d) ? work.filter((x) => x !== d) : [...work, d].sort();
    if (next.length === 0) return; // pelo menos um dia útil
    save({ workDays: next });
  };
  return (
    <div className="set-page">
      <h2>{t("settings.calendar")}</h2>
      <div className="set-card">
        <div className="set-field">
          <span>{t("settings.calWorkDays")}</span>
          <div className="workday-chips">
            {Array.from({ length: 7 }, (_, i) => (first + i) % 7).map((d) => (
              <button key={d} type="button" className={"wd-chip" + (work.includes(d) ? " on" : "")}
                onClick={() => toggleDay(d)}>{dayName(d)}</button>
            ))}
          </div>
        </div>
        <p className="desc" style={{ margin: "-4px 0 10px" }}>{t("settings.calWorkDaysHint")}</p>
        <label className="set-field">
          <span>{t("settings.calWeekStart")}</span>
          <select value={value.weekStart ?? "monday"} onChange={(e) => save({ weekStart: e.target.value as "monday" | "sunday" })}>
            <option value="monday">{t("settings.calMonday")}</option>
            <option value="sunday">{t("settings.calSunday")}</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.calDefaultView")}</span>
          <select value={value.defaultView ?? "month"} onChange={(e) => save({ defaultView: e.target.value as "month" | "week" | "day" | "list" })}>
            <option value="month">{t("settings.calViewMonth")}</option>
            <option value="week">{t("settings.calViewWeek")}</option>
            <option value="day">{t("settings.calViewDay")}</option>
            <option value="list">{t("settings.calViewList")}</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.calDuration")}</span>
          <select value={value.defaultDuration ?? 60} onChange={(e) => save({ defaultDuration: Number(e.target.value) })}>
            <option value={30}>30 min</option>
            <option value={60}>1 h</option>
            <option value={90}>1 h 30</option>
            <option value={120}>2 h</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.calReminder")}</span>
          <select value={value.defaultReminder ?? 10} onChange={(e) => save({ defaultReminder: Number(e.target.value) })}>
            <option value={0}>{t("settings.calNoReminder")}</option>
            <option value={5}>5 min</option>
            <option value={10}>10 min</option>
            <option value={30}>30 min</option>
            <option value={60}>1 h</option>
          </select>
        </label>
      </div>
    </div>
  );
}

// Ficheiros: vista por defeito + expiração por defeito do link partilhado.
function FilesSection() {
  const { t } = useTranslation();
  const { value, save } = useSection("files");
  return (
    <div className="set-page">
      <h2>{t("settings.files")}</h2>
      <div className="set-card">
        <label className="set-field">
          <span>{t("settings.filesView")}</span>
          <select value={value.view ?? "list"} onChange={(e) => save({ view: e.target.value as "grid" | "list" })}>
            <option value="list">{t("settings.filesViewList")}</option>
            <option value="grid">{t("settings.filesViewGrid")}</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.filesLinkExpiry")}</span>
          <select value={value.linkExpiryDays ?? 0} onChange={(e) => save({ linkExpiryDays: Number(e.target.value) })}>
            <option value={0}>{t("settings.filesNeverExpires")}</option>
            <option value={7}>7 {t("settings.days")}</option>
            <option value={30}>30 {t("settings.days")}</option>
            <option value={90}>90 {t("settings.days")}</option>
          </select>
        </label>
      </div>

      {/* Sincronização de pastas: é uma definição DO Drive, não uma app à parte — por isso vive
          aqui em vez de numa secção própria. Fora do app de desktop, o próprio bloco explica que
          só existe lá. */}
      <SyncSection />
    </div>
  );
}

// Meet: entrar com micro/câmara desligados por defeito.
function MeetSection() {
  const { t } = useTranslation();
  const { value, save } = useSection("meet");
  return (
    <div className="set-page">
      <h2>{t("settings.meet")}</h2>
      <div className="set-card">
        <div className="ios-list">
          <div className="ios-row">
            <span className="ios-row-label">{t("settings.meetEnterMuted")}</span>
            <Switch checked={value.enterMuted === true} onChange={(on) => save({ enterMuted: on })} label={t("settings.meetEnterMuted")} />
          </div>
          <div className="ios-row">
            <span className="ios-row-label">{t("settings.meetCamOff")}</span>
            <Switch checked={value.camOff === true} onChange={(on) => save({ camOff: on })} label={t("settings.meetCamOff")} />
          </div>
        </div>
      </div>
    </div>
  );
}

// Kanban: quadro inicial (ou o primeiro).
function KanbanSection() {
  const { t } = useTranslation();
  const { value, save } = useSection("kanban");
  const boardsQ = useQuery({ queryKey: ["boards"], queryFn: () => backend.boards(), retry: false });
  const boards = boardsQ.data ?? [];
  return (
    <div className="set-page">
      <h2>{t("settings.kanban")}</h2>
      <div className="set-card">
        <label className="set-field">
          <span>{t("settings.kanbanBoard")}</span>
          <select value={value.defaultBoard ?? ""} onChange={(e) => save({ defaultBoard: e.target.value })}>
            <option value="">{t("settings.kanbanFirstBoard")}</option>
            {boards.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
        </label>
      </div>
    </div>
  );
}

// Notificações desktop: permissão do browser + escolha por app. Preferências por-dispositivo
// (localStorage) — a permissão é do browser, então activar aqui não afeta outros aparelhos.
const NOTIF_DOMAINS: NotifDomain[] = ["email", "chat", "meet", "files", "tasks"];
const WEBPUSH_FLAG = "mailin.webpush.on";
function NotificationsSection() {
  const { t } = useTranslation();
  const [perm, setPerm] = useState(notifPermission());
  const [prefs, setPrefs] = useState(notifPrefs());
  const [closedOn, setClosedOn] = useState(() => { try { return localStorage.getItem(WEBPUSH_FLAG) === "1"; } catch { return false; } });

  // Ao conceder permissão, tenta também subscrever o Web Push (para notificar com o app fechado).
  // Se o servidor não tiver VAPID, a subscrição falha em silêncio e ficam só as notificações
  // com a aba aberta — em ambos os casos a experiência com a aba aberta funciona.
  const ask = async () => {
    const p = await requestNotifPermission();
    setPerm(p);
    if (p === "granted") { const ok = await enableWebPush(); setClosed(ok); }
  };
  const setClosed = (on: boolean) => { setClosedOn(on); try { localStorage.setItem(WEBPUSH_FLAG, on ? "1" : "0"); } catch { /* modo privado */ } };
  const toggleClosed = async (on: boolean) => {
    if (on) { const ok = await enableWebPush(); setClosed(ok); }
    else { await disableWebPush(); setClosed(false); }
  };
  const toggle = (d: NotifDomain, on: boolean) => { setNotifPref(d, on); setPrefs(notifPrefs()); };

  if (!notifSupported()) {
    return (
      <div className="set-page">
        <h2>{t("settings.notifications")}</h2>
        <div className="set-card"><p className="desc">{t("settings.notifUnsupported")}</p></div>
      </div>
    );
  }

  return (
    <div className="set-page">
      <h2>{t("settings.notifications")}</h2>
      <div className="set-card">
        {perm !== "granted" ? (
          <div className="set-field col">
            <span>{t("settings.notifEnable")}</span>
            <p className="desc">{perm === "denied" ? t("settings.notifDenied") : t("settings.notifAskHint")}</p>
            {perm !== "denied" && (
              <button className="send" style={{ alignSelf: "flex-start" }} onClick={() => void ask()}>
                {t("settings.notifAllow")}
              </button>
            )}
          </div>
        ) : (
          <>
            <div className="ios-group-label">{t("settings.notifPerApp")}</div>
            <div className="ios-list">
              {NOTIF_DOMAINS.map((d) => (
                <div key={d} className="ios-row">
                  <span className="ios-row-label">{t(`settings.notifDomain.${d}`)}</span>
                  <Switch checked={prefs[d]} onChange={(on) => toggle(d, on)} label={t(`settings.notifDomain.${d}`)} />
                </div>
              ))}
            </div>
            {/* App nativo (Tauri): notifica pelo SO sempre que está a correr (mesmo minimizado),
                por isso mostra uma nota em vez do Web Push (que o WKWebView não suporta). Na web,
                o toggle só aparece quando o browser suporta Push; senão explica porquê. */}
            {isTauri() ? (
              <div className="ios-list">
                <div className="ios-row">
                  <div className="ios-row-text">
                    <span className="ios-row-label">{t("settings.notifNativeTitle")}</span>
                    <span className="ios-row-sub">{t("settings.notifNativeHint")}</span>
                  </div>
                </div>
              </div>
            ) : pushSupported() ? (
              <div className="ios-list">
                <div className="ios-row">
                  <div className="ios-row-text">
                    <span className="ios-row-label">{t("settings.notifClosed")}</span>
                    <span className="ios-row-sub">{t("settings.notifTabHint")}</span>
                  </div>
                  <Switch checked={closedOn} onChange={(on) => void toggleClosed(on)} label={t("settings.notifClosed")} />
                </div>
              </div>
            ) : (
              <div className="ios-list">
                <div className="ios-row">
                  <div className="ios-row-text">
                    <span className="ios-row-label">{t("settings.notifClosed")}</span>
                    <span className="ios-row-sub">{t("settings.notifPushUnsupported")}</span>
                  </div>
                </div>
              </div>
            )}
            <button className="chipbtn" style={{ alignSelf: "flex-start" }}
              onClick={() => { void testNotification(t("settings.notifTestTitle"), t("settings.notifTestBody")).then((ok) => { if (!ok) toast(t("settings.notifTestFail")); }); }}>
              {t("settings.notifTest")}
            </button>
          </>
        )}
      </div>
    </div>
  );
}

// Sincronização de ficheiros: o motor corre DENTRO desta app (Tauri). O modelo é por PARES —
// escolhe-se uma pasta local, ela é criada no Drive, e só o que lá está sincroniza. Na web não há
// sync local, por isso mostra-se uma nota.
function SyncSection() {
  const { t } = useTranslation();
  const supported = syncSupported();
  const [status, setStatus] = useState<SyncStatus | null>(null);
  const [bandwidth, setBandwidth] = useState(0);
  const [busy, setBusy] = useState(false);
  // Conta atual (e-mail): isola o sync por utilizador. Sem isto, ao trocar de conta na mesma máquina
  // apareciam as pastas da conta anterior. O daemon compara e esconde a config de outra conta.
  const [account, setAccount] = useState("");
  useEffect(() => { if (supported) backend.me().then((m) => setAccount(m.email || "")).catch(() => {}); }, [supported]);

  // Sonda o estado enquanto a página está aberta (o motor corre em fundo e vai mudando).
  useEffect(() => {
    if (!supported) return;
    let alive = true;
    const tick = () => { syncApi.status(account).then((s) => { if (alive) setStatus(s); }).catch(() => {}); };
    tick();
    const id = setInterval(tick, 1500);
    return () => { alive = false; clearInterval(id); };
  }, [supported, account]);

  useEffect(() => { if (supported) syncApi.bandwidth().then(setBandwidth).catch(() => {}); }, [supported]);

  if (!supported) {
    return (
      <>
        <h3 className="set-sub">{t("settings.sync")}</h3>
        <div className="set-card"><p className="desc">{t("settings.syncOnlyDesktop")}</p></div>
      </>
    );
  }

  const addFolder = async () => {
    try {
      setBusy(true);
      const pair = await syncApi.addFolder(account);
      if (pair) toast(t("settings.syncFolderAdded", { name: pair.name }));
    } catch (e) { toast((e as Error).message); } finally { setBusy(false); }
  };

  const removeFolder = async (localPath: string) => {
    try { await syncApi.removeFolder(localPath); toast(t("settings.syncFolderRemoved")); }
    catch (e) { toast((e as Error).message); }
  };

  const pairs = status?.pairs ?? [];
  const phaseLabel = !status ? "…"
    : pairs.length === 0 ? t("settings.syncNoFolders")
    : status.paused ? t("settings.syncPaused")
    : status.phase === "syncing" ? t("settings.syncSyncing")
    : status.phase === "error" ? `${t("settings.syncError")}: ${status.message}`
    : status.message || t("settings.syncUpToDate");

  return (
    <>
      <h3 className="set-sub">{t("settings.sync")}</h3>

      <div className="set-card">
        <p className="desc">{t("settings.syncIntro")}</p>
        <div className="ios-list">
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.syncState")}</span>
              <span className="ios-row-sub">{phaseLabel}</span>
            </div>
            <span className={"sync-dot " + (status?.phase === "error" ? "err" : status?.phase === "syncing" ? "on" : "")} />
          </div>
        </div>
        <div className="drawer-actions">
          <button className="send" disabled={busy} onClick={() => void addFolder()}>
            {busy ? <Spinner size={13} /> : t("settings.syncAddFolder")}
          </button>
          {pairs.length > 0 && (
            <>
              <button className="chipbtn" onClick={() => void syncApi.now()}>{t("settings.syncNow")}</button>
              <button className="chipbtn" onClick={() => void syncApi.setPaused(!status?.paused)}>
                {status?.paused ? t("settings.syncResume") : t("settings.syncPause")}
              </button>
            </>
          )}
        </div>
      </div>

      {pairs.length > 0 && (
        <div className="set-card">
          <div className="ios-group-label">{t("settings.syncFolders")}</div>
          <div className="ios-list">
            {pairs.map((p) => (
              <div key={p.local_path} className="ios-row">
                <div className="ios-row-text">
                  <span className="ios-row-label">{p.name}</span>
                  <span className="ios-row-sub">{p.local_path}</span>
                </div>
                <button className="chipbtn danger" onClick={() => void removeFolder(p.local_path)}>
                  {t("settings.syncStop")}
                </button>
              </div>
            ))}
          </div>
          <p className="desc">{t("settings.syncStopHint")}</p>
        </div>
      )}

      <div className="set-card">
        <label className="set-field">
          <span>{t("settings.syncBandwidth")}</span>
          <select value={bandwidth} onChange={(e) => { const v = Number(e.target.value); setBandwidth(v); void syncApi.setBandwidth(v); }}>
            <option value={0}>{t("settings.syncNoLimit")}</option>
            <option value={256}>256 KB/s</option>
            <option value={1024}>1 MB/s</option>
            <option value={5120}>5 MB/s</option>
          </select>
        </label>
      </div>
    </>
  );
}

// Dispositivos: tokens para clientes de sincronização (inclui esta app). O token cru só aparece
// uma vez, na criação — mostramos num bloco copiável com aviso de que não será exibido de novo.
function DevicesSection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["devices"], queryFn: () => devicesApi.list(), retry: false });
  const revoke = useMutation({
    mutationFn: (id: string) => devicesApi.revoke(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["devices"] }); toast(t("settings.deviceRevoked")); },
    onError: (e) => toast((e as Error).message),
  });

  const devices: DeviceToken[] = q.data?.devices ?? [];
  const fmt = (s: string | null) => (s ? new Date(s).toLocaleString() : "—");

  return (
    <div className="set-page">
      <h2>{t("settings.devices")}</h2>
      <div className="set-card">
        {/* Criar um token à mão saiu daqui: a app de desktop cria-o sozinha ao ligar a
            sincronização (syncClient.connect), e não havia onde colar um token feito à mão — o
            motor de sync é uma biblioteca da app, não um daemon separado. O endpoint continua a
            existir, para automações e para quando voltar a fazer sentido na UI. */}
        <p className="desc">{t("settings.devicesHint")}</p>

        {q.isLoading ? <div className="loading">…</div> : devices.length === 0 ? (
          <p className="desc">{t("settings.deviceNone")}</p>
        ) : (
          <div className="ios-list">
            {devices.map((d) => (
              <div key={d.id} className="ios-row">
                <div className="ios-row-text">
                  <span className="ios-row-label">{d.name} {d.revoked_at && <em>· {t("settings.deviceRevokedTag")}</em>}</span>
                  <span className="ios-row-sub">{d.token_prefix}… · {t("settings.deviceLastUsed")}: {fmt(d.last_used_at)}</span>
                </div>
                {!d.revoked_at && (
                  <button className="chipbtn danger" disabled={revoke.isPending} onClick={() => revoke.mutate(d.id)}>{t("settings.deviceRevoke")}</button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// E-mail: a assinatura. Guardada em settings.email; anexada a cada envio pelo compositor.
function EmailSection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["settings"], queryFn: () => settingsApi.get(), retry: false });
  const [f, setF] = useState<EmailSettings>({ signature: "", signatureOn: true, aiSummary: true });

  useEffect(() => { if (q.data?.email) setF({ signature: q.data.email.signature ?? "", signatureOn: q.data.email.signatureOn !== false, aiSummary: q.data.email.aiSummary !== false }); }, [q.data]);

  const save = useMutation({
    mutationFn: () => settingsApi.patch({ email: f }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["settings"] }); toast(t("settings.saved")); },
    onError: (e) => toast((e as Error).message),
  });

  if (q.isLoading) return <div className="loading">…</div>;
  return (
    <div className="set-page">
      <h2>{t("settings.email")}</h2>
      <div className="set-card">
        <div className="set-field col">
          <span>{t("settings.signature")}</span>
          <SignatureEditor value={f.signature ?? ""} onChange={(html) => setF((p) => ({ ...p, signature: html }))} />
          <p className="desc">{t("settings.signatureHint")}</p>
        </div>
        <div className="ios-list">
          <div className="ios-row">
            <span className="ios-row-label">{t("settings.signatureOn")}</span>
            <Switch checked={f.signatureOn !== false} onChange={(on) => setF((p) => ({ ...p, signatureOn: on }))} label={t("settings.signatureOn")} />
          </div>
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.aiSummary")}</span>
              <span className="ios-row-sub">{t("settings.aiSummaryHint")}</span>
            </div>
            <Switch checked={f.aiSummary !== false} onChange={(on) => setF((p) => ({ ...p, aiSummary: on }))} label={t("settings.aiSummary")} />
          </div>
        </div>
        <div className="drawer-actions">
          <button className="send" disabled={save.isPending} onClick={() => save.mutate()}>
            {save.isPending ? <Spinner size={13} /> : t("contacts.save")}
          </button>
        </div>
      </div>
    </div>
  );
}
