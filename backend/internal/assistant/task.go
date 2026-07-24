package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Estados de uma tarefa em fundo (§3.6). São os mesmos no servidor, no worker e na interface — daí
// viverem aqui, e não em cada um deles com o risco de divergirem numa letra.
const (
	TaskQueued    = "queued"
	TaskRunning   = "running"
	TaskWaiting   = "awaiting_confirmation"
	TaskDone      = "done"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"
)

// TaskLease é quanto tempo o arrendamento de um worker vale. Passado isto sem renovação, a tarefa
// volta à fila: é assim que se recupera de um worker que morreu a meio (§7.5).
//
// Generoso de propósito. Uma execução agêntica pode ficar minutos num passo — sobretudo à espera de
// uma confirmação humana —, e roubar uma tarefa viva a um worker que está a trabalhar seria pior do
// que esperar de mais por uma que morreu.
const TaskLease = 15 * time.Minute

// TaskStore é o que o executor precisa da persistência. A implementação real está no worker; o
// teste passa um duplo. O motor continua sem falar com a base de dados (§5).
type TaskStore interface {
	// Progress grava os passos já feitos e o estado atual. Chamado a CADA passo: é isto que
	// permite retomar e é isto que a bolha mostra.
	Progress(ctx context.Context, taskID uuid.UUID, status string, steps []Step) error
	// Cancelled diz se foi pedido cancelamento. Consultado entre passos.
	Cancelled(ctx context.Context, taskID uuid.UUID) (bool, error)
	// Finish fecha a tarefa.
	Finish(ctx context.Context, taskID uuid.UUID, res TaskResult) error
}

// TaskResult é o desfecho de uma tarefa.
type TaskResult struct {
	Status    string
	Answer    string
	Stopped   string
	ErrorCode string
	Steps     []Step
	RunID     uuid.UUID
}

// Task é o trabalho a executar.
type Task struct {
	ID             uuid.UUID
	Identity       Identity
	Agent          Agent
	Message        string
	ConversationID uuid.UUID
	History        []Turn
}

// RunTask executa uma tarefa em fundo do princípio ao fim.
//
// Três coisas distinguem isto de uma execução síncrona, e as três vêm de §7.5:
//
//  1. o progresso é gravado a cada passo, para a tarefa poder retomar depois de um reinício;
//  2. as ferramentas de escrita passam pelo registo de efeitos, para uma retoma não repetir o que
//     já aconteceu (lei 5.3);
//  3. o cancelamento é verificado entre passos — o que já foi feito fica registado, não se perde.
//
// Nunca devolve erro por falha do modelo: uma tarefa que falha é uma tarefa com estado 'failed' e
// um código de i18n, não uma exceção que ninguém apanha.
func RunTask(ctx context.Context, eng *Engine, store TaskStore, effects EffectLog, t Task) TaskResult {
	var seen []Step

	// O cancelamento corta o contexto, o que pára a chamada ao modelo a meio se for preciso. A
	// verificação corre entre passos: cancelar é um pedido, e o efeito chega no passo seguinte.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cancelled := false

	opts := RunOptions{
		Message:        t.Message,
		History:        t.History,
		ConversationID: t.ConversationID,
		Effects:        effects,
		OnStep: func(steps []Step) {
			seen = steps
			if err := store.Progress(runCtx, t.ID, TaskRunning, steps); err != nil {
				// Perder o progresso não deita fora o trabalho já feito: a tarefa continua, e no
				// pior caso a retoma repete um passo que a guarda de efeitos já reconhece.
				return
			}
			if stop, err := store.Cancelled(context.WithoutCancel(runCtx), t.ID); err == nil && stop {
				cancelled = true
				cancel()
			}
		},
	}

	run, err := eng.RunStream(runCtx, t.Identity, t.Agent, opts, nil)
	steps := run.Steps
	if len(steps) == 0 {
		steps = seen
	}

	res := TaskResult{Steps: steps, RunID: run.ID}
	switch {
	case cancelled:
		// O que já fez fica registado (§7.5). Uma tarefa cancelada não é uma tarefa sem história.
		res.Status = TaskCancelled
	case err != nil:
		res.Status = TaskFailed
		res.ErrorCode = "assistant.failed"
	default:
		res.Status = TaskDone
		res.Answer = run.Answer
		res.Stopped = run.Stopped
	}
	if ferr := store.Finish(context.WithoutCancel(ctx), t.ID, res); ferr != nil {
		return res
	}
	return res
}

// TaskSteps serializa os passos para gravar. Um erro devolve uma lista vazia em vez de falhar: o
// progresso é informação, e perdê-lo não pode derrubar a tarefa.
func TaskSteps(steps []Step) json.RawMessage {
	if len(steps) == 0 {
		return json.RawMessage("[]")
	}
	out, err := json.Marshal(steps)
	if err != nil {
		return json.RawMessage("[]")
	}
	return out
}

// ParseTaskSteps lê os passos gravados.
func ParseTaskSteps(raw json.RawMessage) ([]Step, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []Step
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("assistant: task steps: %w", err)
	}
	return out, nil
}
