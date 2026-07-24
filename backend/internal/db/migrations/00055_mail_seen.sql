-- +goose Up
-- Push de e-mail novo com app fechado: marca até quando já vimos a caixa de cada utilizador. O worker
-- compara com a chegada mais recente e notifica só o que é novo desde a última verificação.
-- NULL = ainda não estabelecemos a base (primeira verificação só regista, não notifica — evita enxurrada).
ALTER TABLE mail_users ADD COLUMN mail_seen_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE mail_users DROP COLUMN mail_seen_at;
