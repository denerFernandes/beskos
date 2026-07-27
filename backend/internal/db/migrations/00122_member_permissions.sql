-- +goose Up
-- Exceções de permissão POR MEMBRO, por cima do papel: `allow` concede uma capacidade que o papel
-- não dá; `allow=false` retira uma que dá. O papel continua a ser o ponto de partida — isto é para
-- os casos que nenhum papel de fábrica cobre (ex.: só o financeiro do escritório pode apagar
-- domínios, ou um admin que não deve mexer em faturação).
CREATE TABLE member_permissions (
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    allow      BOOLEAN NOT NULL,
    created_by UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, permission)
);
CREATE INDEX idx_member_permissions_tenant ON member_permissions(tenant_id);

-- +goose Down
DROP TABLE member_permissions;
