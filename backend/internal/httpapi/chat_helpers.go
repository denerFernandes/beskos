package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/chat"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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

func dmKey(a, b uuid.UUID) string {
	ids := []string{a.String(), b.String()}
	sort.Strings(ids)
	return strings.Join(ids, ":")
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
