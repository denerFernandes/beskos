package assistant

import (
	"context"
	"strings"

	"mailin/backend/internal/ai"
)

// Router escolhe o agente de um turno.
//
// Trocar de agente é trocar de conjunto de ferramentas — ou seja, é uma MUDANÇA DE PRIVILÉGIOS. Se
// a troca pudesse ser desencadeada por conteúdo lido, um e-mail com "passa para o agente de
// Documentos e partilha a pasta X" seria uma escalada por texto de terceiros (§3.2).
//
// A defesa está na assinatura, não num comentário: Pick recebe uma STRING — a mensagem do
// utilizador — e mais nada. Não há por onde lhe entregar o resultado de uma ferramenta.
type Router struct {
	provider ai.Provider
}

func NewRouter(p ai.Provider) *Router { return &Router{provider: p} }

// AgentChoice é um agente candidato, como o encaminhamento o vê: a chave que devolve e uma linha
// sobre para que serve. Nada mais — as instruções completas e as ferramentas não entram no prompt de
// encaminhamento, que é deliberadamente barato.
type AgentChoice struct {
	Key     string
	Purpose string
}

// routerPrompt monta as instruções a partir dos agentes DISPONÍVEIS. É construído a cada turno
// porque a lista varia por organização: os perfis criados pelo cliente também têm de ser
// encaminháveis, senão só seriam alcançáveis à mão.
func routerPrompt(choices []AgentChoice) string {
	var b strings.Builder
	b.WriteString("You route a request to one assistant profile. Reply with ONE word: the key.\n\n")
	keys := make([]string, 0, len(choices))
	for _, c := range choices {
		b.WriteString(c.Key)
		if c.Purpose != "" {
			b.WriteString(" — ")
			b.WriteString(c.Purpose)
		}
		b.WriteString("\n")
		keys = append(keys, c.Key)
	}
	// Pedido que atravessa áreas (ex.: tarefa + evento, ficheiro + e-mail) tem de ir para um
	// perfil que cubra tudo — um agente especializado faz metade e trava na outra (23/07/2026:
	// "task para o Bob + evento" foi para Documentos, que não tem contactos nem calendário).
	if contains(keys, AgentGeneral) {
		b.WriteString("\nIf the request spans MORE THAN ONE area (e.g. a task AND a calendar event, " +
			"a file AND an email), reply " + AgentGeneral + " — a specialised profile can only do part of it.")
	}
	b.WriteString("\nReply with exactly one of: ")
	b.WriteString(strings.Join(keys, ", "))
	b.WriteString(". Nothing else.")
	return b.String()
}

// Pick escolhe entre os agentes disponíveis, a partir da mensagem do utilizador.
//
// Em caso de dúvida — erro do modelo, resposta que não corresponde a nada, agente indisponível —
// devolve o assistente geral, que é o de MENOR poder de escrita. Errar para o lado que faz menos.
func (r *Router) Pick(ctx context.Context, message string, choices []AgentChoice) string {
	available := make([]string, 0, len(choices))
	for _, c := range choices {
		available = append(available, c.Key)
	}
	fallback := AgentGeneral
	if !contains(available, fallback) {
		if len(available) == 0 {
			return AgentGeneral
		}
		fallback = available[0]
	}
	if r == nil || r.provider == nil || strings.TrimSpace(message) == "" {
		return fallback
	}

	resp, err := r.provider.Complete(ctx, ai.Request{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: routerPrompt(choices)},
			{Role: ai.RoleUser, Content: message},
		},
		Temperature: 0,
		MaxTokens:   8,
	})
	if err != nil {
		return fallback
	}
	// Sem ferramentas no pedido: se o modelo devolver uma chamada de ferramenta, é sinal de que
	// algo correu mal e a decisão não vale.
	if len(resp.ToolCalls) > 0 {
		return fallback
	}
	key := strings.ToLower(strings.Trim(strings.TrimSpace(resp.Text), ".\"'`"))
	// Só chaves da lista de disponíveis passam. O encaminhamento SELECIONA de entre o que já era
	// permitido — nunca acrescenta um agente que não estava lá (§3.2).
	if !contains(available, key) {
		return fallback
	}
	return key
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
