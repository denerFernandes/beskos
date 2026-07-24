-- +goose Up
-- Eventos de entrega do SES (bounce/complaint/delivery) recebidos via SNS (webhook /webhooks/ses).
-- Base da DELIVERABILITY por tenant no manager: taxa de bounce/complaint sobre o enviado —
-- a métrica que protege a reputação do IP (a AWS suspende contas acima dos limiares dela).
CREATE TABLE mail_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Resolvido pelo domínio do remetente; NULL se o domínio não for de nenhum tenant.
    tenant_id  UUID REFERENCES tenants(id) ON DELETE SET NULL,
    type       TEXT NOT NULL CHECK (type IN ('bounce', 'complaint', 'delivery')),
    subtype    TEXT NOT NULL DEFAULT '',   -- Permanent | Transient | abuse | ...
    recipient  TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL DEFAULT '',   -- remetente (mail.source)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_mail_events_tenant_day ON mail_events(tenant_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS mail_events;
