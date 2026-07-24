package mailbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// EmailHeader é o cabeçalho de uma mensagem devolvido por uma pesquisa. Sem corpo: quem quiser
// o conteúdo pede-o à parte (Read), para não arrastar megabytes numa listagem.
type EmailHeader struct {
	ID         string    `json:"id"`
	ThreadID   string    `json:"thread_id"`
	From       string    `json:"from"`
	To         []string  `json:"to"`
	Subject    string    `json:"subject"`
	Preview    string    `json:"preview"`
	ReceivedAt time.Time `json:"received_at"`
	HasAttach  bool      `json:"has_attachment"`
}

// EmailBody é o conteúdo de uma mensagem. Text é o corpo em texto simples; o HTML não é devolvido
// de propósito, porque quem consome isto é um modelo de linguagem e a marcação só adiciona ruído
// (e superfície de injeção — §6.1).
type EmailBody struct {
	EmailHeader
	Text        string   `json:"text"`
	Attachments []string `json:"attachments"`
}

// bodyCharLimit trava o corpo devolvido. Um e-mail com um anexo em texto de 10 MB estouraria a
// janela de contexto do modelo e a fatura junto.
const bodyCharLimit = 20000

// SearchEmails procura mensagens por texto livre em toda a caixa (JMAP Email/query com filtro
// "text", que o Stalwart aplica a remetente, assunto e corpo). Mais recentes primeiro.
//
// query vazia devolve as mensagens mais recentes da conta, o que é o comportamento útil para
// "o que chegou hoje?".
func (u *UserClient) SearchEmails(ctx context.Context, query string, limit int) ([]EmailHeader, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	acc, err := u.accountID(ctx)
	if err != nil {
		return nil, err
	}
	filter := map[string]any{}
	if q := strings.TrimSpace(query); q != "" {
		filter["text"] = q
	}
	resp, err := u.jmap(ctx, []any{
		[]any{"Email/query", map[string]any{
			"accountId": acc, "filter": filter,
			"sort":  []any{map[string]any{"property": "receivedAt", "isAscending": false}},
			"limit": limit,
		}, "q"},
		[]any{"Email/get", map[string]any{
			"accountId": acc, "#ids": map[string]any{"resultOf": "q", "name": "Email/query", "path": "/ids"},
			"properties": []string{"id", "threadId", "from", "to", "subject", "preview", "receivedAt", "hasAttachment"},
		}, "g"},
	})
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 {
		return nil, fmt.Errorf("jmap user: incomplete search response")
	}
	var got struct {
		List []wireEmail `json:"list"`
	}
	if err := json.Unmarshal(resp[1], &got); err != nil {
		return nil, err
	}
	out := make([]EmailHeader, 0, len(got.List))
	for _, e := range got.List {
		out = append(out, e.header())
	}
	return out, nil
}

// ReadEmail devolve o cabeçalho e o corpo em texto de uma mensagem. O id vem de uma pesquisa
// anterior; se não existir (ou for de outra conta), o Stalwart devolve lista vazia e isto dá erro
// de não encontrado — nunca o conteúdo de outra caixa.
func (u *UserClient) ReadEmail(ctx context.Context, id string) (EmailBody, error) {
	if strings.TrimSpace(id) == "" {
		return EmailBody{}, fmt.Errorf("jmap user: empty email id")
	}
	acc, err := u.accountID(ctx)
	if err != nil {
		return EmailBody{}, err
	}
	resp, err := u.jmap(ctx, []any{
		[]any{"Email/get", map[string]any{
			"accountId": acc, "ids": []string{id},
			"properties": []string{"id", "threadId", "from", "to", "subject", "preview", "receivedAt",
				"hasAttachment", "textBody", "bodyValues", "attachments"},
			"fetchTextBodyValues": true,
			"maxBodyValueBytes":   bodyCharLimit,
		}, "g"},
	})
	if err != nil {
		return EmailBody{}, err
	}
	if len(resp) < 1 {
		return EmailBody{}, fmt.Errorf("jmap user: incomplete read response")
	}
	var got struct {
		List []wireEmail `json:"list"`
	}
	if err := json.Unmarshal(resp[0], &got); err != nil {
		return EmailBody{}, err
	}
	if len(got.List) == 0 {
		return EmailBody{}, fmt.Errorf("jmap user: email %s not found", id)
	}
	e := got.List[0]
	body := EmailBody{EmailHeader: e.header(), Text: e.text()}
	for _, a := range e.Attachments {
		name := a.Name
		if name == "" {
			name = a.Type
		}
		if name != "" {
			body.Attachments = append(body.Attachments, name)
		}
	}
	return body, nil
}

// wireEmail é a forma JMAP de um Email, com só os campos que pedimos.
type wireEmail struct {
	ID            string      `json:"id"`
	ThreadID      string      `json:"threadId"`
	Subject       string      `json:"subject"`
	Preview       string      `json:"preview"`
	ReceivedAt    string      `json:"receivedAt"`
	HasAttachment bool        `json:"hasAttachment"`
	From          []wireAddr  `json:"from"`
	To            []wireAddr  `json:"to"`
	TextBody      []wirePart  `json:"textBody"`
	BodyValues    wireBodyMap `json:"bodyValues"`
	Attachments   []wirePart  `json:"attachments"`
}

type wireAddr struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type wirePart struct {
	PartID string `json:"partId"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

type wireBodyMap map[string]struct {
	Value       string `json:"value"`
	IsTruncated bool   `json:"isTruncated"`
}

func (e wireEmail) header() EmailHeader {
	h := EmailHeader{
		ID: e.ID, ThreadID: e.ThreadID, Subject: e.Subject,
		Preview: e.Preview, HasAttach: e.HasAttachment,
	}
	h.ReceivedAt, _ = time.Parse(time.RFC3339, e.ReceivedAt)
	if len(e.From) > 0 {
		h.From = e.From[0].display()
	}
	for _, a := range e.To {
		h.To = append(h.To, a.display())
	}
	return h
}

func (a wireAddr) display() string {
	if a.Name != "" && a.Email != "" {
		return a.Name + " <" + a.Email + ">"
	}
	if a.Name != "" {
		return a.Name
	}
	return a.Email
}

// text junta as partes de texto do corpo, resolvendo cada partId no mapa bodyValues.
func (e wireEmail) text() string {
	var b strings.Builder
	for _, p := range e.TextBody {
		v, ok := e.BodyValues[p.PartID]
		if !ok {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(v.Value)
	}
	s := b.String()
	if len(s) > bodyCharLimit {
		s = s[:bodyCharLimit] + "\n[...truncated]"
	}
	return s
}
