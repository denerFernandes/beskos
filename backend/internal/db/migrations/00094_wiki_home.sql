-- +goose Up
-- Home personalizável da app Wiki (Bloco 2 B5): uma "página" por tenant, editada por admins da org
-- (PermOrgSettings) com o mesmo editor BlockNote, mostrada no ecrã inicial da Wiki no lugar do
-- "Selecione uma wiki". body_json (blocos BlockNote) é a fonte; text/html derivados como nas páginas.
CREATE TABLE wiki_home (
    tenant_id  UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    body_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    body_text  TEXT NOT NULL DEFAULT '',
    body_html  TEXT NOT NULL DEFAULT '',
    updated_by UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS wiki_home;
