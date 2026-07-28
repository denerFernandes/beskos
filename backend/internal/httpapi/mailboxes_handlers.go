package httpapi

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
	"mailin/backend/internal/rbac"
)

// Gestão de caixas pelo painel: listar e trocar a senha (admin). A senha não é guardada por nós;
// só é aplicada no Stalwart (brief §2). A leitura de e-mail segue JMAP direto.

func (s *Server) handleListMailboxes(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	list, err := s.q.ListMailUsersByTenant(r.Context(), u.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mailbox.list_failed")
		return
	}
	// Devolve só o necessário (nunca há senha aqui).
	out := make([]map[string]any, len(list))
	for i, m := range list {
		out[i] = map[string]any{"id": m.ID, "email": m.Email, "name": m.Name, "role": m.Role, "created_at": m.CreatedAt}
	}
	writeJSON(w, http.StatusOK, out)
}

type resetPasswordReq struct {
	Password string `json:"password"`
}

// handleResetMailboxPassword troca a senha de uma caixa no Stalwart (checa posse do tenant).
func (s *Server) handleResetMailboxPassword(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "mailbox.invalid_id")
		return
	}
	mu, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: u.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "mailbox.not_found")
		return
	}
	// Anti-escalonamento (lei §5.2): não deixar um papel resetar a senha de alguém de rank ≥ o seu.
	if mu.ID != u.ID && !rbac.CanManage(rbac.Role(u.Role), rbac.Role(mu.Role)) {
		writeErr(w, http.StatusForbidden, "rbac.forbidden")
		return
	}
	var req resetPasswordReq
	if !decode(w, r, &req) {
		return
	}
	if req.Password == "" {
		writeErr(w, http.StatusBadRequest, "mailbox.missing_fields")
		return
	}
	if mu.StalwartAccountID == nil {
		writeErr(w, http.StatusConflict, "mailbox.not_provisioned")
		return
	}
	if err := s.sw.SetAccountPassword(r.Context(), *mu.StalwartAccountID, req.Password); err != nil {
		log.Printf("criar caixa no Stalwart falhou: %v", err)
		writeErr(w, http.StatusBadGateway, "mailbox.stalwart_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email,
		Acao: "mailbox.password_reset", Alvo: mu.Email,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
