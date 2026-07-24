-- +goose Up
-- Avisos da organização (mural de notícias). Admins/owner publicam; todos os membros do tenant leem.
CREATE TABLE announcements (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    author_id   UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    author_name TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    published   BOOLEAN NOT NULL DEFAULT true, -- despublicado = invisível para membros (rascunho)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_announcements_tenant ON announcements (tenant_id, created_at DESC);

-- +goose Down
DROP TABLE announcements;
