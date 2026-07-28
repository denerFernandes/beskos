import { useState, useRef, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { backend, colleaguesApi, type CalEvent, type Tag, type Calendar } from "./backend";
import { EventModal } from "./EventModal";
import { CalendarSidebar, NewCalendarDrawer, NewCategoryDrawer, CAL_COLORS } from "./CalendarSidebar";
import { BookingDrawer } from "./BookingDrawer";
import { TimeOffDrawer } from "./TimeOffDrawer";
import { useDisabledApps } from "./AdminView";
import { MonthGrid, ListView, TimeGrid } from "./CalendarGrids";
import { Spinner, AiThinking } from "./Spinner";
import { toast } from "./toast";
import { triggerDownload } from "../apiBase";
import { hueFor } from "./util";
import { IcSparkle, IcPlus, IcUpload, IcDownload, IcSearch, IcVideo } from "../panel/icons";
import { useUserSettings } from "./useSettings";
import "./calendar.css";

// Página do calendário (24/07/2026): layout da referência SyncWise com o chrome no padrão da
// casa (sidebar como a de Contactos; CATEGORIAS = tags do tenant com paleta pastel). Vista
// padrão: SEMANA. Cartões com fundo suave da categoria em todas as vistas.
type View = "month" | "week" | "day" | "list";
// Cores para a agenda de COLEGAS (free-busy) — distintas das dos meus calendários.
const COLLEAGUE_COLORS = ["#8257C7", "#3B6FB0", "#C0692F", "#0F766E", "#BE185D"];
const startOfDay = (d: Date) => { const x = new Date(d); x.setHours(0, 0, 0, 0); return x; };
const addDays = (d: Date, n: number) => { const x = new Date(d); x.setDate(x.getDate() + n); return x; };
const weekStart = (d: Date, startsOn = 1) => { const x = startOfDay(d); x.setDate(x.getDate() - ((x.getDay() - startsOn + 7) % 7)); return x; };

export function CalendarView({ search, eventParam }: {
  search: string;
  /** Evento a abrir (/mail/cal/<id>?d=<data>) — destino dos avisos de convite/RSVP/reserva. */
  eventParam?: string;
}) {
  const { t, i18n } = useTranslation();
  const [urlParams] = useSearchParams();
  const qc = useQueryClient();
  const [view, setView] = useState<View>("week");
  const [cursor, setCursor] = useState(new Date());
  const [modal, setModal] = useState<{ event?: CalEvent | null; day?: Date } | null>(null);
  const [newCal, setNewCal] = useState(false);
  const [newCat, setNewCat] = useState(false);
  const [bookingCfg, setBookingCfg] = useState(false);
  const [timeOff, setTimeOff] = useState(false);
  const mailEnabled = !useDisabledApps().includes("mail");
  const [agenda, setAgenda] = useState<string | null>(null);
  // Busca local do header (ícone da lupa, como a referência) — soma-se à busca global do topo.
  const [q2, setQ2] = useState("");
  const [qOpen, setQOpen] = useState(false);

  const settings = useUserSettings();
  const weekStartsOn = settings.calendar?.weekStart === "sunday" ? 0 : 1;
  // Dias de trabalho (Definições → Calendário): fora deles a grade fica desabilitada para criar.
  const workDays = settings.calendar?.workDays ?? [0, 1, 2, 3, 4, 5, 6];
  const viewApplied = useRef(false);
  useEffect(() => {
    if (!viewApplied.current && settings.calendar?.defaultView) { viewApplied.current = true; setView(settings.calendar.defaultView); }
  }, [settings.calendar?.defaultView]);

  const range = (() => {
    if (view === "day") return [startOfDay(cursor), addDays(startOfDay(cursor), 1)];
    if (view === "week") { const s = weekStart(cursor, weekStartsOn); return [s, addDays(s, 7)]; }
    if (view === "list") { const s = startOfDay(cursor); return [s, addDays(s, 30)]; }
    const first = new Date(cursor.getFullYear(), cursor.getMonth(), 1);
    const s = weekStart(first, weekStartsOn); return [s, addDays(s, 42)];
  })();
  const evQ = useQuery({ queryKey: ["events", range[0].toISOString(), view], queryFn: () => backend.events(range[0].toISOString(), range[1].toISOString()), retry: false });

  // Deep link /mail/cal/<id>?d=<data>: veio de uma notificação. Primeiro leva a grade para a data do
  // evento (a agenda só carrega o intervalo visível); quando esse intervalo chega, abre a ficha.
  const day = urlParams.get("d");
  useEffect(() => {
    if (!eventParam || !day) return;
    const d = new Date(day);
    if (!isNaN(d.getTime())) setCursor(d);
  }, [eventParam, day]);
  const openedEvent = useRef<string | null>(null);
  useEffect(() => {
    if (!eventParam || openedEvent.current === eventParam) return;
    // Procura na resposta CRUA: um calendário escondido não deve engolir um link direto.
    const ev = (evQ.data ?? []).find((e) => e.id === eventParam);
    if (!ev) return; // ainda a carregar (ou evento apagado): não faz nada
    openedEvent.current = eventParam;
    setModal({ event: ev });
  }, [eventParam, evQ.data]);
  const calsQ = useQuery({ queryKey: ["calendars"], queryFn: () => backend.calendars(), retry: false });
  const cals = calsQ.data ?? [];
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const toggleCal = (id: string) => setHidden((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n; });
  // Categorias = tags partilhadas do tenant. Escondê-las filtra os eventos dessa categoria.
  const tagsQ = useQuery({ queryKey: ["tags"], queryFn: () => backend.tagsList(), retry: false });
  const cats = tagsQ.data ?? [];
  const [hiddenCats, setHiddenCats] = useState<Set<string>>(new Set());
  const toggleCat = (name: string) => setHiddenCats((s) => { const n = new Set(s); n.has(name) ? n.delete(name) : n.add(name); return n; });
  // Free-busy: agenda de um colega sobreposta na grade.
  const [fbUser, setFbUser] = useState("");
  const colleaguesQ = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const fbQ = useQuery({ queryKey: ["freebusy", fbUser, range[0].toISOString(), view], queryFn: () => backend.freeBusy(fbUser, range[0].toISOString(), range[1].toISOString()), enabled: !!fbUser, retry: false });
  const busy = fbQ.data ?? [];
  const fbColleague = (colleaguesQ.data ?? []).find((p) => p.id === fbUser);
  const fbName = fbColleague ? (fbColleague.name || fbColleague.email) : "";
  const fbIdx = (colleaguesQ.data ?? []).findIndex((p) => p.id === fbUser);
  const fbColor = fbIdx >= 0 ? COLLEAGUE_COLORS[fbIdx % COLLEAGUE_COLORS.length] : COLLEAGUE_COLORS[0];

  // Cor do cartão: a CATEGORIA do evento (primeira tag no catálogo) manda; sem categoria, a cor do
  // próprio evento. O pastel é derivado no CSS (color-mix com branco).
  const catColor = useMemo(() => { const m = new Map<string, string>(); for (const c of cats) m.set(c.name, c.color); return m; }, [cats]);
  const colorOf = (e: CalEvent) => { for (const tg of e.tags ?? []) { const c = catColor.get(tg); if (c) return c; } return e.color; };
  const catOf = (e: CalEvent) => (e.tags ?? []).find((tg) => catColor.has(tg));

  const term = (search || q2).trim().toLowerCase();
  const events = (evQ.data ?? []).filter((e) =>
    (!e.calendar_id || !hidden.has(e.calendar_id)) &&
    !hiddenCats.has(catOf(e) ?? "") &&
    (!term || (e.title ?? "").toLowerCase().includes(term) || (e.description ?? "").toLowerCase().includes(term)));
  const refresh = () => qc.invalidateQueries({ queryKey: ["events"] });
  const reschedule = useMutation({
    mutationFn: ({ e, start, end }: { e: CalEvent; start: Date; end: Date }) =>
      backend.updateEvent(e.id, {
        title: e.title, description: e.description, location: e.location, color: e.color, tags: e.tags,
        remind_minutes: e.remind_minutes, all_day: e.all_day, recur: e.recur, recur_until: e.recur_until ?? "",
        invitee_ids: [], force: true, starts_at: start.toISOString(), ends_at: end.toISOString(),
      }),
    onSuccess: () => refresh(), onError: (er) => toast((er as Error).message),
  });
  const refreshCals = () => qc.invalidateQueries({ queryKey: ["calendars"] });
  const createCal = useMutation({ mutationFn: (v: { name: string; color: string }) => backend.createCalendar(v.name, v.color), onSuccess: () => { refreshCals(); setNewCal(false); }, onError: (e) => toast((e as Error).message) });
  const deleteCal = useMutation({ mutationFn: (id: string) => backend.deleteCalendar(id), onSuccess: () => { refreshCals(); refresh(); }, onError: (e) => toast((e as Error).message) });
  const createCat = useMutation({ mutationFn: (v: { name: string; color: string }) => backend.tagUpsert(v.name, v.color), onSuccess: () => { qc.invalidateQueries({ queryKey: ["tags"] }); setNewCat(false); }, onError: (e) => toast((e as Error).message) });
  const deleteCat = useMutation({ mutationFn: (id: string) => backend.tagDelete(id), onSuccess: () => qc.invalidateQueries({ queryKey: ["tags"] }), onError: (e) => toast((e as Error).message) });
  const icsInput = useRef<HTMLInputElement>(null);
  const importIcs = useMutation({
    mutationFn: (f: File) => backend.importEvents(f),
    onSuccess: (r) => { refresh(); toast(t("cal.imported", { count: r.imported })); },
    onError: (e) => toast((e as Error).message),
  });

  const step = (dir: number) => setCursor((c) => view === "day" ? addDays(c, dir) : view === "week" ? addDays(c, dir * 7) : view === "list" ? addDays(c, dir * 30) : new Date(c.getFullYear(), c.getMonth() + dir, 1));
  // Título como a referência: "January 2024" — mês + ano em todas as vistas.
  const label = cursor.toLocaleDateString(i18n.language, { month: "long", year: "numeric" });

  const aiAgenda = useMutation({
    mutationFn: () => { const d = startOfDay(cursor); return backend.aiAgenda(d.toISOString(), addDays(d, 1).toISOString()); },
    onSuccess: (r) => setAgenda(r.agenda || t("cal.agendaEmpty")), onError: (e) => toast((e as Error).message),
  });

  // "Próximo evento" flutuante (semana/dia, como a referência): o primeiro que ainda vai começar.
  const upcoming = (view === "week" || view === "day")
    ? events.filter((e) => new Date(e.starts_at) > new Date()).sort((a, b) => a.starts_at.localeCompare(b.starts_at))[0]
    : undefined;

  return (
    <div className="calsw">
      <CalendarSidebar
        cals={cals} hiddenCals={hidden} onToggleCal={toggleCal} onAddCal={() => setNewCal(true)} onDeleteCal={(c: Calendar) => deleteCal.mutate(c.id)}
        cats={cats} hiddenCats={hiddenCats} onToggleCat={toggleCat} onAddCat={() => setNewCat(true)} onDeleteCat={(tg: Tag) => deleteCat.mutate(tg.id)}
        colleagues={colleaguesQ.data ?? []} fbUser={fbUser} setFbUser={setFbUser} />

      <div className="calsw-main">
        <div className="csw-top">
          <div className="csw-title"><h2>{label}</h2>
            <button className="csw-nav" onClick={() => step(-1)}>‹</button>
            <button className="csw-nav" onClick={() => step(1)}>›</button>
            <button className="csw-today" onClick={() => setCursor(new Date())}>{t("cal.today")}</button>
          </div>
          <div className="csw-views">
            {(["day", "week", "month", "list"] as const).map((v) => (
              <button key={v} className={view === v ? "on" : ""} onClick={() => setView(v)}>{t("cal.view_" + v)}</button>
            ))}
          </div>
          <div className="grow" />
          {qOpen
            ? <input className="csw-q" autoFocus value={q2} placeholder={t("cal.searchEvents")}
                onChange={(e) => setQ2(e.target.value)} onBlur={() => { if (!q2) setQOpen(false); }} />
            : <button className="csw-ib" title={t("cal.searchEvents")} onClick={() => setQOpen(true)}><IcSearch size={15} /></button>}
          <button className="csw-ib" title={t("cal.import")} onClick={() => icsInput.current?.click()}><IcUpload size={15} /></button>
          <input ref={icsInput} type="file" accept=".ics,text/calendar" hidden onChange={(e) => { const f = e.target.files?.[0]; if (f) importIcs.mutate(f); e.target.value = ""; }} />
          <button className="csw-ib" title={t("cal.export")} onClick={() => triggerDownload(backend.eventsExportUrl(), "calendar.ics")}><IcDownload size={15} /></button>
          <button className="csw-ib" title={t("cal.aiAgenda")} onClick={() => aiAgenda.mutate()} disabled={aiAgenda.isPending}>{aiAgenda.isPending ? <Spinner size={15} /> : <IcSparkle size={15} />}</button>
          <button className="csw-today" onClick={() => setTimeOff(true)}>{t("cal.timeoff")}</button>
          <button className="csw-today" onClick={() => setBookingCfg(true)}>{t("cal.booking")}</button>
          <button className="csw-add" onClick={() => setModal({ day: cursor })}><IcPlus size={15} /> {t("cal.newEvent")}</button>
        </div>

        <div className="cal2-body">
          {view === "list"
            ? <ListView from={startOfDay(cursor)} days={30} events={events} colorOf={colorOf} onEvent={(e) => setModal({ event: e })} lang={i18n.language} />
            : view === "month"
            ? <MonthGrid cursor={cursor} events={events} busy={busy} busyLabel={fbName} busyColor={fbColor} colorOf={colorOf} workDays={workDays} onDay={(d) => setModal({ day: d })} onEvent={(e) => setModal({ event: e })} lang={i18n.language} weekStartsOn={weekStartsOn} />
            : <TimeGrid days={view === "day" ? [startOfDay(cursor)] : Array.from({ length: 7 }, (_, i) => addDays(weekStart(cursor, weekStartsOn), i))}
                events={events} busy={busy} busyLabel={fbName} busyColor={fbColor} colorOf={colorOf} workDays={workDays} onSlot={(d) => setModal({ day: d })} onEvent={(e) => setModal({ event: e })}
                onReschedule={(e, start, end) => reschedule.mutate({ e, start, end })} lang={i18n.language} />}
        </div>

        {upcoming && (
          <button className="csw-upnext" onClick={() => setModal({ event: upcoming })}>
            {upcoming.meeting_room && <span className="ic"><IcVideo size={15} /></span>}
            <span className="tx"><b>{upcoming.title}</b><i>{t("cal.upcoming")}</i></span>
            {(upcoming.attendees ?? []).length > 0 && (
              <span className="csw-avs">
                {(upcoming.attendees ?? []).slice(0, 3).map((a) => a.avatar
                  ? <img key={a.email} src={a.avatar} alt="" title={a.name || a.email} />
                  : <i key={a.email} style={{ background: hueFor(a.email) }} title={a.name || a.email}>{(a.name || a.email).slice(0, 1).toUpperCase()}</i>)}
                {(upcoming.attendees ?? []).length > 3 && <em>+{(upcoming.attendees ?? []).length - 3}</em>}
              </span>
            )}
          </button>
        )}
      </div>

      {modal && <EventModal initial={modal.event} day={modal.day} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh(); }} />}
      {newCal && <NewCalendarDrawer defaultColor={CAL_COLORS[cals.length % CAL_COLORS.length]} pending={createCal.isPending} onClose={() => setNewCal(false)} onCreate={(name, color) => createCal.mutate({ name, color })} />}
      {bookingCfg && <BookingDrawer onClose={() => setBookingCfg(false)} />}
      {timeOff && <TimeOffDrawer mailEnabled={mailEnabled} onClose={() => setTimeOff(false)} onSaved={() => setTimeOff(false)} />}
      {newCat && <NewCategoryDrawer pending={createCat.isPending} onClose={() => setNewCat(false)} onCreate={(name, color) => createCat.mutate({ name, color })} />}
      {agenda !== null && (
        <div className="cal2-agenda">
          <div className="ca-head"><b><IcSparkle size={15} /> {t("cal.aiAgenda")}</b><button className="ib" title={t("common.close")} onClick={() => setAgenda(null)}>✕</button></div>
          {aiAgenda.isPending ? <AiThinking /> : <p>{agenda}</p>}
        </div>
      )}
    </div>
  );
}
