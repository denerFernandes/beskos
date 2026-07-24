-- +goose Up
-- Bookmarks do canal: coleção curada de LINKS e FICHEIROS (estilo "Files & links" do Slack). Membros
-- adicionam e removem. Ficheiro referencia um storage_file; o download é autorizado por participação
-- no canal (não pela posse do ficheiro).
CREATE TABLE channel_bookmarks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,                 -- 'link' | 'file'
    title      TEXT NOT NULL DEFAULT '',
    url        TEXT NOT NULL DEFAULT '',
    file_id    UUID REFERENCES storage_files(id) ON DELETE CASCADE,
    file_name  TEXT NOT NULL DEFAULT '',
    file_size  BIGINT NOT NULL DEFAULT 0,
    added_by   UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_bookmarks_channel ON channel_bookmarks (channel_id, created_at DESC);

-- +goose Down
DROP TABLE channel_bookmarks;
