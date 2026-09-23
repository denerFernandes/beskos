-- +goose Up
-- Manager (docs/manager.md): users of the SaaS OPERATOR CONSOLE -- completely separate from
-- mail_users (another app, another API, another authentication). TOTP is MANDATORY: the first
-- login forces enrollment before a session is issued (totp_confirmed).
CREATE TABLE manager_users (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL DEFAULT '',
    password_hash  TEXT NOT NULL,
    role           TEXT NOT NULL DEFAULT 'admin' CHECK (role IN ('admin', 'viewer')),
    -- TOTP secret encrypted at rest (secretbox, "enc:" prefix). Empty = not generated yet.
    totp_secret    TEXT NOT NULL DEFAULT '',
    -- TOTP only counts as active (and the account can log in) after a code from the app is confirmed.
    totp_confirmed BOOLEAN NOT NULL DEFAULT false,
    -- Anti-replay: last accepted TOTP time step (a code is valid only once).
    last_totp_step BIGINT NOT NULL DEFAULT 0,
    active         BOOLEAN NOT NULL DEFAULT true,
    last_login_at  TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Opaque-token sessions (revocable by DELETE), short TTL (8h -- an operations tool).
CREATE TABLE manager_sessions (
    token           TEXT PRIMARY KEY,
    manager_user_id UUID NOT NULL REFERENCES manager_users(id) ON DELETE CASCADE,
    ip              TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_manager_sessions_user ON manager_sessions(manager_user_id);

-- Full manager audit log: EVERY write action records who/when/what (jsonb payload).
CREATE TABLE manager_audit (
    id          BIGSERIAL PRIMARY KEY,
    actor_id    UUID REFERENCES manager_users(id) ON DELETE SET NULL,
    actor_email TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    target      TEXT NOT NULL DEFAULT '',
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_manager_audit_created ON manager_audit(created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS manager_audit;
DROP TABLE IF EXISTS manager_sessions;
DROP TABLE IF EXISTS manager_users;
