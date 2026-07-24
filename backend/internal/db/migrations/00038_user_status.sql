-- +goose Up
-- Status do utilizador (estilo Slack): emoji + texto curto (ex.: 🌴 "De férias"). Vazio = sem status.
ALTER TABLE mail_users
    ADD COLUMN status_emoji TEXT NOT NULL DEFAULT '',
    ADD COLUMN status_text  TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mail_users DROP COLUMN status_emoji, DROP COLUMN status_text;
