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
	"notes":    {ToolNotesSearch, ToolNotesRead, ToolNotesCreate, ToolNotesEdit},
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

// WithoutTools tira da lista as ferramentas que a organização desligou. É um TETO: só remove, nunca
// acrescenta — nem para agentes criados pelo cliente, que já são validados contra o catálogo. Serve
// o corte fino ("a IA nunca envia e-mail") sem tocar no que cada agente é (§3.2, decisão 3).
func WithoutTools(tools []string, blocked []string) []string {
	if len(blocked) == 0 || len(tools) == 0 {
		return tools
	}
	off := make(map[string]bool, len(blocked))
	for _, b := range blocked {
		off[b] = true
	}
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if !off[t] {
			out = append(out, t)
		}
	}
	return out
}

// AllTools é o catálogo completo, para a governança poder listar o que há para desligar.
func AllTools() []string {
	return []string{
		ToolMailSearch, ToolMailRead, ToolMailDraft, ToolMailSend,
		ToolContactsSearch, ToolContactsCreate, ToolContactsShare,
		ToolDriveSearch, ToolDocsCreate, ToolDocsRead, ToolDocsEdit, ToolDocsShare,
		ToolTasksCreate, ToolCalendarCreate,
		ToolWikiSearch, ToolWikiRead, ToolWikiCreatePage, ToolWikiEditPage,
	}
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
