-- +goose Up
-- Senha opcional no link público (além da expiração já configurável): o download exige a senha.
ALTER TABLE file_links ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE file_links DROP COLUMN password_hash;
