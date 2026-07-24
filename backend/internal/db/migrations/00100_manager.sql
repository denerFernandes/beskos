-- +goose Up
-- Manager (docs/manager.md M0): utilizadores do PAINEL DO OPERADOR do SaaS — completamente
-- separados de mail_users (outra app, outra API, outra autenticação). TOTP é OBRIGATÓRIO:
-- o primeiro login força o enrollment antes de emitir sessão (totp_confirmed).
CREATE TABLE manager_users (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL DEFAULT '',
    password_hash  TEXT NOT NULL,
    role           TEXT NOT NULL DEFAULT 'admin' CHECK (role IN ('admin', 'viewer')),
    -- Segredo TOTP cifrado em repouso (secretbox, prefixo "enc:"). Vazio = ainda não gerado.
    totp_secret    TEXT NOT NULL DEFAULT '',
    -- Só depois de confirmar um código do app o TOTP conta como ativo (e a conta pode logar).
    totp_confirmed BOOLEAN NOT NULL DEFAULT false,
    -- Anti-replay: último passo de tempo TOTP aceito (um código só vale uma vez).
    last_totp_step BIGINT NOT NULL DEFAULT 0,
    active         BOOLEAN NOT NULL DEFAULT true,
    last_login_at  TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Sessões por token opaco (revogável por DELETE), TTL curto (8h — ferramenta de operação).
CREATE TABLE manager_sessions (
    token           TEXT PRIMARY KEY,
    manager_user_id UUID NOT NULL REFERENCES manager_users(id) ON DELETE CASCADE,
    ip              TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_manager_sessions_user ON manager_sessions(manager_user_id);

-- Audit log integral do manager: TODA ação de escrita regista quem/quando/o quê (payload jsonb).
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
