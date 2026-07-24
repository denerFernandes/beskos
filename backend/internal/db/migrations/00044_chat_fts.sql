-- +goose Up
-- Full-text search do chat: coluna tsvector GERADA (auto-atualiza) + índice GIN. 'simple' (sem
-- stemming por idioma) serve para PT/EN misturados. Substitui o ILIKE das buscas de mensagem.
ALTER TABLE messages ADD COLUMN body_tsv tsvector GENERATED ALWAYS AS (to_tsvector('simple', body)) STORED;
CREATE INDEX idx_messages_body_tsv ON messages USING GIN (body_tsv);

-- +goose Down
DROP INDEX IF EXISTS idx_messages_body_tsv;
ALTER TABLE messages DROP COLUMN IF EXISTS body_tsv;
