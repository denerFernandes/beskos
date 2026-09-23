-- +goose Up
-- SES delivery events (bounce/complaint/delivery) received via SNS (webhook /webhooks/ses).
-- Basis of per-tenant DELIVERABILITY in the manager: bounce/complaint rate over what was sent --
-- the metric that protects the IP's reputation (AWS suspends accounts above its thresholds).
CREATE TABLE mail_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Resolved from the sender's domain; NULL if the domain belongs to no tenant.
    tenant_id  UUID REFERENCES tenants(id) ON DELETE SET NULL,
    type       TEXT NOT NULL CHECK (type IN ('bounce', 'complaint', 'delivery')),
    subtype    TEXT NOT NULL DEFAULT '',   -- Permanent | Transient | abuse | ...
    recipient  TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL DEFAULT '',   -- sender (mail.source)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_mail_events_tenant_day ON mail_events(tenant_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS mail_events;
