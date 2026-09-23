-- +goose Up
-- A tabela `domains` está inteiramente em inglês menos uma coluna. `dominio` era a única palavra
-- portuguesa do esquema e arrastava o nome para o Go gerado (`Dominio`) e daí para o JSON da API,
-- que é como uma gralha de nomenclatura acaba a ser contrato público. Ver docs/engineering-standards.md §4.6.
--
-- RENAME e não uma coluna nova: preserva os dados, o índice UNIQUE e a chave estrangeira sem copiar
-- nada. É instantâneo — o Postgres só altera o catálogo — e reversível pela migração Down.
--
-- ATENÇÃO À ORDEM DE IMPLANTAÇÃO: esta migração e o código novo têm de ir juntos. O código antigo
-- pergunta por `dominio` e deixa de encontrar a coluna assim que isto corre.
ALTER TABLE domains RENAME COLUMN dominio TO domain;

-- +goose Down
ALTER TABLE domains RENAME COLUMN domain TO dominio;
