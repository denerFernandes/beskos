package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// CONTATOS: empresa ou pessoa, numa lista só. A pessoa pode pertencer a uma empresa (company_id).
//
// VER é regido pelo GRUPO: sou o dono, ou estou numa EQUIPA com que algum grupo do contato foi
// partilhado (a query decide — ver ListContacts). Uma pessoa herda da empresa dela.
// EDITAR/APAGAR é só do dono — ver ≠ mexer. Deixar NOTA é de quem vê: comentar não é editar.

type contactOut struct {
	db.ListContactsRow
	GroupIDs []uuid.UUID `json:"group_ids"`
}

func (s *Server) handleListContacts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListContacts(r.Context(), db.ListContactsParams{OwnerUserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.failed")
		return
	}
	byContact := map[uuid.UUID][]uuid.UUID{}
	if mems, err := s.q.ListContactMemberships(r.Context(), mu.ID); err == nil {
		for _, m := range mems {
			byContact[m.ContactID] = append(byContact[m.ContactID], m.GroupID)
		}
	}
	out := make([]contactOut, len(rows))
	for i, c := range rows {
		out[i] = contactOut{ListContactsRow: c, GroupIDs: orEmpty(byContact[c.ID])}
	}
	writeJSON(w, http.StatusOK, out)
}

type contactReq struct {
	Kind        string      `json:"kind"`
	Name        string      `json:"name"`
	Email       string      `json:"email"`
	Phone       string      `json:"phone"`
	Company     string      `json:"company"` // empresa em texto livre (quando não há contato-empresa)
	Domain      string      `json:"domain"`
	Notes       string      `json:"notes"`
	Tags        []string    `json:"tags"`
	GroupIDs    []uuid.UUID `json:"group_ids"`
	Title       string      `json:"title"`
	Mobile      string      `json:"mobile"`
	IsWhatsapp  bool        `json:"is_whatsapp"`
	AllowPhone  bool        `json:"allow_phone"`
	AllowEmail  bool        `json:"allow_email"`
	CompanyID   string      `json:"company_id"`
	ExtraEmails []string    `json:"extra_emails"` // e-mails secundários (o principal é Email)
	ExtraPhones []string    `json:"extra_phones"` // telefones secundários (o principal é Phone/Mobile)
	// Campos customizados livres (rótulo → valor), guardados como JSONB.
	CustomFields map[string]string `json:"custom_fields"`
}

// customJSON serializa os campos customizados (objeto vazio se nada).
func (r contactReq) customJSON() json.RawMessage {
	if len(r.CustomFields) == 0 {
		return json.RawMessage("{}")
	}
	b, err := json.Marshal(r.CustomFields)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// cleanList tira entradas vazias/só-espaço; nil vira []string{} (o array NOT NULL do banco).
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// kind normaliza: desconhecido vira 'person'.
func (r contactReq) kind() string {
	if r.Kind == "company" {
		return "company"
	}
	return "person"
}

func (r contactReq) tags() []string {
	if r.Tags == nil {
		return []string{}
	}
	return r.Tags
}

// companyID só vale para pessoa e só aceita um contato-empresa que o usuário VÊ, do mesmo tenant.
// Empresa dentro de empresa não existe (o CHECK do banco recusaria).
// companyID resolve a empresa da pessoa: primeiro a explícita (raw); se não houver, tenta o vínculo
// automático por domínio (uma empresa do dono com o mesmo domínio da pessoa). Zero rede/3º.
func (s *Server) companyID(ctx context.Context, mu db.MailUser, kind, raw, domain, email string) pgtype.UUID {
	if kind != "person" {
		return pgtype.UUID{}
	}
	if raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			c, err := s.q.GetContact(ctx, db.GetContactParams{ID: id, OwnerUserID: mu.ID, TenantID: mu.TenantID})
			if err == nil && c.Kind == "company" {
				return pgtype.UUID{Bytes: id, Valid: true}
			}
		}
	}
	// Vínculo automático por domínio: usa o domínio informado ou o do e-mail.
	d := strings.TrimSpace(domain)
	if d == "" {
		d = domainOf(email)
	}
	if d == "" {
		return pgtype.UUID{}
	}
	co, err := s.q.FindCompanyByDomain(ctx, db.FindCompanyByDomainParams{TenantID: mu.TenantID, OwnerUserID: mu.ID, Domain: d})
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: co.ID, Valid: true}
}

