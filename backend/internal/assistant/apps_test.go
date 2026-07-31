package assistant

import "testing"

// Desligar uma app tem de cortar as ferramentas dela ao assistente — é o que o cliente pediu, e o
// que separa "some do menu" de "a IA deixa mesmo de conseguir".
func TestAllowedToolsDropsDisabledApps(t *testing.T) {
	tools := []string{ToolMailSend, ToolContactsSearch, ToolCalendarCreate, ToolTasksCreate, ToolDocsCreate}

	// Nada desligado → tudo fica.
	if got := AllowedTools(tools, nil); len(got) != len(tools) {
		t.Fatalf("sem apps desligadas, esperava tudo: %v", got)
	}

	// Contactos e Calendário desligados → as ferramentas deles saem; o resto fica.
	got := AllowedTools(tools, []string{"contacts", "cal"})
	has := map[string]bool{}
	for _, g := range got {
		has[g] = true
	}
	if has[ToolContactsSearch] || has[ToolCalendarCreate] {
		t.Errorf("ferramentas de app desligada continuaram: %v", got)
	}
	if !has[ToolMailSend] || !has[ToolTasksCreate] || !has[ToolDocsCreate] {
		t.Errorf("cortou ferramentas de apps LIGADAS: %v", got)
	}
}

// O agente de Documentos vive dos Ficheiros: sem essa app, o agente sai do encaminhamento.
func TestAgentAvailableFollowsApps(t *testing.T) {
	if !AgentAvailable(AgentDocs, nil) {
		t.Error("sem apps desligadas, o agente de docs devia estar disponível")
	}
	if AgentAvailable(AgentDocs, []string{"files"}) {
		t.Error("com Ficheiros desligado, o agente de docs NÃO devia estar disponível")
	}
	if !AgentAvailable(AgentGeneral, []string{"files"}) {
		t.Error("o agente geral não depende de uma só app")
	}
}

// O teto de ferramentas é uma restrição: tira, nunca acrescenta. Um engano ao contrário aqui daria
// à IA uma capacidade que a organização desligou de propósito.
func TestWithoutToolsSoRemove(t *testing.T) {
	tools := []string{ToolMailSearch, ToolMailSend, ToolWikiRead}
	got := WithoutTools(tools, []string{ToolMailSend})
	if len(got) != 2 {
		t.Fatalf("esperava 2 ferramentas, veio %d (%v)", len(got), got)
	}
	for _, g := range got {
		if g == ToolMailSend {
			t.Error("a ferramenta desligada continuou na lista")
		}
	}
	// Desligar algo que o agente não tem não pode acrescentá-lo.
	got = WithoutTools([]string{ToolWikiRead}, []string{ToolMailSend, ToolDocsEdit})
	if len(got) != 1 || got[0] != ToolWikiRead {
		t.Errorf("lista alterada indevidamente: %v", got)
	}
	// Sem nada desligado, a lista vem intacta.
	if len(WithoutTools(tools, nil)) != len(tools) {
		t.Error("sem bloqueios, a lista tem de ficar igual")
	}
}

// Turning off the Notes app has to take the note tools away from the assistant too. The admin
// toggle says "the AI assistant loses access"; a tool that survived it would make that a lie, and
// the person who switched the app off would have no way of knowing.
func TestNotesAppGatesItsTools(t *testing.T) {
	all := []string{ToolNotesSearch, ToolNotesRead, ToolNotesCreate, ToolNotesEdit, ToolMailSearch}
	got := AllowedTools(all, []string{"notes"})
	if len(got) != 1 || got[0] != ToolMailSearch {
		t.Errorf("with Notes off, %v survived; only the mail tool should have", got)
	}
}

// And with the app on, nothing is removed.
func TestNotesToolsSurviveWhenTheAppIsOn(t *testing.T) {
	all := []string{ToolNotesSearch, ToolNotesEdit}
	if got := AllowedTools(all, []string{"wiki"}); len(got) != 2 {
		t.Errorf("switching the wiki off must not remove note tools: %v", got)
	}
}
