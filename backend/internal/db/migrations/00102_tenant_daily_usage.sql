-- +goose Up
-- Rollup diário de uso por tenant (manager M3, docs/manager.md §3.5). O worker congela o dia
-- (upsert idempotente, a cada hora, para hoje e ontem); dashboards/relatórios leem daqui em vez
-- de agregar as tabelas grandes ao vivo.
CREATE TABLE tenant_daily_usage (
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    day              DATE NOT NULL,
    users            INT NOT NULL DEFAULT 0,
    users_active     INT NOT NULL DEFAULT 0,  -- mail_seen_at no dia
    storage_bytes    BIGINT NOT NULL DEFAULT 0,
    files            INT NOT NULL DEFAULT 0,
    ai_tokens        BIGINT NOT NULL DEFAULT 0,
    meet_minutes     INT NOT NULL DEFAULT 0,  -- soma de sessões (joined_at→left_at) no dia
    recordings       INT NOT NULL DEFAULT 0,
    recordings_bytes BIGINT NOT NULL DEFAULT 0,
    wiki_pages       INT NOT NULL DEFAULT 0,
    cards            INT NOT NULL DEFAULT 0,
    -- Envio de e-mail: fica 0 até o relay SES medir (registado no doc §4 como knob sem medição).
    mail_sent        INT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, day)
);
CREATE INDEX idx_tenant_daily_usage_day ON tenant_daily_usage(day DESC);

-- +goose Down
DROP TABLE IF EXISTS tenant_daily_usage;
