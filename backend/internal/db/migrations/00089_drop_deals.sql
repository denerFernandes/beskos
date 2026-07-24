-- +goose Up
-- Remove o pipeline de negócios (deals): o produto é correio + colaboração, não um CRM de vendas.
-- Decisão do produto (não somos um CRM). A funcionalidade e o seu código saíram por inteiro.
DROP TABLE IF EXISTS deals;

-- +goose Down
-- Recria a tabela tal como estava (00071), caso se reverta.
CREATE TABLE deals (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    title         TEXT NOT NULL,
    value_cents   BIGINT NOT NULL DEFAULT 0,
    stage         TEXT NOT NULL DEFAULT 'lead',
    contact_id    UUID REFERENCES contacts(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_deals_tenant ON deals(tenant_id, stage);
