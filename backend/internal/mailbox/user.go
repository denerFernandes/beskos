// Package mailbox fala JMAP como um USUÁRIO FINAL (não o admin), usando a credencial Basic da caixa
// (a mesma que o broker guarda cifrada). Usado por trabalhos server-side que precisam mexer na caixa
// do utilizador sem ele estar presente — ex.: devolver um e-mail "snoozed" à Caixa de entrada.
package mailbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type UserClient struct {
	base  string // ex.: http://stalwart:8080
	basic string // base64(email:password) — o valor do jmap_cred decifrado
	http  *http.Client
}

func NewUserClient(base, basic string) *UserClient {
	return &UserClient{base: base, basic: basic, http: &http.Client{Timeout: 20 * time.Second}}
}

func (u *UserClient) do(ctx context.Context, path string, body any) (map[string]json.RawMessage, error) {
	var reader *bytes.Reader
	method := http.MethodGet
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
		method = http.MethodPost
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+u.basic)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := u.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jmap user: status %d", resp.StatusCode)
	}
	var out map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (u *UserClient) accountID(ctx context.Context) (string, error) {
	raw, err := u.do(ctx, "/jmap/session", nil)
	if err != nil {
		return "", err
	}
	var pa map[string]string
	if err := json.Unmarshal(raw["primaryAccounts"], &pa); err != nil {
		return "", err
	}
	if id := pa["urn:ietf:params:jmap:mail"]; id != "" {
		return id, nil
	}
	return "", fmt.Errorf("jmap user: no mail account")
}

// inboxID resolve o id da Caixa de entrada (role inbox) da conta.
func (u *UserClient) inboxID(ctx context.Context, acc string) (string, error) {
	resp, err := u.jmap(ctx, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": acc, "ids": nil, "properties": []string{"role"}}, "m"},
	})
	if err != nil {
		return "", err
	}
	var list struct {
		List []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"list"`
	}
	if err := json.Unmarshal(resp[0], &list); err != nil {
		return "", err
	}
	for _, m := range list.List {
		if m.Role == "inbox" {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("jmap user: no inbox")
}

// jmap faz uma chamada /jmap/ e devolve os argumentos de cada methodResponse (na ordem).
func (u *UserClient) jmap(ctx context.Context, calls []any) ([]json.RawMessage, error) {
	raw, err := u.do(ctx, "/jmap/", map[string]any{
		"using":       []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"},
		"methodCalls": calls,
	})
	if err != nil {
		return nil, err
	}
	var responses [][]json.RawMessage
	if err := json.Unmarshal(raw["methodResponses"], &responses); err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(responses))
	for _, m := range responses {
		if len(m) >= 2 {
			out = append(out, m[1]) // o objeto de argumentos
		}
	}
	return out, nil
}

// InboxMsg é o mínimo para uma notificação de e-mail novo.
type InboxMsg struct {
	From       string
	Subject    string
	ReceivedAt time.Time
}

// InboxRecent devolve as mensagens mais recentes da Caixa de entrada (mais nova primeiro). O worker
// filtra pelas que chegaram depois da última verificação.
func (u *UserClient) InboxRecent(ctx context.Context, limit int) ([]InboxMsg, error) {
	acc, err := u.accountID(ctx)
	if err != nil {
		return nil, err
	}
	inbox, err := u.inboxID(ctx, acc)
	if err != nil {
		return nil, err
	}
	resp, err := u.jmap(ctx, []any{
		[]any{"Email/query", map[string]any{
			"accountId": acc, "filter": map[string]any{"inMailbox": inbox},
			"sort": []any{map[string]any{"property": "receivedAt", "isAscending": false}}, "limit": limit,
		}, "q"},
		[]any{"Email/get", map[string]any{
			"accountId": acc, "#ids": map[string]any{"resultOf": "q", "name": "Email/query", "path": "/ids"},
			"properties": []string{"from", "subject", "receivedAt"},
		}, "g"},
	})
	if err != nil || len(resp) < 2 {
		return nil, err
	}
	var got struct {
		List []struct {
			Subject    string `json:"subject"`
			ReceivedAt string `json:"receivedAt"`
			From       []struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"from"`
		} `json:"list"`
	}
	if err := json.Unmarshal(resp[1], &got); err != nil {
		return nil, err
	}
	out := make([]InboxMsg, 0, len(got.List))
	for _, e := range got.List {
		when, _ := time.Parse(time.RFC3339, e.ReceivedAt)
		from := ""
		if len(e.From) > 0 {
			from = e.From[0].Name
			if from == "" {
				from = e.From[0].Email
			}
		}
		out = append(out, InboxMsg{From: from, Subject: e.Subject, ReceivedAt: when})
	}
	return out, nil
}

