-- +goose Up
-- Meet: reuniões (agendadas/instantâneas) + participantes com RSVP. Integra com o calendário
-- (event_id) e com o LiveKit (room_name + tempos reais via webhook).

CREATE TABLE meetings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id   UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    room_name       TEXT NOT NULL UNIQUE,
    scheduled_start TIMESTAMPTZ,          -- NULL = reunião instantânea
    scheduled_end   TIMESTAMPTZ,
    actual_start    TIMESTAMPTZ,          -- via webhook room_started
    actual_end      TIMESTAMPTZ,          -- via webhook room_finished (→ duração)
    status          TEXT NOT NULL DEFAULT 'scheduled', -- scheduled | live | ended | canceled
    event_id        UUID REFERENCES events(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE meeting_participants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id  UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,            -- convidado (pode ser externo)
    user_id     UUID REFERENCES mail_users(id) ON DELETE SET NULL, -- se for do tenant
    role        TEXT NOT NULL DEFAULT 'guest',   -- host | guest
    rsvp        TEXT NOT NULL DEFAULT 'pending', -- pending | accepted | declined | tentative
    notified_at TIMESTAMPTZ,
    joined_at   TIMESTAMPTZ,
    left_at     TIMESTAMPTZ,
    UNIQUE(meeting_id, email)
);

CREATE INDEX idx_meetings_tenant ON meetings(tenant_id, scheduled_start);
CREATE INDEX idx_meetings_room ON meetings(room_name);
CREATE INDEX idx_mparticipants_meeting ON meeting_participants(meeting_id);
CREATE INDEX idx_mparticipants_email ON meeting_participants(tenant_id, email);

-- +goose Down
DROP TABLE IF EXISTS meeting_participants;
DROP TABLE IF EXISTS meetings;
