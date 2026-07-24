-- +goose Up
-- CRM — fase 6: chamados (tickets) e atendimentos.
-- Decisões (com o cliente): sistema completo (status, prioridade, responsável, thread, SLA);
-- SLA = prazo FIXO POR PRIORIDADE, configurável pelo admin; ticket é por contato.
-- FILA: não existe entidade "fila" — o campo group_id aponta para os `user_groups` que já temos
-- (opcional, nulo por padrão). Recomendação registrada: numa empresa pequena o que resolve é
-- "responsável", não roteamento de fila. O campo fica pronto sem custo nem dados a migrar.

-- Numeração por tenant: o UPDATE ... RETURNING trava a linha do tenant, então dois chamados
-- criados ao mesmo tempo nunca recebem o mesmo número (MAX(number)+1 daria empate).
ALTER TABLE tenants ADD COLUMN ticket_seq BIGINT NOT NULL DEFAULT 0;

CREATE TABLE tickets (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    number       BIGINT NOT NULL,               -- #1, #2… por tenant
    subject      TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open',  -- open | pending | resolved | closed
    priority     TEXT NOT NULL DEFAULT 'normal',-- low | normal | high | urgent
    contact_id   UUID REFERENCES contacts(id) ON DELETE SET NULL,
    -- Quem pediu, por e-mail. Sempre gravado: o chamado precisa sobreviver ao contato ser
    -- apagado, e um e-mail de fora pode abrir chamado sem ter contato ainda.
    requester_email TEXT NOT NULL DEFAULT '',
    assignee_id  UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    group_id     UUID REFERENCES user_groups(id) ON DELETE SET NULL,
    created_by   UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    source       TEXT NOT NULL DEFAULT 'ui',    -- ui | email
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- SLA: os prazos são CONGELADOS na criação a partir da política vigente. Mudar a política
    -- depois não pode reescrever a régua de um chamado que já estava correndo.
    due_first_response_at TIMESTAMPTZ,
    due_resolution_at     TIMESTAMPTZ,
    first_response_at     TIMESTAMPTZ,
    resolved_at           TIMESTAMPTZ,
    closed_at             TIMESTAMPTZ,
    CONSTRAINT tickets_number_uq UNIQUE (tenant_id, number),
    CONSTRAINT tickets_status_ck CHECK (status IN ('open', 'pending', 'resolved', 'closed')),
    CONSTRAINT tickets_priority_ck CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);
CREATE INDEX idx_tickets_tenant  ON tickets(tenant_id, status, created_at DESC);
CREATE INDEX idx_tickets_contact ON tickets(contact_id);

-- Thread de respostas. `internal` = nota entre colegas: NUNCA sai para o cliente.
CREATE TABLE ticket_messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    ticket_id  UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    author_id  UUID REFERENCES mail_users(id) ON DELETE SET NULL, -- nulo = veio do cliente
    from_email TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL,
    internal   BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ticket_messages ON ticket_messages(ticket_id, created_at);

-- Política de SLA por prioridade, por tenant. Minutos até a 1ª resposta e até resolver.
CREATE TABLE sla_policies (
    tenant_id              UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    priority               TEXT NOT NULL,
    first_response_minutes INT NOT NULL,
    resolution_minutes     INT NOT NULL,
    PRIMARY KEY (tenant_id, priority),
    CONSTRAINT sla_priority_ck CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);

-- Padrão para os tenants que já existem (e o mesmo que a API aplica a tenant novo).
INSERT INTO sla_policies (tenant_id, priority, first_response_minutes, resolution_minutes)
SELECT t.id, p.priority, p.frm, p.res
FROM tenants t
CROSS JOIN (VALUES
    ('urgent',  60,   480),   -- 1h / 8h
    ('high',    240,  1440),  -- 4h / 24h
    ('normal',  480,  4320),  -- 8h / 3d
    ('low',     1440, 10080)  -- 24h / 7d
) AS p(priority, frm, res);

-- +goose Down
DROP TABLE IF EXISTS sla_policies;
DROP TABLE IF EXISTS ticket_messages;
DROP TABLE IF EXISTS tickets;
ALTER TABLE tenants DROP COLUMN ticket_seq;
