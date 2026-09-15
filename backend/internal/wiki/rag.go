package wiki

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// RAG semântico (docs/wiki-features.md §7.2): as páginas published de espaços com ai_indexed são
// partidas em trechos e embeddadas por um worker idempotente; a busca é kNN por cosseno SEMPRE
// filtrada pela permissão de quem pergunta (SearchWikiChunks aplica a mesma superfície vis, §7.4).

// Embedder é o cliente de embeddings (implementado por ai.Embedder). Interface local para o pacote
// wiki não depender do pacote ai (a ligação faz-se no main/httpapi).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// chunkSize/chunkOverlap: ~500 tokens por trecho com sobreposição de um parágrafo (§7.2).
const (
	chunkSize    = 2000 // caracteres por trecho (aprox. 500 tokens)
	maxChunks    = 64   // teto por página (páginas gigantes não explodem o índice)
	embedBatch   = 32   // trechos por chamada ao provedor
	searchLimit  = 8    // default de resultados da busca
	minChunkText = 20   // trechos mais curtos que isto não valem embedding
)

// chunkText parte o texto em trechos de ~chunkSize caracteres em fronteiras de parágrafo, repetindo
// o último parágrafo como sobreposição. Determinístico e puro (testável).
func chunkText(text string) []string {
	paras := strings.Split(text, "\n")
	var chunks []string
	var cur strings.Builder
	lastPara := ""
	flush := func() {
		if s := strings.TrimSpace(cur.String()); len(s) >= minChunkText {
			chunks = append(chunks, s)
		}
		cur.Reset()
	}
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Parágrafo maior que um trecho: corta duro em fatias de chunkSize.
		for len(p) > chunkSize {
			flush()
			cut := p[:chunkSize]
			chunks = append(chunks, cut)
			p = p[chunkSize:]
		}
		if cur.Len() > 0 && cur.Len()+len(p)+1 > chunkSize {
			flush()
			// Sobreposição: o novo trecho começa com o último parágrafo do anterior.
			if lastPara != "" && len(lastPara) < chunkSize/2 {
				cur.WriteString(lastPara)
				cur.WriteByte('\n')
			}
		}
		cur.WriteString(p)
		cur.WriteByte('\n')
		lastPara = p
		if len(chunks) >= maxChunks {
			return chunks
		}
	}
	flush()
	if len(chunks) > maxChunks {
		chunks = chunks[:maxChunks]
	}
	return chunks
}

// vecLiteral formata um embedding como literal pgvector "[0.1,0.2,...]" (o SQL casta ::vector).
func vecLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// EmbedPending (re)embedda até maxPages páginas desatualizadas e limpa chunks obsoletos. Idempotente:
// pode correr em qualquer cadência; falha numa página não trava as restantes. Devolve quantas páginas
// foram processadas.
func (s *Service) EmbedPending(ctx context.Context, e Embedder, maxPages int32) (int, error) {
	if _, err := s.q.DeleteStaleWikiChunks(ctx); err != nil {
		return 0, fmt.Errorf("wiki embed: cleanup stale chunks: %w", err)
	}
	pages, err := s.q.ListWikiPagesNeedingEmbedding(ctx, maxPages)
	if err != nil {
		return 0, fmt.Errorf("wiki embed: list pending pages: %w", err)
	}
	done := 0
	var firstErr error
	for _, p := range pages {
		if err := s.embedPage(ctx, e, p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		done++
	}
	return done, firstErr
}

func (s *Service) embedPage(ctx context.Context, e Embedder, p db.ListWikiPagesNeedingEmbeddingRow) error {
	chunks := chunkText(p.BodyText)
	// Página não-vazia mas abaixo do mínimo de um trecho: indexa na mesma como um único chunk —
	// senão ficaria eternamente na lista de pendentes (sem chunk fresco) e fora do RAG.
	if len(chunks) == 0 {
		if t := strings.TrimSpace(p.BodyText); t != "" {
			chunks = []string{t}
		}
	}
	if len(chunks) == 0 {
		// Nada embeddável: garante que não sobra índice antigo desta página.
		if err := s.q.DeleteWikiChunksFrom(ctx, db.DeleteWikiChunksFromParams{PageID: p.ID, FromIndex: 0}); err != nil {
			return fmt.Errorf("wiki embed: clear page %s: %w", p.ID, err)
		}
		return nil
	}
	for start := 0; start < len(chunks); start += embedBatch {
		end := min(start+embedBatch, len(chunks))
		batch := chunks[start:end]
		// O título entra no input do embedding (contexto), mas o chunk_text guardado é o trecho cru.
		inputs := make([]string, len(batch))
		for i, c := range batch {
			inputs[i] = p.Title + "\n\n" + c
		}
		vecs, err := e.Embed(ctx, inputs)
		if err != nil {
			return fmt.Errorf("wiki embed: page %s: %w", p.ID, err)
		}
		for i, v := range vecs {
			if err := s.q.UpsertWikiChunk(ctx, db.UpsertWikiChunkParams{
				TenantID: p.TenantID, PageID: p.ID, SpaceID: p.SpaceID,
				ChunkIndex: int32(start + i), ChunkText: batch[i], Embedding: vecLiteral(v),
			}); err != nil {
				return fmt.Errorf("wiki embed: upsert chunk %d of page %s: %w", start+i, p.ID, err)
			}
		}
	}
	// Apara o que sobrou de uma versão anterior maior.
	if err := s.q.DeleteWikiChunksFrom(ctx, db.DeleteWikiChunksFromParams{
		PageID: p.ID, FromIndex: int32(len(chunks)),
	}); err != nil {
		return fmt.Errorf("wiki embed: trim page %s: %w", p.ID, err)
	}
	return nil
}

// SearchChunks embedda a pergunta e devolve os trechos mais próximos QUE O UTILIZADOR PODE VER
// (a query aplica tenant + vis + ai_indexed + published). limit<=0 usa o default.
func (s *Service) SearchChunks(ctx context.Context, e Embedder, tenantID, userID uuid.UUID, query string, limit int32) ([]db.SearchWikiChunksRow, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrInvalid
	}
	if limit <= 0 {
		limit = searchLimit
	}
	vecs, err := e.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("wiki search: embed query: %w", err)
	}
	// B9: busca híbrida — 1ª passagem com pré-filtro FTS (barato, GIN); se não devolver nada
	// (vocabulário diferente do texto, típico de perguntas), repete só com o kNN semântico (recall).
	params := db.SearchWikiChunksParams{
		QueryEmbedding: vecLiteral(vecs[0]), TenantID: tenantID, UserID: userID,
		MaxResults: limit, UseFts: true, QueryText: query,
	}
	rows, err := s.q.SearchWikiChunks(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows, nil
	}
	params.UseFts = false
	return s.q.SearchWikiChunks(ctx, params)
}

// SearchPagesText é a busca LEXICAL, direta às páginas e sem embeddings. Serve a instalação sem
// EMBEDDINGS_API_KEY: aí não existem chunks (só nascem no embedPage), por isso a alternativa não
// pode ser procurar neles. Casa palavras em vez de significado — para termos exatos chega.
func (s *Service) SearchPagesText(ctx context.Context, tenantID, userID uuid.UUID, query string, limit int32) ([]db.SearchWikiPagesTextRow, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrInvalid
	}
	if limit <= 0 {
		limit = searchLimit
	}
	return s.q.SearchWikiPagesText(ctx, db.SearchWikiPagesTextParams{
		TenantID: tenantID, UserID: userID, QueryText: query, MaxResults: limit,
	})
}
