package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	s.ensureGeneral(r.Context(), mu.TenantID, mu.ID)
	mine, err := s.q.ListChannelsForUser(r.Context(), db.ListChannelsForUserParams{UserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	// Nome de exibição da DM = o outro participante (com id para presença).
	out := make([]map[string]any, 0, len(mine))
	for _, c := range mine {
		name, otherID := c.Name, ""
		if c.Kind == "dm" {
			name, otherID = s.dmOther(r.Context(), c.ID, mu.ID)
		}
		out = append(out, map[string]any{
			"id": c.ID, "kind": c.Kind, "name": name, "topic": c.Topic, "other_id": otherID,
			// Só marcamos como "o canal por defeito" enquanto o nome for o de origem: renomeado,
			// o utilizador escolheu um nome e é esse que a UI deve mostrar (sem tradução).
			"is_general": c.IsGeneral && c.Name == generalChannelName,
			"unread":     c.Unread, "last_read_at": iso(c.LastReadAt),
			"listed": c.Listed, "add_policy": c.AddPolicy, "my_role": c.Role, "notif_pref": c.NotifPref,
		})
	}
	pub, _ := s.q.ListPublicChannels(r.Context(), db.ListPublicChannelsParams{TenantID: mu.TenantID, UserID: mu.ID})
	discover := make([]map[string]any, 0, len(pub))
	for _, c := range pub {
		discover = append(discover, map[string]any{"id": c.ID, "kind": c.Kind, "name": c.Name, "topic": c.Topic})
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out, "discover": discover})
}

type openGroupDMReq struct {
	Emails []string `json:"emails"`
}

