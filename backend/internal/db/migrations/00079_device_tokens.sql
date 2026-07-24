-- +goose Up
-- Tokens de dispositivo para clientes headless (o daemon de sincronização de ficheiros, escrito em
-- Rust, que corre fora do browser e não tem cookie de sessão). Diferente da sessão do webmail:
--   - vida longa (o daemon fica ligado por meses) → guardamos só o HASH SHA-256, nunca o token cru;
--   - escopo restrito ('sync' = ler o feed de changes + baixar) — não dá acesso ao resto da API;
--   - revogável por dispositivo e com expiração opcional.
CREATE TABLE device_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,                     -- rótulo do dispositivo ("MacBook do João")
    token_hash   TEXT NOT NULL UNIQUE,              -- SHA-256 do token em hex; o cru só aparece uma vez
    token_prefix TEXT NOT NULL,                     -- primeiros chars, para identificar na lista
    scope        TEXT NOT NULL DEFAULT 'sync',
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,                       -- NULL = não expira
    revoked_at   TIMESTAMPTZ,                       -- preenchido = revogado (deixa de autenticar)
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Listagem dos tokens ativos de um utilizador (o índice cobre o caso comum).
CREATE INDEX idx_device_tokens_user ON device_tokens (mail_user_id, created_at DESC);

-- +goose Down
DROP TABLE device_tokens;
