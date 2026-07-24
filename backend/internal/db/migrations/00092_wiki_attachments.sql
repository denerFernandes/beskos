-- +goose Up
-- Anexos das páginas da wiki (docs/wiki-features.md §3.6): liga ficheiros do Files (storage_files) a
-- uma página, reusando o storage existente (não reinventa upload). Anexar/desanexar exige editor;
-- descarregar exige viewer da página (conferido no serviço). Multi-tenant: tenant_id + índice.
CREATE TABLE wiki_attachments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    page_id    UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    added_by   UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (page_id, file_id)
);
CREATE INDEX idx_wiki_attachments_page ON wiki_attachments(tenant_id, page_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS wiki_attachments;
