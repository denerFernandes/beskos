-- +goose Up
-- Daily tenant usage rollup. The worker upserts today and yesterday hourly.
CREATE TABLE tenant_daily_usage (
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    day              DATE NOT NULL,
    users            INT NOT NULL DEFAULT 0,
    users_active     INT NOT NULL DEFAULT 0,  -- mail_seen_at on that day
    storage_bytes    BIGINT NOT NULL DEFAULT 0,
    files            INT NOT NULL DEFAULT 0,
    ai_tokens        BIGINT NOT NULL DEFAULT 0,
    meet_minutes     INT NOT NULL DEFAULT 0,  -- sum of sessions (joined_at->left_at) on that day
    recordings       INT NOT NULL DEFAULT 0,
    recordings_bytes BIGINT NOT NULL DEFAULT 0,
    wiki_pages       INT NOT NULL DEFAULT 0,
    cards            INT NOT NULL DEFAULT 0,
    -- Email sending: stays 0 until the SES relay measures it (recorded in docs/manager.md §4 as a knob
    -- without metering).
    mail_sent        INT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, day)
);
CREATE INDEX idx_tenant_daily_usage_day ON tenant_daily_usage(day DESC);

-- +goose Down
DROP TABLE IF EXISTS tenant_daily_usage;
