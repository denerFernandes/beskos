-- +goose Up
-- Anti-replay do TOTP: guarda o último passo de tempo aceito. Um código só vale se o seu passo for
-- maior que o último usado — assim um código interceptado não é reutilizável dentro da janela.
ALTER TABLE mail_users ADD COLUMN last_totp_step BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE mail_users DROP COLUMN last_totp_step;
