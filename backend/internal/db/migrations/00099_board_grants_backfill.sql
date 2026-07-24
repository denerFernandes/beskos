-- +goose Up
-- Correção: quadros criados entre o 00098 e este fix ganharam dono mas NENHUM grant (ficaram
-- invisíveis aos colegas). Backfill: todo o quadro com dono e sem grants recebe o grant org=editor
-- (o default colaborativo pretendido; managers podem removê-lo para restringir).
INSERT INTO board_grants (tenant_id, board_id, subject_type, subject_id, role)
SELECT b.tenant_id, b.id, 'org', NULL, 'editor'
FROM boards b
WHERE b.owner_user_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM board_grants g WHERE g.board_id = b.id);

-- +goose Down
-- Sem down: remover grants criados aqui não é distinguível dos criados por utilizadores.
