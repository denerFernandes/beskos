-- +goose Up
-- Histórico/atividade do card: quem fez o quê e quando (criou, moveu, editou). Cai com o card.
-- detail guarda um dado livre do evento (ex.: o column_id de destino num "moved"); a UI resolve o nome.
CREATE TABLE card_activity (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    card_id    UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    actor_id   UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    kind       TEXT NOT NULL,          -- created | moved | updated
    detail     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_card_activity ON card_activity (card_id, created_at DESC);

-- +goose Down
DROP TABLE card_activity;
