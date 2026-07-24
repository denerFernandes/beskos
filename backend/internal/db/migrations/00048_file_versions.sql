-- +goose Up
-- Histórico de versões navegável: cada save do OnlyOffice antes sobrescrevia o blob e perdia o
-- anterior. Agora, antes de gravar, a versão CORRENTE é fotografada aqui — dá para listar e restaurar.
-- Blobs são content-addressed (tenant/hash) e dedup, então uma versão pode partilhar blob com o
-- ficheiro ou outra versão: a purga só apaga o blob quando NENHUM registo (ficheiro OU versão) o usa.
CREATE TABLE file_versions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    blob_key   TEXT NOT NULL,
    hash       TEXT NOT NULL,
    tamanho    BIGINT NOT NULL,
    versao     INT NOT NULL,
    created_by UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_file_versions ON file_versions (file_id, versao DESC);

-- +goose Down
DROP TABLE file_versions;
