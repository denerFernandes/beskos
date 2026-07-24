package assistant

import (
	"context"
	"sync"
	"testing"

	"mailin/backend/internal/ai"
	"mailin/backend/internal/office"
)

// fakeFiles é um Drive em memória: guarda o conteúdo e a versão de cada ficheiro, e conta as
// escrituras para se poder afirmar que uma recusa NÃO gravou nada.
type fakeFiles struct {
	mu       sync.Mutex
	version  int32
	editable bool
	edits    int
	// bumpBefore simula outra pessoa a gravar entre a leitura e a escrita: sobe a versão uma vez.
	bumpBefore bool
}

func (f *fakeFiles) read(_ context.Context, _ Identity, fileID string) (FileContent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return FileContent{ID: fileID, Name: "relatorio.docx", Kind: office.KindDocument,
		Text: "conteúdo atual", Version: f.version, Hash: "h", Editable: f.editable}, nil
}

func (f *fakeFiles) edit(_ context.Context, _ Identity, fileID string, _ office.Plan, expect int32) (DriveHit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bumpBefore {
		f.version++
		f.bumpBefore = false
	}
	if f.version != expect {
		return DriveHit{}, errConflict
	}
	f.version++
	f.edits++
	return DriveHit{ID: fileID, Name: "relatorio.docx", Kind: "file", Version: f.version}, nil
}

var errConflict = &conflictError{}

type conflictError struct{}

func (*conflictError) Error() string { return "the file changed since you read it" }

func editRegistry(f *fakeFiles) *Registry {
	return BuildRegistry(Deps{DocsRead: f.read, DocsEdit: f.edit})
}

func readCall(fileID string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "r1", Name: ToolDocsRead, Arguments: `{"file_id":"` + fileID + `"}`}}, FinishReason: "tool_calls"}
}

func editCall(fileID string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "e1", Name: ToolDocsEdit,
		Arguments: `{"file_id":"` + fileID + `","kind":"document","summary":"acrescentei uma secção","blocks":[{"text":"novo"}]}`}}, FinishReason: "tool_calls"}
}

// INVARIANTE (§5.1, regra 1): docs.edit exige ter lido o ficheiro NESTA execução. Sem leitura, o
// modelo escreveria por cima de conteúdo que nunca viu — por isso a edição é recusada.
func TestEditRequiresReadInThisRun(t *testing.T) {
	f := &fakeFiles{version: 3, editable: true}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		editCall("file-1"), // edita SEM ler primeiro
		textResp("não deu"),
	}}, editRegistry(f), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "muda o relatório")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.edits != 0 {
		t.Fatal("EDITOU SEM LER PRIMEIRO — a regra 1 de §5.1 foi violada")
	}
	if run.Steps[0].ErrorCode == "" {
		t.Error("a edição sem leitura devia falhar a ferramenta")
	}
}

// Lido primeiro, na mesma execução, a edição passa — e grava uma versão nova.
func TestEditAfterReadSucceeds(t *testing.T) {
	f := &fakeFiles{version: 3, editable: true}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		readCall("file-1"),
		editCall("file-1"),
		textResp("acrescentei a secção"),
	}}, editRegistry(f), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "acrescenta uma secção ao relatório")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.edits != 1 {
		t.Fatalf("edições = %d, quero 1", f.edits)
	}
	if f.version != 4 {
		t.Errorf("versão = %d, quero 4 (subiu uma)", f.version)
	}
	// REGRA 5: o cartão diz o que mudou, não "documento atualizado".
	var editStep *Step
	for i := range run.Steps {
		if run.Steps[i].Tool == ToolDocsEdit {
			editStep = &run.Steps[i]
		}
	}
	if editStep == nil || editStep.Summary != "acrescentei uma secção" {
		t.Errorf("o cartão de edição não descreve a mudança: %+v", editStep)
	}
}

// INVARIANTE (§5.1, regra 3): se o ficheiro mudou entre a leitura e a escrita, a edição é recusada
// — não se perde o trabalho de quem gravou entretanto.
func TestEditRefusedWhenFileChangedSinceRead(t *testing.T) {
	f := &fakeFiles{version: 3, editable: true, bumpBefore: true} // alguém grava entre ler e editar
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		readCall("file-1"),
		editCall("file-1"),
		textResp("não consegui, mudou entretanto"),
	}}, editRegistry(f), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "muda o relatório")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.edits != 0 {
		t.Fatal("EDITOU POR CIMA DE UMA MUDANÇA CONCORRENTE — a regra 3 de §5.1 foi violada")
	}
	var editStep *Step
	for i := range run.Steps {
		if run.Steps[i].Tool == ToolDocsEdit {
			editStep = &run.Steps[i]
		}
	}
	if editStep == nil || editStep.ErrorCode == "" {
		t.Error("a edição sobre uma versão desatualizada devia falhar")
	}
}

// A leitura de um ficheiro NÃO autoriza a edição de OUTRO. A marca é por ficheiro, não um
// "já li alguma coisa" global.
func TestReadingOneFileDoesNotAuthoriseEditingAnother(t *testing.T) {
	f := &fakeFiles{version: 1, editable: true}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		readCall("file-A"),
		editCall("file-B"), // leu A, tenta editar B
		textResp("não"),
	}}, editRegistry(f), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	eng.Run(context.Background(), testIdentity(), ag, "edita o B")
	if f.edits != 0 {
		t.Fatal("editou um ficheiro que nunca foi lido nesta execução")
	}
}

// A marca de leitura é POR EXECUÇÃO: uma leitura numa execução não deixa editar noutra. Sem isto,
// uma leitura de ontem autorizaria uma escrita de hoje.
func TestReadMarkDoesNotLeakBetweenRuns(t *testing.T) {
	f := &fakeFiles{version: 1, editable: true}
	ag, _ := Builtin(AgentGeneral)

	// Execução 1: só lê.
	eng1 := NewEngine(&fakeProvider{script: []ai.Response{readCall("file-1"), textResp("li")}},
		editRegistry(f), &memAudit{}, 0)
	eng1.Run(context.Background(), testIdentity(), ag, "lê")

	// Execução 2: tenta editar sem ler. Tem de ser recusada.
	eng2 := NewEngine(&fakeProvider{script: []ai.Response{editCall("file-1"), textResp("tentei")}},
		editRegistry(f), &memAudit{}, 0)
	eng2.Run(context.Background(), testIdentity(), ag, "edita")

	if f.edits != 0 {
		t.Fatal("uma leitura de outra execução autorizou esta edição")
	}
}

// docs.edit não pede confirmação: a guarda é a versão nova e o histórico, não a confirmação (§5.1).
func TestEditDoesNotAskForConfirmation(t *testing.T) {
	f := &fakeFiles{version: 1, editable: true}
	conf := &scriptedConfirmer{approve: false}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		readCall("file-1"), editCall("file-1"), textResp("feito"),
	}}, editRegistry(f), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	eng.Run(context.Background(), testIdentity(), ag, "edita")
	if len(conf.asked) != 0 {
		t.Error("docs.edit pediu confirmação")
	}
	if f.edits != 1 {
		t.Error("docs.edit não gravou apesar de não precisar de confirmação")
	}
}
