-- +goose Up
-- Apps desligadas por organização: o admin liga/desliga apps e elas somem do menu para todos, a
-- integração entre elas é cortada, e o assistente perde as ferramentas e o agente dessa app. Vazio =
-- tudo ligado. Guarda-se a lista de DESLIGADAS (não a de ligadas) para novas apps entrarem ligadas.
ALTER TABLE tenants ADD COLUMN disabled_apps TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE tenants DROP COLUMN disabled_apps;
