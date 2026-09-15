package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/office"
)

// DocsCreate grava um ficheiro Office no Drive do utilizador e devolve o que foi criado.
//
// Recebe o PLANO, não os bytes: quem constrói o ficheiro é o servidor (decisão §12.1, ponto 2), e
// o modelo descreve o conteúdo. É essa separação que garante que sai um ficheiro Office válido em
// vez de texto com a extensão trocada.
type DocsCreate func(ctx context.Context, id Identity, name string, plan office.Plan) (DriveHit, error)

type docsCreateArgs struct {
	Kind   string         `json:"kind"`
	Name   string         `json:"name"`
	Title  string         `json:"title"`
	Blocks []office.Block `json:"blocks"`
	Sheets []office.Sheet `json:"sheets"`
	Slides []office.Slide `json:"slides"`
}

func docsTools(create DocsCreate) []Tool {
	return []Tool{
		{
			Name: ToolDocsCreate,
			Description: "Create a Word document, an Excel spreadsheet or a PowerPoint presentation " +
				"in the user's Drive, from a plan you write. Use `blocks` for a document, `sheets` " +
				"for a spreadsheet, `slides` for a presentation. The file is built on the server, so " +
				"you describe the content and never the file format.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"kind": {"type": "string", "enum": ["document", "spreadsheet", "presentation"]},
					"name": {"type": "string", "description": "File name, without extension."},
					"title": {"type": "string", "description": "Title shown inside the file."},
					"blocks": {
						"type": "array",
						"description": "Document content, in order.",
						"items": {
							"type": "object",
							"properties": {
								"style": {"type": "string", "enum": ["heading", "paragraph", "bullet"]},
								"text": {"type": "string"}
							},
							"required": ["text"]
						}
					},
					"sheets": {
						"type": "array",
						"description": "Spreadsheet tabs. The first row is usually the header.",
						"items": {
							"type": "object",
							"properties": {
								"name": {"type": "string"},
								"rows": {"type": "array", "items": {"type": "array", "items": {"type": "string"}}}
							},
							"required": ["rows"]
						}
					},
					"slides": {
						"type": "array",
						"description": "Presentation slides, in order.",
						"items": {
							"type": "object",
							"properties": {
								"title": {"type": "string"},
								"bullets": {"type": "array", "items": {"type": "string"}}
							},
							"required": ["title"]
						}
					}
				},
				"required": ["kind", "name"]
			}`),
			Writes: true,
			// Sem Confirm: o ficheiro fica no Drive do próprio utilizador e não sai da organização.
			// Partilhar é que sai — e essa é outra ferramenta, com confirmação (§6.3).
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a docsCreateArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if _, ok := office.Ext(a.Kind); !ok {
					return Result{}, fmt.Errorf("kind must be document, spreadsheet or presentation")
				}
				name := strings.TrimSpace(a.Name)
				if name == "" {
					return Result{}, fmt.Errorf("name is required")
				}
				plan := office.Plan{
					Kind: a.Kind, Title: strings.TrimSpace(a.Title),
					Blocks: a.Blocks, Sheets: a.Sheets, Slides: a.Slides,
				}
				if emptyPlan(plan) {
					// Um ficheiro vazio é quase sempre o modelo a falhar o esquema, não o pedido do
					// utilizador. Dizê-lo dá-lhe a hipótese de se corrigir; gravar o vazio não.
					return Result{}, fmt.Errorf("the plan has no content: fill blocks, sheets or slides")
				}
				hit, err := create(ctx, id, name, plan)
				if err != nil {
					return Result{}, fmt.Errorf("document creation failed: %w", err)
				}
				return Result{
					Summary: hit.Name,
					Data:    map[string]any{"file_id": hit.ID, "name": hit.Name, "size": hit.Size},
				}, nil
			},
		},
	}
}

func emptyPlan(p office.Plan) bool {
	return len(p.Blocks) == 0 && len(p.Sheets) == 0 && len(p.Slides) == 0 && p.Title == ""
}
