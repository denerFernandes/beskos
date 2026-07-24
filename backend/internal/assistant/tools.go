package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"mailin/backend/internal/ai"
)

// Nomes das ferramentas. Constantes porque aparecem em três sítios (catálogo, lista de permissões
// do agente, registo de auditoria) e um erro de escrita entre eles seria silencioso.
const (
	ToolMailSearch     = "mail.search"
	ToolMailRead       = "mail.read"
	ToolMailDraft      = "mail.draft"
	ToolMailSend       = "mail.send"
	ToolDriveSearch    = "drive.search"
	ToolDocsCreate     = "docs.create"
	ToolDocsRead       = "docs.read"
	ToolDocsEdit       = "docs.edit"
	ToolDocsShare      = "docs.share"
	ToolTasksCreate    = "tasks.create"
	ToolCalendarCreate = "calendar.create"
	ToolContactsSearch = "contacts.search"
	ToolContactsCreate = "contacts.create"
	ToolContactsShare  = "contacts.share"
	ToolWikiSearch     = "wiki.search"
	ToolWikiRead       = "wiki.read"
	ToolWikiCreatePage = "wiki.create_page"
	ToolWikiEditPage   = "wiki.edit_page"
)

// Result é o que uma ferramenta devolve.
type Result struct {
	// Summary é a linha que o cartão da conversa mostra: o registo da chamada real, não uma frase
	// do modelo a afirmar que fez (§3.3).
	Summary string
	// Data volta ao modelo, sempre embrulhada como conteúdo não fiável (§6.1).
	Data any
	// Choices, quando presente, são respostas rápidas que o frontend mostra como BOTÕES (ex.: qual
	// dos vários contactos "Bob"). Clicar envia Value como a próxima mensagem — poupa ao utilizador
	// escrever a resposta. É extra à conversa; não substitui a pergunta em texto do modelo.
	Choices []Choice
}

// Choice é um botão de resposta rápida: Label é o que se vê, Value o que se envia ao clicar.
type Choice struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Handler executa a ferramenta. Recebe a Identity do pedido — NÃO a lê dos args, que vêm do modelo.
type Handler func(ctx context.Context, id Identity, args json.RawMessage) (Result, error)

// Tool é uma função tipada que o modelo pode invocar. É a única forma de tocar em dados.
type Tool struct {
	Name        string
	Description string
	// Parameters é um JSON Schema do tipo "object". NUNCA declara tenant_id nem user_id: um
	// parâmetro que aceitasse tenant seria um atravessamento de inquilino à espera de acontecer
	// (§6.2). Há um teste que o garante.
	Parameters json.RawMessage
	// Writes marca ferramentas de escrita.
	Writes bool
	// Confirm marca o que SAI PARA FORA ou DESTRÓI: enviar, partilhar, convidar, apagar, webhooks
	// (§6.3). Uma ferramenta assim nunca corre sem aprovação humana com o conteúdo exato à vista, e
	// a aprovação é individual — não há aprovação em bloco. É a defesa que não depende de o modelo
	// se portar bem, e por isso é a que sustenta a decisão de alcance amplo (§12.1, decisão 1).
	Confirm bool
	// ConfirmWhen, quando definida, decide a confirmação POR CHAMADA a partir dos args — para
	// ferramentas em que só ALGUNS usos saem para fora. Ex.: criar um evento a solo é interno (sem
	// confirmação), mas criar um evento COM convidados avisa outra pessoa (convidar → §6.3, confirma).
	// Se definida, tem precedência sobre Confirm. Na dúvida (args ilegíveis) devolve true: fail-closed.
	ConfirmWhen func(args json.RawMessage) bool
	Handler     Handler
}

// Registry é o catálogo de ferramentas disponíveis na instalação. Não é o mesmo que a lista de
// permissões de um agente: o catálogo é o que existe, a lista é o que cada agente pode usar.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry constrói o catálogo. Nomes duplicados são erro de programação e entram em pânico na
// inicialização — melhor falhar no arranque do que despachar a ferramenta errada em produção.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		if _, dup := r.tools[t.Name]; dup {
			panic("assistant: duplicate tool " + t.Name)
		}
		r.tools[t.Name] = t
	}
	return r
}

// Get devolve uma ferramenta do catálogo.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names devolve os nomes do catálogo, por ordem — útil para testes e para o ecrã de auditoria.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Schemas traduz para o formato do fornecedor as ferramentas que ESTE agente pode usar. Uma
// ferramenta pedida pelo agente mas ausente do catálogo é ignorada em silêncio: significa que a
// instalação não a tem configurada (ex.: Drive sem armazenamento), não que houve um ataque.
func (r *Registry) Schemas(allowed []string) []ai.Tool {
	out := make([]ai.Tool, 0, len(allowed))
	for _, name := range allowed {
		t, ok := r.tools[name]
		if !ok {
			continue
		}
		out = append(out, ai.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

// allows diz se o agente tem esta ferramenta na sua lista de permissões. A verificação corre no
// servidor a cada chamada, não no prompt (§6.1, defesa 2).
func allows(ag Agent, tool string) bool {
	for _, t := range ag.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

// decodeArgs desserializa os argumentos do modelo para o tipo esperado da ferramenta. Argumentos
// vazios são tratados como objeto vazio: alguns modelos omitem-nos quando o schema não tem campos
// obrigatórios.
func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}
