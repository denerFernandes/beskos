-- +goose Up
-- Soft-delete de eventos para permitir "desfazer exclusão" (undo estilo Gmail): a exclusão marca
-- deleted_at e o restore o limpa, preservando id, convites e vínculo com a reunião. As listagens
-- filtram deleted_at IS NULL. Um expurgo definitivo pode vir depois (worker), fora deste escopo.
ALTER TABLE events ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_events_live ON events(owner_user_id, starts_at) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_events_live;
ALTER TABLE events DROP COLUMN deleted_at;
