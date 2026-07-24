-- +goose Up
-- Billing: idempotência de webhooks (brief §10). A assinatura vive em subscriptions (00001)
-- e o plano atual do tenant em tenants.plano.
CREATE TABLE processed_webhooks (
    event_id     TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS processed_webhooks;
