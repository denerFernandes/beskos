-- +goose Up
-- Biblioteca de GIFs pessoal (decisão do cliente, 2026-07-16: sem Giphy/Tenor).
--
-- Não há tabela de ficheiro nova: o GIF é um `storage_files` como qualquer outro, já no MinIO.
-- Isto aqui é só a marca "este ficheiro está na minha gaveta de GIFs" — quem já anexou um GIF
-- pode guardá-lo e reusar sem voltar a carregar.
--
-- Pessoal de propósito: é o gosto de cada um, não um activo da empresa. Se um dia fizer sentido
-- partilhar, o caminho é o mesmo dos contatos — um grupo, não um campo por GIF.
CREATE TABLE gif_library (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    -- ON DELETE CASCADE: apagar o ficheiro no Drive tira-o da gaveta. Uma entrada apontando para
    -- um ficheiro que já não existe seria um GIF partido na cara de quem o escolhesse.
    file_id    UUID NOT NULL REFERENCES storage_files(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT gif_library_uq UNIQUE (owner_user_id, file_id)
);
CREATE INDEX idx_gif_library ON gif_library(owner_user_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS gif_library;
