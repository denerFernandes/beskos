package assistant

import (
	"context"

	"mailin/backend/internal/ai"
)

// Tipos de evento emitidos durante uma execução. São o que a conversa mostra ao vivo.
const (
	// EventDelta é um pedaço de texto da resposta final.
	EventDelta = "delta"
	// EventStep é uma chamada de ferramenta CONCLUÍDA — o cartão de ação (§3.3). Nunca uma frase a
	// dizer que fez: o registo da chamada real, com o resultado ou o código de erro.
	EventStep = "step"
	// EventConfirm pede aprovação humana para uma ação que sai para fora ou destrói (§6.3). A
	// execução PÁRA aqui até haver resposta — é deliberadamente incómodo, porque é o único ponto
	// onde um humano vê o que vai realmente acontecer.
	EventConfirm = "confirm"
	// EventDone fecha a execução e leva o Run completo.
	EventDone = "done"
)

// Event é o que o motor emite enquanto trabalha.
type Event struct {
	Kind    string          `json:"kind"`
	Text    string          `json:"text,omitempty"`
	Step    *Step           `json:"step,omitempty"`
	Run     *Run            `json:"run,omitempty"`
	Confirm *ConfirmRequest `json:"confirm,omitempty"`
}

// complete faz um passo do ciclo. Usa streaming quando o fornecedor o suporta E há quem escute;
// caso contrário cai em Complete. A resposta é a mesma nos dois caminhos — o ciclo não sabe qual
// correu, e é isso que impede o streaming de se tornar um segundo motor a manter em paralelo.
func (e *Engine) complete(ctx context.Context, req ai.Request, emit func(Event)) (ai.Response, error) {
	sp, ok := e.provider.(ai.StreamingProvider)
	if !ok || emit == nil {
		return e.provider.Complete(ctx, req)
	}
	return sp.Stream(ctx, req, func(chunk string) {
		emit(Event{Kind: EventDelta, Text: chunk})
	})
}

// historyMessages traduz o histórico persistido para o fio do modelo.
//
// Duas coisas ficam de fora de propósito: mensagens vazias (não dizem nada e gastam contexto) e
// tudo o que não seja utilizador ou assistente. Em particular, os RESULTADOS DE FERRAMENTAS de
// turnos anteriores não voltam ao modelo: são dados de terceiros de um momento que já passou, e
// reinjetá-los a cada turno daria a um e-mail lido ontem uma segunda oportunidade de mandar no
// modelo (§6.1). O que o assistente concluiu a partir deles está na sua própria resposta.
func historyMessages(history []Turn) []ai.Message {
	out := make([]ai.Message, 0, len(history))
	for _, t := range history {
		if t.Content == "" {
			continue
		}
		switch t.Role {
		case string(ai.RoleUser):
			out = append(out, ai.Message{Role: ai.RoleUser, Content: t.Content})
		case string(ai.RoleAssistant):
			out = append(out, ai.Message{Role: ai.RoleAssistant, Content: t.Content})
		}
	}
	return out
}
