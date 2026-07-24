-- +goose Up
-- Política de segurança DA ORGANIZAÇÃO (pedido 22/07/2026): com mfa_required ligado, todo
-- colaborador do tenant sem 2º fator é OBRIGADO a configurar TOTP ou SMS no próximo acesso
-- (gate bloqueante no webmail via /me). A página admin/security passa a gerir isto.
ALTER TABLE tenants ADD COLUMN mfa_required BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE tenants DROP COLUMN IF EXISTS mfa_required;
