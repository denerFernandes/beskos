-- +goose Up
-- RAG semântico da wiki (docs/wiki-features.md §7.2). Requer a extensão pgvector (imagem
-- pgvector/pgvector:pg16). Cada linha é um trecho (chunk) de uma página, com o seu embedding.
-- Provedor de embeddings escolhido: OpenAI text-embedding-3-small → dimensão D = 1536.
-- Um worker (re)computa os chunks ao gravar/apagar página (idempotente). A busca é kNN por cosseno,
-- SEMPRE filtrada pela permissão de quem pergunta (CTE vis) + tenant_id + ai_indexed.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE wiki_chunks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    page_id     UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    space_id    UUID NOT NULL REFERENCES wiki_spaces(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL,
    chunk_text  TEXT NOT NULL,
    embedding   vector(1536) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (page_id, chunk_index)
);
CREATE INDEX idx_wiki_chunks_page ON wiki_chunks(tenant_id, page_id);
-- Índice ANN por cosseno (ivfflat). O filtro de permissão é aplicado na query (join com a CTE vis).
CREATE INDEX idx_wiki_chunks_embedding ON wiki_chunks
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- +goose Down
DROP TABLE IF EXISTS wiki_chunks;
-- Mantém a extensão vector (pode ser usada por outras features); remover só se explicitamente pedido.
