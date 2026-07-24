-- +goose Up
-- Contato ÚNICO: empresa ou pessoa, tudo da organização (decisão do cliente, 2026-07-15, tarde).
--
-- Histórico honesto: de manhã separámos agenda (privada) de cliente (partilhado) porque o modelo
-- se contradizia. A separação resolveu a contradição, mas criou dois lugares para "gente" — e a
-- queixa original nunca foi a tabela única, foi a CERIMÓNIA da privacidade (seletor de 3 níveis,
-- tabelas de partilha). Este passo mantém o corte da cerimónia e desfaz a separação:
--
--   `contacts` = tudo. kind = 'company' | 'person'. `company_id` aponta para o contato-empresa.
--
-- VISIBILIDADE pelo GRUPO, não por contato: o contato é do dono e fica visível a quem está nas
-- EQUIPAS (user_groups) com que os GRUPOS DE CONTATO dele foram partilhados. Decide-se uma vez por
-- grupo — era escolher contato a contato que pesava. Grupo não partilhado = só o dono vê.
-- EDITAR/APAGAR continua só do dono (ver ≠ mexer).

ALTER TABLE contacts ADD COLUMN kind       TEXT NOT NULL DEFAULT 'person';
ALTER TABLE contacts ADD COLUMN company_id UUID REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE contacts ADD COLUMN domain     TEXT NOT NULL DEFAULT '';   -- só faz sentido em kind='company'
ALTER TABLE contacts ADD COLUMN allow_phone BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD COLUMN allow_email BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD CONSTRAINT contacts_kind_ck CHECK (kind IN ('company', 'person'));
-- Empresa não é subordinada de ninguém; pessoa aponta para uma empresa (ou para ninguém).
ALTER TABLE contacts ADD CONSTRAINT contacts_company_ck CHECK (kind = 'person' OR company_id IS NULL);
CREATE INDEX idx_contacts_company ON contacts(company_id);
CREATE INDEX idx_contacts_tenant_kind ON contacts(tenant_id, kind, name);

-- Partilha de um GRUPO DE CONTATOS com uma EQUIPA de utilizadores.
CREATE TABLE contact_group_shares (
    group_id      UUID NOT NULL REFERENCES contact_groups(id) ON DELETE CASCADE,
    user_group_id UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_group_id)
);

-- 1) clients → contacts, MANTENDO os ids: notas, tarefas e chamados continuam a apontar certo.
INSERT INTO contacts (id, tenant_id, owner_user_id, kind, name, email, phone, domain, notes, tags, created_at)
SELECT c.id, c.tenant_id, c.owner_user_id, c.kind, c.name, c.email, c.phone, c.domain, c.notes, c.tags, c.created_at
FROM clients c;

-- 2) client_contacts → contacts (pessoas), ligadas à empresa pelo company_id.
INSERT INTO contacts (tenant_id, owner_user_id, kind, name, email, phone, mobile, is_whatsapp,
                      allow_phone, allow_email, title, company_id, notes, tags, created_at)
SELECT p.tenant_id,
       (SELECT cl.owner_user_id FROM clients cl WHERE cl.id = p.client_id),
       'person', p.name, p.email, p.phone, p.mobile, p.is_whatsapp,
       p.allow_phone, p.allow_email, p.title, p.client_id, p.notes, '{}', p.created_at
FROM client_contacts p;

-- 3) O trabalho volta a apontar para o contato (o id é o mesmo do antigo cliente).
ALTER TABLE cards     RENAME COLUMN client_id TO contact_id;
ALTER TABLE tickets   RENAME COLUMN client_id TO contact_id;
ALTER TABLE crm_notes RENAME COLUMN client_id TO contact_id;

-- 4) As FKs apontavam para `clients`; agora apontam para `contacts`.
ALTER TABLE cards     DROP CONSTRAINT IF EXISTS cards_client_id_fkey;
ALTER TABLE tickets   DROP CONSTRAINT IF EXISTS tickets_client_id_fkey;
ALTER TABLE crm_notes DROP CONSTRAINT IF EXISTS crm_notes_client_id_fkey;
ALTER TABLE cards     ADD CONSTRAINT cards_contact_id_fkey     FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE tickets   ADD CONSTRAINT tickets_contact_id_fkey   FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE crm_notes ADD CONSTRAINT crm_notes_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE CASCADE;

