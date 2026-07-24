-- +goose Up
-- Remove o subsistema de chamados (decisão do cliente, 2026-07-15).
--
-- Construído e testado hoje (00021), removido no mesmo dia: o produto é "mais simples e
-- simplificado que o Workspace", e helpdesk com SLA/fila/thread é outro produto — não cabe nessa
-- promessa. O contato mantém NOTAS e TAREFAS, que é o que resolve o dia a dia de uma empresa
-- pequena. Se um dia voltar, a 00021 fica no histórico com o desenho inteiro.
--
-- Seguro: não havia chamado nenhum (só as políticas de SLA padrão que a própria 00021 criou).

DROP TABLE IF EXISTS ticket_messages;
DROP TABLE IF EXISTS tickets;
DROP TABLE IF EXISTS sla_policies;
ALTER TABLE tenants DROP COLUMN IF EXISTS ticket_seq;

-- +goose Down
-- Recria o esquema (sem dados). O desenho original está em 00021_tickets.sql.
ALTER TABLE tenants ADD COLUMN ticket_seq BIGINT NOT NULL DEFAULT 0;

CREATE TABLE tickets (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    number       BIGINT NOT NULL,
    subject      TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open',
    priority     TEXT NOT NULL DEFAULT 'normal',
    contact_id   UUID REFERENCES contacts(id) ON DELETE SET NULL,
    requester_email TEXT NOT NULL DEFAULT '',
    assignee_id  UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    group_id     UUID REFERENCES user_groups(id) ON DELETE SET NULL,
    created_by   UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    source       TEXT NOT NULL DEFAULT 'ui',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
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

CREATE TABLE ticket_messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    ticket_id  UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    author_id  UUID REFERENCES mail_users(id) ON DELETE SET NULL,
    from_email TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL,
    internal   BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ticket_messages ON ticket_messages(ticket_id, created_at);

CREATE TABLE sla_policies (
    tenant_id              UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    priority               TEXT NOT NULL,
    first_response_minutes INT NOT NULL,
    resolution_minutes     INT NOT NULL,
    PRIMARY KEY (tenant_id, priority),
    CONSTRAINT sla_priority_ck CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);
