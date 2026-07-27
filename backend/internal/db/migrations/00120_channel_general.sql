-- +goose Up
-- Canal por defeito do tenant: existe sempre e toda a gente entra nele. A marca fica na BD (não o
-- nome traduzido — o backend não assume idioma, lei §4.6): guarda-se "General" e o frontend mostra
-- a tradução enquanto o nome estiver intacto. Se alguém o renomear, passa a valer o nome escolhido.
ALTER TABLE channels ADD COLUMN is_general BOOLEAN NOT NULL DEFAULT false;
-- Um único canal por defeito por tenant (o índice parcial é também a defesa contra corridas entre
-- dois pedidos a criá-lo ao mesmo tempo).
CREATE UNIQUE INDEX idx_channels_general ON channels(tenant_id) WHERE is_general;

-- +goose Down
DROP INDEX idx_channels_general;
ALTER TABLE channels DROP COLUMN is_general;
