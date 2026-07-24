-- +goose Up
-- Templates de página (Bloco 2 B7): uma página marcada como template aparece no picker de "nova
-- página a partir de template" do seu espaço. O conteúdo é copiado no momento da criação (frontend
-- lê o body_json do template e cria a página nova com ele — sem magia no servidor).
ALTER TABLE wiki_pages ADD COLUMN is_template BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX idx_wiki_pages_templates ON wiki_pages(tenant_id, space_id) WHERE is_template;

-- +goose Down
DROP INDEX IF EXISTS idx_wiki_pages_templates;
ALTER TABLE wiki_pages DROP COLUMN IF EXISTS is_template;
