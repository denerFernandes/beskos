-- +goose Up
-- Evento de dia inteiro: sem hora, ocupa o(s) dia(s) todo(s). Multi-dia é starts_at/ends_at em dias
-- diferentes (já suportado no schema); o all_day só muda a apresentação (linha de topo, sem hora).
ALTER TABLE events ADD COLUMN all_day BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE events DROP COLUMN all_day;
