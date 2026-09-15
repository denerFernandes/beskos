package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Múltiplos calendários (por usuário) + free-busy (agenda de colega, só ocupado/livre).

func (s *Server) handleListCalendars(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	cals, err := s.q.ListCalendars(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "calendars.failed")
		return
	}
	if cals == nil {
		cals = []db.Calendar{}
	}
	writeJSON(w, http.StatusOK, cals)
}

type calendarReq struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Server) handleCreateCalendar(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req calendarReq
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "calendars.name_required")
		return
	}
	if req.Color == "" {
		req.Color = "#116466"
	}
	c, err := s.q.CreateCalendar(r.Context(), db.CreateCalendarParams{TenantID: mu.TenantID, OwnerUserID: mu.ID, Name: strings.TrimSpace(req.Name), Color: req.Color})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "calendars.failed")
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateCalendar(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "calendars.invalid_id")
		return
	}
	var req calendarReq
	if !decode(w, r, &req) {
		return
	}
	if req.Color == "" {
		req.Color = "#116466"
	}
	c, err := s.q.UpdateCalendar(r.Context(), db.UpdateCalendarParams{ID: id, OwnerUserID: mu.ID, Name: strings.TrimSpace(req.Name), Color: req.Color})
	if err != nil {
		writeErr(w, http.StatusNotFound, "calendars.not_found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteCalendar(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "calendars.invalid_id")
		return
	}
	// Eventos do calendário ficam sem categoria (ON DELETE SET NULL), não são apagados.
	if err := s.q.DeleteCalendar(r.Context(), db.DeleteCalendarParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "calendars.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleFreeBusy devolve os blocos ocupados de um colega (mesmo tenant) num intervalo — só início/fim,
// sem título nem detalhes. Serve para agendar sem expor a agenda alheia.
func (s *Server) handleFreeBusy(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	target, err := uuid.Parse(r.URL.Query().Get("user"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_id")
		return
	}
	from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "events.invalid_range")
		return
	}
	// O alvo tem de ser um colega do mesmo tenant (nunca expõe agenda de fora).
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: target, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "events.not_found")
		return
	}
	// EndsAt = limite inferior (ends_at > from), StartsAt = limite superior (starts_at < to).
	blocks, err := s.q.FreeBusy(r.Context(), db.FreeBusyParams{OwnerUserID: pgUUID(target), TenantID: mu.TenantID, EndsAt: from, StartsAt: to})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.failed")
		return
	}
	if blocks == nil {
		blocks = []db.FreeBusyRow{}
	}
	writeJSON(w, http.StatusOK, blocks)
}
