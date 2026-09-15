package assistant

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/ai"
)

// failingProvider falha sempre — serve para provar que a dúvida cai no agente mais fraco.
type failingProvider struct{}

func (failingProvider) Name() string { return "boom" }
func (failingProvider) Complete(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{}, errors.New("modelo indisponível")
}

// allBuiltins são os agentes de origem como o encaminhamento os vê.
func allBuiltins() []AgentChoice {
	out := make([]AgentChoice, 0, 3)
	for _, k := range BuiltinKeys() {
		ag, _ := Builtin(k)
		out = append(out, AgentChoice{Key: ag.Key, Purpose: ag.Purpose})
	}
	return out
}

func TestRouterPicksTheNamedAgent(t *testing.T) {
	for _, want := range []string{AgentInbox, AgentDocs, AgentGeneral} {
		r := NewRouter(&fakeProvider{script: []ai.Response{textResp(want)}})
		if got := r.Pick(context.Background(), "faz qualquer coisa", allBuiltins()); got != want {
			t.Errorf("Pick = %q, quero %q", got, want)
		}
	}
}

// O modelo é desarrumado: devolve pontuação, maiúsculas, aspas. Isso não pode virar um fallback
// silencioso, senão o encaminhamento nunca funcionaria e ninguém daria por isso.
func TestRouterToleratesUntidyAnswers(t *testing.T) {
	for _, raw := range []string{"Inbox", " inbox ", "inbox.", `"inbox"`} {
		r := NewRouter(&fakeProvider{script: []ai.Response{textResp(raw)}})
		if got := r.Pick(context.Background(), "resume os e-mails de ontem", allBuiltins()); got != AgentInbox {
			t.Errorf("Pick(%q) = %q, quero %q", raw, got, AgentInbox)
		}
	}
}

// EM CASO DE DÚVIDA, o de menor poder de escrita (§3.2). Erro do modelo, resposta inventada,
// resposta vazia, mensagem vazia: tudo cai no assistente geral, nunca no mais capaz.
func TestRouterFallsBackToTheWeakestAgent(t *testing.T) {
	cases := map[string]*Router{
		"erro do modelo":   NewRouter(failingProvider{}),
		"agente inventado": NewRouter(&fakeProvider{script: []ai.Response{textResp("superagente")}}),
		"resposta vazia":   NewRouter(&fakeProvider{script: []ai.Response{textResp("")}}),
		"sem provider":     NewRouter(nil),
		"router nulo":      func() *Router { var r *Router; return r }(),
	}
	for name, r := range cases {
		if got := r.Pick(context.Background(), "algo ambíguo", allBuiltins()); got != AgentGeneral {
			t.Errorf("%s: Pick = %q, quero %q", name, got, AgentGeneral)
		}
	}
	// Mensagem vazia não vale a pena gastar uma chamada ao modelo.
	r := NewRouter(&fakeProvider{script: []ai.Response{textResp(AgentDocs)}})
	if got := r.Pick(context.Background(), "   ", allBuiltins()); got != AgentGeneral {
		t.Errorf("mensagem vazia: Pick = %q, quero %q", got, AgentGeneral)
	}
}

// INVARIANTE (§3.2): o encaminhamento SELECIONA de entre o que já era permitido — nunca acrescenta
// um agente fora da lista. Um modelo que devolva "docs" quando "docs" não está disponível não pode
// conceder acesso a ele.
func TestRouterNeverPicksAnUnavailableAgent(t *testing.T) {
	r := NewRouter(&fakeProvider{script: []ai.Response{textResp(AgentDocs)}})
	only := []AgentChoice{{Key: AgentGeneral}, {Key: AgentInbox}}
	if got := r.Pick(context.Background(), "faz uma apresentação", only); got != AgentGeneral {
		t.Errorf("Pick = %q — escolheu um agente que não estava disponível", got)
	}
}

