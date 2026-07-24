package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mailin/backend/internal/ai"
	"mailin/backend/internal/office"
)

// docsRegistry monta um catálogo só com docs.create, ligado a um criador que regista o plano.
func docsRegistry(capture *office.Plan) *Registry {
	return BuildRegistry(Deps{
		Docs: func(_ context.Context, _ Identity, name string, plan office.Plan) (DriveHit, error) {
			*capture = plan
			return DriveHit{ID: "f1", Name: name, Kind: "file", Size: 123}, nil
		},
	})
}

func docsCall(args string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "d1", Name: ToolDocsCreate, Arguments: args}}, FinishReason: "tool_calls"}
}

// docs.create constrói o ficheiro no servidor a partir de um PLANO — o modelo descreve o conteúdo,
// nunca os bytes. O plano que chega ao criador tem de ser o que o modelo pediu.
func TestDocsCreatePassesThePlanToTheServer(t *testing.T) {
	var got office.Plan
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		docsCall(`{"kind":"document","name":"Relatório","title":"Q1","blocks":[{"style":"heading","text":"Resumo"},{"style":"paragraph","text":"corpo"}]}`),
		textResp("criei o documento"),
	}}, docsRegistry(&got), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "faz um relatório")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Kind != office.KindDocument || got.Title != "Q1" {
		t.Errorf("o plano não chegou como pedido: %+v", got)
	}
	if len(got.Blocks) != 2 || got.Blocks[0].Text != "Resumo" {
		t.Errorf("os blocos não chegaram: %+v", got.Blocks)
	}
	if len(run.Steps) != 1 || run.Steps[0].Summary != "Relatório" {
		t.Errorf("o cartão de ação não mostra o ficheiro: %+v", run.Steps)
	}
}

// Um plano vazio não se grava: é quase sempre o modelo a falhar o esquema, e dizê-lo dá-lhe a
// hipótese de se corrigir em vez de deixar no Drive um ficheiro sem nada.
func TestDocsCreateRefusesEmptyPlan(t *testing.T) {
	var got office.Plan
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		docsCall(`{"kind":"presentation","name":"Vazio"}`),
		textResp("não consegui"),
	}}, docsRegistry(&got), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "faz uma apresentação")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Steps[0].ErrorCode == "" {
		t.Error("um plano vazio devia falhar a ferramenta, não gravar um ficheiro vazio")
	}
	if got.Kind != "" {
		t.Error("o criador foi chamado com um plano vazio")
	}
}

// Um tipo desconhecido é recusado na ferramenta, antes de tocar no armazenamento.
func TestDocsCreateRefusesUnknownKind(t *testing.T) {
	var got office.Plan
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		docsCall(`{"kind":"pdf","name":"x","blocks":[{"text":"a"}]}`),
		textResp("não dá"),
	}}, docsRegistry(&got), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, _ := eng.Run(context.Background(), testIdentity(), ag, "faz um pdf")
	if run.Steps[0].ErrorCode == "" {
		t.Error("um tipo desconhecido devia falhar a ferramenta")
	}
}

// docs.create NÃO pede confirmação: o ficheiro fica no Drive do próprio utilizador e não sai da
// organização. Partilhar é que sai, e é outra ferramenta (§6.3).
func TestDocsCreateDoesNotAskForConfirmation(t *testing.T) {
	var got office.Plan
	conf := &scriptedConfirmer{approve: false}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		docsCall(`{"kind":"document","name":"x","blocks":[{"text":"a"}]}`),
		textResp("pronto"),
	}}, docsRegistry(&got), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, _ := eng.Run(context.Background(), testIdentity(), ag, "faz um documento")
	if len(conf.asked) != 0 {
		t.Error("docs.create pediu confirmação — mas não sai da organização")
	}
	if run.Steps[0].ErrorCode != "" {
		t.Errorf("a ferramenta falhou: %+v", run.Steps[0])
	}
}

// O agente de Documentos consegue criar ficheiros; o de Correio não. É o mesmo isolamento por
// perfil da A3, agora com a ferramenta nova.
func TestDocsCreateIsScopedByAgent(t *testing.T) {
	docs, _ := Builtin(AgentDocs)
	if !allows(docs, ToolDocsCreate) {
		t.Error("o agente de Documentos não consegue criar ficheiros")
	}
	inbox, _ := Builtin(AgentInbox)
	if allows(inbox, ToolDocsCreate) {
		t.Error("o agente de Correio consegue criar ficheiros — devia estar fora do seu âmbito")
	}
}

// O schema de docs.create não declara tenant_id nem user_id: a identidade vem do pedido, como em
// toda a parte (§6.2). Reforça o teste geral com a ferramenta nova.
func TestDocsCreateSchemaHasNoTenant(t *testing.T) {
	reg := docsRegistry(&office.Plan{})
	tool, _ := reg.Get(ToolDocsCreate)
	schema := strings.ToLower(string(tool.Parameters))
	for _, f := range []string{"tenant", "user_id", "owner"} {
		if strings.Contains(schema, `"`+f+`"`) {
			t.Errorf("o schema de docs.create declara %q", f)
		}
	}
	// E é um JSON Schema válido.
	var v any
	if err := json.Unmarshal(tool.Parameters, &v); err != nil {
		t.Errorf("o schema não é JSON válido: %v", err)
	}
}
