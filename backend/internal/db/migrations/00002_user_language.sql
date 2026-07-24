-- +goose Up
-- Preferência de idioma do usuário (i18n, lei §4). Vazio = automático (detecção pelo browser);
-- valor definido (ex.: 'pt-BR', 'en') tem prioridade sobre o browser e é persistido na conta.
ALTER TABLE panel_users ADD COLUMN language TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE panel_users DROP COLUMN language;
