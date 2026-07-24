package httpapi

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/assistant"
)

// assistantConfirmWait é quanto tempo uma ação espera por decisão. Passado o prazo, é RECUSADA —
// nunca aprovada por omissão. Um envio que saísse porque ninguém respondeu seria exatamente o modo
// de falhar que a confirmação existe para impedir.
const assistantConfirmWait = 5 * time.Minute

// assistantConfirms guarda as ações à espera de decisão.
//
// Em memória de propósito: a execução da fase A2 é síncrona e vive dentro de um pedido HTTP, por
// isso morre com o servidor de qualquer maneira. A espera DURÁVEL — a tarefa que fica "a aguardar
// confirmação" mesmo depois de um reinício (§6.3) — pertence à fase A4, que é quando a execução
// passa a viver no worker.
type assistantConfirms struct {
	mu      sync.Mutex
	pending map[uuid.UUID]pendingConfirm
}

type pendingConfirm struct {
	tenant uuid.UUID
	user   uuid.UUID
	// reply tem buffer 1: quem decide nunca fica bloqueado, mesmo que o pedido já tenha desistido.
	reply chan bool
}

func newAssistantConfirms() *assistantConfirms {
	return &assistantConfirms{pending: make(map[uuid.UUID]pendingConfirm)}
}

// Ask regista a ação e espera pela decisão. Implementa assistant.Confirmer.
//
// Três formas de acabar, e duas delas são "não": decisão explícita, o utilizador fechou o separador
// (contexto cancelado), ou o prazo esgotou-se. Só a primeira pode aprovar.
func (c *assistantConfirms) Ask(ctx context.Context, id assistant.Identity, req assistant.ConfirmRequest) (bool, error) {
	reply := make(chan bool, 1)
	c.mu.Lock()
	c.pending[req.ID] = pendingConfirm{tenant: id.TenantID, user: id.UserID, reply: reply}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, req.ID)
		c.mu.Unlock()
	}()

	timer := time.NewTimer(assistantConfirmWait)
	defer timer.Stop()
	select {
	case ok := <-reply:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return false, nil
	}
}

// decide entrega a decisão. Devolve false quando a ação não existe (já decidida, expirada, ou de
// outra pessoa) — e não distingue esses casos entre si: dizer "existe mas não é tua" a quem
// adivinhasse um id já seria informação a mais.
func (c *assistantConfirms) decide(id uuid.UUID, tenant, user uuid.UUID, approve bool) bool {
	c.mu.Lock()
	p, ok := c.pending[id]
	c.mu.Unlock()
	if !ok || p.tenant != tenant || p.user != user {
		return false
	}
	select {
	case p.reply <- approve:
		return true
	default:
		// Já havia uma decisão a caminho. Aprovar duas vezes não envia duas vezes.
		return false
	}
}

type assistantConfirmReq struct {
	Approve bool `json:"approve"`
}

// handleAssistantConfirm aprova ou rejeita UMA ação (§6.3). Uma por pedido: não existe aprovação
// em bloco, e é deliberado — cada coisa que sai é vista e aprovada por si.
func (s *Server) handleAssistantConfirm(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.confirm_unknown")
		return
	}
	var req assistantConfirmReq
	if !decode(w, r, &req) {
		return
	}
	// Duas esperas possíveis, e a ordem importa pouco porque um id só existe numa delas: a da
	// execução SÍNCRONA (em memória, neste processo) e a de uma TAREFA em fundo (na base, e o
	// worker está do outro lado a sondar).
	if !s.confirms.decide(id, mu.TenantID, mu.ID, req.Approve) &&
		!s.decideDurablePendingAction(r, id, req.Approve) {
		writeErr(w, http.StatusNotFound, "assistant.confirm_unknown")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "approved": req.Approve})
}
