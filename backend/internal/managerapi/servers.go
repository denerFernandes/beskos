package managerapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
)

// Servidores da infra (custo fixo POR VPS — pedido 22/07/2026, migração 00108). O custo fixo das
// finanças = soma dos servidores ativos + "outros fixos" do cost_model. Escritas admin-only + audit.

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListManagerServers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

type serverReq struct {
	ID           string `json:"id"` // vazio = criar
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	MonthlyCents int32  `json:"monthly_cents"`
	Notes        string `json:"notes"`
}

func (s *Server) handleSaveServer(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req serverReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" || req.MonthlyCents < 0 {
		writeErr(w, http.StatusBadRequest, "manager.server_invalid")
		return
	}
	id := uuid.New()
	if req.ID != "" {
		parsed, err := uuid.Parse(req.ID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "manager.invalid_id")
			return
		}
		id = parsed
	}
	row, err := s.q.UpsertManagerServer(r.Context(), db.UpsertManagerServerParams{
		ID: id, Name: req.Name, Provider: req.Provider,
		MonthlyCents: req.MonthlyCents, Notes: req.Notes,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), managerUserFrom(r.Context()), "server_saved", row.Name,
		map[string]any{"id": row.ID, "monthly_cents": row.MonthlyCents, "provider": row.Provider})
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleSetServerActive(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "manager.invalid_id")
		return
	}
	var req activeReq
	if !decode(w, r, &req) {
		return
	}
	if err := s.q.SetManagerServerActive(r.Context(), db.SetManagerServerActiveParams{ID: id, Active: req.Active}); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), managerUserFrom(r.Context()), "server_active_set", id.String(),
		map[string]any{"active": req.Active})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "active": req.Active})
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "manager.invalid_id")
		return
	}
	if err := s.q.DeleteManagerServer(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), managerUserFrom(r.Context()), "server_deleted", id.String(), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
