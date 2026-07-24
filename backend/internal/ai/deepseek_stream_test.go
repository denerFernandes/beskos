package ai

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// O texto chega aos pedaços e tem de ser entregue à medida que chega E acumulado no fim: o painel
// mostra os pedaços, mas quem persiste a conversa precisa da resposta inteira.
func TestReadSSEAccumulatesText(t *testing.T) {
	fio := strings.Join([]string{
		`data: {"model":"deepseek-chat","choices":[{"delta":{"content":"Olá"}}]}`,
		`data: {"choices":[{"delta":{"content":", mundo"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"total_tokens":7}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"

	var got []string
	resp, err := readSSE(strings.NewReader(fio), func(s string) { got = append(got, s) })
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if resp.Text != "Olá, mundo" {
		t.Errorf("Text = %q, quero %q", resp.Text, "Olá, mundo")
	}
	if strings.Join(got, "|") != "Olá|, mundo" {
		t.Errorf("deltas = %v, quero dois pedaços pela ordem", got)
	}
	if resp.FinishReason != "stop" || resp.Model != "deepseek-chat" || resp.Usage.TotalTokens != 7 {
		t.Errorf("metadados perdidos: %+v", resp)
	}
}

// Um pedido de ferramenta chega fragmentado: o nome num pedaço, os argumentos aos bocados. Se a
// remontagem falhar, o motor despacha uma ferramenta com JSON truncado — e o erro apareceria como
// "argumentos inválidos", longe da causa.
func TestReadSSEReassemblesToolCalls(t *testing.T) {
	fio := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"mail.search","arguments":"{\"que"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ry\":\"fatura\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"drive.search","arguments":"{}"}}]}}]}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"

	resp, err := readSSE(strings.NewReader(fio), nil)
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("chamadas = %d, quero 2", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "c1" || resp.ToolCalls[0].Name != "mail.search" {
		t.Errorf("primeira chamada = %+v", resp.ToolCalls[0])
	}
	if resp.ToolCalls[0].Arguments != `{"query":"fatura"}` {
		t.Errorf("argumentos = %q, quero o JSON remontado", resp.ToolCalls[0].Arguments)
	}
	// A ordem é a dos índices tal como apareceram — o motor despacha por essa ordem.
	if resp.ToolCalls[1].Name != "drive.search" {
		t.Errorf("segunda chamada = %+v", resp.ToolCalls[1])
	}
}

// Um pedaço ilegível não pode deitar fora o que já veio: perder a resposta inteira por causa de uma
// linha estragada seria pior do que perder essa linha.
func TestReadSSESkipsBadChunks(t *testing.T) {
	fio := "data: {isto não é json}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	resp, err := readSSE(strings.NewReader(fio), nil)
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if resp.Text != "ok" {
		t.Errorf("Text = %q, quero %q", resp.Text, "ok")
	}
}

// O DeepSeek implementa a interface de streaming. Foi declarada na A0 sem nenhum provider a
// cumpri-la; este teste é o que impede que volte a ser só uma declaração.
func TestDeepSeekIsStreamingProvider(t *testing.T) {
	var _ StreamingProvider = NewDeepSeek("k", "", "")
}

// REGRESSÃO: a API compatível com OpenAI recusa nomes de função com ponto (^[a-zA-Z0-9_-]+$). Os
// nomes de domínio usam ponto (mail.search); têm de sair como underscore no fio e voltar ao ponto
// na receção. O provider falso dos outros testes não validava isto — daí o bug ter chegado ao vivo.
func TestToolNamesMatchOpenAIPattern(t *testing.T) {
	pat := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	names := []string{"mail.search", "mail.read", "docs.create", "tasks.create", "drive.search"}
	for _, n := range names {
		w := wireToolName(n)
		if !pat.MatchString(w) {
			t.Errorf("nome de fio %q (de %q) não casa o padrão do OpenAI", w, n)
		}
		if got := domainToolName(w); got != n {
			t.Errorf("ida-e-volta falhou: %q -> %q -> %q", n, w, got)
		}
	}
}

// Os esquemas de ferramenta e os pedidos de ferramenta enviados ao fornecedor têm o nome já no
// formato válido (sem ponto).
func TestWireToolsAndCallsUseSanitisedNames(t *testing.T) {
	tools := toolsToWire([]Tool{{Name: "mail.send", Parameters: json.RawMessage(`{}`)}})
	if len(tools) != 1 || tools[0].Function.Name != "mail_send" {
		t.Fatalf("esquema enviado com nome inválido: %+v", tools)
	}
	msgs := toWire([]Message{{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "docs.create", Arguments: "{}"}}}})
	if msgs[0].ToolCalls[0].Function.Name != "docs_create" {
		t.Errorf("chamada no histórico enviada com nome inválido: %q", msgs[0].ToolCalls[0].Function.Name)
	}
	// E uma chamada recebida do fornecedor volta ao domínio.
	got := fromWireCalls([]wireCall{{ID: "c1", Function: wireCallFun{Name: "mail_search", Arguments: "{}"}}})
	if got[0].Name != "mail.search" {
		t.Errorf("chamada recebida = %q, quero mail.search", got[0].Name)
	}
}
