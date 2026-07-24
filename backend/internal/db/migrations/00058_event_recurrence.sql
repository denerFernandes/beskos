-- +goose Up
-- Eventos recorrentes: recur = frequência (daily|weekly|monthly|yearly; '' = único), recur_until =
-- data limite opcional. O evento guardado é o MESTRE (1ª ocorrência); o backend expande as ocorrências
-- dentro do intervalo pedido ao listar. Editar/apagar afeta a série (MVP).
ALTER TABLE events ADD COLUMN recur TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN recur_until TIMESTAMPTZ;

-- +goose Down
ALTER TABLE events DROP COLUMN recur_until;
ALTER TABLE events DROP COLUMN recur;
