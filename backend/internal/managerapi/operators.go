package managerapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// Manager operator management. Writes are admin-only. There is no email invitation: an admin
// creates the account with a temporary password, and mandatory TOTP completes on first login.

func (s *Server) handleListOperators(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListManagerUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

type operatorReq struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (s *Server) handleCreateOperator(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req operatorReq
	if !decode(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || len(req.Password) < 12 || (req.Role != "admin" && req.Role != "viewer") {
		writeErr(w, http.StatusBadRequest, "manager.operator_invalid")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	u, err := s.q.CreateManagerUser(r.Context(), db.CreateManagerUserParams{
		Email: req.Email, Name: req.Name, PasswordHash: hash, Role: req.Role,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "manager.operator_exists")
		return
	}
	s.audit(r.Context(), managerUserFrom(r.Context()), "operator_created", u.Email,
		map[string]any{"role": u.Role})
	writeJSON(w, http.StatusCreated, publicUser(u))
}

func (s *Server) handleSetOperatorActive(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	me := managerUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "manager.invalid_id")
		return
	}
	// Não se desativa a si próprio (evita trancar o último admin para fora).
	if id == me.ID {
		writeErr(w, http.StatusBadRequest, "manager.operator_self")
		return
	}
	var req activeReq
	if !decode(w, r, &req) {
		return
	}
	if err := s.q.SetManagerUserActive(r.Context(), db.SetManagerUserActiveParams{ID: id, Active: req.Active}); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), me, "operator_active_set", id.String(), map[string]any{"active": req.Active})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "active": req.Active})
}
