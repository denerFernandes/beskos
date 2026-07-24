-- +goose Up
-- Lixeira: soft delete. deleted_at NULL = ativo; preenchido = na lixeira (restaurável). O delete
-- deixa de ser destrutivo — só a purga (esvaziar lixeira) apaga de vez e libera o blob. Trashed
-- ainda conta na cota até a purga (como no Drive). Índice parcial acelera a listagem da lixeira.
ALTER TABLE storage_files ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_files_trash ON storage_files (owner_user_id, deleted_at) WHERE deleted_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_files_trash;
ALTER TABLE storage_files DROP COLUMN deleted_at;
