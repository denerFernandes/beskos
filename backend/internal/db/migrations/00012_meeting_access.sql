-- +goose Up
-- Acesso à reunião (pública ou por aprovação) + "sala de espera" (lobby) para convidados externos.

ALTER TABLE meetings ADD COLUMN access TEXT NOT NULL DEFAULT 'public'; -- public | approval

CREATE TABLE meeting_lobby (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id  UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    room_name   TEXT NOT NULL,
    name        TEXT NOT NULL,           -- nome que o convidado digitou
    identity    TEXT NOT NULL,           -- identity do LiveKit (guest-<rand>)
    status      TEXT NOT NULL DEFAULT 'pending', -- pending | admitted | denied
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lobby_meeting ON meeting_lobby(meeting_id, status);

-- +goose Down
DROP TABLE IF EXISTS meeting_lobby;
ALTER TABLE meetings DROP COLUMN access;
