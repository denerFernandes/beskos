package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// NewTask é uma tarefa a criar num quadro Kanban.
type NewTask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// Due é uma data ISO (AAAA-MM-DD). Vazio = sem prazo.
	Due string `json:"due"`
	// Board é o NOME do quadro. Vazio usa o primeiro do tenant — o modelo não conhece ids de
	// quadros, e obrigá-lo a adivinhar um seria pedir-lhe para inventar.
	Board string `json:"board"`
	// Assignee é o NOME ou E-MAIL do colega responsável (ex.: "Bob"). Vazio = sem responsável.
	Assignee string `json:"assignee"`
}

// TaskRef é a tarefa criada, como o cartão de ação a mostra.
type TaskRef struct {
	ID       string `json:"id"`
	Board    string `json:"board"`
	Number   string `json:"number"`
	Assignee string `json:"assignee"` // nome resolvido do responsável ("" = sem)
}

// TaskCreate cria a tarefa usando as MESMAS consultas do handler equivalente (§5: sem SQL novo).
type TaskCreate func(ctx context.Context, id Identity, t NewTask) (TaskRef, error)

func taskTools(create TaskCreate) []Tool {
	return []Tool{
		{
			Name: ToolTasksCreate,
			Description: "Create a task on one of the user's Kanban boards. The task is internal to " +
				"the organisation: nothing is sent to anyone outside it.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"title": {"type": "string", "description": "Short title of the task."},
					"description": {"type": "string"},
					"due": {"type": "string", "description": "Due date as YYYY-MM-DD."},
					"board": {"type": "string", "description": "Board name. Leave empty for the default board."}
				},
				"required": ["title"]
			}`),
			Writes: true,
			// Sem Confirm: uma tarefa não sai da organização nem destrói nada, e é reversível. A
			// confirmação é para o que sai para fora ou apaga (§6.3) — exigi-la aqui gastaria a
			// atenção de quem confirma no que não importa, e é essa atenção que protege o resto.
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var t NewTask
				if err := decodeArgs(raw, &t); err != nil {
					return Result{}, err
				}
				t.Title = strings.TrimSpace(t.Title)
				if t.Title == "" {
					return Result{}, fmt.Errorf("title is required")
				}
				ref, err := create(ctx, id, t)
				if err != nil {
					return Result{}, fmt.Errorf("task creation failed: %w", err)
				}
				summary := t.Title
				if ref.Number != "" {
					summary = ref.Number + " " + t.Title
				}
				return Result{Summary: summary, Data: ref}, nil
			},
		},
	}
}
