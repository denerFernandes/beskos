package assistant

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// ConfirmRequest é uma ação que espera aprovação humana. Args são os argumentos EXATOS que vão
// correr se a pessoa aprovar — os mesmos bytes que o handler recebe, não uma segunda versão
// formatada para mostrar. Uma pré-visualização que divergisse do que corre seria pior do que não
// haver confirmação nenhuma: daria confiança sem a merecer.
type ConfirmRequest struct {
	ID   uuid.UUID       `json:"id"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// Confirmer pergunta ao humano e devolve a decisão. Bloqueia até haver resposta, o contexto ser
// cancelado, ou o prazo expirar.
//
// Um erro (prazo, ligação perdida) NÃO é uma aprovação: quem implementa devolve false. É a mesma
// regra que o motor aplica quando não há Confirmer nenhum — na dúvida, não sai nada.
type Confirmer interface {
	Ask(ctx context.Context, id Identity, req ConfirmRequest) (bool, error)
}

// WithConfirmer devolve uma CÓPIA do motor com este pedido de confirmação ligado.
//
// Cópia, e não alteração no lugar, porque o motor é partilhado: o worker precisa de um Confirmer
// durável (que escreve na base) enquanto o servidor usa o seu, em memória. Se isto mutasse o
// receptor, ligar um trocaria o do outro — e a confirmação de uma tarefa em fundo passaria a ser
// entregue a um processo que já não está a ouvir.
//
// Sem Confirmer, o motor RECUSA todas as ferramentas marcadas com Confirm em vez de as deixar
// correr. É deliberado: uma dependência de segurança em falta tem de fechar a porta, não abri-la.
func (e *Engine) WithConfirmer(c Confirmer) *Engine {
	cp := *e
	cp.confirm = c
	return &cp
}

// confirmGate decide se uma ferramenta que sai para fora pode correr. Devolve o código de i18n da
// recusa, ou vazio quando pode prosseguir.
func (e *Engine) confirmGate(ctx context.Context, id Identity, tool Tool, args json.RawMessage, emit func(Event)) string {
	needs := tool.Confirm
	if tool.ConfirmWhen != nil {
		needs = tool.ConfirmWhen(args)
	}
	if !needs {
		return ""
	}
	if e.confirm == nil {
		return "assistant.confirm_unavailable"
	}
	req := ConfirmRequest{ID: uuid.New(), Tool: tool.Name, Args: args}
	if emit != nil {
		// A pessoa vê o cartão ANTES de a espera começar — senão o painel ficaria parado sem dizer
		// porquê.
		emit(Event{Kind: EventConfirm, Confirm: &req})
	}
	ok, err := e.confirm.Ask(ctx, id, req)
	if err != nil || !ok {
		return "assistant.confirm_declined"
	}
	return ""
}
