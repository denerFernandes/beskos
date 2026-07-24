-- +goose Up
-- Campos customizados (JSONB livre) e foto/avatar (aponta para um storage_files — reusa /thumb).
ALTER TABLE contacts ADD COLUMN custom_fields JSONB NOT NULL DEFAULT '{}';
ALTER TABLE contacts ADD COLUMN avatar_file_id UUID REFERENCES storage_files(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE contacts DROP COLUMN avatar_file_id;
ALTER TABLE contacts DROP COLUMN custom_fields;
