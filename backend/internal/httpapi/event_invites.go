package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
	"mailin/backend/internal/notify"
	"strings"
)

// Convidados / RSVP. O dono convida colegas (event_invites); o evento entra na agenda deles e cada um
// responde. eventOut acrescenta ao evento se sou convidado (não dono) e a minha resposta.
type eventOut struct {
	db.Event
	Invited bool   `json:"invited"`
	MyRsvp  string `json:"my_rsvp,omitempty"`
	// Attendees vão nos cards do calendário (avatar/iniciais + tooltip nome/e-mail).
	Attendees []eventAttendee `json:"attendees,omitempty"`
}

type eventAttendee struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Avatar string `json:"avatar,omitempty"`
	Status string `json:"status,omitempty"` // invited | accepted | declined | maybe
}

// syncInvites regrava os convidados do evento (só colegas válidos do tenant) e avisa os novos.
func (s *Server) syncInvites(ctx context.Context, ev db.Event, actor db.MailUser, inviteeIDs []string) {
	prev := map[uuid.UUID]bool{}
	if cur, err := s.q.ListEventInvites(ctx, ev.ID); err == nil {
		for _, i := range cur {
			prev[i.UserID] = true
		}
	}
	_ = s.q.ClearEventInvites(ctx, ev.ID)
	for _, raw := range inviteeIDs {
		uid, err := uuid.Parse(raw)
		if err != nil || uid == actor.ID {
			continue
		}
		if _, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: uid, TenantID: actor.TenantID}); err != nil {
			continue // não é do tenant → nunca convida às cegas
		}
		if err := s.q.AddEventInvite(ctx, db.AddEventInviteParams{EventID: ev.ID, TenantID: actor.TenantID, UserID: uid}); err != nil {
			continue
		}
		if !prev[uid] { // avisa só quem é novo (não re-notifica a cada edição)
			s.notifyUser(ctx, actor.TenantID, uid, "event_invite",
				"Invitation: "+ev.Title, actor.Name+" invited you — "+ev.StartsAt.UTC().Format(time.RFC3339), eventLink(ev.ID, ev.StartsAt), uuid.Nil,
				map[string]string{"actor": actor.Name, "title": ev.Title, "when": ev.StartsAt.UTC().Format(time.RFC3339)})
		}
	}
}

// handleRSVP: o convidado responde ao evento (accepted|declined|maybe|pending). Avisa o dono.
func (s *Server) handleEventRSVP(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	valid := map[string]bool{"accepted": true, "declined": true, "maybe": true, "pending": true}
	if !valid[req.Status] {
		writeErr(w, http.StatusBadRequest, "events.invalid_rsvp")
		return
	}
	// Só quem foi convidado responde (o convite prova a posse do direito de responder).
	inv, err := s.q.GetEventInvite(r.Context(), db.GetEventInviteParams{EventID: id, UserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "events.not_invited")
		return
	}
	_ = s.q.SetInviteStatus(r.Context(), db.SetInviteStatusParams{EventID: id, UserID: mu.ID, Status: req.Status})
	// Avisa o dono da resposta.
	if ev, err := s.q.GetEventInTenant(r.Context(), db.GetEventInTenantParams{ID: id, TenantID: mu.TenantID}); err == nil && ev.OwnerUserID.Valid {
		s.notifyUser(r.Context(), mu.TenantID, uuid.UUID(ev.OwnerUserID.Bytes), "event_rsvp",
			"RSVP: "+ev.Title, mu.Name+" replied: "+req.Status, eventLink(ev.ID, ev.StartsAt), uuid.Nil,
			map[string]string{"actor": mu.Name, "title": ev.Title, "status": req.Status})
	}
	_ = inv
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

// handleEventInvites lista os convidados e seus status (dono ou convidado podem ver).
func (s *Server) handleEventInvites(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	rows, _ := s.q.ListEventInvites(r.Context(), id)
	_ = mu
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

// invitedToEvent converte a linha da lista de convidados no db.Event (para reaproveitar a expansão).
func invitedToEvent(iv db.ListInvitedEventsRow) db.Event {
	return db.Event{
		ID: iv.ID, TenantID: iv.TenantID, OwnerUserID: iv.OwnerUserID, Title: iv.Title,
		Description: iv.Description, Location: iv.Location, Color: iv.Color, StartsAt: iv.StartsAt,
		EndsAt: iv.EndsAt, SourceEmailID: iv.SourceEmailID, CreatedAt: iv.CreatedAt, Tags: iv.Tags,
		MeetingRoom: iv.MeetingRoom, RemindMinutes: iv.RemindMinutes, Reminded: iv.Reminded,
		AllDay: iv.AllDay, Recur: iv.Recur, RecurUntil: iv.RecurUntil,
	}
}

// invitedOccurrences expande (se recorrente) ou filtra pelo intervalo um evento a que fui convidado.
func invitedOccurrences(iv db.ListInvitedEventsRow, from, to time.Time) []eventOut {
	m := invitedToEvent(iv)
	var evs []db.Event
	if m.Recur != "" {
		evs = expandRecurrence(m, from, to)
	} else if m.StartsAt.Before(to) && m.EndsAt.After(from) {
		evs = []db.Event{m}
	}
	out := make([]eventOut, 0, len(evs))
	for _, e := range evs {
		out = append(out, eventOut{Event: e, Invited: true, MyRsvp: iv.MyRsvp})
	}
	return out
}

// syncExternals regrava os convidados EXTERNOS (e-mails de fora da organização) e envia o convite
// por e-mail aos NOVOS (best-effort: sem SMTP configurado, só regista). Validação mínima de e-mail;
// duplicados e vazios são ignorados.
func (s *Server) syncExternals(ctx context.Context, ev db.Event, actor db.MailUser, emails []string) {
	if emails == nil {
		return // pedido sem o campo: não mexe (compatibilidade com chamadas antigas)
	}
	prev := map[string]bool{}
	if cur, err := s.q.ListEventExternals(ctx, ev.ID); err == nil {
		for _, e := range cur {
			prev[e] = true
		}
	}
	_ = s.q.DeleteEventExternals(ctx, ev.ID)
	seen := map[string]bool{}
	for _, raw := range emails {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" || seen[email] || !strings.Contains(email, "@") || len(email) > 254 {
			continue
		}
		seen[email] = true
		if err := s.q.AddEventExternal(ctx, db.AddEventExternalParams{
			EventID: ev.ID, TenantID: ev.TenantID, Email: email,
		}); err != nil {
			continue
		}
		if !prev[email] {
			// Gente de FORA da organização não tem app onde receber a notificação: para essas vai
			// e-mail. Quem é de dentro é convidado por `syncInvites` e recebe notificação in-app.
			who := actor.Name
			if who == "" {
				who = actor.Email
			}
			meetURL := ""
			if ev.MeetingRoom != "" && s.publicURL != "" {
				meetURL = s.publicURL + "/j/" + ev.MeetingRoom
			}
			s.sendMsg(email, notify.EventInvite(actor.Language, who, ev.Title,
				ev.StartsAt.Format("02/01/2006 15:04"), ev.Location, meetURL))
		}
	}
}

// handleEventExternals devolve os convidados externos do evento (para o modal de edição).
func (s *Server) handleEventExternals(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	// Posse: o evento tem de ser do tenant (dono OU convidado interno podem ver).
	if _, gerr := s.q.GetEventInTenant(r.Context(), db.GetEventInTenantParams{ID: id, TenantID: mu.TenantID}); gerr != nil {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	emails, err := s.q.ListEventExternals(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(emails))
}
