-- +goose Up
-- Separa AGENDA de CLIENTE (decisão de 2026-07-15: "estamos misturando conceitos").
--
-- Antes: `contacts` servia três donos com regras opostas — agenda (minha, privada), cliente (da
-- empresa, partilhado) e atendimento. Daí o seletor de visibilidade de 3 níveis, as tabelas de
-- partilha, e o sintoma "Chamado #12 de —" (chamado partilhado + contato privado = nome nulo).
--
-- Agora:
--   `contacts`        = agenda pessoal. Sempre privada (só o dono). Serve o "Para:" do e-mail.
--   `clients`         = cliente da empresa (kind = company | person). Sempre visível ao tenant.
--   `client_contacts` = as pessoas de um cliente (cargo, telefone, consentimento LGPD).
--
-- A mesma pessoa pode estar nos dois lados (na minha agenda e como contato de um cliente). Isso é
-- correto, não duplicação: são registos de donos diferentes — como Google Contacts vs CRM.

-- 1) companies → clients. RENAME preserva os ids, então crm_notes/cards/tickets não se perdem.
ALTER TABLE companies RENAME TO clients;
ALTER TABLE clients ADD COLUMN kind  TEXT NOT NULL DEFAULT 'company';
ALTER TABLE clients ADD COLUMN email TEXT NOT NULL DEFAULT '';   -- usado quando kind = 'person'
ALTER TABLE clients ADD CONSTRAINT clients_kind_ck CHECK (kind IN ('company', 'person'));
-- Cliente é activo da empresa, não de quem o cadastrou: a visibilidade deixa de existir.
ALTER TABLE clients DROP COLUMN visibility;
DROP TABLE company_shares;

-- 2) As pessoas de um cliente. O consentimento (LGPD) vive aqui — é a empresa que contata.
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
CREATE INDEX idx_client_contacts ON client_contacts(client_id);

-- 3) Contato COM empresa → é pessoa de um cliente.
INSERT INTO client_contacts (tenant_id, client_id, name, title, email, phone, mobile,
                             is_whatsapp, allow_phone, allow_email, notes, created_at)
SELECT c.tenant_id, c.company_id, c.name, c.title, c.email, c.phone, c.mobile,
       c.is_whatsapp, c.allow_phone, c.allow_email, c.notes, c.created_at
FROM contacts c WHERE c.company_id IS NOT NULL;

-- 4) Contato partilhado SEM empresa → vira cliente pessoa física.
--    Mantém o id do contato para que notas/tarefas/chamados apontem para o mesmo UUID.
INSERT INTO clients (id, tenant_id, owner_user_id, name, domain, phone, notes, tags, created_at, kind, email)
SELECT c.id, c.tenant_id, c.owner_user_id, c.name, '', c.phone, c.notes, c.tags, c.created_at, 'person', c.email
FROM contacts c WHERE c.company_id IS NULL AND c.visibility <> 'private';

-- 5) Remapear o trabalho: contact_id → client_id.
--    Quem tinha empresa passa a apontar para a empresa; quem virou pessoa física manteve o id.
ALTER TABLE cards   ADD COLUMN client_id UUID REFERENCES clients(id) ON DELETE SET NULL;
ALTER TABLE tickets ADD COLUMN client_id UUID REFERENCES clients(id) ON DELETE SET NULL;
UPDATE cards k SET client_id = COALESCE(
    (SELECT c.company_id FROM contacts c WHERE c.id = k.contact_id),
    (SELECT cl.id FROM clients cl WHERE cl.id = k.contact_id))
WHERE k.contact_id IS NOT NULL;
UPDATE tickets t SET client_id = COALESCE(
    (SELECT c.company_id FROM contacts c WHERE c.id = t.contact_id),
    (SELECT cl.id FROM clients cl WHERE cl.id = t.contact_id))
WHERE t.contact_id IS NOT NULL;
ALTER TABLE cards   DROP COLUMN contact_id;
ALTER TABLE tickets DROP COLUMN contact_id;
CREATE INDEX idx_cards_client   ON cards(client_id);
CREATE INDEX idx_tickets_client ON tickets(client_id);

-- 6) Notas do CRM: passam a ser do cliente (contact XOR company deixa de fazer sentido).
ALTER TABLE crm_notes ADD COLUMN client_id UUID REFERENCES clients(id) ON DELETE CASCADE;
UPDATE crm_notes n SET client_id = COALESCE(
    n.company_id,
    (SELECT c.company_id FROM contacts c WHERE c.id = n.contact_id),
    (SELECT cl.id FROM clients cl WHERE cl.id = n.contact_id));
DELETE FROM crm_notes WHERE client_id IS NULL;  -- nota de contato que ficou na agenda: sem cliente
ALTER TABLE crm_notes DROP CONSTRAINT IF EXISTS crm_notes_check;
ALTER TABLE crm_notes DROP COLUMN contact_id;
ALTER TABLE crm_notes DROP COLUMN company_id;
ALTER TABLE crm_notes ALTER COLUMN client_id SET NOT NULL;
CREATE INDEX idx_crm_notes_client ON crm_notes(client_id, created_at DESC);

-- 7) A agenda emagrece: some a visibilidade, a partilha e o que era de CRM.
--    O que se perde aqui já foi copiado para client_contacts no passo 3.
DELETE FROM contacts WHERE company_id IS NULL AND visibility <> 'private'; -- virou cliente pessoa
DELETE FROM contacts WHERE company_id IS NOT NULL;                          -- virou client_contact
DROP TABLE contact_shares;
ALTER TABLE contacts DROP COLUMN visibility;
ALTER TABLE contacts DROP COLUMN company_id;
ALTER TABLE contacts DROP COLUMN allow_phone;   -- consentimento é conceito de cliente, não de agenda
ALTER TABLE contacts DROP COLUMN allow_email;

-- +goose Down
-- ASSIMÉTRICO DE PROPÓSITO. O Up funde e separa populações; desfazer exatamente exigiria saber
-- quem era quem antes, e adivinhar erraria — pior que falhar. O Down devolve o ESQUEMA antigo
-- (para o goose poder descer), não os dados na disposição original. Restaurar dados = restaurar
-- backup. Ver a mesma decisão em 00018_unify_users.sql.
ALTER TABLE contacts ADD COLUMN allow_email BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD COLUMN allow_phone BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE contacts ADD COLUMN company_id UUID;
ALTER TABLE contacts ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private';
CREATE TABLE contact_shares (
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (contact_id, group_id)
);
ALTER TABLE crm_notes ADD COLUMN contact_id UUID REFERENCES contacts(id) ON DELETE CASCADE;
ALTER TABLE crm_notes ADD COLUMN company_id UUID;
ALTER TABLE crm_notes DROP COLUMN client_id;
ALTER TABLE tickets ADD COLUMN contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE cards   ADD COLUMN contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE tickets DROP COLUMN client_id;
ALTER TABLE cards   DROP COLUMN client_id;
DROP TABLE IF EXISTS client_contacts;
ALTER TABLE clients ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private';
ALTER TABLE clients DROP CONSTRAINT clients_kind_ck;
ALTER TABLE clients DROP COLUMN email;
ALTER TABLE clients DROP COLUMN kind;
ALTER TABLE clients RENAME TO companies;
CREATE TABLE company_shares (
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (company_id, group_id)
);
