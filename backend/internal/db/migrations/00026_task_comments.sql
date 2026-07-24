-- +goose Up
-- Tarefas: comentários + responsável que é uma PESSOA de verdade (2026-07-15).
--
-- Problema encontrado: `cards.assignee` era TEXT com um nome. O seletor antigo sugeria contatos da
-- AGENDA, então as tarefas estavam atribuídas a gente sem conta no sistema — impossível notificar.
-- Agora o responsável é FK para mail_users.
--
-- `assignee` (texto) NÃO some: guarda o que não casar com nenhum utilizador, para não apagar a
-- informação de quem já estava lá. Regra: assignee_id XOR assignee — a migração limpa o texto de
-- quem casou, e a UI só escreve assignee_id daqui para a frente.

ALTER TABLE cards ADD COLUMN assignee_id UUID REFERENCES mail_users(id) ON DELETE SET NULL;
CREATE INDEX idx_cards_assignee ON cards(assignee_id);

-- Quem criou a tarefa — para lhe avisar quando ela muda ou recebe comentário. Fica NULO nos cards
-- que já existem: ninguém registou o autor, e chutar (ex.: "o dono do quadro") mandaria e-mail a
-- quem nunca criou nada. Nulo = simplesmente não há a quem avisar.
ALTER TABLE cards ADD COLUMN created_by UUID REFERENCES mail_users(id) ON DELETE SET NULL;

-- 1) Casa direto com um utilizador (nome ou e-mail).
UPDATE cards c SET assignee_id = u.id
FROM mail_users u
WHERE c.assignee <> '' AND u.tenant_id = c.tenant_id
  AND (lower(u.name) = lower(c.assignee) OR lower(u.email) = lower(c.assignee));

-- 2) Casa via CONTATO: o nome era de um contato da agenda; se o e-mail dele for de um utilizador,
--    é a mesma pessoa. Cobre exatamente o estrago do seletor antigo.
UPDATE cards c SET assignee_id = u.id
FROM contacts ct
JOIN mail_users u ON u.tenant_id = ct.tenant_id AND lower(u.email) = lower(ct.email)
WHERE c.assignee_id IS NULL AND c.assignee <> ''
  AND ct.tenant_id = c.tenant_id AND lower(ct.name) = lower(c.assignee) AND ct.email <> '';

-- 3) Quem casou não precisa mais do texto (uma fonte de verdade por card).
UPDATE cards SET assignee = '' WHERE assignee_id IS NOT NULL;

-- Comentários da tarefa. `parent_id` = resposta a outro comentário (thread rasa, 1 nível).
CREATE TABLE card_comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    card_id    UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    parent_id  UUID REFERENCES card_comments(id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    -- Quem foi @mencionado neste comentário. Gravado no envio: recalcular depois pelo texto
    -- notificaria de novo quem já foi avisado, e erraria se a pessoa mudasse de nome.
    mentions   UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_card_comments ON card_comments(card_id, created_at);

-- Preferências de notificação por utilizador. Default: recebe tudo o que lhe diz respeito, mas
-- nunca do que ele próprio fez (isso é regra do código, não configuração).
ALTER TABLE mail_users ADD COLUMN notify_email BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE mail_users DROP COLUMN notify_email;
DROP TABLE IF EXISTS card_comments;
DROP INDEX IF EXISTS idx_cards_assignee;
ALTER TABLE cards DROP COLUMN created_by;
ALTER TABLE cards DROP COLUMN assignee_id;
