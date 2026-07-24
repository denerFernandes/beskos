-- +goose Up
-- (1) Política de MFA da organização ganha o MÉTODO exigido pelo admin (pedido 23/07/2026):
--     'any' = o utilizador escolhe TOTP ou SMS; 'totp'/'sms' = só esse método satisfaz o gate.
ALTER TABLE tenants ADD COLUMN mfa_method TEXT NOT NULL DEFAULT 'any'
    CHECK (mfa_method IN ('any', 'totp', 'sms'));

-- (2) Perfil do utilizador (menu do avatar): cargo e foto. A foto é um data-URL pequeno
--     (o cliente redimensiona para ~128px antes de enviar; teto de tamanho no handler).
ALTER TABLE mail_users ADD COLUMN job_title TEXT NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN avatar TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mail_users DROP COLUMN IF EXISTS avatar;
ALTER TABLE mail_users DROP COLUMN IF EXISTS job_title;
ALTER TABLE tenants DROP COLUMN IF EXISTS mfa_method;
