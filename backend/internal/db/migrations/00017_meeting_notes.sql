-- +goose Up
-- Notas da reunião: um documento compartilhado por reunião, editado ao vivo por quem está na sala.
-- Last-write-wins com autosave (notas de reunião raramente têm dois digitando ao mesmo tempo);
-- CRDT seria complexidade sem uso real aqui.

CREATE TABLE meeting_notes (
    meeting_id UUID PRIMARY KEY REFERENCES meetings(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    body       TEXT NOT NULL DEFAULT '',
    updated_by UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS meeting_notes;
