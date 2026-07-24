-- +goose Up
-- Ausências no calendário (24/07/2026): day off e férias são eventos de dia inteiro com um "kind"
-- próprio — o booking já os respeita (bloqueiam o dia) e a UI mostra-os com estilo distinto.
ALTER TABLE events ADD COLUMN kind TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE events DROP COLUMN kind;
