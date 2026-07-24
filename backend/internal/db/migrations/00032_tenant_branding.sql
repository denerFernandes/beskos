-- +goose Up
-- Marca por organização: cor principal (hex, ex.: "#2563EB") e logo (data URI, guardado inline —
-- logos são pequenos e assim evita-se um endpoint de imagem público). Vazio = usar a marca padrão
-- do Beskos. Só admin/owner alteram (RBAC org.settings).
ALTER TABLE tenants
    ADD COLUMN brand_color TEXT NOT NULL DEFAULT '',
    ADD COLUMN brand_logo  TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tenants DROP COLUMN brand_color, DROP COLUMN brand_logo;
