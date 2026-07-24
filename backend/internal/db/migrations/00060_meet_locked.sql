-- +goose Up
-- Trancar sala a novas entradas: diferente de lock_until_host ("espera o organizador"). Quando o host
-- tranca, NINGUÉM novo entra (interno ou externo), só o próprio host reentra. O lobby de aprovação só
-- vale p/ convidado externo; esta tranca é dura e vale para todos.
ALTER TABLE meetings ADD COLUMN locked BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE meetings DROP COLUMN locked;
