-- +goose Up
-- Breakout rooms: salas paralelas dentro de uma reunião. Cada breakout é uma sala LiveKit própria
-- (room_name derivado do principal). O host cria/abre/fecha; participantes são atribuídos a um.
CREATE TABLE meeting_breakouts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    idx        INT  NOT NULL,           -- 1..N, ordem/derivação do room_name
    name       TEXT NOT NULL,
    room_name  TEXT NOT NULL,           -- <sala_principal>-b<idx>
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_breakouts_meeting ON meeting_breakouts(meeting_id);

-- Atribuição: cada participante fica em no máximo um breakout por reunião.
CREATE TABLE breakout_assignments (
    meeting_id  UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL,
    breakout_id UUID NOT NULL REFERENCES meeting_breakouts(id) ON DELETE CASCADE,
    PRIMARY KEY (meeting_id, user_id)
);
CREATE INDEX idx_breakout_assign_breakout ON breakout_assignments(breakout_id);

-- +goose Down
DROP TABLE breakout_assignments;
DROP TABLE meeting_breakouts;
