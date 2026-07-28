package httpapi

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
	"mailin/backend/internal/stalwart"
)

// Aliases de e-mail: um endereço extra que entrega na caixa de outra pessoa (contacto@empresa.com →
// caixa da Ana). O alias NÃO é um membro da organização — não entra no chat, nem nos grupos, nem na
// lista de pessoas. É só correio, e por isso vive em tabela própria em vez de numa linha de
// mail_users: a separação fica na estrutura, não num filtro que uma query nova podia esquecer.
//
// No Stalwart, aliases são um campo da conta do dono (x:Account, DIVERGENCES §D4). A nossa tabela é
// a fonte de verdade da lista: cada alteração reescreve o conjunto todo na conta, o que torna a
// operação idempotente — e faz da reconciliação um simples "voltar a gravar".

// syncAliasesToStalwart reescreve no Stalwart o conjunto de aliases de uma caixa a partir da nossa
// base. Chamar depois de criar ou apagar.
func (s *Server) syncAliasesToStalwart(ctx context.Context, tenant uuid.UUID, mu db.MailUser) error {
	if s.sw == nil || mu.StalwartAccountID == nil {
		return nil // sem servidor de correio ligado (dev) ou caixa por provisionar
	}
	rows, err := s.q.ListAliasesForStalwart(ctx, db.ListAliasesForStalwartParams{TenantID: tenant, UserID: mu.ID})
	if err != nil {
		return err
	}
	aliases := make([]stalwart.Alias, 0, len(rows))
	for _, a := range rows {
		if a.StalwartDomainID == "" {
			continue // domínio ainda por provisionar lá: não há a que prender o alias
		}
		aliases = append(aliases, stalwart.Alias{Local: a.Local, DomainID: a.StalwartDomainID})
	}
	return s.sw.SetAccountAliases(ctx, *mu.StalwartAccountID, aliases)
}

// handleListAliases lista os aliases do tenant (painel).
func (s *Server) handleListAliases(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListEmailAliases(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "aliases.failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		out = append(out, map[string]any{
			"id": a.ID, "address": a.Address, "domain": a.Domain,
			"owner_id": a.MailUserID, "owner_email": a.OwnerEmail, "owner_name": a.OwnerName,
			"created_at": iso(a.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"aliases": out})
}

type createAliasReq struct {
	Local    string `json:"local"`     // parte antes do @
	DomainID string `json:"domain_id"` // domínio do tenant
	OwnerID  string `json:"owner_id"`  // caixa que recebe
}

// handleCreateAlias cria o alias e provisiona-o no Stalwart.
func (s *Server) handleCreateAlias(w http.ResponseWriter, r *http.Request) {
	actor := mailUserFrom(r.Context())
	var req createAliasReq
	if !decode(w, r, &req) {
		return
	}
	local := strings.ToLower(strings.TrimSpace(req.Local))
	if local == "" || strings.ContainsAny(local, "@ ") {
		writeErr(w, http.StatusBadRequest, "aliases.invalid_local")
		return
	}
	domID, err := uuid.Parse(req.DomainID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "aliases.invalid_domain")
		return
	}
	dom, err := s.q.GetDomainByID(r.Context(), domID)
	// Posse do tenant é verificada aqui e não só na query: o GetDomainByID é por id apenas.
	if err != nil || dom.TenantID != actor.TenantID {
		writeErr(w, http.StatusNotFound, "aliases.invalid_domain")
		return
	}
	ownerID, err := uuid.Parse(req.OwnerID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "aliases.invalid_owner")
		return
	}
	owner, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: ownerID, TenantID: actor.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "aliases.invalid_owner")
		return
	}
	address := local + "@" + dom.Dominio
	// Um alias não pode roubar o endereço de uma caixa existente: o correio deixaria de chegar a quem
	// de direito, e o Stalwart aceitaria a criação sem dizer nada.
	if _, err := s.q.GetMailUserByEmail(r.Context(), address); err == nil {
		writeErr(w, http.StatusConflict, "aliases.address_taken")
		return
	}
	alias, err := s.q.CreateEmailAlias(r.Context(), db.CreateEmailAliasParams{
		TenantID: actor.TenantID, DomainID: domID, MailUserID: owner.ID, Address: address,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "aliases.address_taken")
		return
	}
	if err := s.syncAliasesToStalwart(r.Context(), actor.TenantID, owner); err != nil {
		// O motivo TEM de ficar no log: sem ele, "provision_failed" é um beco sem saída em produção
		// — não se sabe se o Stalwart recusou o formato, o endereço ou a conta.
		log.Printf("alias %s para %s: provisionamento no Stalwart falhou: %v", address, owner.Email, err)
		// Não deixa lixo: se o servidor de correio recusou, o alias não fica a existir só para nós.
		_ = s.q.DeleteEmailAlias(r.Context(), db.DeleteEmailAliasParams{ID: alias.ID, TenantID: actor.TenantID})
		writeErr(w, http.StatusBadGateway, "aliases.provision_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgUUID(actor.TenantID), Actor: actor.Email, Acao: "alias.create", Alvo: address,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": alias.ID, "address": alias.Address, "owner_id": owner.ID, "owner_email": owner.Email,
	})
}

// handleDeleteAlias remove o alias e volta a gravar a lista no Stalwart.
func (s *Server) handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	actor := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "aliases.not_found")
		return
	}
	alias, err := s.q.GetEmailAlias(r.Context(), db.GetEmailAliasParams{ID: id, TenantID: actor.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "aliases.not_found")
		return
	}
	if err := s.q.DeleteEmailAlias(r.Context(), db.DeleteEmailAliasParams{ID: id, TenantID: actor.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "aliases.failed")
		return
	}
	if owner, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: alias.MailUserID, TenantID: actor.TenantID}); err == nil {
		_ = s.syncAliasesToStalwart(r.Context(), actor.TenantID, owner)
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgUUID(actor.TenantID), Actor: actor.Email, Acao: "alias.delete", Alvo: alias.Address,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleMyAddresses devolve os endereços de quem está autenticado: o próprio e os aliases que
// entregam na sua caixa. É o que o compositor mostra no "De:".
func (s *Server) handleMyAddresses(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	aliases, _ := s.q.ListAliasesForUser(r.Context(), db.ListAliasesForUserParams{TenantID: mu.TenantID, UserID: mu.ID})
	writeJSON(w, http.StatusOK, map[string]any{
		"primary": mu.Email,
		"aliases": orEmpty(aliases),
	})
}
