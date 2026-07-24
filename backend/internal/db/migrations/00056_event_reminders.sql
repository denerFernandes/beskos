-- +goose Up
-- Lembretes de evento: remind_minutes = minutos antes do início para avisar (0 = sem lembrete). O
-- worker dispara a notificação (sino + push) quando o momento chega e marca reminded para não repetir.
-- Editar o evento re-arma (reminded volta a false) — assim mudar a hora volta a avisar.
ALTER TABLE events ADD COLUMN remind_minutes INT NOT NULL DEFAULT 10;
ALTER TABLE events ADD COLUMN reminded BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX idx_events_remind ON events (starts_at) WHERE remind_minutes > 0 AND NOT reminded;

-- +goose Down
DROP INDEX IF EXISTS idx_events_remind;
ALTER TABLE events DROP COLUMN reminded;
ALTER TABLE events DROP COLUMN remind_minutes;