// INVARIANTE (§3.2): a decisão sai SÓ da mensagem do utilizador. O Router não recebe resultados de
// ferramentas — nem tem por onde os receber —, e não expõe ferramentas ao modelo no passo de
// encaminhamento. Se um dia alguém lhe acrescentar um parâmetro, este teste é o que dá o alerta.
func TestRouterSeesOnlyTheUserMessage(t *testing.T) {
	p := &fakeProvider{script: []ai.Response{textResp(AgentInbox)}}
	r := NewRouter(p)
	msg := "resume os e-mails de ontem"
	r.Pick(context.Background(), msg, allBuiltins())

	if len(p.lastTools) != 0 {
		t.Errorf("o passo de encaminhamento ofereceu ferramentas ao modelo: %v", p.lastTools)
	}
	if len(p.lastMsgs) != 2 {
		t.Fatalf("mensagens = %d, quero só o sistema e a mensagem do utilizador", len(p.lastMsgs))
	}
	if p.lastMsgs[1].Role != ai.RoleUser || p.lastMsgs[1].Content != msg {
		t.Errorf("o que foi encaminhado não é a mensagem do utilizador: %+v", p.lastMsgs[1])
	}
	// A assinatura é a defesa: um parâmetro a mais aqui seria uma porta para conteúdo lido.
	pick := reflect.TypeOf(r.Pick)
	if pick.NumIn() != 3 || pick.In(1).Kind() != reflect.String || pick.In(2) != reflect.TypeOf([]AgentChoice{}) {
		t.Errorf("Pick mudou de assinatura (%v) — só pode receber contexto, a mensagem e a lista de agentes", pick)
	}
	// E um AgentChoice só leva a chave e a finalidade: as instruções e as ferramentas não entram no
	// prompt de encaminhamento, portanto também não há por aí uma porta para conteúdo lido.
	if n := reflect.TypeOf(AgentChoice{}).NumField(); n != 2 {
		t.Errorf("AgentChoice tem %d campos — só pode levar a chave e a finalidade", n)
	}
}

// O CRITÉRIO DA FASE: o agente de correio não consegue tocar no Drive. Não é uma instrução do
// prompt — é a lista de permissões, validada no servidor a cada chamada.
func TestInboxAgentCannotTouchDrive(t *testing.T) {
	inbox, ok := Builtin(AgentInbox)
	if !ok {
		t.Fatal("o agente de correio não existe")
	}
	if allows(inbox, ToolDriveSearch) {
		t.Error("o agente de correio tem drive.search na lista de permissões")
	}
	// E o simétrico: o de documentos não lê a caixa.
	docs, _ := Builtin(AgentDocs)
	for _, mailTool := range []string{ToolMailSearch, ToolMailRead, ToolMailDraft, ToolMailSend} {
		if allows(docs, mailTool) {
			t.Errorf("o agente de documentos tem %s na lista de permissões", mailTool)
		}
	}
}

// Nenhum agente de origem tem mais ferramentas do que o geral: o encaminhamento nunca leva a
// pessoa a um perfil com mais poder do que o de partida.
func TestNoBuiltinExceedsTheGeneralAgent(t *testing.T) {
	general, _ := Builtin(AgentGeneral)
	for _, key := range BuiltinKeys() {
		ag, _ := Builtin(key)
		for _, tool := range ag.Tools {
			if !allows(general, tool) {
				t.Errorf("o agente %s tem %s, que o geral não tem", key, tool)
			}
		}
	}
}

// A execução recebe o agente JÁ escolhido. Não há troca a meio: o motor não conhece o Router, e é
// isso que impede uma escalada desencadeada por conteúdo lido a meio de uma execução (§3.2).
func TestEngineDoesNotKnowTheRouter(t *testing.T) {
	eng := NewEngine(&fakeProvider{script: []ai.Response{textResp("ok")}}, mailRegistry(stubBox{}), nil, 0)
	et := reflect.TypeOf(*eng)
	for i := 0; i < et.NumField(); i++ {
		if et.Field(i).Type == reflect.TypeOf(&Router{}) {
			t.Error("o motor tem um Router — o encaminhamento tem de ficar fora do ciclo")
		}
	}
}

// O modelo às vezes troca separadores no nome da ferramenta ("wiki.edit.page"). A correção resolve
// para o canónico SÓ dentro da lista do agente — nunca dá acesso a ferramenta de fora da lista.
func TestCanonicalToolNameToleratesSeparators(t *testing.T) {
	ag := Agent{Tools: []string{"wiki.edit_page", "mail.send"}}
	if got := canonicalToolName(ag, "wiki.edit.page"); got != "wiki.edit_page" {
		t.Errorf("want wiki.edit_page, got %q", got)
	}
	if got := canonicalToolName(ag, "wiki_edit_page"); got != "wiki.edit_page" {
		t.Errorf("want wiki.edit_page, got %q", got)
	}
	if got := canonicalToolName(ag, "mail.send"); got != "mail.send" {
		t.Errorf("exact name must pass through, got %q", got)
	}
	if got := canonicalToolName(ag, "drive.search"); got != "drive.search" {
		t.Errorf("tool fora da lista não pode ser 'corrigida': got %q", got)
	}
}
