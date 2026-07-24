-- +goose Up
-- Anexos no card: liga um ficheiro do Files a um card. O download é TENANT-scoped (qualquer membro do
-- quadro abre), diferente do Files pessoal (owner-scoped) — por isso a tabela guarda o tenant.
-- Cai por cascata com o card OU com o ficheiro (se o ficheiro for purgado, o anexo some).
CREATE TABLE card_attachments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    card_id    UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    added_by   UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (card_id, file_id)
);
CREATE INDEX idx_card_attachments ON card_attachments (card_id);

-- +goose Down
DROP TABLE card_attachments;
