-- +goose Up
-- Backend de e-mail (features nossas): sessão do webmail, marcadores, Kanban e calendário.
-- Leitura/envio de e-mail continua JMAP direto (brief §4); estas tabelas são a nossa camada.

-- Sessão do usuário FINAL na nossa API (distinta do painel). Criada validando a credencial
-- do mailbox contra o Stalwart; vincula à mail_user (e portanto ao tenant).
CREATE TABLE webmail_sessions (
    token        TEXT PRIMARY KEY,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Marcadores por tenant (catálogo). A aplicação em e-mails é via keywords JMAP no frontend.
CREATE TABLE labels (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL DEFAULT '#116466',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

-- Kanban: quadros compartilhados por tenant, colunas ordenadas, cards.
CREATE TABLE boards (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE board_columns (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id UUID NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    color    TEXT NOT NULL DEFAULT '#6B6863',
    position INT NOT NULL DEFAULT 0
);

CREATE TABLE cards (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    board_id        UUID NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    column_id       UUID NOT NULL REFERENCES board_columns(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    label           TEXT NOT NULL DEFAULT '',
    assignee        TEXT NOT NULL DEFAULT '',
    due             TEXT NOT NULL DEFAULT '',
    source_email_id TEXT NOT NULL DEFAULT '',   -- e-mail de origem (email→tarefa)
    position        INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Calendário: eventos por usuário final (nossa camada; sync JMAP Calendars fica para depois).
CREATE TABLE events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id   UUID REFERENCES mail_users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    location        TEXT NOT NULL DEFAULT '',
    color           TEXT NOT NULL DEFAULT '#116466',
    starts_at       TIMESTAMPTZ NOT NULL,
    ends_at         TIMESTAMPTZ NOT NULL,
    source_email_id TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_labels_tenant   ON labels(tenant_id);
CREATE INDEX idx_boards_tenant   ON boards(tenant_id);
CREATE INDEX idx_columns_board   ON board_columns(board_id, position);
CREATE INDEX idx_cards_board     ON cards(board_id, column_id, position);
CREATE INDEX idx_events_owner    ON events(owner_user_id, starts_at);

-- +goose Down
DROP TABLE IF EXISTS events, cards, board_columns, boards, labels, webmail_sessions CASCADE;
