package assistant

// App gating: desligar uma app na organização corta o acesso do assistente às ferramentas dessa app
// e, se houver, ao agente de origem que depende dela. É a mesma decisão do rail — uma app desligada
// não existe para ninguém, nem para a IA (pedido do cliente).

// AppTools mapeia cada app do rail para as ferramentas do assistente que ela habilita. Apps sem
// ferramentas (chat, today, meet) não entram.
var AppTools = map[string][]string{
	"mail":     {ToolMailSearch, ToolMailRead, ToolMailDraft, ToolMailSend},
	"contacts": {ToolContactsSearch, ToolContactsCreate},
	"cal":      {ToolCalendarCreate},
	"files":    {ToolDriveSearch, ToolDocsCreate, ToolDocsRead, ToolDocsEdit, ToolDocsShare},
	"kanban":   {ToolTasksCreate},
	"wiki":     {ToolWikiSearch, ToolWikiRead, ToolWikiCreatePage, ToolWikiEditPage},
}

// AppAgent mapeia uma app ao agente de ORIGEM que depende dela: desligar a app tira o agente do
// encaminhamento (ex.: sem Ficheiros, não há agente de Documentos; sem Correio, não há o de Caixa).
var AppAgent = map[string]string{
	"files": AgentDocs,
	"mail":  AgentInbox,
	"wiki":  AgentKnowledge,
}

// AllowedTools filtra a lista de ferramentas de um agente, removendo as das apps desligadas.
func AllowedTools(tools []string, disabledApps []string) []string {
	if len(disabledApps) == 0 {
		return tools
	}
	off := map[string]bool{}
	for _, app := range disabledApps {
		for _, t := range AppTools[app] {
			off[t] = true
		}
	}
	if len(off) == 0 {
		return tools
	}
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if !off[t] {
			out = append(out, t)
		}
	}
	return out
}

// AgentAvailable diz se um agente de origem continua disponível dadas as apps desligadas.
func AgentAvailable(agentKey string, disabledApps []string) bool {
	for app, ag := range AppAgent {
		if ag != agentKey {
			continue
		}
		for _, d := range disabledApps {
			if d == app {
				return false
			}
		}
	}
	return true
}
