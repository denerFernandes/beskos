-- +goose Up
-- Infrastructure cost PER SERVER: each VPS/bare-metal machine with its own monthly cost.
-- The fixed cost in the manager's finances = sum of active servers + the "other fixed" costs of
-- cost_model (which no longer carries the VPSs -- zeroed below so they are not counted twice).
CREATE TABLE manager_servers (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    provider      TEXT NOT NULL DEFAULT '',
    monthly_cents INT NOT NULL DEFAULT 0,
    notes         TEXT NOT NULL DEFAULT '',
    active        BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed: the layout from the infrastructure quote (OVH, 2026-07-22) -- 2x VPS-3 (core + media) +
-- VPS-1 (mail) ~ EUR 65.
INSERT INTO manager_servers (name, provider, monthly_cents, notes) VALUES
    ('vps-core',  'OVH VPS-3', 2600, 'api + postgres + redis + manager'),
    ('vps-media', 'OVH VPS-3', 2600, 'livekit + egress + onlyoffice'),
    ('vps-mail',  'OVH VPS-1', 1300, 'stalwart (IP dedicado ao correio)');

-- The generic fixed cost no longer includes the VPSs (avoids double counting); it remains for
-- "other fixed" costs (domains, backups, licenses...).
UPDATE manager_settings
SET value = jsonb_set(value, '{fixed_month_cents}', '0'::jsonb), updated_at = now()
WHERE key = 'cost_model';

-- +goose Down
DROP TABLE IF EXISTS manager_servers;
