package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// assistantTaskListLimit é quantas tarefas o separador mostra.
const assistantTaskListLimit = 50

type assistantTaskJSON struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	Agent          string          `json:"agent"`
	Message        string          `json:"message"`
	Status         string          `json:"status"`
	Steps          json.RawMessage `json:"steps"`
	Answer         string          `json:"answer,omitempty"`
	ErrorCode      string          `json:"error_code,omitempty"`
	Stopped        string          `json:"stopped,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func taskJSON(t db.AssistantTask) assistantTaskJSON {
	steps := t.Steps
	if len(steps) == 0 || !json.Valid(steps) {
		steps = json.RawMessage("[]")
	}
	return assistantTaskJSON{
		ID: t.ID, ConversationID: t.ConversationID, Agent: t.Agent, Message: t.Message,
		Status: t.Status, Steps: steps, Answer: t.Answer, ErrorCode: t.ErrorCode,
		Stopped: t.Stopped, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

// handleAssistantTasks lista as tarefas do PRÓPRIO utilizador, com o progresso. É o que alimenta o
// separador de Tarefas e o indicador da bolha (§3.6).
func (s *Server) handleAssistantTasks(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListAssistantTasks(r.Context(), db.ListAssistantTasksParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Lim: assistantTaskListLimit,
	})
	if err != nil {
		log.Printf("assistant: list tasks: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	items := make([]assistantTaskJSON, 0, len(rows))
	for _, t := range rows {
		items = append(items, taskJSON(t))
	}

	// As ações à espera de aprovação vão junto: uma tarefa parada tem de dizer o que está à espera,
	// senão fica só "a aguardar" sem se saber de quê (§6.3).
	pending, err := s.q.ListAssistantPendingActions(r.Context(), db.ListAssistantPendingActionsParams{
		TenantID: mu.TenantID, MailUserID: mu.ID,
	})
	if err != nil {
		log.Printf("assistant: list pending actions: %v", err)
	}
	waiting := make([]map[string]any, 0, len(pending))
	for _, p := range pending {
		args := p.Args
		if len(args) == 0 || !json.Valid(args) {
			args = json.RawMessage("{}")
		}
		waiting = append(waiting, map[string]any{
			"id": p.ID, "task_id": p.TaskID, "tool": p.Tool, "args": args,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "pending": waiting})
}

// handleAssistantTaskCancel pede o cancelamento. É um PEDIDO, não um efeito imediato: a tarefa pode
// estar a meio de uma chamada, e o worker vê a bandeira no passo seguinte. O que já fez fica
// registado (§7.5) — uma tarefa cancelada não é uma tarefa sem história.
func (s *Server) handleAssistantTaskCancel(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_task")
		return
	}
	if err := s.q.RequestAssistantTaskCancel(r.Context(), db.RequestAssistantTaskCancelParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID,
	}); err != nil {
		log.Printf("assistant: cancel task: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

// startAssistantTask enfileira o pedido em vez de o correr no pedido HTTP. A conversa devolve logo
// um cartão de tarefa; o trabalho continua no worker (§3.6).
func (s *Server) startAssistantTask(w http.ResponseWriter, r *http.Request, turn assistantTurnState, msg string) {
	mu := mailUserFrom(r.Context())
	row, err := s.q.CreateAssistantTask(r.Context(), db.CreateAssistantTaskParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, ConversationID: turn.conv.ID,
		Agent: turn.agent.Key, Message: msg,
	})
	if err != nil {
		log.Printf("assistant: create task: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"conversation_id": turn.conv.ID,
		"agent":           turn.agent.Key,
		"task":            taskJSON(row),
	})
}

// decideDurablePendingAction entrega a decisão a uma ação de TAREFA (a que sobrevive a reinícios).
// Devolve false quando não há nenhuma ação à espera com este id para este utilizador.
func (s *Server) decideDurablePendingAction(r *http.Request, id uuid.UUID, approve bool) bool {
	mu := mailUserFrom(r.Context())
	decision := "rejected"
	if approve {
		decision = "approved"
	}
	// A consulta filtra por utilizador E exige `decision = ''`: a aprovação é de quem tem a ação, e
	// é de uso único — aprovar duas vezes não envia duas vezes.
	if _, err := s.q.DecideAssistantPendingAction(r.Context(), db.DecideAssistantPendingActionParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID, Decision: decision,
	}); err != nil {
		return false
	}
	return true
}

// taskStatusLabel valida um estado vindo de fora. Existe para o teste poder afirmar que os estados
// do backend e os que a interface conhece são os mesmos conjunto.
func taskStatusValid(status string) bool {
	switch strings.TrimSpace(status) {
	case assistant.TaskQueued, assistant.TaskRunning, assistant.TaskWaiting,
		assistant.TaskDone, assistant.TaskFailed, assistant.TaskCancelled:
		return true
	}
	return false
}
