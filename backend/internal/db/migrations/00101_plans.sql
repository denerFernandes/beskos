-- +goose Up
-- Plan catalog used by billing, with a cached fallback when empty.
-- Tenants reference plans through tenants.plan.
CREATE TABLE plans (
    key                      TEXT PRIMARY KEY,
    name                     TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    price_cents_per_user     INT NOT NULL DEFAULT 0,
    min_users                INT NOT NULL DEFAULT 0,  -- minimum organization size (0 = no minimum)
    max_users                INT NOT NULL DEFAULT 0,  -- cap on mailboxes/users (0 = no cap)
    -- Rail apps allowed on the plan. Empty = all (the gating adds up with tenants.disabled_apps).
    apps_enabled             TEXT[] NOT NULL DEFAULT '{}',
    max_storage_gb           INT NOT NULL DEFAULT 0,  -- tenant storage pool (0 = no cap)
    ai_tokens_per_window     INT NOT NULL DEFAULT 0,  -- AI quota per 6h window (0 = product default)
    meet_max_participants    INT NOT NULL DEFAULT 0,
    meet_max_minutes         INT NOT NULL DEFAULT 0,  -- maximum room duration (0 = no cap)
    -- Meeting recording: none | offline (track egress + ffmpeg assembly)
    -- | offline_priority (same thing, priority queue).
    recording                TEXT NOT NULL DEFAULT 'none'
        CHECK (recording IN ('none', 'offline', 'offline_priority')),
    recording_retention_days INT NOT NULL DEFAULT 30,
    max_daily_send           INT NOT NULL DEFAULT 0,  -- tenant sends per day (0 = no cap)
    onlyoffice_edit          BOOLEAN NOT NULL DEFAULT true,
    wiki_public_links        BOOLEAN NOT NULL DEFAULT true,
    active                   BOOLEAN NOT NULL DEFAULT true,
    sort                     INT NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed: the 4 plans from the in-code catalog, with the SAME limits (zero behavior change).
-- Recording per the decision: entry plan without recording; paid = offline; top = priority.
INSERT INTO plans (key, name, price_cents_per_user, min_users, max_users, max_storage_gb,
                   ai_tokens_per_window, meet_max_participants, recording, max_daily_send, sort)
VALUES
    ('trial',    'Trial',    0,    0, 3,   5,    0, 5,   'none',             200,   0),
    ('starter',  'Starter',  1900, 1, 10,  50,   0, 25,  'offline',          1000,  1),
    ('growth',   'Growth',   1500, 11, 50, 250,  0, 50,  'offline',          5000,  2),
    ('business', 'Business', 1200, 51, 200, 1024, 0, 200, 'offline_priority', 20000, 3);

-- +goose Down
DROP TABLE IF EXISTS plans;
