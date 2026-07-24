-- +goose Up
-- Mensagens salvas por usuário ("saved items" do chat). Uma linha por (usuário, mensagem).
CREATE TABLE saved_messages (
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, message_id)
);
CREATE INDEX idx_saved_messages_user ON saved_messages(user_id, created_at DESC);

-- +goose Down
DROP TABLE saved_messages;
