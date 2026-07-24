-- +goose Up
-- Subscrições de Web Push (notificações desktop com o Beskos fechado). Uma linha por browser/
-- dispositivo: o `endpoint` é a URL do serviço de push do browser e é única globalmente. p256dh/auth
-- são as chaves públicas da subscrição, usadas para cifrar o payload (RFC 8291).
CREATE TABLE push_subscriptions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    endpoint   TEXT NOT NULL UNIQUE,
    p256dh     TEXT NOT NULL,
    auth       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_push_subscriptions_user ON push_subscriptions (user_id);

-- +goose Down
DROP TABLE push_subscriptions;
