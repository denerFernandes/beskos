package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeProvider devolve uma resposta canned e captura o último Request (agnóstico de rede).
type fakeProvider struct {
	reply string
	last  Request
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Complete(_ context.Context, req Request) (Response, error) {
	f.last = req
	return Response{Text: f.reply, Model: "fake-1"}, nil
}

func TestAssistantSummarize(t *testing.T) {
	fp := &fakeProvider{reply: "• Ponto 1\n• Ponto 2"}
	a := NewAssistant(fp, "pt-BR")
	out, err := a.SummarizeEmail(context.Background(), "Assunto X", "Corpo do e-mail")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Ponto 1") {
		t.Errorf("resumo inesperado: %q", out)
	}
	// O corpo/assunto devem chegar ao provider.
	if !strings.Contains(fp.last.Messages[1].Content, "Corpo do e-mail") {
		t.Errorf("prompt não contém o corpo do e-mail")
	}
	if fp.last.Messages[0].Role != RoleSystem {
		t.Errorf("primeira mensagem deve ser system")
	}
}

func TestAssistantExtractTasks(t *testing.T) {
	fp := &fakeProvider{reply: "```json\n[{\"title\":\"Revisar contrato\",\"due\":\"hoje\",\"assignee\":\"Ana\"}]\n```"}
	a := NewAssistant(fp, "pt-BR")
	tasks, err := a.ExtractTasks(context.Background(), "Contrato", "Revisar até hoje")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Revisar contrato" || tasks[0].Due != "hoje" {
		t.Errorf("tarefas inesperadas: %+v", tasks)
	}
}

func TestAssistantExtractTasksBadJSON(t *testing.T) {
	fp := &fakeProvider{reply: "desculpe, não consegui"}
	a := NewAssistant(fp, "pt-BR")
	if _, err := a.ExtractTasks(context.Background(), "x", "y"); err == nil {
		t.Errorf("esperava erro para resposta não-JSON")
	}
}

func TestNewProviderSelection(t *testing.T) {
	if _, err := New(Config{Provider: "deepseek", APIKey: "k"}); err != nil {
		t.Errorf("deepseek deveria construir: %v", err)
	}
	if _, err := New(Config{Provider: "deepseek"}); err == nil {
		t.Errorf("sem API key deveria falhar")
	}
	if _, err := New(Config{Provider: "inexistente", APIKey: "k"}); err == nil {
		t.Errorf("provider desconhecido deveria falhar")
	}
	// A TensorX é o que mantém o conteúdo dentro da UE. Sem este case, o arranque diz "provider
	// desconhecido", a IA fica desligada em silêncio e a app responde 503 — foi o que aconteceu.
	p, err := New(Config{Provider: "tensorx", APIKey: "k"})
	if err != nil {
		t.Fatalf("tensorx deveria construir: %v", err)
	}
	d, ok := p.(*DeepSeek)
	if !ok {
		t.Fatalf("tensorx deveria usar o cliente OpenAI-compatível, veio %T", p)
	}
	if d.baseURL != "https://api.tensorx.ai/v1" {
		t.Errorf("baseURL = %q", d.baseURL)
	}
	if d.model == "" {
		t.Error("modelo por omissão em falta")
	}
	// O log de arranque escreve este nome. Dizer "deepseek" a falar com a TensorX faz o operador
	// acreditar que o conteúdo sai da UE quando não sai — e o contrário, no dia em que sair.
	if p.Name() != "tensorx" {
		t.Errorf("Name() = %q, quero tensorx", p.Name())
	}
	if dd, _ := New(Config{Provider: "deepseek", APIKey: "k"}); dd.Name() != "deepseek" {
		t.Errorf("deepseek: Name() = %q", dd.Name())
	}
}

// Testa o DeepSeek contra um servidor OpenAI-compatível fake: valida payload enviado e parsing.
func TestDeepSeekComplete(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path inesperado: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"deepseek-v4","choices":[{"message":{"role":"assistant","content":"olá"}}],"usage":{"total_tokens":42}}`))
	}))
	defer srv.Close()

	d := NewDeepSeek("secret-key", srv.URL, "deepseek-v4")
	resp, err := d.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "oi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "olá" {
		t.Errorf("texto: %q", resp.Text)
	}
	if resp.Usage.TotalTokens != 42 {
		t.Errorf("usage: %+v", resp.Usage)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("auth header: %q", gotAuth)
	}
	if gotModel != "deepseek-v4" {
		t.Errorf("model enviado: %q", gotModel)
	}
}

func TestDeepSeekError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key","type":"auth"}}`))
	}))
	defer srv.Close()
	d := NewDeepSeek("bad", srv.URL, "deepseek-v4")
	if _, err := d.Complete(context.Background(), Request{}); err == nil || !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("esperava erro de auth, veio: %v", err)
	}
}
