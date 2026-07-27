-- +goose Up
-- Arquivar um canal (estilo Slack): sai das listas e fica só de leitura, mas o histórico continua lá
-- e dá para restaurar. Não substitui o apagar — esse continua a existir e continua a ser definitivo.
ALTER TABLE channels ADD COLUMN archived_at TIMESTAMPTZ;
CREATE INDEX idx_channels_archived ON channels(tenant_id) WHERE archived_at IS NOT NULL;

-- +goose Down
DROP INDEX idx_channels_archived;
ALTER TABLE channels DROP COLUMN archived_at;
