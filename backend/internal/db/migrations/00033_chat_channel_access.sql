-- +goose Up
-- Acesso a canais: "listed" controla se o canal aparece na descoberta (não listado = fechado, só
-- entra por convite); "add_policy" controla quem pode adicionar membros (open = qualquer membro;
-- restricted = só gestores do canal). O papel por-canal (channel_members.role) passa a aceitar
-- 'manager' além de 'owner'/'member' — é uma permissão DO CANAL, independente da role da organização.
ALTER TABLE channels
    ADD COLUMN listed     BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN add_policy TEXT    NOT NULL DEFAULT 'open'; -- open | restricted

-- +goose Down
ALTER TABLE channels DROP COLUMN listed, DROP COLUMN add_policy;
