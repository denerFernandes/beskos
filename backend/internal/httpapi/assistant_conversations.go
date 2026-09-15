package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// assistantConvListLimit é quantas conversas a lista mostra. Chega para o histórico recente; a
// pesquisa no histórico é trabalho de outra fase.
const assistantConvListLimit = 50

// assistantMsgLimit trava quanto de uma conversa se devolve de uma vez.
const assistantMsgLimit = 200

type assistantConvJSON struct {
	ID        uuid.UUID `json:"id"`
	Agent     string    `json:"agent"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

type assistantMsgJSON struct {
	ID   uuid.UUID `json:"id"`
	Role string    `json:"role"`
	// Agent é quem respondeu. Vazio nas mensagens do utilizador e nas gravadas antes da fase A3.
	Agent     string          `json:"agent,omitempty"`
	Content   string          `json:"content"`
	Steps     json.RawMessage `json:"steps"`
	Stopped   string          `json:"stopped,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// handleAssistantConversations lista o histórico do PRÓPRIO utilizador. A conversa é pessoal e não
// é partilhada nem dentro da mesma organização (§3.5) — daí o filtro por (tenant, utilizador).
func (s *Server) handleAssistantConversations(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListAssistantConversations(r.Context(), db.ListAssistantConversationsParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Lim: assistantConvListLimit,
	})
	if err != nil {
		log.Printf("assistant: list conversations: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	items := make([]assistantConvJSON, 0, len(rows))
	for _, c := range rows {
		items = append(items, assistantConvJSON{ID: c.ID, Agent: c.Agent, Title: c.Title, UpdatedAt: c.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleAssistantConversation devolve um fio com as suas mensagens e cartões de ação. Reabrir uma
// conversa tem de mostrar o mesmo que se viu ao vivo, incluindo o registo das chamadas (§3.3).
func (s *Server) handleAssistantConversation(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_conversation")
		return
	}
	conv, err := s.q.GetAssistantConversation(r.Context(), db.GetAssistantConversationParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID,
	})
	if err != nil {
		// "Não existe" e "não é tua" respondem igual: distingui-las diria a quem adivinhasse um id
		// que ele existe noutra conta.
		writeErr(w, http.StatusNotFound, "assistant.unknown_conversation")
		return
	}
	rows, err := s.q.ListAssistantMessages(r.Context(), db.ListAssistantMessagesParams{
		ConversationID: conv.ID, TenantID: mu.TenantID, MailUserID: mu.ID, Lim: assistantMsgLimit,
	})
	if err != nil {
		log.Printf("assistant: list messages: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	msgs := make([]assistantMsgJSON, 0, len(rows))
	for _, m := range rows {
		steps := m.Steps
		if len(steps) == 0 || !json.Valid(steps) {
			steps = json.RawMessage("[]")
		}
		msgs = append(msgs, assistantMsgJSON{
			ID: m.ID, Role: m.Role, Agent: m.Agent, Content: m.Content, Steps: steps,
			Stopped: m.Stopped, CreatedAt: m.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": conv.ID, "agent": conv.Agent, "pinned_agent": conv.PinnedAgent,
		"title": conv.Title, "messages": msgs,
	})
}

// handleAssistantConversationDelete apaga um fio. As mensagens vão atrás (ON DELETE CASCADE); o
// registo de auditoria NÃO vai — fica com a conversa a NULL, porque "porque é que isto aconteceu?"
// tem de continuar a ter resposta depois de a conversa desaparecer (§6.4).
func (s *Server) handleAssistantConversationDelete(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_conversation")
		return
	}
	if err := s.q.DeleteAssistantConversation(r.Context(), db.DeleteAssistantConversationParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID,
	}); err != nil {
		log.Printf("assistant: delete conversation: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
