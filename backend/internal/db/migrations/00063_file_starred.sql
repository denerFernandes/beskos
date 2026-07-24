-- +goose Up
-- Favoritos/estrelas: marca um ficheiro como favorito do dono (filtro/ordenação no Drive).
ALTER TABLE storage_files ADD COLUMN starred BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE storage_files DROP COLUMN starred;
