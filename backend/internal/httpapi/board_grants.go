package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
)

// Permissões dos quadros do Kanban (padrão dos grants da wiki): pessoa/equipa/org com papéis
// viewer|editor|manager. Quadros LEGADOS (sem dono nem grants) continuam abertos à organização
// (compatibilidade); quem tem card atribuído no quadro é editor automaticamente (regra em SQL).

const (
	boardRoleViewer  = 1
	boardRoleEditor  = 3
	boardRoleManager = 4
)

// boardRole devolve o papel efetivo do utilizador no quadro (0 = sem acesso). Admins da org são
// managers de todos os quadros do tenant — é o que dá gestão aos quadros legados (sem dono) e
// espelha a regra de apagar wikis.
func (s *Server) boardRole(ctx context.Context, mu db.MailUser, boardID uuid.UUID) int {
	role := 0
	if r, err := s.q.BoardRole(ctx, db.BoardRoleParams{
		UserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, BoardID: boardID, TenantID: mu.TenantID,
	}); err == nil {
		role = int(r)
	}
	if mu.Role == "owner" || mu.Role == "admin" || mu.Role == "internal_admin" {
		// Só eleva se o quadro existir no tenant (o rank>0 já o prova; senão confirma).
		if role > 0 {
			return boardRoleManager
		}
		if _, err := s.q.GetBoard(ctx, db.GetBoardParams{ID: boardID, TenantID: mu.TenantID}); err == nil {
			return boardRoleManager
		}
	}
	return role
}

// requireBoardRole escreve 404 (sem acesso — não revela existência) ou 403 (papel insuficiente).
func (s *Server) requireBoardRole(w http.ResponseWriter, r *http.Request, boardID uuid.UUID, need int) bool {
	mu := mailUserFrom(r.Context())
	role := s.boardRole(r.Context(), mu, boardID)
	if role <= 0 {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return false
	}
	if role < need {
		writeErr(w, http.StatusForbidden, "boards.forbidden")
		return false
	}
	return true
}

func (s *Server) handleBoardAccess(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"role": s.boardRole(r.Context(), mu, id)})
}

func (s *Server) handleListBoardGrants(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleManager) {
		return
	}
	grants, err := s.q.ListBoardGrants(r.Context(), db.ListBoardGrantsParams{TenantID: mu.TenantID, BoardID: id})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(grants))
}

func (s *Server) handleSetBoardGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleManager) {
		return
	}
	var req wikiGrantReq // mesmo shape: subject_type/subject_id/role
	if !decode(w, r, &req) {
		return
	}
	if (req.Role != "viewer" && req.Role != "editor" && req.Role != "manager") ||
		(req.SubjectType != "user" && req.SubjectType != "group" && req.SubjectType != "org") {
		writeErr(w, http.StatusBadRequest, "boards.invalid")
		return
	}
	sid := pgtype.UUID{}
	if req.SubjectType != "org" {
		parsed, perr := uuid.Parse(deref(req.SubjectID))
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "boards.invalid")
			return
		}
		sid = pgtype.UUID{Bytes: parsed, Valid: true}
	}
	// Upsert idempotente: um papel por sujeito.
	_ = s.q.DeleteBoardGrantSubject(r.Context(), db.DeleteBoardGrantSubjectParams{
		TenantID: mu.TenantID, BoardID: id, SubjectType: req.SubjectType, SubjectID: sid,
	})
	g, err := s.q.InsertBoardGrant(r.Context(), db.InsertBoardGrantParams{
		TenantID: mu.TenantID, BoardID: id, SubjectType: req.SubjectType, SubjectID: sid, Role: req.Role,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteBoardGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleManager) {
		return
	}
	gid, err := uuid.Parse(chi.URLParam(r, "grantId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if err := s.q.DeleteBoardGrant(r.Context(), db.DeleteBoardGrantParams{ID: gid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// requireCardEditor resolve o quadro do card e exige papel editor nele (mutações de cards).
func (s *Server) requireCardEditor(w http.ResponseWriter, r *http.Request, cardID uuid.UUID) bool {
	mu := mailUserFrom(r.Context())
	boardID, err := s.q.CardBoard(r.Context(), db.CardBoardParams{ID: cardID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return false
	}
	return s.requireBoardRole(w, r, boardID, boardRoleEditor)
}
