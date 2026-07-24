-- +goose Up
-- MFA do painel: TOTP + SMS + códigos de recuperação. Login em 2 passos.
-- NOTA de segurança: totp_secret é sensível. Hoje guardado em claro; hardening futuro =
-- cifrar em repouso com chave da aplicação/KMS. sms_code/recovery são guardados como HASH.

ALTER TABLE panel_users
    ADD COLUMN totp_secret      TEXT    NOT NULL DEFAULT '',
    ADD COLUMN mfa_totp_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN phone            TEXT    NOT NULL DEFAULT '',
    ADD COLUMN mfa_sms_enabled  BOOLEAN NOT NULL DEFAULT false;

-- Códigos de recuperação (uso único). Guardados com bcrypt.
CREATE TABLE recovery_codes (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   UUID NOT NULL REFERENCES panel_users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at   TIMESTAMPTZ
);
CREATE INDEX idx_recovery_user ON recovery_codes(user_id) WHERE used_at IS NULL;

-- Desafio de login: emitido após a senha quando há MFA; troca por sessão ao verificar o 2º fator.
CREATE TABLE mfa_challenges (
    token         TEXT PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES panel_users(id) ON DELETE CASCADE,
    sms_code_hash TEXT NOT NULL DEFAULT '',
    attempts      INT  NOT NULL DEFAULT 0,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS mfa_challenges, recovery_codes CASCADE;
ALTER TABLE panel_users
    DROP COLUMN totp_secret, DROP COLUMN mfa_totp_enabled,
    DROP COLUMN phone, DROP COLUMN mfa_sms_enabled;
