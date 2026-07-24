-- +goose Up
-- i18n das notificações (CLAUDE.md §4.6): o backend deixa de depender do texto em PT gravado em
-- title/body. Passa a guardar os dados estruturados do evento em `args` (JSONB) e o frontend
-- traduz por `server.notif.<kind>` no idioma do usuário. title/body seguem gravados como fallback
-- (linhas antigas) e porque e-mail/web-push são renderizados no backend (não há frontend a traduzir).
ALTER TABLE notifications ADD COLUMN args JSONB NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE notifications DROP COLUMN args;
