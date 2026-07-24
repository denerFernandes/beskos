-- +goose Up
-- Snooze de e-mail: a mensagem sai da caixa (o cliente move para a pasta "Snoozed") e o worker a
-- devolve à Caixa de entrada em wake_at — mesmo com o app fechado. Guardamos só o id JMAP e a hora.
CREATE TABLE snoozed_emails (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    email_id     TEXT NOT NULL,   -- id JMAP da mensagem
    wake_at      TIMESTAMPTZ NOT NULL,
    done         BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_snooze_due ON snoozed_emails (wake_at) WHERE NOT done;

-- +goose Down
DROP TABLE snoozed_emails;
