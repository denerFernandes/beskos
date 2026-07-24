-- +goose Up
-- Comentários ricos (pedido do utilizador): o comentário passa a ser um mini-documento BlockNote.
-- body_json é a fonte; body (texto) e body_html derivam ao gravar (mesmo derive das páginas).
-- Comentários antigos (só body) continuam a renderizar pelo texto.
ALTER TABLE wiki_comments ADD COLUMN body_json JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE wiki_comments ADD COLUMN body_html TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE wiki_comments DROP COLUMN IF EXISTS body_html;
ALTER TABLE wiki_comments DROP COLUMN IF EXISTS body_json;
