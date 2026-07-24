package assistant

import (
	"context"
	"sync"
)

// runState é o que uma execução precisa de lembrar ENTRE chamadas de ferramenta. Hoje só uma coisa:
// que ficheiros foram lidos, e em que versão. É o que sustenta a regra dura de §5.1 — `docs.edit`
// exige ter lido o ficheiro NESTA execução, e escrever por cima da versão que leu.
//
// Vive no contexto, não numa assinatura: assim as ferramentas que precisam dele vão buscá-lo, e as
// que não precisam nem o veem. É criado por execução — nunca partilhado entre execuções, senão uma
// leitura de ontem autorizaria uma escrita de hoje.
type runState struct {
	mu   sync.Mutex
	read map[string]ReadMark
}

// ReadMark é o que se sabe de um ficheiro no momento em que foi lido: a versão e a impressão do
// conteúdo. É contra isto que a escrita se valida — se o ficheiro mudou entretanto, a versão já não
// bate certo e a escrita é recusada.
type ReadMark struct {
	Version int32
	Hash    string
}

type runStateKey struct{}

// withRunState devolve um contexto com um registo de leitura novo. O motor chama-o uma vez por
// execução.
func withRunState(ctx context.Context) context.Context {
	return context.WithValue(ctx, runStateKey{}, &runState{read: map[string]ReadMark{}})
}

// MarkRead regista que um ficheiro foi lido nesta execução. Chamado pela ferramenta de leitura.
// Sem estado no contexto é um no-op: uma execução síncrona simples não precisa de o transportar.
func MarkRead(ctx context.Context, fileID string, mark ReadMark) {
	st, ok := ctx.Value(runStateKey{}).(*runState)
	if !ok {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.read[fileID] = mark
}

// ReadMarkFor devolve o que se leu de um ficheiro nesta execução, se é que se leu. O segundo valor
// é false quando o ficheiro NÃO foi lido — e aí a escrita tem de ser recusada, não adivinhada.
func ReadMarkFor(ctx context.Context, fileID string) (ReadMark, bool) {
	st, ok := ctx.Value(runStateKey{}).(*runState)
	if !ok {
		return ReadMark{}, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	m, seen := st.read[fileID]
	return m, seen
}
