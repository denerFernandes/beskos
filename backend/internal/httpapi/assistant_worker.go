package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// assistantConfirmPoll é de quanto em quanto tempo o worker vê se a decisão já chegou. Uma tarefa
// parada à espera de aprovação não tem pressa; sondar de segundo a segundo só encheria o log.
const assistantConfirmPoll = 2 * time.Second

// assistantTaskConfirmWait é quanto uma tarefa em fundo espera por uma decisão antes de desistir.
// Muito mais generoso do que o da execução síncrona: aqui a pessoa não está a olhar para o ecrã, e
// a promessa de §3.6 é que a tarefa avisa e espera. Findo o prazo, é RECUSA — nunca aprovação.
const assistantTaskConfirmWait = 24 * time.Hour

// AssistantTaskWorker corre as tarefas em fundo. Vive no `mailin-worker` (§7.5), mas é construído
// aqui porque precisa exatamente das mesmas dependências do motor síncrono — e ter duas montagens
// do mesmo motor seria ter duas superfícies de risco a divergir.
type AssistantTaskWorker struct {
	q      *db.Queries
	srv    *Server
	engine *assistant.Engine
}

// NewAssistantTaskWorker monta o executor. Recebe um Server já configurado: é dele que vêm o acesso
// à caixa, ao Drive e às tarefas, com as mesmas verificações de posse e de tenant.
func NewAssistantTaskWorker(s *Server) *AssistantTaskWorker {
	return &AssistantTaskWorker{q: s.q, srv: s, engine: s.agent}
}

// Tick reclama uma tarefa e executa-a. Devolve false quando não havia nada a fazer, para quem chama
// poder esperar em vez de girar em vazio.
func (w *AssistantTaskWorker) Tick(ctx context.Context) bool {
	if w.engine == nil || w.srv.ai == nil {
		return false
	}
	lease := pgtype.Interval{Microseconds: int64(assistant.TaskLease / time.Microsecond), Valid: true}
	row, err := w.q.ClaimAssistantTask(ctx, lease)
	if err != nil {
		// Sem tarefas na fila é o caso normal e não é erro digno de log.
		return false
	}
	w.run(ctx, row)
	return true
}

// run executa uma tarefa reclamada.
func (w *AssistantTaskWorker) run(ctx context.Context, row db.AssistantTask) {
	id := assistant.Identity{TenantID: row.TenantID, UserID: row.MailUserID}
	if mu, err := w.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: row.MailUserID, TenantID: row.TenantID}); err == nil {
		id.Email, id.Language = mu.Email, mu.Language
	}

	ag, ok := w.srv.assistantAgentByKey(ctx, row.TenantID, row.Agent)
	if !ok {
		// O agente desapareceu (apagado, ou desligado) entre enfileirar e executar. A tarefa falha
		// com um código traduzível em vez de correr com um perfil que ninguém escolheu.
		w.finish(ctx, row.ID, assistant.TaskResult{Status: assistant.TaskFailed, ErrorCode: "assistant.unknown_agent"})
		return
	}

	history, err := w.history(ctx, row)
	if err != nil {
		log.Printf("assistant-task: history for %s: %v", row.ID, err)
	}

	// O motor do worker precisa do seu próprio Confirmer: o do servidor é em memória e vive noutro
	// processo. Este escreve na base e espera lá — é o que torna a espera durável (§6.3).
	engine := w.engine.WithConfirmer(&taskConfirmer{q: w.q, taskID: row.ID})

	res := assistant.RunTask(ctx, engine, taskStore{q: w.q}, taskEffects{q: w.q, taskID: row.ID}, assistant.Task{
		ID: row.ID, Identity: id, Agent: ag, Message: row.Message,
		ConversationID: row.ConversationID, History: history,
	})

	// A resposta entra na conversa, como a de um turno normal: clicar na notificação abre a
	// conversa onde a tarefa correu, com o resultado à vista (§3.6).
	w.appendAnswer(ctx, row, res)
	w.notify(ctx, row, res)
}

