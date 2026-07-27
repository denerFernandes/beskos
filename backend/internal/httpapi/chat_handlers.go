package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/chat"
	"mailin/backend/internal/db"
)

// Chat estilo Slack. REST para dados + WebSocket (hub Redis) para tempo real. Tudo filtra tenant.

// ---- DTOs (achatam pgtype para JSON limpo que o frontend consome) ----

type reactionDTO struct {
	Emoji string   `json:"emoji"`
	Count int      `json:"count"`
	Mine  bool     `json:"mine"`
	Users []string `json:"users"`
}

type attachmentDTO struct {
	FileID string `json:"file_id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
}

type messageDTO struct {
	ID          string          `json:"id"`
	ChannelID   string          `json:"channel_id"`
	UserID      string          `json:"user_id"`
	Parent      string          `json:"parent_id"`
	Body        string          `json:"body"`
	Author      string          `json:"author"`
	AuthorEmail string          `json:"author_email"`
	Deleted     bool            `json:"deleted"`
	EditedAt    string          `json:"edited_at"`
	CreatedAt   string          `json:"created_at"`
	BotName     string          `json:"bot_name"`
	ReplyCount  int64           `json:"reply_count"`
	Reactions   []reactionDTO   `json:"reactions"`
	Attachments []attachmentDTO `json:"attachments"`
	Saved       bool            `json:"saved"`
}

// markSaved marca o campo Saved das mensagens conforme o que o usuário salvou naquele canal.
func (s *Server) markSaved(ctx context.Context, userID, channelID uuid.UUID, msgs []messageDTO) []messageDTO {
	ids, _ := s.q.ListSavedIDsInChannel(ctx, db.ListSavedIDsInChannelParams{UserID: userID, ChannelID: channelID})
	if len(ids) == 0 {
		return msgs
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id.String()] = true
	}
	for i := range msgs {
		if set[msgs[i].ID] {
			msgs[i].Saved = true
		}
	}
	return msgs
}

// rawMsg é o intermediário comum entre a lista do canal e a lista de respostas da thread.
type rawMsg struct {
	ID, ChannelID, UserID   uuid.UUID
	ParentID                pgtype.UUID
	Body                    string
	EditedAt, DeletedAt     pgtype.Timestamptz
	CreatedAt               time.Time
	AuthorEmail, AuthorName string
	ReplyCount              int64
	BotName                 string
}

// RFC3339Nano (não RFC3339): preserva os sub-segundos. É o timestamp que a paginação usa como
// cursor (?before=) — em segundos, mensagens do mesmo segundo empatam e a página anterior vem vazia.
func iso(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// pgUUID embrulha um uuid para os campos nuláveis do pgx.
func pgUUID(u uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: u, Valid: true} }

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

// ---- helpers de autorização / destinatários ----

// memberOr403 garante que o usuário é membro do canal; devolve a associação.
func (s *Server) memberOr403(w http.ResponseWriter, r *http.Request, channelID uuid.UUID) (db.ChannelMember, bool) {
	mu := mailUserFrom(r.Context())
	m, err := s.q.GetMembership(r.Context(), db.GetMembershipParams{ChannelID: channelID, UserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusForbidden, "chat.not_member")
		return db.ChannelMember{}, false
	}
	return m, true
}

func (s *Server) channelRecipients(ctx context.Context, channelID uuid.UUID) []string {
	ids, _ := s.q.ListMemberUserIDs(ctx, channelID)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func (s *Server) publish(ctx context.Context, evType string, channelID uuid.UUID, recipients []string, payload any) {
	if s.hub == nil {
		return
	}
	raw, _ := json.Marshal(payload)
	s.hub.Publish(ctx, chat.Event{Type: evType, ChannelID: channelID.String(), Recipients: recipients, Payload: raw})
}

// ---- Canais ----

// generalChannelName é o nome GUARDADO do canal por defeito. Fica em inglês (lei §4.6): o frontend
// mostra a tradução enquanto o nome estiver intacto — se alguém o renomear, vale o nome escolhido.
const generalChannelName = "General"

// ensureGeneral garante que o tenant tem o canal por defeito e que quem está a pedir a lista é
// membro dele. Fazer isto na listagem (e não só no provisionamento) cobre também os tenants e os
// utilizadores que já existiam. São duas leituras indexadas no caso normal, sem escrita.
func (s *Server) ensureGeneral(ctx context.Context, tenant, user uuid.UUID) {
	ch, err := s.q.GetGeneralChannel(ctx, tenant)
	if err != nil {
		created, cerr := s.q.CreateGeneralChannel(ctx, db.CreateGeneralChannelParams{
			TenantID: tenant, Name: generalChannelName, CreatedBy: user,
		})
		if cerr != nil {
			// Corrida com outro pedido: o índice parcial deixou passar só um. Lê o que ficou.
			if ch, err = s.q.GetGeneralChannel(ctx, tenant); err != nil {
				log.Printf("chat: ensure general channel: %v", cerr)
				return
			}
		} else {
			ch = created
		}
	}
	if _, err := s.q.GetMembership(ctx, db.GetMembershipParams{ChannelID: ch.ID, UserID: user}); err == nil {
		return // já é membro
	}
	if err := s.q.AddChannelMember(ctx, db.AddChannelMemberParams{
		ChannelID: ch.ID, TenantID: tenant, UserID: user, Role: "member",
	}); err != nil {
		log.Printf("chat: join general channel: %v", err)
	}
}

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

func (s *Server) dmDisplayName(ctx context.Context, channelID, me uuid.UUID) string {
	name, _ := s.dmOther(ctx, channelID, me)
	return name
}

// dmOther devolve o nome de exibição e o id do "outro" de uma DM. Numa DM de grupo (3+), junta os
// nomes dos demais ("Bob, Carol") e não devolve um id de presença (não há um único outro).
func (s *Server) dmOther(ctx context.Context, channelID, me uuid.UUID) (string, string) {
	members, _ := s.q.ListChannelMembers(ctx, channelID)
	names := make([]string, 0, len(members))
	otherID := ""
	for _, m := range members {
		if m.UserID == me {
			continue
		}
		n := m.Name
		if n == "" {
			n = m.Email
		}
		names = append(names, n)
		otherID = m.UserID.String()
	}
	switch len(names) {
	case 0:
		return "DM", ""
	case 1:
		return names[0], otherID
	default:
		return strings.Join(names, ", "), ""
	}
}

// dmKeyN é a chave canônica de uma DM (par ou grupo): ids ordenados e juntos. O mesmo conjunto de
// pessoas resolve sempre para a mesma chave → reutiliza o canal em vez de duplicar.
func dmKeyN(ids []uuid.UUID) string {
	ss := make([]string, len(ids))
	for i, id := range ids {
		ss[i] = id.String()
	}
	sort.Strings(ss)
	return strings.Join(ss, ":")
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

// managerOr403 garante que o utilizador é gestor do canal (owner ou manager) — a permissão DO
// canal, independente da role da organização. Devolve a associação.
func (s *Server) managerOr403(w http.ResponseWriter, r *http.Request, channelID uuid.UUID) (db.ChannelMember, bool) {
	m, ok := s.memberOr403(w, r, channelID)
	if !ok {
		return db.ChannelMember{}, false
	}
	if m.Role != "owner" && m.Role != "manager" {
		writeErr(w, http.StatusForbidden, "chat.not_manager")
		return db.ChannelMember{}, false
	}
	return m, true
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

func dmKey(a, b uuid.UUID) string {
	ids := []string{a.String(), b.String()}
	sort.Strings(ids)
	return strings.Join(ids, ":")
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

// ---- Mensagens ----

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	// Saltar para a mensagem: ?around=<id> devolve uma JANELA em torno dela (antigas + novas), para
	// abrir a busca/pin já no contexto. Ordem cronológica; has_more = há mais antigas antes da janela.
	if around := r.URL.Query().Get("around"); around != "" {
		aid, perr := uuid.Parse(around)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "chat.invalid")
			return
		}
		pivot, perr := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: aid, TenantID: mu.TenantID})
		if perr != nil || pivot.ChannelID != id {
			writeErr(w, http.StatusNotFound, "chat.not_found")
			return
		}
		const half = 25
		older, _ := s.q.ListMessagesBeforeByChannel(r.Context(), db.ListMessagesBeforeByChannelParams{ChannelID: id, CreatedAt: pivot.CreatedAt.Add(time.Microsecond), Limit: half}) // inclui o pivô, DESC
		newer, _ := s.q.ListMessagesAfterByChannel(r.Context(), db.ListMessagesAfterByChannelParams{ChannelID: id, CreatedAt: pivot.CreatedAt, Limit: half})                         // ASC
		raw := make([]rawMsg, 0, len(older)+len(newer))
		for i := len(older) - 1; i >= 0; i-- { // DESC → ASC (mais antiga primeiro)
			m := older[i]
			raw = append(raw, rawMsg{ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt, AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName, ReplyCount: m.ReplyCount, BotName: m.BotName})
		}
		for _, m := range newer {
			raw = append(raw, rawMsg{ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt, AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName, ReplyCount: m.ReplyCount, BotName: m.BotName})
		}
		reactions, _ := s.q.ListReactionsByChannel(r.Context(), id)
		atts, _ := s.q.ListAttachmentsByChannel(r.Context(), id)
		writeJSON(w, http.StatusOK, map[string]any{
			"messages": s.markSaved(r.Context(), mu.ID, id, buildMessages(raw, reactions, atts, mu.ID, false)), // raw já cronológico
			"has_more": len(older) == half, "focus": around,
		})
		return
	}
	// Paginação: ?limit (default 50, teto 100) e ?before=<RFC3339 do topo já carregado> para trazer as
	// mensagens ANTERIORES. Sem `before`, é a página mais recente (e marca o canal como lido).
	limit := int32(50)
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, e := strconv.Atoi(l); e == nil && n > 0 && n <= 100 {
			limit = int32(n)
		}
	}
	before := r.URL.Query().Get("before")
	raw := make([]rawMsg, 0, limit)
	if before != "" {
		ts, perr := time.Parse(time.RFC3339Nano, before)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "chat.invalid")
			return
		}
		rows, err := s.q.ListMessagesBeforeByChannel(r.Context(), db.ListMessagesBeforeByChannelParams{ChannelID: id, CreatedAt: ts, Limit: limit})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.failed")
			return
		}
		for _, m := range rows {
			raw = append(raw, rawMsg{ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt, AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName, ReplyCount: m.ReplyCount, BotName: m.BotName})
		}
	} else {
		rows, err := s.q.ListMessagesByChannel(r.Context(), db.ListMessagesByChannelParams{ChannelID: id, Limit: limit})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.failed")
			return
		}
		for _, m := range rows {
			raw = append(raw, rawMsg{ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt, AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName, ReplyCount: m.ReplyCount, BotName: m.BotName})
		}
	}
	hasMore := len(raw) == int(limit) // pode haver mais antigas
	reactions, _ := s.q.ListReactionsByChannel(r.Context(), id)
	atts, _ := s.q.ListAttachmentsByChannel(r.Context(), id)
	msgs := s.markSaved(r.Context(), mu.ID, id, buildMessages(raw, reactions, atts, mu.ID, true))
	if before == "" {
		_ = s.q.UpdateLastRead(r.Context(), db.UpdateLastReadParams{ChannelID: id, UserID: mu.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "has_more": hasMore})
}

// handleThreadReplies devolve o pai + as respostas de uma thread.
func (s *Server) handleThreadReplies(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	parent, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if _, ok := s.memberOr403(w, r, parent.ChannelID); !ok {
		return
	}
	replies, _ := s.q.ListThreadReplies(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	raw := make([]rawMsg, len(replies))
	for i, m := range replies {
		raw[i] = rawMsg{
			ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body,
			EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt,
			AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName,
		}
	}
	reactions, _ := s.q.ListReactionsByChannel(r.Context(), parent.ChannelID)
	atts, _ := s.q.ListAttachmentsByChannel(r.Context(), parent.ChannelID)
	writeJSON(w, http.StatusOK, map[string]any{"replies": buildMessages(raw, reactions, atts, mu.ID, false)})
}

// buildMessages transforma linhas + reações + anexos em DTOs em ordem cronológica. reverse=true
// quando as linhas vêm em ordem desc (lista do canal); false quando já vêm asc (thread).
func buildMessages(rows []rawMsg, reactions []db.ListReactionsByChannelRow, atts []db.ListAttachmentsByChannelRow, me uuid.UUID, reverse bool) []messageDTO {
	type key struct{ msg, emoji string }
	grouped := map[key]*reactionDTO{}
	order := map[string][]string{}
	for _, r := range reactions {
		mid := r.MessageID.String()
		k := key{mid, r.Emoji}
		rd := grouped[k]
		if rd == nil {
			rd = &reactionDTO{Emoji: r.Emoji}
			grouped[k] = rd
			order[mid] = append(order[mid], r.Emoji)
		}
		rd.Count++
		rd.Users = append(rd.Users, r.UserID.String())
		if r.UserID == me {
			rd.Mine = true
		}
	}
	attByMsg := map[string][]attachmentDTO{}
	for _, a := range atts {
		mid := a.MessageID.String()
		attByMsg[mid] = append(attByMsg[mid], attachmentDTO{FileID: a.FileID.String(), Name: a.Name, Size: a.Size})
	}
	toDTO := func(m rawMsg) messageDTO {
		mid := m.ID.String()
		var rs []reactionDTO
		for _, emoji := range order[mid] {
			rs = append(rs, *grouped[key{mid, emoji}])
		}
		author := m.AuthorName
		if author == "" {
			author = m.AuthorEmail
		}
		return messageDTO{
			ID: mid, ChannelID: m.ChannelID.String(), UserID: m.UserID.String(),
			Parent: uuidStr(m.ParentID), Body: m.Body, Author: author, AuthorEmail: m.AuthorEmail,
			Deleted: m.DeletedAt.Valid, EditedAt: tsStr(m.EditedAt), CreatedAt: iso(m.CreatedAt),
			ReplyCount: m.ReplyCount, Reactions: rs, Attachments: attByMsg[mid], BotName: m.BotName,
		}
	}
	out := make([]messageDTO, 0, len(rows))
	if reverse {
		for i := len(rows) - 1; i >= 0; i-- {
			out = append(out, toDTO(rows[i]))
		}
	} else {
		for i := range rows {
			out = append(out, toDTO(rows[i]))
		}
	}
	return out
}

func tsStr(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return iso(t.Time)
}

type sendMessageReq struct {
	Body        string          `json:"body"`
	ParentID    string          `json:"parent_id"`
	Mentions    []string        `json:"mentions"` // ids de usuário mencionados
	Attachments []attachmentDTO `json:"attachments"`
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	// Canal arquivado é só de leitura — senão "arquivar" não queria dizer nada.
	if ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID}); err == nil && ch.ArchivedAt.Valid {
		writeErr(w, http.StatusForbidden, "chat.channel_archived")
		return
	}
	var req sendMessageReq
	if !decode(w, r, &req) {
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" && len(req.Attachments) == 0 {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	var parent pgtype.UUID
	if p, perr := uuid.Parse(req.ParentID); perr == nil {
		parent = pgtype.UUID{Bytes: p, Valid: true}
	}
	msg, err := s.q.CreateMessage(r.Context(), db.CreateMessageParams{
		TenantID: mu.TenantID, ChannelID: id, UserID: mu.ID, ParentID: parent, Body: body,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.send_failed")
		return
	}
	// Anexos: liga cada arquivo (já subido no storage) à mensagem.
	out := make([]attachmentDTO, 0, len(req.Attachments))
	for _, a := range req.Attachments {
		fid, perr := uuid.Parse(a.FileID)
		if perr != nil {
			continue
		}
		_ = s.q.AddMessageAttachment(r.Context(), db.AddMessageAttachmentParams{MessageID: msg.ID, FileID: fid, Name: a.Name, Size: a.Size})
		out = append(out, attachmentDTO{FileID: a.FileID, Name: a.Name, Size: a.Size})
	}
	author := mu.Name
	if author == "" {
		author = mu.Email
	}
	dto := messageDTO{
		ID: msg.ID.String(), ChannelID: id.String(), UserID: mu.ID.String(), Parent: uuidStr(parent),
		Body: body, Author: author, AuthorEmail: mu.Email, CreatedAt: iso(msg.CreatedAt),
		Reactions: []reactionDTO{}, Attachments: out,
	}
	s.publish(r.Context(), "message", id, s.channelRecipients(r.Context(), id), dto)

	preview := trim(body, 120)
	if preview == "" && len(out) > 0 {
		preview = "📎 " + out[0].Name
	}
	s.fanoutMessage(r.Context(), id, mu.TenantID, mu.ID, author, body, preview, req.Mentions)
	writeJSON(w, http.StatusCreated, dto)
}

// fanoutMessage dispara as notificações de uma nova mensagem: menções pessoais, @channel/@here e a
// notificação de "mensagem" (DM sempre; canal "all"). Dedup por pessoa; nunca o autor nem quem silenciou.
func (s *Server) fanoutMessage(ctx context.Context, channelID, tenantID, authorID uuid.UUID, author, body, preview string, mentions []string) {
	notified := map[uuid.UUID]bool{authorID: true}

	// 1) Menções pessoais. O alvo PRECISA ser membro do canal (senão notificaria qualquer UUID, até
	// de outro tenant — auditoria M).
	for _, uidStr := range mentions {
		tid, perr := uuid.Parse(uidStr)
		if perr != nil || notified[tid] {
			continue
		}
		m, merr := s.q.GetMembership(ctx, db.GetMembershipParams{ChannelID: channelID, UserID: tid})
		if merr != nil || m.NotifPref == "none" {
			continue
		}
		s.notifyUser(ctx, tenantID, tid, "chat_mention", author+" mentioned you", preview, "/mail/chat", uuid.Nil,
			map[string]string{"actor": author})
		notified[tid] = true
	}

	meta, _ := s.q.GetChannelMeta(ctx, channelID)
	prefs, _ := s.q.ListChannelMemberPrefs(ctx, channelID)

	// 2) @channel (todos os membros) e @here (só os online). Respeita mute; não vale em DM.
	atChannel := hasMentionToken(body, "channel")
	atHere := hasMentionToken(body, "here")
	if meta.Kind != "dm" && (atChannel || atHere) {
		online := map[string]bool{}
		if atHere && s.hub != nil {
			memberIDs, _ := s.q.ListMemberUserIDs(ctx, channelID)
			for _, o := range s.hub.OnlineAmong(ctx, memberIDs) {
				online[o] = true
			}
		}
		label := "@here"
		if atChannel {
			label = "@channel"
		}
		for _, p := range prefs {
			if notified[p.UserID] || p.NotifPref == "none" {
				continue
			}
			if !atChannel && !online[p.UserID.String()] {
				continue // @here isolado: só quem está online
			}
			s.notifyUser(ctx, tenantID, p.UserID, "chat_mention", author+" called "+label+" in #"+meta.Name, preview, "/mail/chat", uuid.Nil,
				map[string]string{"actor": author, "at": label, "channel": meta.Name})
			notified[p.UserID] = true
		}
	}

	// 3) Mensagem comum: DM sempre avisa o outro; canal avisa quem escolheu "all".
	isDM := meta.Kind == "dm"
	for _, p := range prefs {
		if notified[p.UserID] || p.NotifPref == "none" {
			continue
		}
		if !isDM && p.NotifPref != "all" {
			continue
		}
		title := author
		nkind := "chat_dm"
		if !isDM {
			title = author + " in #" + meta.Name
			nkind = "chat_message"
		}
		s.notifyPushOnly(ctx, p.UserID, nkind, title, preview, "/mail/chat")
	}
}

type forwardReq struct {
	ChannelID string `json:"channel_id"`
}

// handleForwardMessage encaminha uma mensagem para outro canal, criando uma nova mensagem que cita a
// original. Exige ser membro da ORIGEM (para ler) e do DESTINO (para postar).
func (s *Server) handleForwardMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	mid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	src, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: mid, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if _, ok := s.memberOr403(w, r, src.ChannelID); !ok {
		return
	}
	var req forwardReq
	if !decode(w, r, &req) {
		return
	}
	target, err := uuid.Parse(req.ChannelID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	if _, merr := s.q.GetMembership(r.Context(), db.GetMembershipParams{ChannelID: target, UserID: mu.ID}); merr != nil {
		writeErr(w, http.StatusForbidden, "chat.not_member")
		return
	}
	srcAuthor := "?"
	if u, uerr := s.q.GetUserByID(r.Context(), src.UserID); uerr == nil {
		srcAuthor = u.Name
		if srcAuthor == "" {
			srcAuthor = u.Email
		}
	}
	quoted := "> " + strings.ReplaceAll(strings.TrimSpace(src.Body), "\n", "\n> ")
	fwdBody := "↪ **Encaminhada de " + srcAuthor + "**\n" + quoted
	msg, err := s.q.CreateMessage(r.Context(), db.CreateMessageParams{TenantID: mu.TenantID, ChannelID: target, UserID: mu.ID, Body: fwdBody})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.send_failed")
		return
	}
	author := mu.Name
	if author == "" {
		author = mu.Email
	}
	dto := messageDTO{
		ID: msg.ID.String(), ChannelID: target.String(), UserID: mu.ID.String(),
		Body: fwdBody, Author: author, AuthorEmail: mu.Email, CreatedAt: iso(msg.CreatedAt),
		Reactions: []reactionDTO{}, Attachments: []attachmentDTO{},
	}
	s.publish(r.Context(), "message", target, s.channelRecipients(r.Context(), target), dto)
	s.fanoutMessage(r.Context(), target, mu.TenantID, mu.ID, author, fwdBody, trim(fwdBody, 120), nil)
	writeJSON(w, http.StatusCreated, dto)
}

// handleSaveMessage / handleUnsaveMessage: salvar/dessalvar uma mensagem (saved items do usuário).
func (s *Server) handleSaveMessage(w http.ResponseWriter, r *http.Request) {
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
	_ = s.q.SaveMessage(r.Context(), db.SaveMessageParams{UserID: mu.ID, MessageID: mid, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *Server) handleUnsaveMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	mid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	_ = s.q.UnsaveMessage(r.Context(), db.UnsaveMessageParams{UserID: mu.ID, MessageID: mid})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unsaved"})
}

// handleListSaved lista as mensagens salvas do usuário (painel "Salvos").
func (s *Server) handleListSaved(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListSavedMessages(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{
			"id": m.ID, "channel_id": m.ChannelID, "channel_name": m.ChannelName, "channel_kind": m.ChannelKind,
			"author": authorName(m.AuthorName, m.AuthorEmail), "body": m.Body,
			"created_at": iso(m.CreatedAt), "saved_at": iso(m.SavedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": out})
}

// hasMentionToken diz se o corpo contém @channel/@here como token (precedido de início/espaço e
// seguido de fim/espaço/pontuação) — evita casar "@channel.com" ou "email@here.io".
func hasMentionToken(body, tok string) bool {
	at := "@" + tok
	low := strings.ToLower(body)
	for i := 0; ; {
		j := strings.Index(low[i:], at)
		if j < 0 {
			return false
		}
		p := i + j
		startOK := p == 0 || low[p-1] == ' ' || low[p-1] == '\n' || low[p-1] == '\t'
		end := p + len(at)
		endOK := end == len(low) || strings.ContainsRune(" \n\t.,!?;:", rune(low[end]))
		if startOK && endOK {
			return true
		}
		i = p + 1
	}
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (s *Server) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	msg, err := s.q.EditMessage(r.Context(), db.EditMessageParams{ID: id, UserID: mu.ID, Body: strings.TrimSpace(req.Body)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	s.publish(r.Context(), "message_edit", msg.ChannelID, s.channelRecipients(r.Context(), msg.ChannelID),
		map[string]any{"id": msg.ID.String(), "body": msg.Body, "edited_at": tsStr(msg.EditedAt)})
	writeJSON(w, http.StatusOK, map[string]any{"id": msg.ID.String(), "body": msg.Body, "edited_at": tsStr(msg.EditedAt)})
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	msg, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	// O autor apaga a própria; um gestor/owner do canal modera (apaga a de terceiros).
	if msg.UserID == mu.ID {
		if err := s.q.DeleteMessage(r.Context(), db.DeleteMessageParams{ID: id, UserID: mu.ID}); err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.failed")
			return
		}
	} else {
		if _, ok := s.managerOr403(w, r, msg.ChannelID); !ok {
			return
		}
		if err := s.q.DeleteMessageAny(r.Context(), db.DeleteMessageAnyParams{ID: id, TenantID: mu.TenantID}); err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.failed")
			return
		}
	}
	s.publish(r.Context(), "message_delete", msg.ChannelID, s.channelRecipients(r.Context(), msg.ChannelID),
		map[string]any{"id": id.String()})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleDeleteChannel exclui um canal — só o OWNER. A cascata remove membros e mensagens.
// canArchiveChannel: o DONO do canal ou um admin/dono da organização. O admin da organização pode
// não ser membro do canal (privado) — daí a elevação vir antes da associação.
func (s *Server) canArchiveChannel(r *http.Request, channelID uuid.UUID) bool {
	mu := mailUserFrom(r.Context())
	if mu.Role == "owner" || mu.Role == "admin" || mu.Role == "internal_admin" {
		return true
	}
	m, err := s.q.GetMembership(r.Context(), db.GetMembershipParams{ChannelID: channelID, UserID: mu.ID})
	return err == nil && m.Role == "owner"
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

// ---- Reações ----

func (s *Server) handleReact(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	var req struct {
		Emoji  string `json:"emoji"`
		Remove bool   `json:"remove"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Emoji == "" {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	msg, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	// Só membro do canal reage: todos os irmãos (listar/enviar/pin/busca) checam memberOr403 — sem
	// isto, um não-membro reage numa mensagem de canal privado e a reação é transmitida aos membros
	// (auditoria M — quebra de autorização de canal).
	if _, ok := s.memberOr403(w, r, msg.ChannelID); !ok {
		return
	}
	if req.Remove {
		_ = s.q.RemoveReaction(r.Context(), db.RemoveReactionParams{MessageID: id, UserID: mu.ID, Emoji: req.Emoji})
	} else {
		_ = s.q.AddReaction(r.Context(), db.AddReactionParams{MessageID: id, UserID: mu.ID, Emoji: req.Emoji})
	}
	s.publish(r.Context(), "reaction", msg.ChannelID, s.channelRecipients(r.Context(), msg.ChannelID),
		map[string]any{"message_id": id.String(), "emoji": req.Emoji, "user_id": mu.ID.String(), "remove": req.Remove})
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

// ---- Anexos (download autorizado por participação no canal) ----

func (s *Server) handleChatAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	fid, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	att, err := s.q.GetChatAttachment(r.Context(), db.GetChatAttachmentParams{FileID: fid, UserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	obj, err := s.storage.Get(r.Context(), att.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	if r.URL.Query().Get("inline") == "1" {
		ct := mime.TypeByExtension("." + extOf(att.Name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", att.Name))
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", att.Name))
	}
	_, _ = io.Copy(w, obj)
}

// ---- Presença (online/ausente) ----

// handleChatPresence devolve quais membros do tenant estão online agora.
func (s *Server) handleChatPresence(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if s.hub == nil {
		writeJSON(w, http.StatusOK, map[string]any{"online": []string{}})
		return
	}
	users, _ := s.q.ListMailUsersByTenant(r.Context(), mu.TenantID)
	ids := make([]uuid.UUID, len(users))
	presByID := make(map[string]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
		presByID[u.ID.String()] = u.Presence
	}
	online := s.hub.OnlineAmong(r.Context(), ids)
	// Presença rica só para quem está REALMENTE online (offline sempre ganha).
	presence := make(map[string]string, len(online))
	for _, id := range online {
		p := presByID[id]
		if p == "" {
			p = "active"
		}
		presence[id] = p
	}
	writeJSON(w, http.StatusOK, map[string]any{"online": online, "presence": presence})
}

// ---- Busca de mensagens ----

// resolveColleague acha um usuário do tenant por e-mail/nome (exato, depois contém). uuid.Nil se não achar.
func (s *Server) resolveColleague(ctx context.Context, tenant uuid.UUID, who string) uuid.UUID {
	who = strings.ToLower(strings.TrimSpace(who))
	if who == "" {
		return uuid.Nil
	}
	users, _ := s.q.ListMailUsersByTenant(ctx, tenant)
	for _, u := range users {
		if strings.ToLower(u.Email) == who || strings.ToLower(u.Name) == who {
			return u.ID
		}
	}
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), who) || strings.Contains(strings.ToLower(u.Email), who) {
			return u.ID
		}
	}
	return uuid.Nil
}

func (s *Server) handleChatSearch(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	// Filtros estilo Slack: from:<nome/email>, before:/after:<AAAA-MM-DD>. O resto é o termo (FTS).
	p := db.SearchMessagesParams{UserID: mu.ID, TenantID: mu.TenantID, Lim: 40}
	var termParts []string
	for _, tok := range strings.Fields(q) {
		low := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(low, "from:") && len(tok) > 5:
			if uid := s.resolveColleague(r.Context(), mu.TenantID, tok[5:]); uid != uuid.Nil {
				p.HasFrom, p.FromUser = true, uid
			}
		case strings.HasPrefix(low, "before:") && len(tok) > 7:
			if t, err := time.Parse("2006-01-02", tok[7:]); err == nil {
				p.HasBefore, p.BeforeTs = true, t
			}
		case strings.HasPrefix(low, "after:") && len(tok) > 6:
			if t, err := time.Parse("2006-01-02", tok[6:]); err == nil {
				p.HasAfter, p.AfterTs = true, t
			}
		default:
			termParts = append(termParts, tok)
		}
	}
	p.Term = strings.Join(termParts, " ")
	if len(p.Term) < 2 && !p.HasFrom && !p.HasBefore && !p.HasAfter {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}
	rows, err := s.q.SearchMessages(r.Context(), p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		author := m.AuthorName
		if author == "" {
			author = m.AuthorEmail
		}
		name := m.ChannelName
		if m.ChannelKind == "dm" {
			name = s.dmDisplayName(r.Context(), m.ChannelID, mu.ID)
		}
		out = append(out, map[string]any{
			"id": m.ID, "channel_id": m.ChannelID, "channel_name": name, "channel_kind": m.ChannelKind,
			"author": author, "body": m.Body, "created_at": iso(m.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// ---- WebSocket ----

func (s *Server) handleChatWS(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeErr(w, http.StatusServiceUnavailable, "chat.realtime_disabled")
		return
	}
	mu := mailUserFrom(r.Context())
	// Verificação de Origin: default aceita só same-origin (web) e a allow-list adiciona o app
	// nativo (Tauri). Sem isto (InsecureSkipVerify) qualquer página abriria o WS com o cookie
	// ambiente da vítima — CSWSH (auditoria M).
	opts := &websocket.AcceptOptions{OriginPatterns: originHostPatterns(s.corsOrigins)}
	ws, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ctx := r.Context()
	// Cliente só envia "digitando": {type:"typing", channel_id}. Repassa aos membros do canal.
	onClient := func(data []byte) {
		var in struct {
			Type      string `json:"type"`
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(data, &in) != nil || in.Type != "typing" {
			return
		}
		cid, perr := uuid.Parse(in.ChannelID)
		if perr != nil {
			return
		}
		if _, merr := s.q.GetMembership(ctx, db.GetMembershipParams{ChannelID: cid, UserID: mu.ID}); merr != nil {
			return
		}
		author := mu.Name
		if author == "" {
			author = mu.Email
		}
		s.publish(ctx, "typing", cid, s.channelRecipients(ctx, cid),
			map[string]any{"user_id": mu.ID.String(), "author": author})
	}
	s.hub.Serve(ctx, mu.ID, ws, onClient)
}
