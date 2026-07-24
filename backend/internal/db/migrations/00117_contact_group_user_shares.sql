-- +goose Up
-- Partilha de GRUPO DE CONTATOS com um UTILIZADOR específico (além de com equipas). Antes só se
-- podia partilhar com uma equipa (contact_group_shares); agora também com uma pessoa. A visibilidade
-- do contato passa a considerar as duas vias (ver ListContacts/GetContact). (24/07/2026)
CREATE TABLE contact_group_user_shares (
    group_id UUID NOT NULL REFERENCES contact_groups(id) ON DELETE CASCADE,
    user_id  UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX idx_cgus_user ON contact_group_user_shares(user_id);

-- +goose Down
DROP TABLE contact_group_user_shares;
