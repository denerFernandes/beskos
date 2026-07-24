-- +goose Up
-- Links públicos de download (anexos grandes enviados por e-mail, estilo Gmail). O `token` é um
-- segredo longo e imprevisível: quem o tem baixa o ficheiro, sem login. Sem expiração por decisão
-- do cliente — o link vive enquanto o ficheiro existir (ON DELETE CASCADE limpa ao apagar/revogar).
-- Um ficheiro tem no máximo um link ativo (reutilizado); por isso file_id é único.
CREATE TABLE file_links (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    file_id    UUID NOT NULL UNIQUE REFERENCES storage_files(id) ON DELETE CASCADE,
    token      TEXT NOT NULL UNIQUE,
    created_by UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE file_links;