// history reconstrói o fio anterior da conversa.
func (w *AssistantTaskWorker) history(ctx context.Context, row db.AssistantTask) ([]assistant.Turn, error) {
	msgs, err := w.q.ListAssistantMessages(ctx, db.ListAssistantMessagesParams{
		ConversationID: row.ConversationID, TenantID: row.TenantID,
		MailUserID: row.MailUserID, Lim: assistantHistoryTurns,
	})
	if err != nil {
		return nil, err
	}
	out := make([]assistant.Turn, 0, len(msgs))
	for _, m := range msgs {
		// A mensagem que ORIGINOU a tarefa já está no fio; reenviá-la e mandar RunTask executá-la
		// outra vez daria ao modelo o mesmo pedido duas vezes.
		if m.Role == "user" && m.Content == row.Message {
			continue
		}
		out = append(out, assistant.Turn{Role: m.Role, Content: m.Content})
	}
	return out, nil
}

func (w *AssistantTaskWorker) finish(ctx context.Context, id uuid.UUID, res assistant.TaskResult) {
	if err := (taskStore{q: w.q}).Finish(ctx, id, res); err != nil {
		log.Printf("assistant-task: finish %s: %v", id, err)
	}
}

// appendAnswer grava a resposta da tarefa na conversa.
func (w *AssistantTaskWorker) appendAnswer(ctx context.Context, row db.AssistantTask, res assistant.TaskResult) {
	content := res.Answer
	stopped := res.Stopped
	if res.Status == assistant.TaskCancelled {
		stopped = assistant.TaskCancelled
	}
	if content == "" && stopped == "" && res.ErrorCode != "" {
		stopped = res.ErrorCode
	}
	if _, err := w.q.AppendAssistantMessage(ctx, db.AppendAssistantMessageParams{
		TenantID: row.TenantID, ConversationID: row.ConversationID, Role: "assistant",
		Content: content, Steps: assistant.TaskSteps(res.Steps), Stopped: stopped,
		RunID: pgtype.UUID{Bytes: res.RunID, Valid: res.RunID != uuid.UUID{}}, Agent: row.Agent,
	}); err != nil {
		log.Printf("assistant-task: append answer for %s: %v", row.ID, err)
	}
	if err := w.q.TouchAssistantConversation(ctx, db.TouchAssistantConversationParams{
		ID: row.ConversationID, TenantID: row.TenantID, MailUserID: row.MailUserID,
	}); err != nil {
		log.Printf("assistant-task: touch conversation for %s: %v", row.ID, err)
	}
}

// notify avisa quem pediu. O link leva à CONVERSA onde a tarefa correu: uma notificação que só diz
// "terminou" obriga a procurar onde, e a ligação é o que a torna útil (§3.6).
func (w *AssistantTaskWorker) notify(ctx context.Context, row db.AssistantTask, res assistant.TaskResult) {
	if res.Status == assistant.TaskCancelled {
		// Quem cancelou sabe que cancelou; avisá-lo seria ruído.
		return
	}
	args, err := json.Marshal(map[string]string{"task": row.Message, "status": res.Status})
	if err != nil {
		args = json.RawMessage("{}")
	}
	if _, err := w.q.CreateNotification(ctx, db.CreateNotificationParams{
		TenantID: row.TenantID, UserID: row.MailUserID, Kind: "assistant_task",
		// O título e o corpo são traduzidos pelo frontend por server.notif.<kind> (lei 4.6); o
		// texto aqui é só o fallback em inglês.
		Title: "Assistant task finished", Body: row.Message,
		Link: fmt.Sprintf("/mail?assistant=%s", row.ConversationID),
		Args: args,
	}); err != nil {
		log.Printf("assistant-task: notify for %s: %v", row.ID, err)
	}
}

// --- Implementações de persistência ---

// taskStore grava o progresso e o desfecho.
type taskStore struct{ q *db.Queries }

