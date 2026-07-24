-- +goose Up
-- Templates de resposta (canned responses): textos reutilizáveis do dono para inserir na composição.
-- Escopo por dono (não partilhado nesta fase) e por tenant, como os deals/contatos.
CREATE TABLE mail_templates (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    subject       TEXT NOT NULL DEFAULT '',
    body_html     TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_templates_owner ON mail_templates(owner_user_id, tenant_id);

-- +goose Down
DROP TABLE mail_templates;
