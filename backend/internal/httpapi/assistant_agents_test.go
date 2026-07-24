package httpapi

import (
	"strings"
	"testing"

	"mailin/backend/internal/assistant"
)

// INVARIANTE (§3.2): um agente criado pelo cliente nunca pode ter mais ferramentas do que o teto —
// validado NO SERVIDOR, não na interface. E a recusa é explícita: filtrar em silêncio daria à
// pessoa um agente diferente do que ela julga ter criado.
func TestClientAgentCannotExceedTheToolCeiling(t *testing.T) {
	req := agentWriteReq{Name: "Vendas", Tools: []string{"mail.search", "drive.delete_everything"}}
	if code := req.validate(); code != "assistant.agent_tool_not_allowed" {
		t.Errorf("validate = %q, quero a recusa da ferramenta fora do teto", code)
	}
}

// O que está no teto passa — senão a validação seria só um "não" a tudo.
func TestClientAgentAcceptsToolsWithinTheCeiling(t *testing.T) {
	req := agentWriteReq{Name: "Vendas", Tools: assistant.MaxTools()}
	if code := req.validate(); code != "" {
		t.Errorf("validate = %q, quero aceitar o teto completo", code)
	}
}

// capTools é a segunda barreira, na LEITURA: se uma versão nossa remover uma ferramenta do
// catálogo, um perfil gravado há meses não pode continuar a concedê-la.
func TestCapToolsDropsWhatIsNoLongerAllowed(t *testing.T) {
	got := capTools([]string{"mail.search", "ferramenta.que.ja.nao.existe", "drive.search"})
	if len(got) != 2 || got[0] != "mail.search" || got[1] != "drive.search" {
		t.Errorf("capTools = %v, quero só as que continuam no teto", got)
	}
	// Um perfil só com ferramentas que já não existem fica sem nenhuma — não com todas.
	if got := capTools([]string{"nada.disto.existe"}); len(got) != 0 {
		t.Errorf("capTools = %v, quero vazio", got)
	}
}

func TestClientAgentNameIsRequiredAndBounded(t *testing.T) {
	cases := map[string]agentWriteReq{
		"nome vazio":   {Name: "   "},
		"nome gigante": {Name: strings.Repeat("a", agentNameMax+1)},
	}
	for name, req := range cases {
		if code := req.validate(); code != "assistant.agent_name_invalid" {
			t.Errorf("%s: validate = %q", name, code)
		}
	}
	// Instruções gigantes pagam-se a cada turno — daí o teto.
	long := agentWriteReq{Name: "ok", Instructions: strings.Repeat("x", agentInstructionsMax+1)}
	if code := long.validate(); code != "assistant.agent_too_long" {
		t.Errorf("instruções longas: validate = %q", code)
	}
}

// O teto é o do agente geral: nenhum perfil do cliente pode ir além do que o produto já permite.
func TestToolCeilingIsTheGeneralAgent(t *testing.T) {
	general, _ := assistant.Builtin(assistant.AgentGeneral)
	max := assistant.MaxTools()
	if len(max) != len(general.Tools) {
		t.Fatalf("teto = %v, agente geral = %v", max, general.Tools)
	}
	// E é uma CÓPIA: mexer no que MaxTools devolve não pode mexer no catálogo.
	max[0] = "adulterado"
	if again := assistant.MaxTools(); again[0] == "adulterado" {
		t.Error("MaxTools devolve o slice interno — o catálogo ficou adulterável")
	}
}