// SubmitEmail envia um rascunho já existente (EmailSubmission/set) e, no sucesso, MOVE o e-mail para
// a pasta Enviados. Usado pela outbox server-side e pelo assistente.
//
// Antes usava onSuccessDestroyEmail, que apagava o e-mail — resultado: o enviado desaparecia (o
// Stalwart não guarda cópia em Enviados sozinho). Agora usa onSuccessUpdateEmail para o mudar de
// Rascunhos para Enviados (tira $draft, marca $seen). Sem pasta Enviados resolvida, cai no destruir
// como antes — melhor perder a cópia do que deixar o rascunho eternamente na caixa.
// Erro se o Stalwart recusar a submissão (notCreated) — o worker marca o outbox como 'failed'.
func (u *UserClient) SubmitEmail(ctx context.Context, emailID, identityID, from string, rcpts []string) error {
	acc, err := u.accountID(ctx)
	if err != nil {
		return err
	}
	rcptTo := make([]map[string]any, 0, len(rcpts))
	for _, r := range rcpts {
		rcptTo = append(rcptTo, map[string]any{"email": r})
	}
	submit := map[string]any{
		"accountId": acc,
		"create": map[string]any{
			"sub": map[string]any{
				"emailId":    emailID,
				"identityId": identityID,
				"envelope": map[string]any{
					"mailFrom": map[string]any{"email": from},
					"rcptTo":   rcptTo,
				},
			},
		},
	}
	// Guarda a cópia em Enviados. Se a pasta não existir, destrói (comportamento antigo).
	if sent, err := u.mailboxByRole(ctx, acc, "sent"); err == nil && sent != "" {
		submit["onSuccessUpdateEmail"] = map[string]any{
			"#sub": map[string]any{
				"mailboxIds":      map[string]any{sent: true}, // sai de Rascunhos, entra em Enviados
				"keywords/$draft": nil,                        // deixa de ser rascunho
				"keywords/$seen":  true,                       // enviado por mim → lido
			},
		}
	} else {
		submit["onSuccessDestroyEmail"] = []string{"#sub"}
	}
	raw, err := u.do(ctx, "/jmap/", map[string]any{
		"using":       []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail", "urn:ietf:params:jmap:submission"},
		"methodCalls": []any{[]any{"EmailSubmission/set", submit, "s"}},
	})
	if err != nil {
		return err
	}
	var responses [][]json.RawMessage
	if err := json.Unmarshal(raw["methodResponses"], &responses); err != nil {
		return err
	}
	if len(responses) == 0 || len(responses[0]) < 2 {
		return fmt.Errorf("jmap user: no submission response")
	}
	var res struct {
		NotCreated map[string]json.RawMessage `json:"notCreated"`
	}
	if err := json.Unmarshal(responses[0][1], &res); err != nil {
		return err
	}
	if len(res.NotCreated) > 0 {
		return fmt.Errorf("jmap user: submission rejected: %s", responses[0][1])
	}
	return nil
}

// MoveToInbox devolve a mensagem à Caixa de entrada (mailboxIds = {inbox: true}). Idempotente: se o
// e-mail já não existir, o Email/set apenas o ignora (notUpdated), sem erro para o worker.
func (u *UserClient) MoveToInbox(ctx context.Context, emailID string) error {
	acc, err := u.accountID(ctx)
	if err != nil {
		return err
	}
	inbox, err := u.inboxID(ctx, acc)
	if err != nil {
		return err
	}
	_, err = u.jmap(ctx, []any{
		[]any{"Email/set", map[string]any{
			"accountId": acc,
			"update":    map[string]any{emailID: map[string]any{"mailboxIds": map[string]any{inbox: true}}},
		}, "s"},
	})
	return err
}
