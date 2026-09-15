package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/office"
)

// FileContent é o que uma leitura de ficheiro devolve. Version e Hash identificam a versão exata que
// foi lida: é contra eles que a escrita seguinte se valida (§5.1).
type FileContent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // document | spreadsheet | presentation | other
	Text    string `json:"text"`
	Version int32  `json:"version"`
	Hash    string `json:"-"`
	// Editable é falso para binários (imagens, PDF): esses não se editam às cegas, só se substituem
	// por um envio novo (§5.1). O modelo é avisado, e a ferramenta de edição recusa-os na mesma.
	Editable bool `json:"editable"`
}

// DocsRead lê o conteúdo de texto de um ficheiro do Drive e a sua versão corrente.
type DocsRead func(ctx context.Context, id Identity, fileID string) (FileContent, error)

// DocsEdit grava uma versão NOVA de um ficheiro, a partir de um plano, mas só se a versão corrente
// ainda for expectVersion. Devolve a versão nova.
type DocsEdit func(ctx context.Context, id Identity, fileID string, plan office.Plan, expectVersion int32) (DriveHit, error)

func docsEditTools(read DocsRead, edit DocsEdit) []Tool {
	return []Tool{
		{
			Name: ToolDocsRead,
			Description: "Read a file from the user's Drive: its text content and current version. " +
				"You MUST read a file with this before editing it with " + ToolDocsEdit + ".",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {"file_id": {"type": "string", "description": "File id from drive.search."}},
				"required": ["file_id"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a struct {
					FileID string `json:"file_id"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if strings.TrimSpace(a.FileID) == "" {
					return Result{}, fmt.Errorf("file_id is required")
				}
				fc, err := read(ctx, id, a.FileID)
				if err != nil {
					return Result{}, fmt.Errorf("read failed: %w", err)
				}
				// Regista a leitura NESTA execução: é o que docs.edit vai exigir (§5.1, regra 1).
				MarkRead(ctx, fc.ID, ReadMark{Version: fc.Version, Hash: fc.Hash})
				return Result{Summary: fc.Name, Data: fc}, nil
			},
		},
		{
			Name: ToolDocsEdit,
			Description: "Replace the content of an existing Drive file with a new version, built " +
				"from a plan. You must have read the file with " + ToolDocsRead + " in this same " +
				"turn first. Each edit is a new version; the previous one is kept and can be restored.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file_id": {"type": "string"},
					"kind": {"type": "string", "enum": ["document", "spreadsheet", "presentation"]},
					"title": {"type": "string"},
					"summary": {"type": "string", "description": "One line describing what you changed."},
					"blocks": {"type": "array", "items": {"type": "object", "properties": {"style": {"type": "string"}, "text": {"type": "string"}}, "required": ["text"]}},
					"sheets": {"type": "array", "items": {"type": "object", "properties": {"name": {"type": "string"}, "rows": {"type": "array", "items": {"type": "array", "items": {"type": "string"}}}}, "required": ["rows"]}},
					"slides": {"type": "array", "items": {"type": "object", "properties": {"title": {"type": "string"}, "bullets": {"type": "array", "items": {"type": "string"}}}, "required": ["title"]}}
				},
				"required": ["file_id", "kind"]
			}`),
			Writes: true,
			// Sem Confirm: o ficheiro é do próprio utilizador e não sai da organização. A guarda que
			// protege isto não é a confirmação — é a versão nova (reversível) e o histórico (§5.1,
			// regra 2). Enganar-se custa um clique a repor, não o ficheiro.
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a struct {
					FileID  string         `json:"file_id"`
					Kind    string         `json:"kind"`
					Title   string         `json:"title"`
					Summary string         `json:"summary"`
					Blocks  []office.Block `json:"blocks"`
					Sheets  []office.Sheet `json:"sheets"`
					Slides  []office.Slide `json:"slides"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if strings.TrimSpace(a.FileID) == "" {
					return Result{}, fmt.Errorf("file_id is required")
				}
				if _, ok := office.Ext(a.Kind); !ok {
					return Result{}, fmt.Errorf("kind must be document, spreadsheet or presentation")
				}
				// REGRA 1 (§5.1): ler antes de escrever. Sem uma leitura nesta execução, o modelo
				// escreveria por cima de conteúdo que nunca viu. Isto é invariante, não conselho.
				mark, seen := ReadMarkFor(ctx, a.FileID)
				if !seen {
					return Result{}, fmt.Errorf("you must read this file with %s before editing it", ToolDocsRead)
				}
				plan := office.Plan{
					Kind: a.Kind, Title: strings.TrimSpace(a.Title),
					Blocks: a.Blocks, Sheets: a.Sheets, Slides: a.Slides,
				}
				if emptyPlan(plan) {
					return Result{}, fmt.Errorf("the new content is empty")
				}
				// REGRA 3 (§5.1): editar em cima da versão que se leu. A escrita leva a versão lida;
				// se o ficheiro mudou entretanto, o servidor recusa e não perde o trabalho de ninguém.
				hit, err := edit(ctx, id, a.FileID, plan, mark.Version)
				if err != nil {
					return Result{}, fmt.Errorf("edit failed: %w", err)
				}
				// A leitura passa a apontar para a versão NOVA: um segundo edit no mesmo turno é
				// legítimo e não deve bater na guarda de concorrência por causa da sua própria escrita.
				MarkRead(ctx, a.FileID, ReadMark{Version: hit.Version})
				// REGRA 5 (§5.1): dizer o que mudou. O resumo do modelo é o que o cartão mostra; sem
				// ele, "documento atualizado" não deixa ninguém julgar se correu bem.
				summary := strings.TrimSpace(a.Summary)
				if summary == "" {
					summary = "updated " + hit.Name
				}
				return Result{
					Summary: summary,
					Data:    map[string]any{"file_id": hit.ID, "name": hit.Name, "version": hit.Version},
				}, nil
			},
		},
	}
}
