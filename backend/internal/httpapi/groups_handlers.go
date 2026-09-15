package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Grupos de utilizadores. Cada grupo tem um canal privado no chat criado junto — e a associação
// grupo↔canal é mantida em sincronia: entrar no grupo entra no canal, sair sai.
// Gestão exige members.manage (RBAC); listar exige members.view.

type groupDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ChannelID string `json:"channel_id"`
	Members   int64  `json:"members"`
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListUserGroups(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "groups.failed")
		return
	}
	out := make([]groupDTO, 0, len(rows))
	for _, g := range rows {
		out = append(out, groupDTO{ID: g.ID.String(), Name: g.Name, ChannelID: uuidStr(g.ChannelID), Members: g.Members})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateGroup cria o grupo e o canal privado do chat correspondente.
func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Name    string   `json:"name"`
		Members []string `json:"members"` // ids de usuário (opcional)
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "groups.invalid")
		return
	}
	g, err := s.q.CreateUserGroup(r.Context(), db.CreateUserGroupParams{TenantID: mu.TenantID, Name: name, CreatedBy: mu.ID})
	if err != nil {
		writeErr(w, http.StatusConflict, "groups.name_taken")
		return
	}
	// Canal privado do grupo. Se falhar, o grupo continua válido (só sem canal) — não perdemos
	// o grupo por causa do chat.
	ch, cerr := s.q.CreateChannel(r.Context(), db.CreateChannelParams{
		TenantID: mu.TenantID, Kind: "private", Name: name, Topic: "", DmKey: "", CreatedBy: mu.ID,
	})
	if cerr == nil {
		_ = s.q.SetUserGroupChannel(r.Context(), db.SetUserGroupChannelParams{ID: g.ID, ChannelID: pgUUID(ch.ID)})
		g.ChannelID = pgUUID(ch.ID)
	}
	// O criador entra no grupo; os demais, se vieram no request.
	s.addMember(r.Context(), g, mu.TenantID, mu.ID)
	for _, id := range req.Members {
		if uid, perr := uuid.Parse(id); perr == nil {
			s.addMember(r.Context(), g, mu.TenantID, uid)
		}
	}
	writeJSON(w, http.StatusCreated, groupDTO{ID: g.ID.String(), Name: g.Name, ChannelID: uuidStr(g.ChannelID)})
}

// addMember põe o usuário no grupo E no canal do grupo (as duas coisas andam juntas).
func (s *Server) addMember(ctx context.Context, g db.UserGroup, tenantID, userID uuid.UUID) {
	_ = s.q.AddUserToGroup(ctx, db.AddUserToGroupParams{GroupID: g.ID, UserID: userID, TenantID: tenantID})
	if g.ChannelID.Valid {
		_ = s.q.AddChannelMember(ctx, db.AddChannelMemberParams{
			ChannelID: uuid.UUID(g.ChannelID.Bytes), TenantID: tenantID, UserID: userID, Role: "member",
		})
	}
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "groups.invalid")
		return
	}
	g, err := s.q.UpdateUserGroup(r.Context(), db.UpdateUserGroupParams{ID: id, TenantID: mu.TenantID, Name: name})
	if err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	// O canal segue o nome do grupo.
	if g.ChannelID.Valid {
		_, _ = s.q.UpdateChannelName(r.Context(), db.UpdateChannelNameParams{ID: uuid.UUID(g.ChannelID.Bytes), TenantID: mu.TenantID, Name: name})
	}
	writeJSON(w, http.StatusOK, groupDTO{ID: g.ID.String(), Name: g.Name, ChannelID: uuidStr(g.ChannelID)})
}

// handleDeleteGroup apaga o grupo. O canal do chat PERMANECE (com o histórico) — apagar conversa
// junto seria perda de dado silenciosa.
func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	if err := s.q.DeleteUserGroup(r.Context(), db.DeleteUserGroupParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "groups.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	if _, err := s.q.GetUserGroup(r.Context(), db.GetUserGroupParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	members, _ := s.q.ListGroupMembers(r.Context(), id)
	writeJSON(w, http.StatusOK, orEmpty(members))
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	uid, err := uuid.Parse(req.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid")
		return
	}
	g, err := s.q.GetUserGroup(r.Context(), db.GetUserGroupParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	// O alvo precisa ser do MESMO tenant (nunca confiar no id vindo do request).
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: uid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "members.not_found")
		return
	}
	s.addMember(r.Context(), g, mu.TenantID, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

// handleUserGroups lista os grupos de UM usuário (o mesmo usuário pode estar em vários).
func (s *Server) handleUserGroups(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "members.invalid_id")
		return
	}
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "members.not_found")
		return
	}
	groups, _ := s.q.ListGroupsForUser(r.Context(), id)
	writeJSON(w, http.StatusOK, orEmpty(groups))
}

// handleSetUserGroups define de uma vez em QUAIS grupos o usuário está (o front manda a lista
// completa). Calcula a diferença e sincroniza também os canais do chat.
func (s *Server) handleSetUserGroups(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "members.invalid_id")
		return
	}
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "members.not_found")
		return
	}
	var req struct {
		GroupIDs []string `json:"group_ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.syncUserGroups(r.Context(), mu.TenantID, id, req.GroupIDs)
	groups, _ := s.q.ListGroupsForUser(r.Context(), id)
	writeJSON(w, http.StatusOK, orEmpty(groups))
}

// syncUserGroups põe o usuário exatamente nos grupos pedidos: entra nos que faltam, sai dos que
// sobram — e o canal de cada grupo acompanha. Grupos de outro tenant são ignorados.
func (s *Server) syncUserGroups(ctx context.Context, tenantID, userID uuid.UUID, want []string) {
	target := map[uuid.UUID]bool{}
	for _, raw := range want {
		gid, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		g, err := s.q.GetUserGroup(ctx, db.GetUserGroupParams{ID: gid, TenantID: tenantID})
		if err != nil { // não é do tenant → ignora (nunca confiar no id do request)
			continue
		}
		target[gid] = true
		s.addMember(ctx, g, tenantID, userID)
	}
	current, _ := s.q.ListGroupsForUser(ctx, userID)
	for _, c := range current {
		if target[c.ID] {
			continue
		}
		g, err := s.q.GetUserGroup(ctx, db.GetUserGroupParams{ID: c.ID, TenantID: tenantID})
		if err != nil {
			continue
		}
		_ = s.q.RemoveUserFromGroup(ctx, db.RemoveUserFromGroupParams{GroupID: c.ID, UserID: userID})
		if g.ChannelID.Valid {
			_ = s.q.RemoveChannelMember(ctx, db.RemoveChannelMemberParams{ChannelID: uuid.UUID(g.ChannelID.Bytes), UserID: userID})
		}
	}
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	uid, uerr := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil || uerr != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	g, err := s.q.GetUserGroup(r.Context(), db.GetUserGroupParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	_ = s.q.RemoveUserFromGroup(r.Context(), db.RemoveUserFromGroupParams{GroupID: id, UserID: uid})
	if g.ChannelID.Valid {
		_ = s.q.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{ChannelID: uuid.UUID(g.ChannelID.Bytes), UserID: uid})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
