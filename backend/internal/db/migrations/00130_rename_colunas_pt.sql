-- +goose Up
-- As últimas doze colunas em português do esquema. Como o `dominio` da 00129, cada uma arrastava o
-- nome até ao Go gerado (`Nome`, `Plano`, `Tamanho`) e daí até ao JSON da API — um descuido de
-- nomenclatura que acabou a ser contrato público. Ver CLAUDE.md §4.6.
--
-- Nenhuma colide com uma coluna inglesa já existente na mesma tabela: foi verificado tabela a
-- tabela antes de escrever isto, incluindo os ADD/DROP COLUMN das migrações posteriores.
--
-- RENAME preserva dados, índices e chaves estrangeiras, e só mexe no catálogo — é instantâneo
-- mesmo nas tabelas grandes (storage_files, usage), ao contrário de criar-copiar-apagar.
--
-- ATENÇÃO À ORDEM DE IMPLANTAÇÃO: esta migração e o código novo têm de ir juntos.
ALTER TABLE audit_log      RENAME COLUMN acao            TO action;
ALTER TABLE audit_log      RENAME COLUMN alvo            TO target;
ALTER TABLE file_versions  RENAME COLUMN tamanho         TO size;
ALTER TABLE file_versions  RENAME COLUMN versao          TO version;
ALTER TABLE storage_files  RENAME COLUMN nome            TO name;
ALTER TABLE storage_files  RENAME COLUMN tamanho         TO size;
ALTER TABLE storage_files  RENAME COLUMN versao          TO version;
ALTER TABLE storage_shares RENAME COLUMN nivel           TO level;
ALTER TABLE subscriptions  RENAME COLUMN faixa_de_caixas TO mailbox_range;
ALTER TABLE tenants        RENAME COLUMN nome            TO name;
ALTER TABLE tenants        RENAME COLUMN plano           TO plan;
ALTER TABLE usage          RENAME COLUMN periodo         TO period;

-- +goose Down
ALTER TABLE audit_log      RENAME COLUMN action        TO acao;
ALTER TABLE audit_log      RENAME COLUMN target        TO alvo;
ALTER TABLE file_versions  RENAME COLUMN size          TO tamanho;
ALTER TABLE file_versions  RENAME COLUMN version       TO versao;
ALTER TABLE storage_files  RENAME COLUMN name          TO nome;
ALTER TABLE storage_files  RENAME COLUMN size          TO tamanho;
ALTER TABLE storage_files  RENAME COLUMN version       TO versao;
ALTER TABLE storage_shares RENAME COLUMN level         TO nivel;
ALTER TABLE subscriptions  RENAME COLUMN mailbox_range TO faixa_de_caixas;
ALTER TABLE tenants        RENAME COLUMN name          TO nome;
ALTER TABLE tenants        RENAME COLUMN plan          TO plano;
ALTER TABLE usage          RENAME COLUMN period        TO periodo;
