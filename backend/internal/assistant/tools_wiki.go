package assistant

import (
	"context"
	"encoding/json"
	"fmt"
)

// Ferramentas da Wiki (docs/wiki-features.md §7.5). Leitura (wiki.search/wiki.read) sem confirmação;
// escrita (wiki.create_page/wiki.edit_page) com Confirm — o utilizador aprova o conteúdo exato antes
// de qualquer gravação (§6.3). Os handlers dos adapters revalidam canAccess (editor) no serviço —
// nunca confiam no modelo. O texto recuperado é DADO não confiável, nunca instrução (§6.1): o motor
// já embrulha Result.Data como conteúdo, e as instruções do agente reforçam-no.

// WikiHit é um trecho recuperado, com os dados de citação (página, título, slug).
type WikiHit struct {
	PageID  string `json:"page_id"`
	Title   string `json:"title"`
	Slug    string `json:"slug"`
	Snippet string `json:"snippet"`
	// Distance é a distância de cosseno (menor = mais relevante); dá ao modelo noção de confiança.
	Distance float64 `json:"distance"`
}

// WikiPageData é uma página devolvida ao modelo (texto puro; nunca HTML). Link é o caminho interno
// da app (/mail/wiki/<space>/<page>) para o modelo citar e o utilizador abrir (B6).
type WikiPageData struct {
	PageID  string `json:"page_id"`
	Title   string `json:"title"`
	Slug    string `json:"slug"`
	Status  string `json:"status"`
	Version int32  `json:"version"`
	Text    string `json:"text"`
	Link    string `json:"link,omitempty"`
}

// WikiNewPage é o pedido de criação: espaço por nome/chave (resolvido entre os visíveis ao
// utilizador), título e conteúdo em texto simples.
type WikiNewPage struct {
	Space   string `json:"space"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// WikiPageEdit é o pedido de alteração de uma página existente.
type WikiPageEdit struct {
	PageID  string `json:"page_id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Wiki service accessors — functions, like the rest of the catalog. They always receive the
// caller's Identity; the httpapi adapters wire them to the internal/wiki service (same visible surface).
type (
	WikiSearch     func(ctx context.Context, id Identity, query string, limit int) ([]WikiHit, error)
	WikiRead       func(ctx context.Context, id Identity, pageID string) (WikiPageData, error)
	WikiCreatePage func(ctx context.Context, id Identity, in WikiNewPage) (WikiPageData, error)
	WikiEditPage   func(ctx context.Context, id Identity, in WikiPageEdit) (WikiPageData, error)
)

// wikiTools monta as ferramentas da wiki a partir das funções ligadas (nil = ferramenta ausente).
func wikiTools(search WikiSearch, read WikiRead, create WikiCreatePage, edit WikiEditPage) []Tool {
	var tools []Tool
	if search != nil {
		tools = append(tools, Tool{
			Name: ToolWikiSearch,
			Description: "Semantic search over the organisation's wiki (knowledge base). Returns the " +
				"most relevant excerpts THE USER CAN SEE, with page title and id for citation. Use it " +
				"to answer questions from written company knowledge; always cite the page.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "What to look for, as a question or phrase."},
					"limit": {"type": "integer", "description": "Max excerpts to return (default 8)."}
				},
				"required": ["query"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a struct {
					Query string `json:"query"`
					Limit int    `json:"limit"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				hits, err := search(ctx, id, a.Query, a.Limit)
				if err != nil {
					return Result{}, err
				}
				return Result{
					Summary: fmt.Sprintf("Searched the wiki for %q: %d excerpt(s)", a.Query, len(hits)),
					Data:    hits,
				}, nil
			},
		})
	}
	if read != nil {
		tools = append(tools, Tool{
			Name: ToolWikiRead,
			Description: "Read one wiki page in full (plain text) by its page_id — e.g. a page found " +
				"via wiki.search. Only pages the user can see.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {"page_id": {"type": "string", "description": "The page id."}},
				"required": ["page_id"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a struct {
					PageID string `json:"page_id"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				page, err := read(ctx, id, a.PageID)
				if err != nil {
					return Result{}, err
				}
				return Result{Summary: fmt.Sprintf("Read wiki page %q", page.Title), Data: page}, nil
			},
		})
	}
	if create != nil {
		tools = append(tools, Tool{
			Name: ToolWikiCreatePage,
			Description: "Create a new wiki page (as a draft) in a space the user can edit. The user " +
				"approves the exact title and content before anything is written. `space` is the space " +
				"name or key as the user knows it.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"space": {"type": "string", "description": "Name or key of the wiki/space."},
					"title": {"type": "string"},
					"content": {"type": "string", "description": "Page content in MARKDOWN: # headings, - bullets, 1. numbered, - [ ] checklists, > quotes, fenced code blocks, **bold**, links. It is rendered with the editor native blocks."}
				},
				"required": ["space", "title", "content"]
			}`),
			Writes:  true,
			Confirm: true,
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a WikiNewPage
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				page, err := create(ctx, id, a)
				if err != nil {
					return Result{}, err
				}
				return Result{Summary: fmt.Sprintf("Created wiki page %q (draft)", page.Title), Data: page}, nil
			},
		})
	}
	if edit != nil {
		tools = append(tools, Tool{
			Name: ToolWikiEditPage,
			Description: "Rewrite an existing wiki page (title and/or content) the user can edit. The " +
				"user approves the exact new content before it is saved; the previous version stays in " +
				"the page history.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"page_id": {"type": "string"},
					"title": {"type": "string", "description": "New title; empty keeps the current one."},
					"content": {"type": "string", "description": "The FULL new content in MARKDOWN (headings, lists, checklists, quotes, code)."}
				},
				"required": ["page_id", "content"]
			}`),
			Writes:  true,
			Confirm: true,
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a WikiPageEdit
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				page, err := edit(ctx, id, a)
				if err != nil {
					return Result{}, err
				}
				return Result{Summary: fmt.Sprintf("Updated wiki page %q (v%d)", page.Title, page.Version), Data: page}, nil
			},
		})
	}
	return tools
}
