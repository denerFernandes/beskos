package httpapi

import (
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
	"mailin/backend/internal/provisioning"
	"mailin/backend/internal/stalwart"
)

type createDomainReq struct {
	Dominio string `json:"dominio"`
}

// handleCreateDomain registra um domínio no estado 'pending' e responde na hora.
// O worker (máquina de estados §7) avança o provisionamento de forma assíncrona.
func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req createDomainReq
	if !decode(w, r, &req) {
		return
	}
	req.Dominio = strings.ToLower(strings.TrimSpace(req.Dominio))
	if req.Dominio == "" {
		writeErr(w, http.StatusBadRequest, "domains.name_required")
		return
	}
	d, err := s.q.CreateDomain(r.Context(), db.CreateDomainParams{
		TenantID: u.TenantID, Dominio: req.Dominio,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "domains.already_exists")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Acao: "domain.create", Alvo: req.Dominio,
	})
	writeJSON(w, http.StatusCreated, d)
}

// handleGetDomain devolve o domínio (estado + registros DNS para a tela de dns_pending),
// checando posse do tenant antes (brief §2/§10).
func (s *Server) handleGetDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r)
	if !ok {
		return
	}
	zone := ""
	if d.DnsZone != nil {
		zone = *d.DnsZone
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": d.ID, "dominio": d.Dominio, "provision_state": d.ProvisionState,
		"dkim_status": d.DkimStatus, "dns_status": d.DnsStatus,
		"dns_records": orEmpty(provisioning.ParseZone(zone)), // nunca null: domínio novo ainda não tem zona
	})
}

type createMailboxReq struct {
	Login    string `json:"login"`
	Name     string `json:"name"`
	Password string `json:"password"`
	QuotaGB  int64  `json:"quota_gb"`
}

// handleCreateMailbox cria uma mailbox no domínio (só quando 'active'). Checa posse e
// traduz para a chamada JMAP no Stalwart (brief §2).
func (s *Server) handleCreateMailbox(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r)
	if !ok {
		return
	}
	if d.ProvisionState != provisioning.StateActive {
		writeErr(w, http.StatusConflict, "domains.not_active")
		return
	}
	// Cota: nº de mailboxes do tenant vs. limite do plano.
	plan := s.tenantPlan(r, d.TenantID)
	if count, _ := s.q.CountMailboxesByTenant(r.Context(), d.TenantID); int(count) >= plan.MaxMailboxes {
		writeErr(w, http.StatusPaymentRequired, "quota.mailboxes")
		return
	}
	var req createMailboxReq
	if !decode(w, r, &req) {
		return
	}
	if req.Login == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "mailbox.missing_fields")
		return
	}
	quota := req.QuotaGB
	if quota <= 0 {
		quota = 5
	}
	acctID, err := s.sw.CreateAccount(r.Context(), stalwart.AccountInput{
		Login: req.Login, DomainID: strval(d.StalwartID), DisplayName: req.Name,
		Password: req.Password, QuotaBytes: quota << 30,
	})
	if err != nil {
		log.Printf("criar caixa no Stalwart falhou: %v", err)
		writeErr(w, http.StatusBadGateway, "mailbox.stalwart_failed")
		return
	}
	email := req.Login + "@" + d.Dominio
	mu, err := s.q.CreateMailUser(r.Context(), db.CreateMailUserParams{
		TenantID: d.TenantID, Email: email, Name: req.Name, Role: "user",
		StalwartAccountID: &acctID, QuotaBytes: quota << 30,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "mailbox.already_exists")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: d.TenantID, Valid: true}, Actor: mailUserFrom(r.Context()).Email,
		Acao: "mailbox.create", Alvo: email,
	})
	s.syncSeats(d.TenantID) // seats na Stripe acompanham (rateio no período)
	writeJSON(w, http.StatusCreated, mu)
}

// handleDeleteDomain remove um domínio: recusa se ainda há caixas nele (apagar utilizadores
// primeiro — remoção nunca é em cascata sobre dados de pessoas), apaga no Stalwart, remove a
// identidade no SES (best-effort) e por fim a linha. Ordem pensada para retomada: se o Stalwart
// falhar, nada foi perdido; se a linha falhar, repetir a chamada é seguro (tudo idempotente).
func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r)
	if !ok {
		return
	}
	u := mailUserFrom(r.Context())
	if n, err := s.q.CountMailUsersOnDomain(r.Context(), db.CountMailUsersOnDomainParams{
		TenantID: d.TenantID, Dominio: &d.Dominio,
	}); err != nil || n > 0 {
		writeErr(w, http.StatusConflict, "domains.has_mailboxes")
		return
	}
	if id := strval(d.StalwartID); id != "" {
		if err := s.sw.DeleteDomain(r.Context(), id); err != nil {
			writeErr(w, http.StatusBadGateway, "domains.stalwart_failed")
			return
		}
	}
	// Identidade SES: best-effort (sem SES configurado, ou falha transitória, não bloqueia — a
	// identidade órfã pode ser removida à mão na AWS; fica no log).
	if s.sesRelay != nil {
		if err := s.sesRelay.DeleteIdentity(r.Context(), d.Dominio); err != nil {
			log.Printf("domains: ses delete identity %s: %v", d.Dominio, err)
		}
	}
	if err := s.q.DeleteDomainRow(r.Context(), db.DeleteDomainRowParams{ID: d.ID, TenantID: d.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "domains.delete_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: d.TenantID, Valid: true}, Actor: u.Email, Acao: "domain.delete", Alvo: d.Dominio,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ownedDomain carrega o domínio da rota e garante que pertence ao tenant do usuário.
func (s *Server) ownedDomain(w http.ResponseWriter, r *http.Request) (db.Domain, bool) {
	u := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "domains.invalid_id")
		return db.Domain{}, false
	}
	d, err := s.q.GetDomainByID(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "domains.not_found")
		return db.Domain{}, false
	}
	if d.TenantID != u.TenantID {
		// Não vaza existência de recurso de outro tenant.
		writeErr(w, http.StatusNotFound, "domains.not_found")
		return db.Domain{}, false
	}
	return d, true
}

func strval(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
