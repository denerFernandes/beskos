-- +goose Up
-- Cifra de envelope dos ficheiros (docs/cifra-ficheiros.md). O R2 passa a guardar só cifrado; as
-- chaves ficam do nosso lado.
--
-- HIERARQUIA:  KEK (ambiente, versionada) → chave do tenant → chave do objeto → objeto
--
-- Só a chave do TENANT vive aqui. A chave de cada objeto viaja dentro do próprio objeto, embrulhada
-- pela chave do tenant — e é isso que torna a escrita de um ficheiro ATÓMICA: uma escrita só, que o
-- armazenamento faz por inteiro ou não faz. Guardar a chave do objeto numa tabela obrigaria a duas
-- escritas e qualquer ordem teria uma janela de falha: o objeto primeiro deixa cifrado sem chave
-- (dados perdidos); a linha primeiro deixa linhas órfãs. Quem tem o objeto fica com a chave
-- embrulhada e não a consegue abrir — precisaria desta tabela e da KEK, e não tem nenhuma das duas.
--
-- Porquê uma chave POR TENANT: apagar esta linha torna todos os ficheiros do tenant ilegíveis de
-- imediato, o que é uma garantia de eliminação muito mais forte do que "apagámos as linhas" — e é
-- escrutinável no acordo de subcontratação. É a mesma lei que obriga toda a consulta a filtrar
-- tenant_id (CLAUDE.md §5.1), aplicada às chaves.

CREATE TABLE tenant_keys (
    tenant_id   UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    -- Chave do tenant, embrulhada pela KEK do ambiente. Nunca em claro, em lado nenhum.
    wrapped_key BYTEA       NOT NULL,
    -- Versão da KEK que embrulhou. Rodar a KEK re-embrulha ESTA tabela e mais nada: os objetos não
    -- se tocam, porque a chave que está dentro deles foi embrulhada pela chave do tenant, que não
    -- muda. É a razão de existir do envelope.
    kek_version INT         NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at  TIMESTAMPTZ
);

-- +goose Down
DROP TABLE tenant_keys;
