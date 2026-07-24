-- +goose Up
-- Preferências do utilizador, organizadas por app: {"email":{"signature":"…"}, "chat":{…}}.
--
-- JSONB (decisão do cliente, 2026-07-16): acrescentar uma preferência nova nunca mais precisa de
-- migração — é só mais uma chave. As que já existem (language, notify_email) ficam como colunas
-- próprias: já estão a ser usadas e são de tipo fixo. Isto é só para o que cresce.
--
-- Default '{}' (não NULL): o código lê sempre um objeto, sem ter de testar nulo a cada acesso.
ALTER TABLE mail_users ADD COLUMN settings JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE mail_users DROP COLUMN settings;
