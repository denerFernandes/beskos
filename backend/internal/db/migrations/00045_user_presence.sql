-- +goose Up
-- Presença rica do chat definida pelo usuário: active | away | dnd (não perturbe). Combina com a
-- presença REAL (Redis/online): offline sempre ganha; online mostra este estado. dnd suprime o push.
ALTER TABLE mail_users ADD COLUMN presence TEXT NOT NULL DEFAULT 'active';

-- +goose Down
ALTER TABLE mail_users DROP COLUMN presence;
