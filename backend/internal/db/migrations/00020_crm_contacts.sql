-- +goose Up
-- CRM — parte 1: empresas, subcontatos com cargo, telefones com consentimento e privacidade.
-- Decisões (com o cliente): `companies` é tabela separada; contato pertence a uma empresa e tem
-- cargo; contato pode ser privado, compartilhado com grupos, ou de toda a organização.

CREATE TABLE companies (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    domain        TEXT NOT NULL DEFAULT '',   -- site/domínio (ajuda a casar e-mails com a empresa)
    phone         TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    tags          TEXT[] NOT NULL DEFAULT '{}',
    -- Privacidade: private = só o dono; groups = dono + grupos em company_shares; tenant = todos.
    -- Default fail-closed: quem cria decide abrir; nada vaza por omissão.
    visibility    TEXT NOT NULL DEFAULT 'private',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_companies_tenant ON companies(tenant_id);

-- Contato ganha empresa, cargo, consentimento e privacidade.
ALTER TABLE contacts ADD COLUMN company_id UUID REFERENCES companies(id) ON DELETE SET NULL;
ALTER TABLE contacts ADD COLUMN title      TEXT NOT NULL DEFAULT '';   -- cargo
ALTER TABLE contacts ADD COLUMN mobile     TEXT NOT NULL DEFAULT '';
-- Consentimento por canal (LGPD): dizer se PODE contatar, e por onde.
ALTER TABLE contacts ADD COLUMN is_whatsapp  BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE contacts ADD COLUMN allow_phone  BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD COLUMN allow_email  BOOLEAN NOT NULL DEFAULT true;
-- ATENÇÃO: hoje os contatos são privados de fato (as queries filtram só por owner_user_id).
-- Default 'private' preserva exatamente esse comportamento — 'tenant' exporia, de uma vez, a
-- agenda inteira de todo mundo para a organização.
ALTER TABLE contacts ADD COLUMN visibility   TEXT NOT NULL DEFAULT 'private';
CREATE INDEX idx_contacts_company ON contacts(company_id);

-- Compartilhamento com grupos (só vale quando visibility = 'groups').
CREATE TABLE contact_shares (
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (contact_id, group_id)
);
CREATE TABLE company_shares (
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (company_id, group_id)
);

-- Notas do CRM (timeline por contato/empresa) — separadas do campo `notes` livre.
CREATE TABLE crm_notes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    contact_id UUID REFERENCES contacts(id) ON DELETE CASCADE,
    company_id UUID REFERENCES companies(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Uma nota é de um contato OU de uma empresa, nunca das duas nem de nenhuma.
    CHECK ((contact_id IS NULL) <> (company_id IS NULL))
);
CREATE INDEX idx_crm_notes_contact ON crm_notes(contact_id, created_at DESC);
CREATE INDEX idx_crm_notes_company ON crm_notes(company_id, created_at DESC);

-- Tarefa do cliente = card do Kanban ligado ao contato (decisão: reusar o Kanban).
ALTER TABLE cards ADD COLUMN contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL;
CREATE INDEX idx_cards_contact ON cards(contact_id);

-- +goose Down
ALTER TABLE cards DROP COLUMN contact_id;
DROP TABLE IF EXISTS crm_notes;
DROP TABLE IF EXISTS company_shares;
DROP TABLE IF EXISTS contact_shares;
ALTER TABLE contacts DROP COLUMN company_id;
ALTER TABLE contacts DROP COLUMN title;
ALTER TABLE contacts DROP COLUMN mobile;
ALTER TABLE contacts DROP COLUMN is_whatsapp;
ALTER TABLE contacts DROP COLUMN allow_phone;
ALTER TABLE contacts DROP COLUMN allow_email;
ALTER TABLE contacts DROP COLUMN visibility;
DROP TABLE IF EXISTS companies;
