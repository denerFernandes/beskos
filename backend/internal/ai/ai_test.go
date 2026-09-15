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

// fakeProvider returns a canned reply and captures the last Request (network-agnostic).
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
		t.Errorf("unexpected summary: %q", out)
	}
	// The body/subject must reach the provider.
	if !strings.Contains(fp.last.Messages[1].Content, "Corpo do e-mail") {
		t.Errorf("prompt does not contain the email body")
	}
	if fp.last.Messages[0].Role != RoleSystem {
		t.Errorf("first message must be system")
	}
}

func TestAssistantExtractTasks(t *testing.T) {
	fp := &fakeProvider{reply: "```json\n[{\"title\":\"Revisar contrato\",\"due\":\"hoje\",\"assignee\":\"ana@acme.com\"}]\n```"}
	a := NewAssistant(fp, "pt-BR")
	people := []Person{{Name: "Ana Silva", Email: "ana@acme.com"}}
	tasks, err := a.ExtractTasks(context.Background(), "Contract", "Review by today", people)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Revisar contrato" || tasks[0].Due != "hoje" {
		t.Errorf("unexpected tasks: %+v", tasks)
	}
	if tasks[0].Assignee != "ana@acme.com" {
		t.Errorf("assignee = %q, want the address from the roster", tasks[0].Assignee)
	}
}

// An address the model made up assigns the work to nobody while the screen claims it was assigned.
// Blank is the honest answer, and the one that makes someone pick an owner.
func TestExtractTasksDropsInventedAssignee(t *testing.T) {
	fp := &fakeProvider{reply: `[{"title":"X","due":"","assignee":"quemquer@outra.com"}]`}
	a := NewAssistant(fp, "pt-BR")
	tasks, err := a.ExtractTasks(context.Background(), "s", "b", []Person{{Name: "Ana", Email: "ana@acme.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Assignee != "" {
		t.Errorf("assignee = %q, want it blank", tasks[0].Assignee)
	}
}

// The roster has to reach the model, or it can only echo the name written in the text -- which is
// the whole reason "Dener" never matched a user displayed as "D Fernandes".
func TestExtractTasksSendsTheRoster(t *testing.T) {
	fp := &fakeProvider{reply: "[]"}
	a := NewAssistant(fp, "pt-BR")
	_, _ = a.ExtractTasks(context.Background(), "s", "b", []Person{{Name: "D Fernandes", Email: "df@acme.com"}})
	if !strings.Contains(fp.last.Messages[0].Content, "df@acme.com") {
		t.Errorf("the prompt does not carry the roster: %q", fp.last.Messages[0].Content)
	}
}

func TestAssistantExtractTasksBadJSON(t *testing.T) {
	fp := &fakeProvider{reply: "sorry, I couldn't"}
	a := NewAssistant(fp, "pt-BR")
	if _, err := a.ExtractTasks(context.Background(), "x", "y", nil); err == nil {
		t.Errorf("expected error for non-JSON response")
	}
}

func TestNewProviderSelection(t *testing.T) {
	if _, err := New(Config{Provider: "deepseek", APIKey: "k"}); err != nil {
		t.Errorf("deepseek should build: %v", err)
	}
	if _, err := New(Config{Provider: "deepseek"}); err == nil {
		t.Errorf("should fail without an API key")
	}
	if _, err := New(Config{Provider: "inexistente", APIKey: "k"}); err == nil {
		t.Errorf("unknown provider should fail")
	}
	// TensorX is what keeps the content inside the EU. Without this case, startup says "unknown
	// provider", AI is silently disabled, and the app answers 503 — that is what happened.
	p, err := New(Config{Provider: "tensorx", APIKey: "k"})
	if err != nil {
		t.Fatalf("tensorx should build: %v", err)
	}
	d, ok := p.(*DeepSeek)
	if !ok {
		t.Fatalf("tensorx should use the OpenAI-compatible client, got %T", p)
	}
	if d.baseURL != "https://api.tensorx.ai/v1" {
		t.Errorf("baseURL = %q", d.baseURL)
	}
	if d.model == "" {
		t.Error("default model missing")
	}
	// The startup log writes this name. Saying "deepseek" while talking to TensorX makes the
	// operator believe content leaves the EU when it does not — and the opposite, the day it does.
	if p.Name() != "tensorx" {
		t.Errorf("Name() = %q, want tensorx", p.Name())
	}
	if dd, _ := New(Config{Provider: "deepseek", APIKey: "k"}); dd.Name() != "deepseek" {
		t.Errorf("deepseek: Name() = %q", dd.Name())
	}
}

// Tests DeepSeek against a fake OpenAI-compatible server: validates the sent payload and parsing.
func TestDeepSeekComplete(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"deepseek-v4","choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"total_tokens":42}}`))
	}))
	defer srv.Close()

	d := NewDeepSeek("secret-key", srv.URL, "deepseek-v4")
	resp, err := d.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello" {
		t.Errorf("text: %q", resp.Text)
	}
	if resp.Usage.TotalTokens != 42 {
		t.Errorf("usage: %+v", resp.Usage)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("auth header: %q", gotAuth)
	}
	if gotModel != "deepseek-v4" {
		t.Errorf("model sent: %q", gotModel)
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
		t.Errorf("expected an auth error, got: %v", err)
	}
}