// handleOpenGroupDM abre (ou reutiliza) uma DM de GRUPO (3+ pessoas, incluindo quem abre).
func (s *Server) handleOpenGroupDM(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req openGroupDMReq
	if !decode(w, r, &req) {
		return
	}
	idset := map[uuid.UUID]bool{mu.ID: true}
	for _, e := range req.Emails {
		u, err := s.q.GetMailUserByEmail(r.Context(), strings.TrimSpace(e))
		if err == nil && u.TenantID == mu.TenantID {
			idset[u.ID] = true
		}
	}
	if len(idset) < 3 {
		writeErr(w, http.StatusBadRequest, "chat.group_min") // 2 pessoas = DM normal (/chat/dm)
		return
	}
	ids := make([]uuid.UUID, 0, len(idset))
	for id := range idset {
		ids = append(ids, id)
	}
	key := dmKeyN(ids)
	ch, err := s.q.GetDMByKey(r.Context(), db.GetDMByKeyParams{TenantID: mu.TenantID, DmKey: key})
	if err != nil {
		ch, err = s.q.CreateChannel(r.Context(), db.CreateChannelParams{
			TenantID: mu.TenantID, Kind: "dm", Name: "", Topic: "", DmKey: key, CreatedBy: mu.ID,
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.create_failed")
			return
		}
		for id := range idset {
			_ = s.q.AddChannelMember(r.Context(), db.AddChannelMemberParams{ChannelID: ch.ID, TenantID: mu.TenantID, UserID: id, Role: "member"})
		}
	}
	name, _ := s.dmOther(r.Context(), ch.ID, mu.ID)
	writeJSON(w, http.StatusOK, map[string]any{"id": ch.ID, "kind": "dm", "name": name, "topic": ""})
}

type createChannelReq struct {
	Name      string `json:"name"`
	Topic     string `json:"topic"`
	Kind      string `json:"kind"`       // public | private
	Listed    *bool  `json:"listed"`     // false = não listado (só por convite); default true
	AddPolicy string `json:"add_policy"` // open | restricted; default open
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req createChannelReq
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	kind := "public"
	if req.Kind == "private" {
		kind = "private"
	}
	ch, err := s.q.CreateChannel(r.Context(), db.CreateChannelParams{
		TenantID: mu.TenantID, Kind: kind, Name: name, Topic: req.Topic, DmKey: "", CreatedBy: mu.ID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.create_failed")
		return
	}
	// Acesso: listado (default) e política de adição. Um canal privado é sempre não listado.
	listed := kind == "public" && (req.Listed == nil || *req.Listed)
	policy := "open"
	if req.AddPolicy == "restricted" {
		policy = "restricted"
	}
	_ = s.q.UpdateChannelAccess(r.Context(), db.UpdateChannelAccessParams{ID: ch.ID, TenantID: mu.TenantID, Listed: listed, AddPolicy: policy})
	_ = s.q.AddChannelMember(r.Context(), db.AddChannelMemberParams{ChannelID: ch.ID, TenantID: mu.TenantID, UserID: mu.ID, Role: "owner"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": ch.ID, "kind": ch.Kind, "name": ch.Name, "topic": ch.Topic})
}

func (s *Server) handleJoinChannel(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID})
	// Auto-entrada só em canais públicos E listados. Privados e não listados são só por convite.
	if err != nil || ch.Kind != "public" || !ch.Listed {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if err := s.q.AddChannelMember(r.Context(), db.AddChannelMemberParams{ChannelID: id, TenantID: mu.TenantID, UserID: mu.ID, Role: "member"}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "members", id, s.channelRecipients(r.Context(), id), map[string]any{"channel_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "joined"})
}

// handleAddMember adiciona um utilizador (do mesmo tenant) ao canal. Se a política é "restricted",
// só gestores adicionam; se "open", qualquer membro adiciona.
func (s *Server) handleChatAddMember(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID})
	if err != nil || ch.Kind == "dm" {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	// Autorização conforme a política do canal.
	if ch.AddPolicy == "restricted" {
		if _, ok := s.managerOr403(w, r, id); !ok {
			return
		}
	} else if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	target, err := s.q.GetMailUserByEmail(r.Context(), req.Email)
	if err != nil || target.TenantID != mu.TenantID {
		writeErr(w, http.StatusBadRequest, "chat.user_not_found")
		return
	}
	if err := s.q.AddChannelMember(r.Context(), db.AddChannelMemberParams{ChannelID: id, TenantID: mu.TenantID, UserID: target.ID, Role: "member"}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "members", id, s.channelRecipients(r.Context(), id), map[string]any{"channel_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

// handleRemoveMember remove um membro. Gestores removem qualquer um; um membro pode sair sozinho.
// O dono (created_by) não pode ser removido.
func (s *Server) handleChatRemoveMember(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	target, err2 := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if ch.CreatedBy == target {
		writeErr(w, http.StatusBadRequest, "chat.cannot_remove_owner")
		return
	}
	if target == mu.ID { // sair do canal
		if _, ok := s.memberOr403(w, r, id); !ok {
			return
		}
	} else if _, ok := s.managerOr403(w, r, id); !ok {
		return
	}
	if err := s.q.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{ChannelID: id, UserID: target}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "members", id, s.channelRecipients(r.Context(), id), map[string]any{"channel_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleSetMemberRole promove/despromove um membro a gestor (só gestores). O dono não muda.
func (s *Server) handleChatSetMemberRole(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	target, err2 := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.managerOr403(w, r, id); !ok {
		return
	}
	ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID})
	if err != nil || ch.CreatedBy == target {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	role := "member"
	if req.Role == "manager" {
		role = "manager"
	}
	if err := s.q.UpdateMemberRole(r.Context(), db.UpdateMemberRoleParams{ChannelID: id, UserID: target, Role: role}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "members", id, s.channelRecipients(r.Context(), id), map[string]any{"channel_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleUpdateChannel altera nome/tópico/visibilidade/política (só gestores).
func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.managerOr403(w, r, id); !ok {
		return
	}
	ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID})
	if err != nil || ch.Kind == "dm" {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	var req struct {
		Name      string `json:"name"`
		Topic     string `json:"topic"`
		Listed    *bool  `json:"listed"`
		AddPolicy string `json:"add_policy"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = ch.Name
	}
	listed := ch.Kind == "public" && (req.Listed == nil || *req.Listed)
	policy := ch.AddPolicy
	if req.AddPolicy == "open" || req.AddPolicy == "restricted" {
		policy = req.AddPolicy
	}
	if err := s.q.UpdateChannelSettings(r.Context(), db.UpdateChannelSettingsParams{
		ID: id, TenantID: mu.TenantID, Name: name, Topic: req.Topic, Listed: listed, AddPolicy: policy,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "channel", id, s.channelRecipients(r.Context(), id), map[string]any{"channel_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type openDMReq struct {
	Email string `json:"email"`
}

// handleOpenDM cria (ou reabre) a conversa direta com outro usuário do tenant.
func (s *Server) handleOpenDM(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req openDMReq
	if !decode(w, r, &req) {
		return
	}
	other, err := s.q.GetMailUserByEmail(r.Context(), strings.TrimSpace(req.Email))
	if err != nil || other.TenantID != mu.TenantID {
		writeErr(w, http.StatusNotFound, "chat.user_not_found")
		return
	}
	key := dmKey(mu.ID, other.ID)
	ch, err := s.q.GetDMByKey(r.Context(), db.GetDMByKeyParams{TenantID: mu.TenantID, DmKey: key})
	if err != nil {
		ch, err = s.q.CreateChannel(r.Context(), db.CreateChannelParams{
			TenantID: mu.TenantID, Kind: "dm", Name: "", Topic: "", DmKey: key, CreatedBy: mu.ID,
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.create_failed")
			return
		}
		_ = s.q.AddChannelMember(r.Context(), db.AddChannelMemberParams{ChannelID: ch.ID, TenantID: mu.TenantID, UserID: mu.ID, Role: "member"})
		_ = s.q.AddChannelMember(r.Context(), db.AddChannelMemberParams{ChannelID: ch.ID, TenantID: mu.TenantID, UserID: other.ID, Role: "member"})
	}
	name := other.Name
	if name == "" {
		name = other.Email
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": ch.ID, "kind": "dm", "name": name, "topic": ""})
}

func (s *Server) handleListChannelMembers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	members, _ := s.q.ListChannelMembers(r.Context(), id)
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		out = append(out, map[string]any{"user_id": m.UserID, "email": m.Email, "name": m.Name, "role": m.Role, "status_emoji": m.StatusEmoji, "status_text": m.StatusText, "last_read_at": iso(m.LastReadAt)})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSearchChannelMembers: membros com busca (nome/e-mail) e paginação — para canais grandes.
func (s *Server) handleSearchChannelMembers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	q := r.URL.Query().Get("q")
	const size = 20
	page := 1
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 1 {
		page = p
	}
	rows, err := s.q.SearchChannelMembers(r.Context(), db.SearchChannelMembersParams{
		ChannelID: id, Column2: q, Limit: size, Offset: int32((page - 1) * size),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	total, _ := s.q.CountChannelMembers(r.Context(), db.CountChannelMembersParams{ChannelID: id, Column2: q})
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{"user_id": m.UserID, "email": m.Email, "name": m.Name, "role": m.Role, "status_emoji": m.StatusEmoji, "status_text": m.StatusText})
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out, "total": total, "page": page})
}

// handleArchiveChannel arquiva/restaura um canal. Arquivado sai das listas e ninguém escreve nele —
// mas o histórico fica e dá para restaurar, ao contrário de apagar.
func (s *Server) handleArchiveChannel(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID})
	if err != nil || ch.Kind == "dm" {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if !s.canArchiveChannel(r, id) {
		writeErr(w, http.StatusForbidden, "chat.not_owner")
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Mesma razão do apagar: o canal por defeito voltaria na listagem seguinte (ensureGeneral).
	if req.Archived && ch.IsGeneral {
		writeErr(w, http.StatusBadRequest, "chat.cannot_archive_general")
		return
	}
	out, err := s.q.SetChannelArchived(r.Context(), db.SetChannelArchivedParams{ID: id, TenantID: mu.TenantID, Archived: req.Archived})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": out.ID, "archived": out.ArchivedAt.Valid})
}

// handleListArchivedChannels lista os canais arquivados que a pessoa pode restaurar.
func (s *Server) handleListArchivedChannels(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	isAdmin := mu.Role == "owner" || mu.Role == "admin" || mu.Role == "internal_admin"
	list, err := s.q.ListArchivedChannels(r.Context(), db.ListArchivedChannelsParams{
		TenantID: mu.TenantID, IsAdmin: isAdmin, UserID: mu.ID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, c := range list {
		out = append(out, map[string]any{
			"id": c.ID, "kind": c.Kind, "name": c.Name, "topic": c.Topic,
			"is_general":  c.IsGeneral && c.Name == generalChannelName,
			"archived_at": iso(c.ArchivedAt.Time),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	m, ok := s.managerOr403(w, r, id)
	if !ok {
		return
	}
	if m.Role != "owner" {
		writeErr(w, http.StatusForbidden, "chat.not_owner")
		return
	}
	// O canal por defeito não se apaga: seria recriado na listagem seguinte e o dono ficaria a
	// olhar para um canal que "voltou". Dizemos porquê em vez de deixar acontecer.
	if ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID}); err == nil && ch.IsGeneral {
		writeErr(w, http.StatusBadRequest, "chat.cannot_delete_general")
		return
	}
	recipients := s.channelRecipients(r.Context(), id)
	if err := s.q.DeleteChannel(r.Context(), db.DeleteChannelParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	s.publish(r.Context(), "channel_delete", id, recipients, map[string]any{"id": id.String()})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleMarkChannelUnread marca o canal como não lido A PARTIR de uma mensagem: recua o last_read_at
// para logo antes dela, para a divisória "novas mensagens" aparecer ali no próximo abrir.
func (s *Server) handleMarkChannelUnread(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	mid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	msg, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: mid, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if _, ok := s.memberOr403(w, r, msg.ChannelID); !ok {
		return
	}
	_ = s.q.SetLastReadAt(r.Context(), db.SetLastReadAtParams{
		ChannelID: msg.ChannelID, UserID: mu.ID, LastReadAt: msg.CreatedAt.Add(-time.Microsecond),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMarkChannelRead(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	_ = s.q.UpdateLastRead(r.Context(), db.UpdateLastReadParams{ChannelID: id, UserID: mu.ID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
