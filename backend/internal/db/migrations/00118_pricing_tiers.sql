-- +goose Up
-- Restructures the catalog into FEATURE TIERS: base currency USD; Base/Business/Pro
-- self-serve + Enterprise "contact us" (landing page only, outside the self-serve checkout so it
-- does not become a $0 checkout). Replaces the old volume bands (starter/growth/business, whose
-- per-seat price DECREASED). Safe: there is only 1 tenant, on trial; no plan had been mirrored in
-- Stripe yet. Base price in USD cents; `prices` carries EUR/GBP/BRL/PLN (currency_options).
DELETE FROM plans WHERE key IN ('starter', 'growth', 'business');

INSERT INTO plans (key, name, description, price_cents_per_user, min_users, max_users,
                   max_storage_gb, meet_max_participants, recording, recording_retention_days,
                   max_daily_send, prices, active, sort)
VALUES
    ('base',     'Base',     '', 700,  1, 10,  30,   25,  'offline',          30,  1000,
        '{"EUR":699,"GBP":599,"BRL":3900,"PLN":2900}',    true, 1),
    ('business', 'Business', '', 1200, 1, 50,  200,  100, 'offline',          30,  5000,
        '{"EUR":1199,"GBP":999,"BRL":6900,"PLN":4900}',   true, 2),
    ('pro',      'Pro',      '', 2000, 1, 200, 1024, 200, 'offline_priority', 30,  20000,
        '{"EUR":1999,"GBP":1699,"BRL":10900,"PLN":7900}', true, 3)
ON CONFLICT (key) DO UPDATE SET
    name                  = EXCLUDED.name,
    price_cents_per_user  = EXCLUDED.price_cents_per_user,
    min_users             = EXCLUDED.min_users,
    max_users             = EXCLUDED.max_users,
    max_storage_gb        = EXCLUDED.max_storage_gb,
    meet_max_participants = EXCLUDED.meet_max_participants,
    recording             = EXCLUDED.recording,
    max_daily_send        = EXCLUDED.max_daily_send,
    prices                = EXCLUDED.prices,
    active                = EXCLUDED.active,
    sort                  = EXCLUDED.sort,
    updated_at            = now();

-- +goose Down
-- Does not recreate the old bands (the 00101 seed is the historical reference); only removes the
-- new ones.
DELETE FROM plans WHERE key IN ('base', 'business', 'pro');
