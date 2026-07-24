package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
)

// Checklist/subtarefas de um card. Acesso via cardOr404 (mesmo tenant). Itens caem por cascata.

func (s *Server) handleListChecklist(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.cardOr404(w, r)
	if !ok {
		return
	}
	rows, _ := s.q.ListChecklist(r.Context(), db.ListChecklistParams{CardID: c.ID, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleCreateChecklistItem(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.cardOr404(w, r)
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeErr(w, http.StatusBadRequest, "checklist.empty")
		return
	}
	pos, _ := s.q.NextChecklistPos(r.Context(), db.NextChecklistPosParams{CardID: c.ID, TenantID: mu.TenantID})
	item, err := s.q.CreateChecklistItem(r.Context(), db.CreateChecklistItemParams{
		TenantID: mu.TenantID, CardID: c.ID, Text: text, Position: pos,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "checklist.create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleToggleChecklistItem(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if _, ok := s.cardOr404(w, r); !ok { // valida posse do card (tenant) antes de mexer no item
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "checklist.invalid_id")
		return
	}
	var req struct {
		Done bool `json:"done"`
	}
	if !decode(w, r, &req) {
		return
	}
	item, err := s.q.SetChecklistDone(r.Context(), db.SetChecklistDoneParams{ID: itemID, TenantID: mu.TenantID, Done: req.Done})
	if err != nil {
		writeErr(w, http.StatusNotFound, "checklist.not_found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if _, ok := s.cardOr404(w, r); !ok {
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "checklist.invalid_id")
		return
	}
	if err := s.q.DeleteChecklistItem(r.Context(), db.DeleteChecklistItemParams{ID: itemID, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "checklist.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
