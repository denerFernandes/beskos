package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/mailbox"
	"github.com/beskosorg/beskos/backend/internal/office"
)

// recordingBox regista o que lhe mandaram enviar, para se poder afirmar que NADA saiu.
type recordingBox struct {
	mu    sync.Mutex
	sent  []mailbox.Draft
	saved []mailbox.Draft
}

func (b *recordingBox) SearchEmails(context.Context, string, int) ([]mailbox.EmailHeader, error) {
	return nil, nil
}
func (b *recordingBox) ReadEmail(context.Context, string) (mailbox.EmailBody, error) {
	return mailbox.EmailBody{}, nil
}
func (b *recordingBox) CreateDraft(_ context.Context, d mailbox.Draft) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.saved = append(b.saved, d)
	return "draft-1", nil
}
func (b *recordingBox) SendEmail(_ context.Context, d mailbox.Draft) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, d)
	return "sent-1", nil
}

// scriptedConfirmer responde sempre o mesmo e guarda o que lhe perguntaram.
type scriptedConfirmer struct {
	approve bool
	err     error
	asked   []ConfirmRequest
}

func (c *scriptedConfirmer) Ask(_ context.Context, _ Identity, req ConfirmRequest) (bool, error) {
	c.asked = append(c.asked, req)
	return c.approve, c.err
}

func sendCall(args string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "s1", Name: ToolMailSend, Arguments: args}}, FinishReason: "tool_calls"}
}

const sendArgs = `{"to":["ana@exemplo.pt"],"subject":"Fatura","text":"Segue a fatura."}`

// INVARIANTE (§6.3): nada sai sem aprovação. Recusada, a ferramenta NÃO corre — e o modelo recebe a
// recusa como observação, para poder dizer ao utilizador em vez de tentar outra vez às cegas.
func TestSendRefusedWithoutApproval(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: false}
	audit := &memAudit{}
	eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("não enviei")}},
		mailRegistry(box), audit, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "envia a fatura à Ana")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(box.sent) != 0 {
		t.Fatalf("SAIU CORREIO SEM APROVAÇÃO: %+v", box.sent)
	}
	if len(run.Steps) != 1 || run.Steps[0].ErrorCode != "assistant.confirm_declined" {
		t.Errorf("passo = %+v, quero a recusa registada como código de i18n", run.Steps)
	}
	// A recusa fica na auditoria: "porque é que este e-mail NÃO foi enviado?" também tem de ter
	// resposta (§6.4).
	if len(audit.entries) != 1 || audit.entries[0].Error == "" {
		t.Errorf("auditoria = %+v, quero a recusa registada", audit.entries)
	}
}

// FAIL-CLOSED: sem Confirmer ligado, uma ferramenta que sai para fora é recusada — não executada.
// Uma dependência de segurança em falta tem de fechar a porta, não abri-la.
func TestSendRefusedWithoutConfirmerAtAll(t *testing.T) {
	box := &recordingBox{}
	eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("ok")}},
		mailRegistry(box), &memAudit{}, 0) // sem WithConfirmer
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "envia")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(box.sent) != 0 {
		t.Fatalf("SAIU CORREIO SEM MECANISMO DE CONFIRMAÇÃO: %+v", box.sent)
	}
	if run.Steps[0].ErrorCode != "assistant.confirm_unavailable" {
		t.Errorf("código = %q, quero assistant.confirm_unavailable", run.Steps[0].ErrorCode)
	}
}

// Um erro do Confirmer (prazo esgotado, ligação perdida) NÃO é uma aprovação.
func TestConfirmerErrorIsNotApproval(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: true, err: context.DeadlineExceeded}
	eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("ok")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	if _, err := eng.Run(context.Background(), testIdentity(), ag, "envia"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(box.sent) != 0 {
		t.Fatalf("UM ERRO DE CONFIRMAÇÃO DEIXOU SAIR CORREIO: %+v", box.sent)
	}
}

// Aprovada, a ferramenta corre — e corre com os argumentos EXATOS que foram mostrados. Uma
// pré-visualização que divergisse do que corre daria confiança sem a merecer.
func TestApprovedSendRunsTheExactContentShown(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: true}
	eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("enviado")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	var shown *ConfirmRequest
	_, err := eng.RunStream(context.Background(), testIdentity(), ag, RunOptions{Message: "envia"},
		func(ev Event) {
			if ev.Kind == EventConfirm {
				shown = ev.Confirm
			}
		})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	if len(box.sent) != 1 {
		t.Fatalf("envios = %d, quero 1", len(box.sent))
	}
	if box.sent[0].To[0] != "ana@exemplo.pt" || box.sent[0].Text != "Segue a fatura." {
		t.Errorf("enviou outra coisa: %+v", box.sent[0])
	}
	if shown == nil {
		t.Fatal("o utilizador não viu cartão de confirmação nenhum")
	}
	// Os bytes mostrados são os bytes executados, não uma segunda versão formatada.
	var got, want map[string]any
	_ = json.Unmarshal(shown.Args, &got)
	_ = json.Unmarshal([]byte(sendArgs), &want)
	if got["text"] != want["text"] || got["subject"] != want["subject"] {
		t.Errorf("o que foi mostrado (%v) diverge do que correu (%v)", got, want)
	}
	if len(conf.asked) != 1 {
		t.Errorf("pedidos de confirmação = %d, quero exatamente 1 (sem aprovação em bloco)", len(conf.asked))
	}
}

