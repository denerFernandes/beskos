-- +goose Up
-- Manager settings: key->jsonb pairs editable in the operator console. First use: the COST MODEL
-- (monthly fixed infrastructure cost + per-driver rates) that feeds the per-tenant margin.
-- Estimates -- reconcile against the real invoices monthly (docs/manager.md §8).
CREATE TABLE manager_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Defaults from the infrastructure quote (OVH ~EUR 65/month fixed; R2 ~1.5c/GB-month; DeepSeek
-- ~30c/1M tokens; SES ~10c/1k emails). All in euro CENTS; editable in the manager.
INSERT INTO manager_settings (key, value) VALUES ('cost_model', '{
    "currency": "EUR",
    "fixed_month_cents": 6500,
    "storage_gb_month_cents": 2,
    "ai_per_mtok_cents": 30,
    "email_per_1k_cents": 10
}'::jsonb);

-- +goose Down
DROP TABLE IF EXISTS manager_settings;
