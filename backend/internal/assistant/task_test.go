package assistant

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/ai"
)

// memEffects é um registo de efeitos em memória — o mesmo papel que a tabela
// assistant_task_effects, mas sem base de dados.
type memEffects struct {
	mu   sync.Mutex
	done map[string]string
}

func newMemEffects() *memEffects { return &memEffects{done: map[string]string{}} }

func (m *memEffects) Seen(_ context.Context, fp string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.done[fp]
	return s, ok, nil
}

func (m *memEffects) Record(_ context.Context, fp, summary string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done[fp] = summary
	return nil
}

// memStore recolhe o progresso e o desfecho, e responde ao pedido de cancelamento.
type memStore struct {
	mu       sync.Mutex
	progress [][]Step
	result   TaskResult
	cancel   bool
	// cancelAfter manda cancelar depois de N passos.
	cancelAfter int
}

func (m *memStore) Progress(_ context.Context, _ uuid.UUID, _ string, steps []Step) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Step, len(steps))
	copy(cp, steps)
	m.progress = append(m.progress, cp)
	if m.cancelAfter > 0 && len(m.progress) >= m.cancelAfter {
		m.cancel = true
	}
	return nil
}

func (m *memStore) Cancelled(context.Context, uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancel, nil
}

func (m *memStore) Finish(_ context.Context, _ uuid.UUID, res TaskResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.result = res
	return nil
}

func testTask(ag Agent, msg string) Task {
	return Task{ID: uuid.New(), Identity: testIdentity(), Agent: ag, Message: msg}
}

// INVARIANTE (lei 5.3, §7.5): uma retoma NÃO reenvia o mesmo e-mail. A tarefa reinicia o ciclo do
// princípio; a guarda de efeitos reconhece o envio já feito e não o repete.
func TestResumeDoesNotSendTwice(t *testing.T) {
	box := &recordingBox{}
	effects := newMemEffects()
	ag, _ := Builtin(AgentGeneral)
	script := []ai.Response{sendCall(sendArgs), textResp("enviado")}

	// Primeira passagem: o e-mail sai.
	eng := NewEngine(&fakeProvider{script: script}, mailRegistry(box), &memAudit{}, 0).
		WithConfirmer(&scriptedConfirmer{approve: true})
	res := RunTask(context.Background(), eng, &memStore{}, effects, testTask(ag, "envia a fatura"))
	if res.Status != TaskDone {
		t.Fatalf("primeira passagem: estado = %q", res.Status)
	}
	if len(box.sent) != 1 {
		t.Fatalf("envios na primeira passagem = %d, quero 1", len(box.sent))
	}

	// O servidor foi abaixo; a tarefa volta à fila e reexecuta com o MESMO registo de efeitos.
	eng2 := NewEngine(&fakeProvider{script: script}, mailRegistry(box), &memAudit{}, 0).
		WithConfirmer(&scriptedConfirmer{approve: true})
	res2 := RunTask(context.Background(), eng2, &memStore{}, effects, testTask(ag, "envia a fatura"))
	if res2.Status != TaskDone {
		t.Fatalf("retoma: estado = %q", res2.Status)
	}
	if len(box.sent) != 1 {
		t.Fatalf("A RETOMA REENVIOU: envios = %d, quero continuar em 1", len(box.sent))
	}
}

// Sem registo de efeitos (execução síncrona), a repetição acontece — e deve: aí é uma ação nova do
// utilizador, não uma retoma de máquina. Este teste existe para deixar claro que a guarda é
// deliberadamente limitada às tarefas, e não um acidente.
func TestWithoutEffectLogARerunActuallyReruns(t *testing.T) {
	box := &recordingBox{}
	ag, _ := Builtin(AgentGeneral)
	for i := 0; i < 2; i++ {
		eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("ok")}},
			mailRegistry(box), &memAudit{}, 0).WithConfirmer(&scriptedConfirmer{approve: true})
		if _, err := eng.Run(context.Background(), testIdentity(), ag, "envia"); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	if len(box.sent) != 2 {
		t.Errorf("envios = %d, quero 2 — sem registo de efeitos não há deduplicação", len(box.sent))
	}
}