// Cada ação que sai é aprovada INDIVIDUALMENTE: dois envios num turno são duas perguntas.
func TestEachOutgoingActionIsApprovedSeparately(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: true}
	two := ai.Response{ToolCalls: []ai.ToolCall{
		{ID: "s1", Name: ToolMailSend, Arguments: sendArgs},
		{ID: "s2", Name: ToolMailSend, Arguments: `{"to":["bruno@exemplo.pt"],"text":"Olá"}`},
	}, FinishReason: "tool_calls"}
	eng := NewEngine(&fakeProvider{script: []ai.Response{two, textResp("feito")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	if _, err := eng.Run(context.Background(), testIdentity(), ag, "envia aos dois"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 2 {
		t.Errorf("perguntas = %d, quero 2 — não há aprovação em bloco (§6.3)", len(conf.asked))
	}
}

// `mail.draft` grava sem sair da organização, portanto NÃO pede confirmação. Exigi-la aqui gastaria
// a atenção de quem confirma no que não importa — e é essa atenção que protege o envio.
func TestDraftDoesNotAskForConfirmation(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: false}
	draft := ai.Response{ToolCalls: []ai.ToolCall{{ID: "d1", Name: ToolMailDraft, Arguments: sendArgs}}, FinishReason: "tool_calls"}
	eng := NewEngine(&fakeProvider{script: []ai.Response{draft, textResp("rascunho pronto")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "escreve um rascunho")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 0 {
		t.Errorf("um rascunho não sai da organização e não devia pedir confirmação")
	}
	if len(box.saved) != 1 {
		t.Fatalf("rascunhos gravados = %d, quero 1", len(box.saved))
	}
	if len(box.sent) != 0 {
		t.Fatal("mail.draft ENVIOU — a separação entre gravar e enviar é o que sustenta a A2")
	}
	if run.Steps[0].ErrorCode != "" {
		t.Errorf("passo com erro: %+v", run.Steps[0])
	}
}

// O catálogo tem de manter a marcação certa: o que sai para fora leva Confirm, o resto não. Este
// teste é o que impede que uma ferramenta nova entre sem a marca.
func TestOutgoingToolsAreMarkedConfirm(t *testing.T) {
	reg := BuildRegistry(Deps{
		Mail:  func(context.Context, Identity) (MailBox, error) { return &recordingBox{}, nil },
		Drive: func(context.Context, Identity, string, int) ([]DriveHit, error) { return nil, nil },
		Docs:  func(context.Context, Identity, string, office.Plan) (DriveHit, error) { return DriveHit{}, nil },
		DocsShare: func(context.Context, Identity, string, []string, string) (ShareResult, error) {
			return ShareResult{}, nil
		},
		Tasks: func(context.Context, Identity, NewTask) (TaskRef, error) { return TaskRef{}, nil },
	})
	// Sai da organização ou destrói → confirmação obrigatória. Partilhar dá acesso a outra pessoa.
	mustConfirm := map[string]bool{ToolMailSend: true, ToolDocsShare: true}
	for _, name := range reg.Names() {
		tool, _ := reg.Get(name)
		if want := mustConfirm[name]; tool.Confirm != want {
			t.Errorf("%s: Confirm = %v, quero %v", name, tool.Confirm, want)
		}
		// Uma ferramenta que escreve mas não sai continua a ter de estar marcada como escrita.
		if strings.HasSuffix(name, ".search") || strings.HasSuffix(name, ".read") {
			if tool.Writes {
				t.Errorf("%s é de leitura mas está marcada como escrita", name)
			}
		}
	}
}

// WithConfirmer não pode mexer no motor de origem: o worker e o servidor partilham-no, e cada um
// precisa do seu — um durável, outro em memória. Se mutasse, ligar um desligaria o outro.
func TestWithConfirmerDoesNotMutateTheOriginal(t *testing.T) {
	base := NewEngine(&fakeProvider{script: []ai.Response{textResp("ok")}}, mailRegistry(&recordingBox{}), nil, 0)
	if base.confirm != nil {
		t.Fatal("o motor de base já vem com um Confirmer")
	}
	a := base.WithConfirmer(&scriptedConfirmer{approve: true})
	b := base.WithConfirmer(&scriptedConfirmer{approve: false})

	if base.confirm != nil {
		t.Error("WithConfirmer alterou o motor de origem")
	}
	if a.confirm == b.confirm {
		t.Error("as duas cópias partilham o mesmo Confirmer")
	}
	// E a cópia mantém tudo o resto: um teto de passos perdido aqui seria uma falha de segurança
	// silenciosa (§9.1).
	if a.maxSteps != base.maxSteps || a.registry != base.registry || a.provider != base.provider {
		t.Error("a cópia perdeu configuração do motor de origem")
	}
}
