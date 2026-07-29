-- +goose Up
-- Troca do modelo de embeddings: OpenAI `text-embedding-3-small` → `qwen/qwen3-embedding-8b`
-- servido na UE (docs/landing-europa.md, Fase 0). Vetores de modelos diferentes NÃO são
-- comparáveis: a distância entre um vetor antigo e uma pergunta nova é ruído, e a busca semântica
-- devolveria resultados errados sem dar erro. Por isso limpa-se tudo — o worker reindexa sozinho
-- (ListWikiPagesNeedingEmbedding apanha as páginas sem chunk mais fresco que updated_at).
--
-- A dimensão continua 1536: o Qwen3 é Matryoshka e trunca a pedido (EMBEDDINGS_DIMENSIONS), o que
-- mantém a coluna e o índice ivfflat como estão — o pgvector não indexa acima de 2000 dimensões.
DELETE FROM wiki_chunks;

-- +goose Down
-- Sem volta: os embeddings antigos não se recuperam. O worker reindexa com o modelo configurado.
SELECT 1;
