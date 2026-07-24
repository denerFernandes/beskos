-- Catálogo de planos (BD como fonte de verdade — manager M2). O PRODUTO só lê (com cache);
-- quem escreve é o manager-api. Ver docs/manager.md §4 ("regra de ouro").

-- name: ListPlans :many
SELECT * FROM plans ORDER BY sort, key;

-- name: GetPlan :one
SELECT * FROM plans WHERE key = @key;

-- name: UpsertPlan :one
INSERT INTO plans (key, name, description, price_cents_per_user, min_users, max_users,
    apps_enabled, max_storage_gb, ai_tokens_per_window, meet_max_participants, meet_max_minutes,
    recording, recording_retention_days, max_daily_send, onlyoffice_edit, wiki_public_links, sort, prices)
VALUES (@key, @name, @description, @price_cents_per_user, @min_users, @max_users,
    @apps_enabled, @max_storage_gb, @ai_tokens_per_window, @meet_max_participants, @meet_max_minutes,
    @recording, @recording_retention_days, @max_daily_send, @onlyoffice_edit, @wiki_public_links, @sort, @prices)
ON CONFLICT (key) DO UPDATE SET
    name = EXCLUDED.name, description = EXCLUDED.description,
    price_cents_per_user = EXCLUDED.price_cents_per_user,
    min_users = EXCLUDED.min_users, max_users = EXCLUDED.max_users,
    apps_enabled = EXCLUDED.apps_enabled, max_storage_gb = EXCLUDED.max_storage_gb,
    ai_tokens_per_window = EXCLUDED.ai_tokens_per_window,
    meet_max_participants = EXCLUDED.meet_max_participants,
    meet_max_minutes = EXCLUDED.meet_max_minutes,
    recording = EXCLUDED.recording,
    recording_retention_days = EXCLUDED.recording_retention_days,
    max_daily_send = EXCLUDED.max_daily_send,
    onlyoffice_edit = EXCLUDED.onlyoffice_edit, wiki_public_links = EXCLUDED.wiki_public_links,
    sort = EXCLUDED.sort, prices = EXCLUDED.prices, updated_at = now()
RETURNING *;

-- name: SetPlanActive :exec
UPDATE plans SET active = @active, updated_at = now() WHERE key = @key;

-- name: ManagerSetTenantPlan :exec
UPDATE tenants SET plano = @plano WHERE id = @id;
