-- +goose Up
-- Uma pessoa, uma permissão por ficheiro. Sem esta restrição, partilhar de novo com o mesmo
-- endereço criava uma SEGUNDA linha e a permissão que valia passava a depender da ordem de
-- leitura — foi o que impediu mudar "leitor" para "editor" sem apagar e voltar a partilhar.
-- Duplicados anteriores: fica a linha mais permissiva (owner > editor > leitor > link).
DELETE FROM storage_shares s USING storage_shares keep
WHERE s.file_id = keep.file_id AND s.principal = keep.principal AND s.id <> keep.id
  AND CASE keep.nivel WHEN 'owner' THEN 3 WHEN 'editor' THEN 2 WHEN 'leitor' THEN 1 ELSE 0 END
    > CASE s.nivel  WHEN 'owner' THEN 3 WHEN 'editor' THEN 2 WHEN 'leitor' THEN 1 ELSE 0 END;
-- Empates (mesmo nível repetido): fica a de id menor.
DELETE FROM storage_shares s USING storage_shares keep
WHERE s.file_id = keep.file_id AND s.principal = keep.principal AND s.nivel = keep.nivel AND s.id > keep.id;

CREATE UNIQUE INDEX storage_shares_file_principal_key ON storage_shares (file_id, principal);

-- +goose Down
DROP INDEX IF EXISTS storage_shares_file_principal_key;
