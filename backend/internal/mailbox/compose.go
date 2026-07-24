package mailbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Draft é uma mensagem por enviar, tal como o assistente a propõe. Sem anexos: o assistente da
// fase A2 escreve texto, e um anexo que ele escolhesse sozinho seria conteúdo a sair sem ninguém
// ter visto o que era.
type Draft struct {
	To      []string `json:"to"`
	Cc      []string `json:"cc,omitempty"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	// InReplyTo, quando preenchido, liga o rascunho ao fio de uma mensagem existente.
	InReplyTo string `json:"in_reply_to,omitempty"`
}

// CreateDraft grava um rascunho na pasta de rascunhos e devolve o seu id JMAP. NÃO envia: é a
// separação que faz de `mail.draft` uma ferramenta sem confirmação e de `mail.send` uma com.
func (u *UserClient) CreateDraft(ctx context.Context, d Draft) (string, error) {
	acc, err := u.accountID(ctx)
	if err != nil {
		return "", err
	}
	drafts, err := u.mailboxByRole(ctx, acc, "drafts")
	if err != nil {
		return "", err
	}
	_, from, name, err := u.primaryIdentity(ctx, acc)
	if err != nil {
		return "", err
	}
	return u.createEmail(ctx, acc, drafts, d, fromAddr(from, name))
}

// SendEmail grava o rascunho e submete-o. Devolve o id do rascunho criado, para o registo saber
// sobre o que a ação foi.
//
// O envio é sempre gravar-depois-submeter porque é assim que o JMAP funciona (EmailSubmission
// aponta para um Email que já existe) — e tem uma vantagem: se a submissão falhar, o texto não se
// perde, fica nos rascunhos.
func (u *UserClient) SendEmail(ctx context.Context, d Draft) (string, error) {
	acc, err := u.accountID(ctx)
	if err != nil {
		return "", err
	}
	drafts, err := u.mailboxByRole(ctx, acc, "drafts")
	if err != nil {
		return "", err
	}
	identityID, from, name, err := u.primaryIdentity(ctx, acc)
	if err != nil {
		return "", err
	}
	emailID, err := u.createEmail(ctx, acc, drafts, d, fromAddr(from, name))
	if err != nil {
		return "", err
	}
	rcpts := append(append([]string{}, d.To...), d.Cc...)
	if err := u.SubmitEmail(ctx, emailID, identityID, from, rcpts); err != nil {
		return emailID, err
	}
	return emailID, nil
}

// createEmail cria o Email/set com o corpo em texto simples. from é o "De:" da conta autenticada —
// sem ele o e-mail sairia com remetente vazio ("—").
func (u *UserClient) createEmail(ctx context.Context, acc, mailboxID string, d Draft, from map[string]any) (string, error) {
	if len(d.To) == 0 {
		return "", fmt.Errorf("jmap user: draft has no recipient")
	}
	email := map[string]any{
		"mailboxIds": map[string]any{mailboxID: true},
		"keywords":   map[string]any{"$draft": true},
		"from":       []any{from},
		"to":         addrList(d.To),
		"subject":    d.Subject,
		"bodyValues": map[string]any{"body": map[string]any{"value": d.Text}},
		"textBody":   []any{map[string]any{"partId": "body", "type": "text/plain"}},
	}
	if len(d.Cc) > 0 {
		email["cc"] = addrList(d.Cc)
	}
	if d.InReplyTo != "" {
		email["inReplyTo"] = []string{d.InReplyTo}
	}

	resp, err := u.jmap(ctx, []any{
		[]any{"Email/set", map[string]any{
			"accountId": acc,
			"create":    map[string]any{"d": email},
		}, "s"},
	})
	if err != nil {
		return "", err
	}
	if len(resp) < 1 {
		return "", fmt.Errorf("jmap user: no create response")
	}
	var got struct {
		Created map[string]struct {
			ID string `json:"id"`
		} `json:"created"`
		NotCreated map[string]json.RawMessage `json:"notCreated"`
	}
	if err := json.Unmarshal(resp[0], &got); err != nil {
		return "", err
	}
	if len(got.NotCreated) > 0 {
		return "", fmt.Errorf("jmap user: draft rejected: %s", resp[0])
	}
	created, ok := got.Created["d"]
	if !ok || created.ID == "" {
		return "", fmt.Errorf("jmap user: draft created without id")
	}
	return created.ID, nil
}

// mailboxByRole resolve o id de uma pasta pelo papel JMAP (inbox, drafts, sent, trash).
func (u *UserClient) mailboxByRole(ctx context.Context, acc, role string) (string, error) {
	resp, err := u.jmap(ctx, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": acc, "ids": nil, "properties": []string{"role"}}, "m"},
	})
	if err != nil {
		return "", err
	}
	if len(resp) < 1 {
		return "", fmt.Errorf("jmap user: no mailbox response")
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
		if m.Role == role {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("jmap user: no %s mailbox", role)
}

// primaryIdentity devolve a identidade de envio da conta: id, endereço e nome do "De:". O assistente
// NUNCA escolhe o remetente: envia como a pessoa autenticada e mais nada (§6.2). O nome é preciso
// para o "De:" não sair vazio no e-mail — sem ele, o destinatário via só "—".
func (u *UserClient) primaryIdentity(ctx context.Context, acc string) (id, email, name string, err error) {
	raw, err := u.do(ctx, "/jmap/", map[string]any{
		"using": []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:submission"},
		"methodCalls": []any{
			[]any{"Identity/get", map[string]any{"accountId": acc}, "i"},
		},
	})
	if err != nil {
		return "", "", "", err
	}
	var responses [][]json.RawMessage
	if err := json.Unmarshal(raw["methodResponses"], &responses); err != nil {
		return "", "", "", err
	}
	if len(responses) == 0 || len(responses[0]) < 2 {
		return "", "", "", fmt.Errorf("jmap user: no identity response")
	}
	var got struct {
		List []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"list"`
	}
	if err := json.Unmarshal(responses[0][1], &got); err != nil {
		return "", "", "", err
	}
	if len(got.List) == 0 {
		return "", "", "", fmt.Errorf("jmap user: account has no send identity")
	}
	return got.List[0].ID, got.List[0].Email, got.List[0].Name, nil
}

// fromAddr monta o endereço do "De:" na forma JMAP. Nome vazio → só o endereço (melhor do que "—").
func fromAddr(email, name string) map[string]any {
	a := map[string]any{"email": email}
	if strings.TrimSpace(name) != "" {
		a["name"] = name
	}
	return a
}

// addrList traduz endereços de texto para a forma JMAP. Aceita "Nome <a@b>" e "a@b".
func addrList(in []string) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, raw := range in {
		name, email := splitAddr(raw)
		a := map[string]any{"email": email}
		if name != "" {
			a["name"] = name
		}
		out = append(out, a)
	}
	return out
}

func splitAddr(raw string) (name, email string) {
	s := strings.TrimSpace(raw)
	open := strings.LastIndex(s, "<")
	close := strings.LastIndex(s, ">")
	if open >= 0 && close > open {
		return strings.TrimSpace(strings.Trim(s[:open], ` "`)), strings.TrimSpace(s[open+1 : close])
	}
	return "", s
}
