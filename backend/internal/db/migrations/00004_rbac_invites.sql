-- +goose Up
-- RBAC: convites de usuários para o painel (papéis fixos + permissões em internal/rbac).

CREATE TABLE invites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL,
    token       TEXT NOT NULL UNIQUE,
    invited_by  TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_invites_tenant ON invites(tenant_id) WHERE accepted_at IS NULL;

-- O primeiro usuário de uma organização passa a ser "owner" (antes: tenant_admin).
UPDATE panel_users SET role = 'owner' WHERE role = 'tenant_admin';

-- +goose Down
DROP TABLE IF EXISTS invites CASCADE;