func (t taskStore) Progress(ctx context.Context, id uuid.UUID, status string, steps []assistant.Step) error {
	return t.q.SaveAssistantTaskProgress(ctx, db.SaveAssistantTaskProgressParams{
		ID: id, Status: status, Steps: assistant.TaskSteps(steps),
	})
}

// Cancelled renova o arrendamento ao mesmo tempo que lê a bandeira: o worker prova que está vivo
// exatamente quando verifica se deve parar, portanto uma tarefa a trabalhar nunca é dada como
// abandonada.
func (t taskStore) Cancelled(ctx context.Context, id uuid.UUID) (bool, error) {
	row, err := t.q.HeartbeatAssistantTask(ctx, id)
	if err != nil {
		return false, err
	}
	return row.CancelRequested, nil
}

func (t taskStore) Finish(ctx context.Context, id uuid.UUID, res assistant.TaskResult) error {
	return t.q.FinishAssistantTask(ctx, db.FinishAssistantTaskParams{
		ID: id, Status: res.Status, Answer: res.Answer, ErrorCode: res.ErrorCode,
		Stopped: res.Stopped, Steps: assistant.TaskSteps(res.Steps),
		RunID: pgtype.UUID{Bytes: res.RunID, Valid: res.RunID != uuid.UUID{}},
	})
}

// taskEffects é a guarda de idempotência da retoma (lei 5.3).
type taskEffects struct {
	q      *db.Queries
	taskID uuid.UUID
}

func (t taskEffects) Seen(ctx context.Context, fingerprint string) (string, bool, error) {
	summary, err := t.q.GetAssistantTaskEffect(ctx, db.GetAssistantTaskEffectParams{
		TaskID: t.taskID, Fingerprint: fingerprint,
	})
	if err != nil {
		// Não encontrado é o caso normal: ainda não aconteceu.
		return "", false, nil
	}
	return summary, true, nil
}

func (t taskEffects) Record(ctx context.Context, fingerprint, summary string) error {
	return t.q.RecordAssistantTaskEffect(ctx, db.RecordAssistantTaskEffectParams{
		TaskID: t.taskID, Fingerprint: fingerprint, Summary: summary,
	})
}

// taskConfirmer escreve a ação à espera na base e sonda pela decisão. É a espera DURÁVEL prometida
// em §6.3: a tarefa fica "a aguardar confirmação" e continua lá depois de um reinício, porque o
// estado está na base e não na memória de um processo.
type taskConfirmer struct {
	q      *db.Queries
	taskID uuid.UUID
}

func (c *taskConfirmer) Ask(ctx context.Context, id assistant.Identity, req assistant.ConfirmRequest) (bool, error) {
	args := req.Args
	if len(args) == 0 || !json.Valid(args) {
		args = json.RawMessage("{}")
	}
	row, err := c.q.CreateAssistantPendingAction(ctx, db.CreateAssistantPendingActionParams{
		TaskID: c.taskID, TenantID: id.TenantID, MailUserID: id.UserID, Tool: req.Tool, Args: args,
	})
	if err != nil {
		// Não conseguir REGISTAR o pedido é não conseguir perguntar. E não perguntar é não aprovar.
		return false, fmt.Errorf("pending action: %w", err)
	}
	if err := c.q.SaveAssistantTaskProgress(ctx, db.SaveAssistantTaskProgressParams{
		ID: c.taskID, Status: assistant.TaskWaiting, Steps: json.RawMessage("[]"),
	}); err != nil {
		log.Printf("assistant-task: mark waiting %s: %v", c.taskID, err)
	}

	deadline := time.NewTimer(assistantTaskConfirmWait)
	defer deadline.Stop()
	tick := time.NewTicker(assistantConfirmPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			// O prazo esgotou-se sem resposta. Isso é um "não".
			return false, nil
		case <-tick.C:
			got, err := c.q.GetAssistantPendingAction(ctx, row.ID)
			if err != nil {
				continue
			}
			switch got.Decision {
			case "approved":
				return true, nil
			case "rejected":
				return false, nil
			}
		}
	}
}
