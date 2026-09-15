package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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
		// TODA A GENTE MENOS QUEM ESTÁ A ESCREVER. Ver "fulano está a escrever…" com o nosso próprio
		// nome enquanto escrevemos não é só ruído: parece que há outra pessoa com o mesmo nome no
		// canal. Também vale entre aparelhos — se estou a escrever no portátil, o telemóvel não tem
		// de me anunciar a mim mesmo.
		//
		// Filtrado AQUI e não na interface porque é o único evento que não interessa à própria
		// pessoa: os restantes (mensagem nova, edição) têm de chegar a todos os aparelhos dela para
		// se manterem sincronizados. É por isso que channelRecipients continua a incluir toda a gente.
		self := mu.ID.String()
		all := s.channelRecipients(ctx, cid)
		others := make([]string, 0, len(all))
		for _, id := range all {
			if id != self {
				others = append(others, id)
			}
		}
		if len(others) == 0 {
			return // canal só comigo (notas pessoais): não há a quem anunciar
		}
		s.publish(ctx, "typing", cid, others,
			map[string]any{"user_id": self, "author": author})
	}
	s.hub.Serve(ctx, mu.ID, ws, onClient)
}
