-- +goose Up
-- Feed de mudanças para o cliente de sincronização (daemon desktop, escrito em Rust): o daemon
-- pergunta "o que mudou desde o cursor X?" e reconcilia a pasta local. `updated_at` é atualizado
-- automaticamente por trigger em QUALQUER UPDATE — captura rename (nome), move (folder_id), nova
-- versão (hash/versao/blob_key), ir para a lixeira (deleted_at) e restauro, sem tocar nos handlers.
-- Ficheiros novos herdam updated_at = created_at pelo default.
ALTER TABLE storage_files ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION storage_files_touch_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_storage_files_updated_at
  BEFORE UPDATE ON storage_files
  FOR EACH ROW EXECUTE FUNCTION storage_files_touch_updated_at();

-- Índice do feed: keyset por (owner, updated_at, id) — ordena e pagina sem varrer a tabela.
CREATE INDEX idx_files_changes ON storage_files (owner_user_id, updated_at, id);

-- +goose Down
DROP INDEX IF EXISTS idx_files_changes;
DROP TRIGGER IF EXISTS trg_storage_files_updated_at ON storage_files;
DROP FUNCTION IF EXISTS storage_files_touch_updated_at();
ALTER TABLE storage_files DROP COLUMN updated_at;
