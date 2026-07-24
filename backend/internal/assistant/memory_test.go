package assistant

import (
	"context"
	"strings"
	"testing"

	"mailin/backend/internal/ai"
)

// INVARIANTE (§6.5): categorias proibidas são recusadas mesmo que o modelo as classifique como
// factos normais. A rede de segurança do servidor tem a última palavra — não se confia só no modelo.
func TestForbiddenCategoriesAreRefusedEvenIfModelAcceptsThem(t *testing.T) {
	cases := map[string]string{
		"a senha do wifi é abc123":                   refuseCredential,
		"o meu token de API é sk-12345":              refuseCredential,
		"o cartão é 4111 1111 1111 1111":             refuseFinancial,
		"IBAN PT50000201231234567890154":             refuseFinancial,
		"tenho uma consulta sobre a minha depressão": refuseHealth,
		"sou do partido X":                           refuseHealth,
	}
	for text, wantReason := range cases {
		// O modelo aceita tudo como facto "preferência". A rede é que recusa.
		facts, refusals := parseExtraction(`{"facts":[{"fact":"` + text + `","category":"preferência"}],"forbidden":[]}`)
		if len(facts) != 0 {
			t.Errorf("%q foi guardado apesar de ser proibido: %+v", text, facts)
		}
		if len(refusals) != 1 || refusals[0].Reason != wantReason {
			t.Errorf("%q: recusa = %+v, quero razão %q", text, refusals, wantReason)
		}
	}
}

// Quando recusa, DI-LO (§6.5): a recusa volta com um facto e uma razão, para o assistente a mostrar
// em vez de ignorar em silêncio.
func TestRefusalCarriesAReason(t *testing.T) {
	_, refusals := parseExtraction(`{"facts":[],"forbidden":["a minha senha é 1234"]}`)
	if len(refusals) != 1 {
		t.Fatalf("recusas = %d, quero 1", len(refusals))
	}
	if refusals[0].Reason == "" || refusals[0].Text == "" {
		t.Error("a recusa não tem razão nem texto — o utilizador não saberia porquê")
	}
}

// Factos normais passam, com a categoria validada.
func TestNormalFactsArePreserved(t *testing.T) {
	facts, refusals := parseExtraction(`{"facts":[{"fact":"mora em Belo Horizonte","category":"local"},{"fact":"assina como Ana","category":"preferência"}],"forbidden":[]}`)
	if len(refusals) != 0 {
		t.Errorf("recusou factos normais: %+v", refusals)
	}
	if len(facts) != 2 || facts[0].Text != "mora em Belo Horizonte" || facts[0].Category != "local" {
		t.Errorf("factos = %+v", facts)
	}
	// Uma categoria inválida cai no default em vez de ser recusada: o facto é bom, só a etiqueta é
	// que veio errada.
	f2, _ := parseExtraction(`{"facts":[{"fact":"prefere reuniões curtas","category":"inventada"}],"forbidden":[]}`)
	if len(f2) != 1 || f2[0].Category != "preferência" {
		t.Errorf("categoria inválida = %+v, quero cair em preferência", f2)
	}
}

// INVARIANTE (§8.2): a extração lê SÓ a mensagem do utilizador. Nunca resultados de ferramentas,
// nunca a resposta do assistente. É a defesa contra o envenenamento permanente da memória.
func TestExtractionReadsOnlyTheUserMessage(t *testing.T) {
	p := &fakeProvider{script: []ai.Response{textResp(`{"facts":[],"forbidden":[]}`)}}
	ExtractMemory(context.Background(), p, "moro em Belo Horizonte")

	if len(p.lastMsgs) != 2 {
		t.Fatalf("mensagens = %d, quero só o sistema e a mensagem do utilizador", len(p.lastMsgs))
	}
	if p.lastMsgs[1].Role != ai.RoleUser || p.lastMsgs[1].Content != "moro em Belo Horizonte" {
		t.Errorf("a extração viu outra coisa além da mensagem do utilizador: %+v", p.lastMsgs[1])
	}
	// E não oferece ferramentas: a extração não age, só lê.
	if len(p.lastTools) != 0 {
		t.Error("a extração ofereceu ferramentas ao modelo")
	}
}

// INVARIANTE (§6.5): a memória entra no prompt como CONTEXTO, nunca como instruções. A moldura tem
// de o dizer, para o modelo não tratar um facto como uma ordem.
func TestMemoryEntersAsContextNotInstructions(t *testing.T) {
	block := MemoryContext([]Fact{{Text: "mora em Belo Horizonte"}})
	if !strings.Contains(block, "mora em Belo Horizonte") {
		t.Error("o facto não entrou no bloco")
	}
	low := strings.ToLower(block)
	if !strings.Contains(low, "context") {
		t.Error("o bloco não se marca como contexto")
	}
	if !strings.Contains(low, "never grant tools") && !strings.Contains(low, "never") {
		t.Error("o bloco não avisa que os factos não concedem ferramentas nem aprovam ações")
	}
	// Sem factos, sem bloco: não se enche o prompt com uma moldura vazia.
	if MemoryContext(nil) != "" {
		t.Error("MemoryContext de vazio devia ser vazio")
	}
}

// A memória chega ao prompt do sistema numa execução real, e como contexto.
func TestMemoryIsInjectedIntoTheSystemPrompt(t *testing.T) {
	p := &fakeProvider{script: []ai.Response{textResp("olá")}}
	eng := NewEngine(p, mailRegistry(stubBox{}), nil, 0)
	ag, _ := Builtin(AgentGeneral)

	eng.RunStream(context.Background(), testIdentity(), ag, RunOptions{
		Message: "que horas são as reuniões?",
		Memory:  []Fact{{Text: "mora em Belo Horizonte", Category: "local"}},
	}, nil)

	if len(p.lastMsgs) == 0 || p.lastMsgs[0].Role != ai.RoleSystem {
		t.Fatal("sem prompt de sistema")
	}
	if !strings.Contains(p.lastMsgs[0].Content, "mora em Belo Horizonte") {
		t.Error("a memória não foi injetada no prompt do sistema")
	}
}

// Uma resposta de extração ilegível não guarda nada nem rebenta: a memória é um extra.
func TestBadExtractionYieldsNothing(t *testing.T) {
	facts, refusals := parseExtraction("desculpa, não percebi")
	if len(facts) != 0 || len(refusals) != 0 {
		t.Errorf("uma resposta ilegível devia dar nada: %+v %+v", facts, refusals)
	}
	// Mas JSON embrulhado em texto ou cerca de código ainda é lido.
	f, _ := parseExtraction("Claro! ```json\n{\"facts\":[{\"fact\":\"mora no Porto\",\"category\":\"local\"}],\"forbidden\":[]}\n``` pronto")
	if len(f) != 1 || f[0].Text != "mora no Porto" {
		t.Errorf("JSON embrulhado não foi lido: %+v", f)
	}
}
