package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
)

// Webhooks de entrada (bots/integrações): um POST externo com {text} vira mensagem no canal, com o
// nome do webhook como autor (bot). O token é a autorização; sem login. CRUD só para membros.

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
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
		name = "Webhook"
	}
	token, err := newLinkToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	hook, err := s.q.CreateWebhook(r.Context(), db.CreateWebhookParams{TenantID: mu.TenantID, ChannelID: id, Token: token, Name: name, CreatedBy: mu.ID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	writeJSON(w, http.StatusCreated, hook)
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	hooks, err := s.q.ListWebhooks(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	if hooks == nil {
		hooks = []db.ChatWebhook{}
	}
	writeJSON(w, http.StatusOK, hooks)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if err := s.q.DeleteWebhook(r.Context(), db.DeleteWebhookParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleInboundWebhook (PÚBLICO, rate-limited): recebe {text} e publica como mensagem-bot no canal.
func (s *Server) handleInboundWebhook(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	hook, err := s.q.GetWebhookByToken(r.Context(), token)
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	body := strings.TrimSpace(req.Text)
	if body == "" {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	msg, err := s.q.CreateBotMessage(r.Context(), db.CreateBotMessageParams{
		TenantID: hook.TenantID, ChannelID: hook.ChannelID, UserID: hook.CreatedBy, Body: body, BotName: hook.Name,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.send_failed")
		return
	}
	dto := messageDTO{
		ID: msg.ID.String(), ChannelID: hook.ChannelID.String(), UserID: hook.CreatedBy.String(), Parent: "",
		Body: body, Author: hook.Name, BotName: hook.Name, CreatedAt: iso(msg.CreatedAt),
		Reactions: []reactionDTO{}, Attachments: []attachmentDTO{},
	}
	s.publish(r.Context(), "message", hook.ChannelID, s.channelRecipients(r.Context(), hook.ChannelID), dto)
	writeJSON(w, http.StatusOK, map[string]string{"status": "posted"})
}