// Dois envios DIFERENTES na mesma tarefa saem os dois. A guarda impede a repetição, não a
// pluralidade — se impedisse, uma tarefa de "responde a estas três pessoas" mandaria uma só.
func TestDifferentWritesInTheSameTaskAllHappen(t *testing.T) {
	box := &recordingBox{}
	ag, _ := Builtin(AgentGeneral)
	two := ai.Response{ToolCalls: []ai.ToolCall{
		{ID: "s1", Name: ToolMailSend, Arguments: sendArgs},
		{ID: "s2", Name: ToolMailSend, Arguments: `{"to":["bruno@exemplo.pt"],"text":"Olá"}`},
	}, FinishReason: "tool_calls"}
	eng := NewEngine(&fakeProvider{script: []ai.Response{two, textResp("feito")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(&scriptedConfirmer{approve: true})

	RunTask(context.Background(), eng, &memStore{}, newMemEffects(), testTask(ag, "responde aos dois"))
	if len(box.sent) != 2 {
		t.Errorf("envios = %d, quero 2 — são pedidos diferentes", len(box.sent))
	}
}

// A impressão digital não depende da ordem das chaves nem dos espaços: o modelo emite o mesmo
// pedido de formas diferentes entre execuções, e duas formas do mesmo envio não podem passar por
// dois envios.
func TestFingerprintIgnoresKeyOrderAndWhitespace(t *testing.T) {
	a := effectFingerprint(ToolMailSend, json.RawMessage(`{"to":["ana@x.pt"],"subject":"Oi","text":"corpo"}`))
	b := effectFingerprint(ToolMailSend, json.RawMessage("{ \"text\":\"corpo\",\n \"subject\" : \"Oi\", \"to\": [\"ana@x.pt\"] }"))
	if a != b {
		t.Error("a mesma mensagem escrita de outra forma deu outra impressão digital")
	}
	// Mas um destinatário diferente é outro efeito.
	c := effectFingerprint(ToolMailSend, json.RawMessage(`{"to":["bruno@x.pt"],"subject":"Oi","text":"corpo"}`))
	if a == c {
		t.Error("destinatários diferentes deram a mesma impressão digital")
	}
	// E a mesma carga por outra ferramenta também.
	d := effectFingerprint(ToolMailDraft, json.RawMessage(`{"to":["ana@x.pt"],"subject":"Oi","text":"corpo"}`))
	if a == d {
		t.Error("ferramentas diferentes deram a mesma impressão digital")
	}
}

// O progresso é gravado a CADA passo. Sem isto não há retoma nem barra de progresso — e a promessa
// de §3.6 ("a bolha mostra progresso mesmo fechada") não teria de onde sair.
func TestTaskSavesProgressEveryStep(t *testing.T) {
	store := &memStore{}
	ag, _ := Builtin(AgentGeneral)
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		toolCallResp(ToolMailSearch, `{"query":"a"}`),
		toolCallResp(ToolMailSearch, `{"query":"b"}`),
		textResp("pronto"),
	}}, mailRegistry(stubBox{}), &memAudit{}, 0)

	RunTask(context.Background(), eng, store, newMemEffects(), testTask(ag, "procura"))
	if len(store.progress) != 2 {
		t.Fatalf("gravações de progresso = %d, quero uma por passo (2)", len(store.progress))
	}
	if len(store.progress[0]) != 1 || len(store.progress[1]) != 2 {
		t.Errorf("o progresso não é cumulativo: %v", store.progress)
	}
	if store.result.Status != TaskDone || store.result.Answer != "pronto" {
		t.Errorf("desfecho = %+v", store.result)
	}
}

// Cancelar pára a tarefa — e o que já foi feito FICA registado (§7.5). Uma tarefa cancelada sem
// história não deixaria ninguém perceber o que aconteceu antes de parar.
func TestCancelStopsAndKeepsWhatWasDone(t *testing.T) {
	store := &memStore{cancelAfter: 1}
	ag, _ := Builtin(AgentGeneral)
	// O modelo pediria ferramentas indefinidamente; o cancelamento é que pára isto.
	eng := NewEngine(&fakeProvider{script: []ai.Response{toolCallResp(ToolMailSearch, `{"query":"x"}`)}},
		mailRegistry(stubBox{}), &memAudit{}, 0)

	res := RunTask(context.Background(), eng, store, newMemEffects(), testTask(ag, "organiza tudo"))
	if res.Status != TaskCancelled {
		t.Errorf("estado = %q, quero %q", res.Status, TaskCancelled)
	}
	if len(res.Steps) == 0 {
		t.Error("a tarefa cancelada não guardou nada do que já tinha feito")
	}
	if store.result.Status != TaskCancelled {
		t.Errorf("o desfecho gravado = %q", store.result.Status)
	}
}

// Uma tarefa cancelada não pode continuar a produzir efeitos depois de parar.
func TestCancelledTaskStopsCallingTools(t *testing.T) {
	box := &recordingBox{}
	store := &memStore{cancelAfter: 1}
	ag, _ := Builtin(AgentGeneral)
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		toolCallResp(ToolMailSearch, `{"query":"x"}`),
		sendCall(sendArgs), // se o ciclo continuasse, isto sairia
	}}, mailRegistry(box), &memAudit{}, 0).WithConfirmer(&scriptedConfirmer{approve: true})

	RunTask(context.Background(), eng, store, newMemEffects(), testTask(ag, "organiza e envia"))
	if len(box.sent) != 0 {
		t.Errorf("SAIU CORREIO DEPOIS DO CANCELAMENTO: %+v", box.sent)
	}
}
