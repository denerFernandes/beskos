-- +goose Up
-- Múltiplos calendários por usuário: cada evento pertence a um calendário (NULL = sem categoria).
-- Free-busy usa os próprios eventos; nenhuma coluna nova é precisa para isso.
CREATE TABLE calendars (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    color         TEXT NOT NULL DEFAULT '#116466',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_calendars_owner ON calendars(owner_user_id);
ALTER TABLE events ADD COLUMN calendar_id UUID REFERENCES calendars(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE events DROP COLUMN calendar_id;
DROP TABLE calendars;
