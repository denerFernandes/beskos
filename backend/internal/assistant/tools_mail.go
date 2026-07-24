package assistant

import (
	"context"
	"encoding/json"
	"fmt"

	"mailin/backend/internal/mailbox"
)

// MailBox é o mínimo que as ferramentas de correio precisam. *mailbox.UserClient satisfá-lo; os
// testes passam um duplo. O motor nunca constrói um: não sabe decifrar a credencial JMAP nem deve.
type MailBox interface {
	SearchEmails(ctx context.Context, query string, limit int) ([]mailbox.EmailHeader, error)
	ReadEmail(ctx context.Context, id string) (mailbox.EmailBody, error)
	// CreateDraft grava sem enviar; SendEmail envia. São métodos distintos porque a diferença entre
	// os dois é a fronteira da confirmação humana (§6.3), e uma flag num só método tornaria essa
	// fronteira invisível na assinatura.
	CreateDraft(ctx context.Context, d mailbox.Draft) (string, error)
	SendEmail(ctx context.Context, d mailbox.Draft) (string, error)
}

// MailAccess resolve a caixa do utilizador a partir da Identity — e SÓ da Identity. Resolver por
// identidade (em vez de receber um cliente já pronto) é o que permite às tarefas em fundo da fase
// A4 correr sem pedido HTTP, e é o que garante que a caixa aberta é sempre a do utilizador
// autenticado (§6.2).
type MailAccess func(ctx context.Context, id Identity) (MailBox, error)

// mailSearchArgs — repare que não há tenant_id nem user_id, e nunca haverá (§6.2).
type mailSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type mailReadArgs struct {
	MessageID string `json:"message_id"`
}

func mailTools(access MailAccess) []Tool {
	return []Tool{
		{
			Name: ToolMailSearch,
			Description: "Search the signed-in user's mailbox by free text. Matches sender, " +
				"subject and body. Returns message headers, newest first, without the body: use " +
				ToolMailRead + " to read one. Leave query empty for the most recent messages.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "Free-text search terms. Empty for most recent."},
					"limit": {"type": "integer", "description": "How many messages to return (1-50, default 20)."}
				}
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a mailSearchArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				box, err := access(ctx, id)
				if err != nil {
					return Result{}, fmt.Errorf("mailbox unavailable: %w", err)
				}
				hits, err := box.SearchEmails(ctx, a.Query, a.Limit)
				if err != nil {
					return Result{}, fmt.Errorf("mail search failed: %w", err)
				}
				return Result{
					Summary: fmt.Sprintf("%d messages", len(hits)),
					Data:    hits,
				}, nil
			},
		},
		{
			Name: ToolMailRead,
			Description: "Read one message from the signed-in user's mailbox: headers plus the " +
				"plain-text body. The message_id comes from a previous " + ToolMailSearch + " call.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"message_id": {"type": "string", "description": "Message id returned by ` + ToolMailSearch + `."}
				},
				"required": ["message_id"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a mailReadArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if a.MessageID == "" {
					return Result{}, fmt.Errorf("message_id is required")
				}
				box, err := access(ctx, id)
				if err != nil {
					return Result{}, fmt.Errorf("mailbox unavailable: %w", err)
				}
				msg, err := box.ReadEmail(ctx, a.MessageID)
				if err != nil {
					return Result{}, fmt.Errorf("mail read failed: %w", err)
				}
				subject := msg.Subject
				if subject == "" {
					subject = "(no subject)"
				}
				return Result{Summary: subject, Data: msg}, nil
			},
		},
	}
}
