-- +goose Up
-- Settings do manager (M4): pares chave→jsonb editáveis no painel do operador. Primeiro uso:
-- o MODELO DE CUSTOS (custo fixo mensal da infra + tarifas por driver) que alimenta a margem
-- por tenant. Estimativas — conciliar com as faturas reais mensalmente (doc §8).
CREATE TABLE manager_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Defaults do consulting de infra (OVH ~€65/mês fixo; R2 ~1.5c/GB-mês; DeepSeek ~30c/1M tokens;
-- SES ~10c/1k e-mails). Tudo em CENTS de euro; editável no manager.
INSERT INTO manager_settings (key, value) VALUES ('cost_model', '{
    "currency": "EUR",
    "fixed_month_cents": 6500,
    "storage_gb_month_cents": 2,
    "ai_per_mtok_cents": 30,
    "email_per_1k_cents": 10
}'::jsonb);

-- +goose Down
DROP TABLE IF EXISTS manager_settings;
