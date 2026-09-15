package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Calendário: eventos por usuário final (nossa camada). Filtra por owner (a mailbox logada).

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_range")
		return
	}
	// Params gerados: EndsAt = limite inferior (ends_at > from), StartsAt = limite superior (starts_at < to).
	events, err := s.q.ListEventsRange(r.Context(), db.ListEventsRangeParams{
		OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, EndsAt: from, StartsAt: to,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.failed")
		return
	}
	// Únicos só: os recorrentes vêm da expansão (senão o mestre apareceria duas vezes no range).
	out := make([]eventOut, 0, len(events))
	for _, e := range events {
		if e.Recur == "" {
			out = append(out, eventOut{Event: e})
		}
	}
	// Expande os mestres recorrentes (próprios) em ocorrências dentro do intervalo [from, to].
	if recurring, rerr := s.q.ListRecurringByOwner(r.Context(), pgtype.UUID{Bytes: mu.ID, Valid: true}); rerr == nil {
		for _, m := range recurring {
			for _, occ := range expandRecurrence(m, from, to) {
				out = append(out, eventOut{Event: occ})
			}
		}
	}
	// Eventos a que fui CONVIDADO entram na minha agenda (marcados invited + a minha resposta).
	if invited, ierr := s.q.ListInvitedEvents(r.Context(), mu.ID); ierr == nil {
		for _, iv := range invited {
			out = append(out, invitedOccurrences(iv, from, to)...)
		}
	}
	// Attaches the INVITEES of every event in a single pass (avatars on the cards).
	ids := make([]uuid.UUID, 0, len(out))
	seen := map[uuid.UUID]bool{}
	for _, e := range out {
		if !seen[e.ID] {
			seen[e.ID] = true
			ids = append(ids, e.ID)
		}
	}
	if len(ids) > 0 {
		byEvent := map[uuid.UUID][]eventAttendee{}
		invitedEmails := map[uuid.UUID]map[string]bool{}
		if rows, aerr := s.q.ListEventInviteesForEvents(r.Context(), ids); aerr == nil {
			for _, a := range rows {
				if a.Email == mu.Email {
					continue // o próprio utilizador não precisa de ver o próprio avatar em tudo
				}
				byEvent[a.EventID] = append(byEvent[a.EventID], eventAttendee{
					Name: a.Name, Email: a.Email, Avatar: a.Avatar, Status: a.Status,
				})
				if invitedEmails[a.EventID] == nil {
					invitedEmails[a.EventID] = map[string]bool{}
				}
				invitedEmails[a.EventID][a.Email] = true
			}
		}
		// The ORGANIZER also "is in the event": added first, unless it's the current user or they
		// are already in the invitee list.
		if cols, cerr := s.q.ListColleagues(r.Context(), mu.TenantID); cerr == nil {
			byID := map[uuid.UUID]db.ListColleaguesRow{}
			for _, c := range cols {
				byID[c.ID] = c
			}
			for i := range out {
				if !out[i].OwnerUserID.Valid {
					continue
				}
				ownerID := uuid.UUID(out[i].OwnerUserID.Bytes)
				if ownerID == mu.ID {
					continue
				}
				if c, ok := byID[ownerID]; ok && !invitedEmails[out[i].ID][c.Email] {
					owner := eventAttendee{Name: c.Name, Email: c.Email, Avatar: c.Avatar, Status: "organizer"}
					byEvent[out[i].ID] = append([]eventAttendee{owner}, byEvent[out[i].ID]...)
				}
			}
		}
		// Externos também "estão no evento": iniciais + selo external (sem avatar/conta).
		if ext, eerr := s.q.ListEventExternalsForEvents(r.Context(), ids); eerr == nil {
			for _, x := range ext {
				byEvent[x.EventID] = append(byEvent[x.EventID], eventAttendee{
					Email: x.Email, Status: "external",
				})
			}
		}
		for i := range out {
			out[i].Attendees = byEvent[out[i].ID]
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// expandRecurrence gera as ocorrências de um mestre recorrente que caem em [from, to]. Cada ocorrência
// é uma cópia com starts_at/ends_at deslocados (mesmo id do mestre — editar/apagar afeta a série).
func expandRecurrence(m db.Event, from, to time.Time) []db.Event {
	dur := m.EndsAt.Sub(m.StartsAt)
	limit := to
	if m.RecurUntil.Valid && m.RecurUntil.Time.Before(limit) {
		limit = m.RecurUntil.Time
	}
	next := func(t time.Time) time.Time {
		switch m.Recur {
		case "daily":
			return t.AddDate(0, 0, 1)
		case "weekly":
			return t.AddDate(0, 0, 7)
		case "monthly":
			return t.AddDate(0, 1, 0)
		case "yearly":
			return t.AddDate(1, 0, 0)
		}
		return t.AddDate(100, 0, 0) // desconhecido: encerra rápido
	}
	out := []db.Event{}
	occ := m.StartsAt
	for i := 0; i < 1000 && !occ.After(limit); i++ { // teto de segurança contra laço infinito
		end := occ.Add(dur)
		if end.After(from) { // sobrepõe o intervalo pedido
			e := m
			e.StartsAt = occ
			e.EndsAt = end
			out = append(out, e)
		}
		occ = next(occ)
	}
	return out
}

type createEventReq struct {
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Location       string   `json:"location"`
	Color          string   `json:"color"`
	StartsAt       string   `json:"starts_at"`
	EndsAt         string   `json:"ends_at"`
	SourceEmail    string   `json:"source_email_id"`
	Tags           []string `json:"tags"`
	CreateMeet     bool     `json:"create_meet"`     // cria uma sala do Meet vinculada
	Force          bool     `json:"force"`           // confirma mesmo havendo conflito de horário
	RemindMinutes  *int     `json:"remind_minutes"`  // minutos antes p/ lembrar (nil = default 10; 0 = sem)
	AllDay         bool     `json:"all_day"`         // evento de dia inteiro (sem hora)
	Recur          string   `json:"recur"`           // '' | daily | weekly | monthly | yearly
	RecurUntil     string   `json:"recur_until"`     // limite da recorrência (RFC3339, opcional)
	InviteeIDs     []string `json:"invitee_ids"`     // colegas convidados (RSVP)
	ExternalEmails []string `json:"external_emails"` // convidados de fora (recebem e-mail)
	CalendarID     string   `json:"calendar_id"`     // calendário do evento ("" = sem categoria)
}

// calendarID valida que o calendário é do próprio usuário; caso contrário devolve NULL.
func (s *Server) calendarID(ctx context.Context, mu db.MailUser, raw string) pgtype.UUID {
	if raw == "" {
		return pgtype.UUID{}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return pgtype.UUID{}
	}
	if _, err := s.q.GetCalendar(ctx, db.GetCalendarParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// recurValues normaliza a recorrência: só a escala fixa; qualquer outra coisa vira ” (único).
func recurValues(recur, until string) (string, pgtype.Timestamptz) {
	switch recur {
	case "daily", "weekly", "monthly", "yearly":
	default:
		recur = ""
	}
	var u pgtype.Timestamptz
	if recur != "" && until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			u = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	return recur, u
}

// remindOrDefault: nil = 10 (default como no Google Calendar); caso contrário o valor pedido (0 = sem).
func remindOrDefault(p *int) int32 {
	if p == nil {
		return 10
	}
	if *p < 0 {
		return 0
	}
	return int32(*p)
}

func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req createEventReq
	if !decode(w, r, &req) {
		return
	}
	starts, err1 := time.Parse(time.RFC3339, req.StartsAt)
	ends, err2 := time.Parse(time.RFC3339, req.EndsAt)
	if err1 != nil || err2 != nil || req.Title == "" {
		writeErr(w, http.StatusBadRequest, "events.invalid")
		return
	}
	if req.Color == "" {
		req.Color = "#116466"
	}
	// Aviso de conflito: se já houver evento sobrepondo o horário e o usuário não confirmou (force),
	// devolve 409 com os conflitos para o frontend perguntar (modal).
	if !req.Force {
		if conflicts, err := s.q.EventsOverlapping(r.Context(), db.EventsOverlappingParams{
			OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, EndsAt: starts, StartsAt: ends,
		}); err == nil && len(conflicts) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error_code": "events.conflict", "conflicts": conflicts})
			return
		}
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	recurN, recurU := recurValues(req.Recur, req.RecurUntil)
	event, err := s.q.CreateEvent(r.Context(), db.CreateEventParams{
		TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Title: req.Title, Description: req.Description, Location: req.Location, Color: req.Color,
		StartsAt: starts, EndsAt: ends, SourceEmailID: req.SourceEmail, Tags: req.Tags,
		RemindMinutes: remindOrDefault(req.RemindMinutes), AllDay: req.AllDay,
		Recur: recurN, RecurUntil: recurU, CalendarID: s.calendarID(r.Context(), mu, req.CalendarID),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.create_failed")
		return
	}
	// Integração com o Meet: cria a sala e vincula ao evento.
	if req.CreateMeet {
		event.MeetingRoom = s.attachMeetToEvent(r.Context(), mu, event.ID, req.Title, starts, ends)
	}
	s.syncInvites(r.Context(), event, mu, req.InviteeIDs)
	s.syncExternals(r.Context(), event, mu, req.ExternalEmails)
	writeJSON(w, http.StatusCreated, event)
}

type updateEventReq struct {
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Location       string   `json:"location"`
	Color          string   `json:"color"`
	StartsAt       string   `json:"starts_at"`
	EndsAt         string   `json:"ends_at"`
	Tags           []string `json:"tags"`
	Force          bool     `json:"force"`           // confirma mesmo com conflito de horário
	RemindMinutes  *int     `json:"remind_minutes"`  // minutos antes p/ lembrar (nil = 10; 0 = sem)
	AllDay         bool     `json:"all_day"`         // evento de dia inteiro
	Recur          string   `json:"recur"`           // '' | daily | weekly | monthly | yearly
	RecurUntil     string   `json:"recur_until"`     // limite (RFC3339, opcional)
	InviteeIDs     []string `json:"invitee_ids"`     // colegas convidados (RSVP)
	ExternalEmails []string `json:"external_emails"` // convidados de fora (recebem e-mail)
	CalendarID     string   `json:"calendar_id"`     // calendário do evento ("" = sem categoria)
}

func (s *Server) handleUpdateEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	var req updateEventReq
	if !decode(w, r, &req) {
		return
	}
	starts, err1 := time.Parse(time.RFC3339, req.StartsAt)
	ends, err2 := time.Parse(time.RFC3339, req.EndsAt)
	if err1 != nil || err2 != nil || req.Title == "" {
		writeErr(w, http.StatusBadRequest, "events.invalid")
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	// Aviso de conflito TAMBÉM na edição (antes só na criação): mesma semântica do create, mas
	// ignorando o próprio evento. Sem force e havendo choque → 409 com os conflitos.
	if !req.Force {
		if conflicts, cerr := s.q.EventsOverlappingExcept(r.Context(), db.EventsOverlappingExceptParams{
			OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, EndsAt: starts, StartsAt: ends, ID: id,
		}); cerr == nil && len(conflicts) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error_code": "events.conflict", "conflicts": conflicts})
			return
		}
	}
	recurN, recurU := recurValues(req.Recur, req.RecurUntil)
	// owner_user_id no WHERE: eventos são pessoais. Sem isto, um colega do mesmo tenant edita a
	// agenda alheia passando o UUID do evento (auditoria H7). "no rows" quando não é o dono → 404.
	event, err := s.q.UpdateEvent(r.Context(), db.UpdateEventParams{
		ID: id, TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Title: req.Title, Description: req.Description,
		Location: req.Location, Color: req.Color, StartsAt: starts, EndsAt: ends, Tags: req.Tags,
		RemindMinutes: remindOrDefault(req.RemindMinutes), AllDay: req.AllDay,
		Recur: recurN, RecurUntil: recurU, CalendarID: s.calendarID(r.Context(), mu, req.CalendarID),
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	s.syncInvites(r.Context(), event, mu, req.InviteeIDs)
	s.syncExternals(r.Context(), event, mu, req.ExternalEmails)
	writeJSON(w, http.StatusOK, event)
}

func (s *Server) handleDeleteEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	if err := s.q.DeleteEvent(r.Context(), db.DeleteEventParams{ID: id, TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}}); err != nil {
		writeErr(w, http.StatusInternalServerError, "events.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// attachMeetToEvent cria uma sala Meet, adiciona o dono como host e vincula-a ao evento. Devolve o
// nome da sala (ou "" se o Meet está desligado ou a criação falhou). Partilhado por criar-com-meet e
// adicionar-meet-a-evento-existente.
func (s *Server) attachMeetToEvent(ctx context.Context, mu db.MailUser, eventID uuid.UUID, title string, starts, ends time.Time) string {
	if s.meet == nil {
		return ""
	}
	room := "beskos-" + uuid.NewString()[:8]
	m, err := s.q.CreateMeeting(ctx, db.CreateMeetingParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID, Title: title, RoomName: room,
		ScheduledStart: pgtype.Timestamptz{Time: starts, Valid: true}, ScheduledEnd: pgtype.Timestamptz{Time: ends, Valid: true},
		EventID: pgtype.UUID{Bytes: eventID, Valid: true}, Access: "public",
	})
	if err != nil {
		return ""
	}
	_, _ = s.q.AddParticipant(ctx, db.AddParticipantParams{MeetingID: m.ID, TenantID: mu.TenantID, Email: mu.Email, UserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, Role: "host"})
	_ = s.q.SetEventMeetingRoom(ctx, db.SetEventMeetingRoomParams{ID: eventID, TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, MeetingRoom: room})
	return room
}

// handleAddMeetToEvent vincula uma sala Meet a um evento já existente (idempotente: se já tem sala,
// devolve-a). Só o dono do evento.
func (s *Server) handleAddMeetToEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	ev, err := s.q.GetEventInTenant(r.Context(), db.GetEventInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil || !ev.OwnerUserID.Valid || uuid.UUID(ev.OwnerUserID.Bytes) != mu.ID || ev.DeletedAt.Valid {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	if ev.MeetingRoom != "" { // já vinculado
		writeJSON(w, http.StatusOK, map[string]string{"meeting_room": ev.MeetingRoom})
		return
	}
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	room := s.attachMeetToEvent(r.Context(), mu, ev.ID, ev.Title, ev.StartsAt, ev.EndsAt)
	if room == "" {
		writeErr(w, http.StatusInternalServerError, "events.meet_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"meeting_room": room})
}

// handleRestoreEvent desfaz uma exclusão recente (undo estilo Gmail): limpa deleted_at. Só o dono.
func (s *Server) handleRestoreEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	ev, err := s.q.RestoreEvent(r.Context(), db.RestoreEventParams{ID: id, TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}})
	if err != nil {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

// handleDuplicateEvent cria uma cópia independente do evento (mesmo dono/tenant): novo id, sem
// convites nem vínculo de reunião. O título vem do frontend (que sabe o locale p/ o sufixo "(cópia)");
// vazio = cópia literal. Não checa conflito de horário — é uma cópia intencional.
func (s *Server) handleDuplicateEvent(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // corpo opcional; ausência/erro = cópia literal, sem escrever no w
	src, err := s.q.GetEventInTenant(r.Context(), db.GetEventInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil || !src.OwnerUserID.Valid || uuid.UUID(src.OwnerUserID.Bytes) != mu.ID || src.DeletedAt.Valid {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	title := src.Title
	if t := strings.TrimSpace(req.Title); t != "" {
		title = t
	}
	dup, err := s.q.CreateEvent(r.Context(), db.CreateEventParams{
		TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Title: title, Description: src.Description, Location: src.Location, Color: src.Color,
		StartsAt: src.StartsAt, EndsAt: src.EndsAt, SourceEmailID: "", Tags: src.Tags,
		RemindMinutes: src.RemindMinutes, AllDay: src.AllDay, Recur: src.Recur, RecurUntil: src.RecurUntil,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, dup)
}
