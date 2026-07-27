-- +goose Up
-- Aliases de e-mail: um endereço extra que entrega na caixa de OUTRA pessoa. Tabela própria e não
-- uma linha em mail_users de propósito — um alias não é gente: não entra no chat, nem na lista de
-- membros, nem nos grupos, e essa separação fica garantida pela estrutura em vez de depender de um
-- filtro que alguém se esqueceria de pôr numa query nova.
-- A cobrança conta-os como utilizadores (queries_usage.sql), que é a regra de negócio pedida.
CREATE TABLE email_aliases (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    domain_id    UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    -- Dono: a caixa que recebe o correio deste endereço. Apagar a caixa leva o alias com ela.
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    address      TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Um endereço só pode existir uma vez, e nunca pode chocar com uma caixa real.
CREATE UNIQUE INDEX idx_email_aliases_address ON email_aliases (lower(address));
CREATE INDEX idx_email_aliases_user ON email_aliases (mail_user_id);
CREATE INDEX idx_email_aliases_tenant ON email_aliases (tenant_id);

-- +goose Down
DROP TABLE email_aliases;
