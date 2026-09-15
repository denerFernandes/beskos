package assistant

import (
	"context"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/ai"
)

// recordingCal regista os eventos que lhe mandaram criar, para se afirmar o que foi (ou não) criado.
type recordingCal struct{ created []NewEvent }

func (c *recordingCal) create(_ context.Context, _ Identity, ev NewEvent) (EventRef, error) {
	c.created = append(c.created, ev)
	return EventRef{ID: "ev-1", Title: ev.Title}, nil
}

func calRegistry(cal *recordingCal) *Registry { return BuildRegistry(Deps{Calendar: cal.create}) }

func calCall(args string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "c1", Name: ToolCalendarCreate, Arguments: args}}, FinishReason: "tool_calls"}
}

// Um evento A SOLO fica na agenda do próprio: não sai da organização e não pede confirmação —
// exigi-la aqui gastaria a atenção de quem confirma no que não importa.
func TestCalendarSoloEventNeedsNoConfirmation(t *testing.T) {
	cal := &recordingCal{}
	conf := &scriptedConfirmer{approve: false} // recusaria, se fosse perguntado
	args := `{"title":"Foco","starts_at":"2026-07-22T10:00:00Z"}`
	eng := NewEngine(&fakeProvider{script: []ai.Response{calCall(args), textResp("criado")}},
		calRegistry(cal), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "cria um bloco de foco")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 0 {
		t.Errorf("evento a solo pediu confirmação (%d) — não devia", len(conf.asked))
	}
	if len(cal.created) != 1 {
		t.Fatalf("eventos criados = %d, quero 1", len(cal.created))
	}
	if run.Steps[0].ErrorCode != "" {
		t.Errorf("passo com erro: %+v", run.Steps[0])
	}
}

// INVARIANTE (§6.3): convidar avisa OUTRA pessoa, portanto pede confirmação. Recusada, o evento NÃO
// é criado — não se convida ninguém sem aprovação.
func TestCalendarWithGuestsRequiresConfirmation(t *testing.T) {
	cal := &recordingCal{}
	conf := &scriptedConfirmer{approve: false}
	args := `{"title":"Reunião","starts_at":"2026-07-22T10:00:00Z","invitees":["bob@acme.pt"]}`
	eng := NewEngine(&fakeProvider{script: []ai.Response{calCall(args), textResp("não criei")}},
		calRegistry(cal), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "marca reunião com o bob")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 1 {
		t.Errorf("convites pedidos à confirmação = %d, quero 1", len(conf.asked))
	}
	if len(cal.created) != 0 {
		t.Fatalf("CONVIDOU SEM APROVAÇÃO: %+v", cal.created)
	}
	if run.Steps[0].ErrorCode != "assistant.confirm_declined" {
		t.Errorf("passo = %+v, quero a recusa registada", run.Steps[0])
	}
}

// Aprovada, cria o evento COM os convidados que foram mostrados.
func TestCalendarWithGuestsApprovedCreatesWithInvitees(t *testing.T) {
	cal := &recordingCal{}
	conf := &scriptedConfirmer{approve: true}
	args := `{"title":"Reunião","starts_at":"2026-07-22T10:00:00Z","invitees":["bob@acme.pt"]}`
	eng := NewEngine(&fakeProvider{script: []ai.Response{calCall(args), textResp("criado")}},
		calRegistry(cal), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	if _, err := eng.Run(context.Background(), testIdentity(), ag, "marca reunião com o bob"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 1 {
		t.Errorf("confirmações = %d, quero 1", len(conf.asked))
	}
	if len(cal.created) != 1 || len(cal.created[0].Invitees) != 1 || cal.created[0].Invitees[0] != "bob@acme.pt" {
		t.Fatalf("evento criado sem os convidados certos: %+v", cal.created)
	}
}
