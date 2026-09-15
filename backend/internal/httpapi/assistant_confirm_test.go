package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/assistant"
)

// ask corre o Ask numa goroutine e devolve um canal com o resultado.
func ask(c *assistantConfirms, ctx context.Context, id assistant.Identity, req assistant.ConfirmRequest) <-chan bool {
	out := make(chan bool, 1)
	go func() {
		ok, _ := c.Ask(ctx, id, req)
		out <- ok
	}()
	// Dá tempo ao Ask de se registar antes de alguém decidir.
	for i := 0; i < 100; i++ {
		c.mu.Lock()
		_, registered := c.pending[req.ID]
		c.mu.Unlock()
		if registered {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return out
}

func testIdent() assistant.Identity {
	return assistant.Identity{TenantID: uuid.New(), UserID: uuid.New()}
}

func TestConfirmApprovalReachesTheWaitingRun(t *testing.T) {
	c := newAssistantConfirms()
	id := testIdent()
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, context.Background(), id, req)

	if !c.decide(req.ID, id.TenantID, id.UserID, true) {
		t.Fatal("decide não encontrou a ação à espera")
	}
	if !<-done {
		t.Error("a aprovação não chegou à execução")
	}
}

// INVARIANTE (§6.2/lei 5.1): a aprovação é de quem tem a ação. Outro utilizador — mesmo do mesmo
// tenant — não aprova o envio de ninguém.
func TestConfirmRejectsAnotherUser(t *testing.T) {
	c := newAssistantConfirms()
	id := testIdent()
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, context.Background(), id, req)

	if c.decide(req.ID, id.TenantID, uuid.New(), true) {
		t.Error("um colega aprovou o envio de outra pessoa")
	}
	if c.decide(req.ID, uuid.New(), id.UserID, true) {
		t.Error("outro tenant aprovou uma ação que não é dele")
	}
	// A ação continua à espera: as tentativas alheias não a decidiram nem a consumiram.
	if !c.decide(req.ID, id.TenantID, id.UserID, true) {
		t.Fatal("a ação foi consumida por uma tentativa alheia")
	}
	if !<-done {
		t.Error("a aprovação legítima não passou")
	}
}

// Aprovar duas vezes não envia duas vezes: a segunda decisão não encontra nada.
func TestConfirmIsSingleUse(t *testing.T) {
	c := newAssistantConfirms()
	id := testIdent()
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, context.Background(), id, req)

	if !c.decide(req.ID, id.TenantID, id.UserID, true) {
		t.Fatal("a primeira decisão devia passar")
	}
	<-done
	if c.decide(req.ID, id.TenantID, id.UserID, true) {
		t.Error("a segunda aprovação passou — isso enviaria o mesmo e-mail duas vezes")
	}
}

// O utilizador fechou o separador: a espera acaba em RECUSA, nunca em aprovação por omissão.
func TestConfirmCancelledContextIsNotApproval(t *testing.T) {
	c := newAssistantConfirms()
	ctx, cancel := context.WithCancel(context.Background())
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, ctx, testIdent(), req)

	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Error("um contexto cancelado foi tratado como aprovação")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask não reagiu ao cancelamento")
	}
}

// Uma ação decidida deixa de ocupar memória — senão um painel deixado aberto acumularia esperas.
func TestConfirmCleansUpAfterDecision(t *testing.T) {
	c := newAssistantConfirms()
	id := testIdent()
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, context.Background(), id, req)
	c.decide(req.ID, id.TenantID, id.UserID, false)
	<-done

	for i := 0; i < 100; i++ {
		c.mu.Lock()
		n := len(c.pending)
		c.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Error("a ação decidida ficou registada")
}