// domainOf extrai o domínio de um e-mail (parte depois do @), em minúsculas.
func domainOf(email string) string {
	i := strings.LastIndexByte(email, '@')
	if i < 0 || i == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[i+1:]))
}

// handleEnrichContacts (POST): liga pessoas sem empresa às empresas do dono pelo domínio. Idempotente.
func (s *Server) handleEnrichContacts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	n, err := s.q.LinkPeopleByDomain(r.Context(), db.LinkPeopleByDomainParams{TenantID: mu.TenantID, OwnerUserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.enrich_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"linked": n})
}

// syncGroups zera e regrava a associação N:N do contato com os grupos.
func (s *Server) syncGroups(ctx context.Context, contactID uuid.UUID, groupIDs []uuid.UUID) {
	_ = s.q.ClearContactGroups(ctx, contactID)
	for _, g := range groupIDs {
		_ = s.q.AddContactToGroup(ctx, db.AddContactToGroupParams{ContactID: contactID, GroupID: g})
	}
}

func (s *Server) handleCreateContact(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req contactReq
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "contacts.name_required")
		return
	}
	c, err := s.q.CreateContact(r.Context(), db.CreateContactParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID, Kind: req.kind(), Name: strings.TrimSpace(req.Name),
		Email: req.Email, Phone: req.Phone, Company: req.Company, Domain: req.Domain,
		Notes: req.Notes, Tags: req.tags(), Title: req.Title, Mobile: req.Mobile,
		IsWhatsapp: req.IsWhatsapp, AllowPhone: req.AllowPhone, AllowEmail: req.AllowEmail,
		CompanyID:   s.companyID(r.Context(), mu, req.kind(), req.CompanyID, req.Domain, req.Email),
		ExtraEmails: cleanList(req.ExtraEmails), ExtraPhones: cleanList(req.ExtraPhones),
		CustomFields: req.customJSON(),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.create_failed")
		return
	}
	s.syncGroups(r.Context(), c.ID, req.GroupIDs)
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateContact(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	var req contactReq
	if !decode(w, r, &req) {
		return
	}
	// UpdateContact filtra por owner_user_id: quem vê mas não é dono leva 404 — nunca edita.
	c, err := s.q.UpdateContact(r.Context(), db.UpdateContactParams{
		ID: id, OwnerUserID: mu.ID, Kind: req.kind(), Name: strings.TrimSpace(req.Name),
		Email: req.Email, Phone: req.Phone, Company: req.Company, Domain: req.Domain,
		Notes: req.Notes, Tags: req.tags(), Title: req.Title, Mobile: req.Mobile,
		IsWhatsapp: req.IsWhatsapp, AllowPhone: req.AllowPhone, AllowEmail: req.AllowEmail,
		CompanyID:   s.companyID(r.Context(), mu, req.kind(), req.CompanyID, req.Domain, req.Email),
		ExtraEmails: cleanList(req.ExtraEmails), ExtraPhones: cleanList(req.ExtraPhones),
		CustomFields: req.customJSON(),
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "contacts.not_owner")
		return
	}
	s.syncGroups(r.Context(), c.ID, req.GroupIDs)
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteContact(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	if err := s.q.DeleteContact(r.Context(), db.DeleteContactParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// contactOr404 carrega o contato respeitando a visibilidade por grupo.
func (s *Server) contactOr404(w http.ResponseWriter, r *http.Request) (db.GetContactRow, bool) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return db.GetContactRow{}, false
	}
	c, err := s.q.GetContact(r.Context(), db.GetContactParams{ID: id, OwnerUserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "contacts.not_found")
		return db.GetContactRow{}, false
	}
	return c, true
}

// handleGetContact: ficha do contato. Empresa vem com as pessoas dela; `is_owner` diz à UI se
// mostra o botão de editar (a API recusa de qualquer forma).
func (s *Server) handleGetContact(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.contactOr404(w, r)
	if !ok {
		return
	}
	groups, _ := s.q.ListContactGroupsOf(r.Context(), c.ID)
	out := map[string]any{
		"contact": c, "group_ids": orEmpty(groups),
		"people": []db.Contact{}, "is_owner": c.OwnerUserID == mu.ID,
	}
	if c.Kind == "company" {
		people, _ := s.q.ListCompanyPeople(r.Context(), db.ListCompanyPeopleParams{
			CompanyID: pgtype.UUID{Bytes: c.ID, Valid: true}, TenantID: mu.TenantID,
		})
		out["people"] = orEmpty(people)
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- Notas, tarefas e chamados do contato ----

func (s *Server) handleListCrmNotes(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.contactOr404(w, r)
	if !ok {
		return
	}
	notes, _ := s.q.ListContactNotes(r.Context(), db.ListContactNotesParams{ContactID: c.ID, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, orEmpty(notes))
}

func (s *Server) handleCreateCrmNote(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.contactOr404(w, r)
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "crm.note_empty")
		return
	}
	// Comentar não é editar: quem VÊ o contato pode deixar nota, mesmo não sendo o dono.
	n, err := s.q.CreateContactNote(r.Context(), db.CreateContactNoteParams{
		TenantID: mu.TenantID, ContactID: c.ID, AuthorID: mu.ID, Body: strings.TrimSpace(req.Body),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "crm.note_failed")
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleDeleteCrmNote(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "crm.invalid_id")
		return
	}
	// Só o autor apaga a própria nota.
	if err := s.q.DeleteCrmNote(r.Context(), db.DeleteCrmNoteParams{ID: id, AuthorID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "crm.note_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListContactTasks(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.contactOr404(w, r)
	if !ok {
		return
	}
	cards, _ := s.q.ListContactCards(r.Context(), db.ListContactCardsParams{
		ContactID: pgtype.UUID{Bytes: c.ID, Valid: true}, TenantID: mu.TenantID,
	})
	writeJSON(w, http.StatusOK, orEmpty(cards))
}

// ---- Grupos de contato e a partilha deles com EQUIPAS ----

// sharedRef é um alvo de partilha (equipa ou pessoa) no formato {id,name} que o picker consome.
type sharedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// handleSetGroupShares troca com QUEM o grupo é partilhado — equipas E/OU pessoas. Só o DONO do
// grupo decide (senão qualquer um abriria a agenda alheia para si ou para a sua equipa).
func (s *Server) handleSetGroupShares(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	var req struct {
		UserGroupIDs []string `json:"user_group_ids"` // equipas
		UserIDs      []string `json:"user_ids"`       // pessoas
	}
	if !decode(w, r, &req) {
		return
	}
	g, err := s.q.GetContactGroupOwned(r.Context(), db.GetContactGroupOwnedParams{ID: id, OwnerUserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "contacts.not_owner")
		return
	}
	// Equipas: substitui o conjunto pelo enviado (só as do próprio tenant).
	_ = s.q.ClearGroupShares(r.Context(), g.ID)
	for _, raw := range req.UserGroupIDs {
		ugid, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		if _, err := s.q.GetUserGroup(r.Context(), db.GetUserGroupParams{ID: ugid, TenantID: mu.TenantID}); err != nil {
			continue
		}
		_ = s.q.SetGroupShare(r.Context(), db.SetGroupShareParams{GroupID: g.ID, UserGroupID: ugid})
	}
	// Pessoas: idem (só colegas do próprio tenant; ignora partilhar consigo mesmo).
	_ = s.q.ClearGroupUserShares(r.Context(), g.ID)
	for _, raw := range req.UserIDs {
		uid, err := uuid.Parse(raw)
		if err != nil || uid == mu.ID {
			continue
		}
		if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: uid, TenantID: mu.TenantID}); err != nil {
			continue
		}
		_ = s.q.SetGroupUserShare(r.Context(), db.SetGroupUserShareParams{GroupID: g.ID, UserID: uid})
	}
	s.writeGroupShares(w, r, g.ID)
}

func (s *Server) handleListGroupShares(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	// Checa posse do grupo antes de listar (sem isto, um id de outro tenant devolvia os alvos da
	// partilha — auditoria M).
	if _, err := s.q.GetContactGroupOwned(r.Context(), db.GetContactGroupOwnedParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusNotFound, "contacts.not_owner")
		return
	}
	s.writeGroupShares(w, r, id)
}

// writeGroupShares devolve com quem o grupo está partilhado: { teams: [...], users: [...] }.
func (s *Server) writeGroupShares(w http.ResponseWriter, r *http.Request, groupID uuid.UUID) {
	teams, _ := s.q.ListGroupShares(r.Context(), groupID)
	users, _ := s.q.ListGroupUserShares(r.Context(), groupID)
	tOut := make([]sharedRef, 0, len(teams))
	for _, t := range teams {
		tOut = append(tOut, sharedRef{ID: t.UserGroupID.String(), Name: t.Name})
	}
	uOut := make([]sharedRef, 0, len(users))
	for _, u := range users {
		uOut = append(uOut, sharedRef{ID: u.UserID.String(), Name: u.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": tOut, "users": uOut})
}

// handleShareTeams lista as equipas do tenant (id+nome) para o PICKER de partilha. NÃO exige
// permissão de admin (ao contrário de /groups): escolher com quem partilho o meu grupo é meu.
func (s *Server) handleShareTeams(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, _ := s.q.ListTeamsForShare(r.Context(), mu.TenantID)
	out := make([]sharedRef, 0, len(rows))
	for _, t := range rows {
		out = append(out, sharedRef{ID: t.ID.String(), Name: t.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListContactGroups(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	groups, err := s.q.ListContactGroups(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(groups))
}

// handleContactGroupEmails devolve os e-mails dos contatos do grupo (lista de distribuição). Verifica
// posse do grupo antes; a query já limita aos contatos do dono.
func (s *Server) handleContactGroupEmails(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	if _, err := s.q.GetContactGroupOwned(r.Context(), db.GetContactGroupOwnedParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusNotFound, "contacts.group_not_found")
		return
	}
	rows, err := s.q.ListContactGroupEmails(r.Context(), db.ListContactGroupEmailsParams{GroupID: id, OwnerUserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

type groupReq struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Server) handleCreateContactGroup(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req groupReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "contacts.group_name_required")
		return
	}
	if req.Color == "" {
		req.Color = "#116466"
	}
	g, err := s.q.CreateContactGroup(r.Context(), db.CreateContactGroupParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID, Name: req.Name, Color: req.Color,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.group_failed")
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) handleUpdateContactGroup(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	var req groupReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "contacts.group_name_required")
		return
	}
	if req.Color == "" {
		req.Color = "#116466"
	}
	g, err := s.q.UpdateContactGroup(r.Context(), db.UpdateContactGroupParams{ID: id, OwnerUserID: mu.ID, Name: req.Name, Color: req.Color})
	if err != nil {
		writeErr(w, http.StatusNotFound, "contacts.not_found")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteContactGroup(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	if err := s.q.DeleteContactGroup(r.Context(), db.DeleteContactGroupParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.group_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleListColleagues devolve os colegas do tenant para os seletores de responsável. Aberto a
// qualquer usuário autenticado de propósito: atribuir trabalho é do dia a dia, e a alternativa
// (dar members.view a todos) entregaria papel e estado de MFA junto.
func (s *Server) handleListColleagues(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListColleagues(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "colleagues.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}
