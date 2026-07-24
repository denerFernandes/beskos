package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"mailin/backend/internal/assistant"
	"mailin/backend/internal/chat"
	"mailin/backend/internal/wiki"
)

// Adapters das ferramentas wiki.* do assistente (docs/wiki-features.md §7.5). Cada função recebe a
// Identity da SESSÃO (§6.2) e chama o serviço internal/wiki — a MESMA superfície vis/canAccess da UI:
// o modelo nunca lê o que o utilizador não veria, e a escrita revalida role>=editor no serviço.

// assistantWikiSearch: busca semântica citável. Sem embeddings configurados devolve um erro claro —
// as restantes ferramentas da wiki continuam a funcionar.
func (s *Server) assistantWikiSearch(ctx context.Context, id assistant.Identity, query string, limit int) ([]assistant.WikiHit, error) {
	if s.wikiEmb == nil {
		return nil, fmt.Errorf("wiki semantic search is not configured (EMBEDDINGS_API_KEY missing)")
	}
	rows, err := s.wiki.SearchChunks(ctx, s.wikiEmb, id.TenantID, id.UserID, query, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("wiki search: %w", err)
	}
	hits := make([]assistant.WikiHit, len(rows))
	for i, r := range rows {
		snippet := r.ChunkText
		if len(snippet) > 700 {
			snippet = snippet[:700] + "…"
		}
		hits[i] = assistant.WikiHit{
			PageID: r.PageID.String(), Title: r.Title, Slug: r.Slug,
			Snippet: snippet, Distance: r.Distance,
		}
	}
	return hits, nil
}

// assistantWikiRead devolve a página em texto puro (nunca HTML) — só se o utilizador a pode ver.
func (s *Server) assistantWikiRead(ctx context.Context, id assistant.Identity, pageID string) (assistant.WikiPageData, error) {
	pid, err := uuid.Parse(pageID)
	if err != nil {
		return assistant.WikiPageData{}, fmt.Errorf("invalid page_id")
	}
	page, _, err := s.wiki.GetPage(ctx, id.TenantID, id.UserID, pid)
	if err != nil {
		return assistant.WikiPageData{}, fmt.Errorf("wiki page not found or not accessible")
	}
	return assistant.WikiPageData{
		PageID: page.ID.String(), Title: page.Title, Slug: page.Slug,
		Status: page.Status, Version: page.Version, Text: page.BodyText,
	}, nil
}

// assistantWikiCreate cria um RASCUNHO num espaço resolvido por nome/chave entre os visíveis do
// utilizador. O serviço revalida role>=editor — a permissão nunca vem do modelo.
func (s *Server) assistantWikiCreate(ctx context.Context, id assistant.Identity, in assistant.WikiNewPage) (assistant.WikiPageData, error) {
	spaces, err := s.wiki.ListSpaces(ctx, id.TenantID, id.UserID)
	if err != nil {
		return assistant.WikiPageData{}, fmt.Errorf("wiki spaces lookup: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(in.Space))
	var spaceID uuid.UUID
	var names []string
	for _, sp := range spaces {
		names = append(names, sp.Name)
		if strings.ToLower(sp.Name) == want || strings.ToLower(sp.Key) == want {
			spaceID = sp.ID
		}
	}
	if spaceID == uuid.Nil {
		return assistant.WikiPageData{}, fmt.Errorf("wiki space %q not found; available: %s", in.Space, strings.Join(names, ", "))
	}
	page, err := s.wiki.CreatePage(ctx, id.TenantID, id.UserID, spaceID, wikiPageInputFromText(nil, in.Title, in.Content, "draft"))
	if err != nil {
		return assistant.WikiPageData{}, fmt.Errorf("wiki create page: %w", err)
	}
	s.notifyWikiPage(ctx, id.UserID.String(), "created", page.SpaceID, page.ID, page.Title)
	return assistant.WikiPageData{
		PageID: page.ID.String(), Title: page.Title, Slug: page.Slug,
		Status: page.Status, Version: page.Version, Text: page.BodyText,
		Link: wikiPageLink(page.SpaceID, page.ID),
	}, nil
}

// assistantWikiEdit regrava uma página existente (conteúdo COMPLETO novo). O serviço revalida
// role>=editor e o bloqueio otimista; a versão anterior fica no histórico (§6).
func (s *Server) assistantWikiEdit(ctx context.Context, id assistant.Identity, in assistant.WikiPageEdit) (assistant.WikiPageData, error) {
	pid, err := uuid.Parse(in.PageID)
	if err != nil {
		return assistant.WikiPageData{}, fmt.Errorf("invalid page_id")
	}
	current, _, err := s.wiki.GetPage(ctx, id.TenantID, id.UserID, pid)
	if err != nil {
		return assistant.WikiPageData{}, fmt.Errorf("wiki page not found or not accessible")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = current.Title
	}
	page, err := s.wiki.UpdatePage(ctx, id.TenantID, id.UserID, pid,
		wikiPageInputFromText(nil, title, in.Content, current.Status), current.Version)
	if err != nil {
		return assistant.WikiPageData{}, fmt.Errorf("wiki edit page: %w", err)
	}
	s.notifyWikiPage(ctx, id.UserID.String(), "updated", page.SpaceID, page.ID, page.Title)
	return assistant.WikiPageData{
		PageID: page.ID.String(), Title: page.Title, Slug: page.Slug,
		Status: page.Status, Version: page.Version, Text: page.BodyText,
		Link: wikiPageLink(page.SpaceID, page.ID),
	}, nil
}

// wikiPageLink monta o caminho interno da app para uma página (deep-link do B1).
func wikiPageLink(spaceID, pageID uuid.UUID) string {
	return "/mail/wiki/" + spaceID.String() + "/" + pageID.String()
}

// notifyWikiPage publica no hub (Redis) um evento "wiki_page" para o PRÓPRIO utilizador que pediu à
// IA (B6): o frontend navega para a página criada e refresca a aberta — o "ver a acontecer".
func (s *Server) notifyWikiPage(ctx context.Context, userID string, action string, spaceID, pageID uuid.UUID, title string) {
	if s.hub == nil {
		return
	}
	payload, err := json.Marshal(map[string]string{
		"action": action, "space_id": spaceID.String(), "page_id": pageID.String(),
		"title": title, "link": wikiPageLink(spaceID, pageID),
	})
	if err != nil {
		return
	}
	s.hub.Publish(ctx, chat.Event{Type: "wiki_page", Recipients: []string{userID}, Payload: payload})
}

// wikiPageInputFromText converte o conteúdo (Markdown, o que o modelo escreve naturalmente) nos
// blocos BlockNote do editor — títulos, listas, checklists, citações, código e marcas inline. A IA
// escreve com os MESMOS componentes que uma pessoa usaria (wiki.MarkdownToBlocks).
func wikiPageInputFromText(parent *uuid.UUID, title, content, status string) wiki.PageInput {
	return wiki.PageInput{ParentID: parent, Title: title, BodyJSON: wiki.MarkdownToBlocks(content), Status: status}
}
