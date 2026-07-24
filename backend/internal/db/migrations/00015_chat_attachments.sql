-- +goose Up
-- Anexos de mensagens do chat: liga uma mensagem a um arquivo do storage (MinIO). name/size
-- denormalizados para render rápido; a autorização de download é por participação no canal.

CREATE TABLE message_attachments (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    size       BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, file_id)
);
CREATE INDEX idx_msg_att_file ON message_attachments(file_id);

-- +goose Down
DROP TABLE IF EXISTS message_attachments;
