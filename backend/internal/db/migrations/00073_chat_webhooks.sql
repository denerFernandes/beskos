-- +goose Up
-- Webhooks de entrada (bots/integrações): um token por canal; um POST externo vira mensagem. A
-- mensagem guarda bot_name para o frontend a mostrar como integração (em vez do autor humano).
ALTER TABLE messages ADD COLUMN bot_name TEXT NOT NULL DEFAULT '';

CREATE TABLE chat_webhooks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    token      TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_by UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_webhooks_channel ON chat_webhooks(channel_id);

-- +goose Down
DROP TABLE chat_webhooks;
ALTER TABLE messages DROP COLUMN bot_name;