DROP TABLE client_contacts;
DROP TABLE clients;

-- Os contatos que vieram de `clients` eram visíveis a todo o tenant. Sem grupo partilhado ficariam
-- só do dono — invisíveis para os colegas de um dia para o outro. Põe-nos num grupo NOVO, partilhado
-- com todas as equipas, preservando quem via o quê.
--
-- Nome próprio de propósito: reaproveitar um grupo existente (ex.: um "Clientes" que alguém já
-- criou) partilharia o grupo PESSOAL dessa pessoa com toda a gente — mudaria a intenção dela sem
-- ela pedir. DISTINCT ON garante um grupo por TENANT, não um por dono.
INSERT INTO contact_groups (tenant_id, owner_user_id, name, color)
SELECT DISTINCT ON (c.tenant_id) c.tenant_id, c.owner_user_id, 'Contatos da empresa', '#116466'
FROM contacts c
WHERE (c.kind = 'company' OR c.company_id IS NOT NULL)
  AND NOT EXISTS (SELECT 1 FROM contact_groups g WHERE g.tenant_id = c.tenant_id AND g.name = 'Contatos da empresa')
ORDER BY c.tenant_id, c.created_at;

INSERT INTO contact_group_members (contact_id, group_id)
SELECT c.id, g.id FROM contacts c
JOIN contact_groups g ON g.tenant_id = c.tenant_id AND g.name = 'Contatos da empresa'
WHERE c.kind = 'company' OR c.company_id IS NOT NULL
ON CONFLICT DO NOTHING;

INSERT INTO contact_group_shares (group_id, user_group_id)
SELECT g.id, ug.id FROM contact_groups g
JOIN user_groups ug ON ug.tenant_id = g.tenant_id
WHERE g.name = 'Contatos da empresa'
ON CONFLICT DO NOTHING;

-- +goose Down
-- ASSIMÉTRICO (mesma razão de 00018 e 00022): o Up funde populações e o Down não sabe quem era
-- quem. Devolve o esquema para o goose poder descer; restaurar dados = restaurar backup.
CREATE TABLE clients (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL DEFAULT 'company',
    name          TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    domain        TEXT NOT NULL DEFAULT '',
    phone         TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    tags          TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE client_contacts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    client_id   UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    email       TEXT NOT NULL DEFAULT '',
    phone       TEXT NOT NULL DEFAULT '',
    mobile      TEXT NOT NULL DEFAULT '',
    is_whatsapp BOOLEAN NOT NULL DEFAULT false,
    allow_phone BOOLEAN NOT NULL DEFAULT true,
    allow_email BOOLEAN NOT NULL DEFAULT true,
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE cards     DROP CONSTRAINT IF EXISTS cards_contact_id_fkey;
ALTER TABLE tickets   DROP CONSTRAINT IF EXISTS tickets_contact_id_fkey;
ALTER TABLE crm_notes DROP CONSTRAINT IF EXISTS crm_notes_contact_id_fkey;
ALTER TABLE cards     RENAME COLUMN contact_id TO client_id;
ALTER TABLE tickets   RENAME COLUMN contact_id TO client_id;
ALTER TABLE crm_notes RENAME COLUMN contact_id TO client_id;
DROP TABLE IF EXISTS contact_group_shares;
DELETE FROM contacts WHERE kind = 'company';
ALTER TABLE contacts DROP CONSTRAINT contacts_company_ck;
ALTER TABLE contacts DROP CONSTRAINT contacts_kind_ck;
ALTER TABLE contacts DROP COLUMN allow_email;
ALTER TABLE contacts DROP COLUMN allow_phone;
ALTER TABLE contacts DROP COLUMN domain;
ALTER TABLE contacts DROP COLUMN company_id;
ALTER TABLE contacts DROP COLUMN kind;
