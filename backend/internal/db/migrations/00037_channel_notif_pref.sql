-- +goose Up
-- Preferência de notificações por canal (por membro): all = tudo; mentions = só menções diretas;
-- none = silenciado. Aplica-se ao gerar notificações de menção no chat.
ALTER TABLE channel_members ADD COLUMN notif_pref TEXT NOT NULL DEFAULT 'all'; -- all | mentions | none

-- +goose Down
ALTER TABLE channel_members DROP COLUMN notif_pref;
