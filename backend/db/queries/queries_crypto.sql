-- Chaves da cifra de envelope dos ficheiros (docs/cifra-ficheiros.md).
--
-- Nenhuma destas consultas devolve chave em claro: o que sai daqui é sempre material embrulhado, e
-- só o pacote `filecrypt` o abre, com a KEK do ambiente.

-- name: GetTenantKey :one
SELECT wrapped_key, kek_version FROM tenant_keys WHERE tenant_id = @tenant_id;

-- name: CreateTenantKey :one
-- ON CONFLICT DO NOTHING + RETURNING não devolve linha quando já existe; por isso o chamador
-- trata a corrida relendo. Dois pedidos simultâneos do mesmo tenant não podem gerar duas chaves —
-- a segunda tornaria ilegível o que a primeira cifrou.
INSERT INTO tenant_keys (tenant_id, wrapped_key, kek_version)
VALUES (@tenant_id, @wrapped_key, @kek_version)
ON CONFLICT (tenant_id) DO NOTHING
RETURNING wrapped_key, kek_version;

-- name: ListTenantKeysByKekVersion :many
-- Para a rotação da KEK: re-embrulha só esta tabela, sem tocar num único objeto.
SELECT tenant_id, wrapped_key, kek_version FROM tenant_keys WHERE kek_version = @kek_version;

-- name: UpdateTenantKeyWrap :exec
UPDATE tenant_keys SET wrapped_key = @wrapped_key, kek_version = @kek_version, rotated_at = now()
WHERE tenant_id = @tenant_id;
