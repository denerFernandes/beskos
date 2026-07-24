package httpapi

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/auth"
	"mailin/backend/internal/db"
	"mailin/backend/internal/rbac"
	"mailin/backend/internal/stalwart"
)

// "Usuário" = caixa de email + conta do painel com um papel (RBAC). Users e Members são a mesma
// coisa; o que os diferencia é o papel. Criar um usuário provisiona a caixa E a conta juntas.

type createUserReq struct {
	DomainID string   `json:"domain_id"`
	Login    string   `json:"login"`
	Name     string   `json:"name"`
	Password string   `json:"password"`
	Role     string   `json:"role"`
	QuotaGB  int64    `json:"quota_gb"`
	Groups   []string `json:"groups"` // o usuário pode entrar em vários grupos de uma vez
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	actor := mailUserFrom(r.Context())
	var req createUserReq
	if !decode(w, r, &req) {
		return
	}
	role := rbac.Role(req.Role)
	if req.Login == "" || req.Password == "" || !rbac.IsAssignableRole(role) {
		writeErr(w, http.StatusBadRequest, "users.invalid")
		return
	}
	// Anti-escalonamento: só um owner cria outro owner.
	if role == rbac.RoleOwner && rbac.Role(actor.Role) != rbac.RoleOwner {
		writeErr(w, http.StatusForbidden, "invites.owner_only")
		return
	}
	domID, err := uuid.Parse(req.DomainID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "domains.invalid_id")
		return
	}
	d, err := s.q.GetDomainByID(r.Context(), domID)
	if err != nil || d.TenantID != actor.TenantID {
		writeErr(w, http.StatusNotFound, "domains.not_found")
		return
	}
	if d.ProvisionState != "active" {
		writeErr(w, http.StatusConflict, "domains.not_active")
		return
	}
	// Cota de caixas.
	plan := s.tenantPlan(r, d.TenantID)
	if count, _ := s.q.CountMailboxesByTenant(r.Context(), d.TenantID); int(count) >= plan.MaxMailboxes {
		writeErr(w, http.StatusPaymentRequired, "quota.mailboxes")
		return
	}
	quota := req.QuotaGB
	if quota <= 0 {
		quota = 5
	}
	email := req.Login + "@" + d.Dominio

	// Caixa no Stalwart + o usuário (identidade unificada: um só registro com caixa, senha e papel).
	acctID, err := s.sw.CreateAccount(r.Context(), stalwart.AccountInput{
		Login: req.Login, DomainID: strval(d.StalwartID), DisplayName: req.Name, Password: req.Password, QuotaBytes: quota << 30,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "mailbox.stalwart_failed")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
		return
	}
	pu, err := s.q.CreateMailUser(r.Context(), db.CreateMailUserParams{
		TenantID: d.TenantID, Email: email, Name: req.Name, Role: req.Role,
		StalwartAccountID: &acctID, QuotaBytes: quota << 30, PasswordHash: hash,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "mailbox.already_exists")
		return
	}
	// Grupos escolhidos na criação (entra também nos canais de chat correspondentes).
	if len(req.Groups) > 0 {
		s.syncUserGroups(r.Context(), d.TenantID, pu.ID, req.Groups)
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: actor.TenantID, Valid: true}, Actor: actor.Email, Acao: "user.create", Alvo: email})
	s.syncSeats(actor.TenantID) // seats na Stripe acompanham (rateio no período)
	writeJSON(w, http.StatusCreated, map[string]any{"id": pu.ID, "email": email, "role": req.Role})
}

type resetUserPwReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleResetUserPassword troca a senha do usuário nas DUAS pontas (caixa no Stalwart + painel).
func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	actor := mailUserFrom(r.Context())
	var req resetUserPwReq
	if !decode(w, r, &req) {
		return
	}
	if req.Password == "" {
		writeErr(w, http.StatusBadRequest, "mailbox.missing_fields")
		return
	}
	mu, err := s.q.GetMailUserByEmail(r.Context(), req.Email)
	if err != nil || mu.TenantID != actor.TenantID || mu.StalwartAccountID == nil {
		writeErr(w, http.StatusNotFound, "mailbox.not_found")
		return
	}
	// Anti-escalonamento (lei §5.2): o ator só reseta a senha de alguém de rank inferior. Sem isto,
	// um admin redefiniria a senha do owner e assumiria a conta + a caixa (via IMAP/JMAP, sem MFA).
	if mu.ID != actor.ID && !rbac.CanManage(rbac.Role(actor.Role), rbac.Role(mu.Role)) {
		writeErr(w, http.StatusForbidden, "rbac.forbidden")
		return
	}
	if err := s.sw.SetAccountPassword(r.Context(), *mu.StalwartAccountID, req.Password); err != nil {
		writeErr(w, http.StatusBadGateway, "mailbox.stalwart_failed")
		return
	}
	// Atualiza também a conta do painel (mesma identidade), se existir.
	if hash, err := auth.HashPassword(req.Password); err == nil {
		_ = s.q.UpdateUserPasswordByEmail(r.Context(), db.UpdateUserPasswordByEmailParams{
			Email: req.Email, TenantID: actor.TenantID, PasswordHash: hash,
		})
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: actor.TenantID, Valid: true}, Actor: actor.Email, Acao: "user.password_reset", Alvo: req.Email})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
