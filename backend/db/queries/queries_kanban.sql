-- Kanban: boards, columns, cards, checklists and dependencies.
--
-- Split out of queries.sql, which had grown to 2309 lines -- four times the 600 the project
-- allows (law 1.4). sqlc reads every file in this directory, so moving a query changes nothing
-- about the generated API; only where it lives.

-- name: ListBoards :many
-- Visibilidade por utilizador: dono, legado (sem dono nem grants = org toda), grant (org/user/grupo)
-- ou card atribuído no quadro (acesso automático).
SELECT b.* FROM boards b WHERE b.tenant_id = @tenant_id AND b.archived_at IS NULL AND (
    b.owner_user_id = @user_id
    OR (b.owner_user_id IS NULL AND NOT EXISTS (SELECT 1 FROM board_grants g WHERE g.board_id = b.id))
    OR EXISTS (
        SELECT 1 FROM board_grants g WHERE g.board_id = b.id AND (
            g.subject_type = 'org'
            OR (g.subject_type = 'user' AND g.subject_id = @user_id)
            OR (g.subject_type = 'group' AND EXISTS (
                SELECT 1 FROM user_group_members ugm
                WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))))
    OR EXISTS (SELECT 1 FROM cards c WHERE c.board_id = b.id AND c.assignee_id = @user_id)
) ORDER BY b.created_at;
-- name: ListArchivedBoards :many
SELECT * FROM boards WHERE tenant_id = $1 AND archived_at IS NOT NULL ORDER BY archived_at DESC;
-- name: SetBoardArchived :one
-- Arquiva (true) ou restaura (false) um quadro. Só no próprio tenant.
UPDATE boards SET archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN now() ELSE NULL END WHERE id = $1 AND tenant_id = $2 RETURNING *;
-- name: CreateBoard :one
-- The prefix comes from the name, by the SAME rule migration 00027 used (first 3 alphanumerics,
-- uppercased; none at all means 'TSK'). It was missing here: the insert left it on the column
-- default of '', so every task key rendered as "-1", "-2" instead of "QUA-1". Every board created
-- after that migration was born this way.
INSERT INTO boards (tenant_id, name, owner_user_id, prefix)
VALUES ($1, $2, $3, COALESCE(
    NULLIF(upper(substring(regexp_replace($2::text, '[^a-zA-Z0-9]', '', 'g') from 1 for 3)), ''),
    'TSK'))
RETURNING *;
-- name: BoardRole :one
-- Papel efetivo (rank) no quadro: 0=sem acesso, 1=viewer, 3=editor, 4=manager. Card atribuído = 3.
SELECT COALESCE(MAX(rank), 0)::int AS role_rank FROM (
    SELECT CASE WHEN b.owner_user_id = @user_id THEN 4
                WHEN b.owner_user_id IS NULL AND NOT EXISTS (
                    SELECT 1 FROM board_grants g WHERE g.board_id = b.id) THEN 3
                ELSE 0 END AS rank
    FROM boards b WHERE b.id = @board_id AND b.tenant_id = @tenant_id
    UNION ALL
    SELECT CASE g.role WHEN 'manager' THEN 4 WHEN 'editor' THEN 3 ELSE 1 END
    FROM board_grants g
    WHERE g.board_id = @board_id AND g.tenant_id = @tenant_id AND (
        g.subject_type = 'org'
        OR (g.subject_type = 'user' AND g.subject_id = @user_id)
        OR (g.subject_type = 'group' AND EXISTS (
            SELECT 1 FROM user_group_members ugm
            WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id)))
    UNION ALL
    SELECT 3 FROM cards c
    WHERE c.board_id = @board_id AND c.tenant_id = @tenant_id AND c.assignee_id = @user_id LIMIT 1
) t;
-- name: ListBoardGrants :many
SELECT * FROM board_grants WHERE tenant_id = @tenant_id AND board_id = @board_id
ORDER BY subject_type, created_at;
-- name: DeleteBoardGrantSubject :exec
DELETE FROM board_grants WHERE tenant_id = @tenant_id AND board_id = @board_id
  AND subject_type = @subject_type AND subject_id IS NOT DISTINCT FROM @subject_id;
-- name: InsertBoardGrant :one
INSERT INTO board_grants (tenant_id, board_id, subject_type, subject_id, role)
VALUES (@tenant_id, @board_id, @subject_type, @subject_id, @role) RETURNING *;
-- name: DeleteBoardGrant :exec
DELETE FROM board_grants WHERE id = @id AND tenant_id = @tenant_id;
-- name: CardBoard :one
SELECT board_id FROM cards WHERE id = @id AND tenant_id = @tenant_id;
-- name: GetBoard :one
SELECT * FROM boards WHERE id = $1 AND tenant_id = $2;
-- name: ListColumns :many
SELECT * FROM board_columns WHERE board_id = $1 ORDER BY position;
-- name: CreateColumn :one
INSERT INTO board_columns (board_id, tenant_id, name, color, position)
VALUES ($1, $2, $3, $4, $5) RETURNING *;
-- name: UpdateColumn :one
UPDATE board_columns SET name = $3, color = $4, wip_limit = $5 WHERE id = $1 AND tenant_id = $2 RETURNING *;
-- name: DeleteColumn :exec
DELETE FROM board_columns WHERE id = $1 AND tenant_id = $2;
-- name: ReorderColumns :exec
-- Reordena as colunas do quadro: define position (1..N) na ordem exata do array, num único UPDATE
-- atômico. Preso ao board_id e ao tenant para não mover coluna alheia. Lê-se por position em ListColumns.
UPDATE board_columns AS bc
SET position = t.ord::int
FROM unnest(sqlc.arg(column_ids)::uuid[]) WITH ORDINALITY AS t(id, ord)
WHERE bc.id = t.id AND bc.board_id = sqlc.arg(board_id) AND bc.tenant_id = sqlc.arg(tenant_id);
-- name: NextCardNumber :one
-- Trava a linha do quadro: dois cards criados ao mesmo tempo nunca pegam o mesmo número.
UPDATE boards SET card_seq = card_seq + 1 WHERE id = $1 RETURNING card_seq, prefix;
-- name: UpdateBoardPrefix :one
-- Prefixo em maiúsculas, sem espaços, até 6 — o resto é decisão de quem gere o quadro.
UPDATE boards SET prefix = $3 WHERE id = $1 AND tenant_id = $2 RETURNING *;
-- name: ListCardsByBoard :many
-- assignee_name: o nome do utilizador responsável, ou o texto legado de quem nunca casou com um
-- utilizador (a migração 00026 preservou-o). A UI mostra um ou outro sem saber a diferença.
SELECT c.*, COALESCE(u.name, NULLIF(u.email, ''), c.assignee) AS assignee_name,
       (SELECT count(*) FROM card_comments cc WHERE cc.card_id = c.id) AS comment_count,
       b.prefix || '-' || c.number AS card_key
FROM cards c
JOIN boards b ON b.id = c.board_id
LEFT JOIN mail_users u ON u.id = c.assignee_id
WHERE c.board_id = $1 AND c.archived_at IS NULL ORDER BY c.column_id, c.position;
-- name: SearchCards :many
-- Busca de tarefas para referenciar noutro sítio (o `/task` do chat). Casa pela chave (QUA-12),
-- pelo número solto (12) ou por texto do título. A visibilidade é a MESMA do ListBoards — sem
-- isso, o chat viraria uma janela para quadros a que a pessoa não tem acesso (lei §5.1/§5.2).
SELECT c.id, c.board_id, c.title, c.label, c.due, c.priority,
       b.prefix || '-' || c.number AS card_key, b.name AS board_name,
       col.name AS column_name,
       COALESCE(u.name, NULLIF(u.email, ''), c.assignee) AS assignee_name
FROM cards c
JOIN boards b ON b.id = c.board_id
JOIN board_columns col ON col.id = c.column_id
LEFT JOIN mail_users u ON u.id = c.assignee_id
WHERE c.tenant_id = @tenant_id AND c.archived_at IS NULL AND b.archived_at IS NULL
  AND (
      upper(b.prefix || '-' || c.number) = upper(@q::text)
      OR c.number::text = @q::text
      OR c.title ILIKE '%' || @q::text || '%'
  )
  AND (
      b.owner_user_id = @user_id
      OR (b.owner_user_id IS NULL AND NOT EXISTS (SELECT 1 FROM board_grants g WHERE g.board_id = b.id))
      OR EXISTS (
          SELECT 1 FROM board_grants g WHERE g.board_id = b.id AND (
              g.subject_type = 'org'
              OR (g.subject_type = 'user' AND g.subject_id = @user_id)
              OR (g.subject_type = 'group' AND EXISTS (
                  SELECT 1 FROM user_group_members ugm
                  WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))))
      OR EXISTS (SELECT 1 FROM cards c2 WHERE c2.board_id = b.id AND c2.assignee_id = @user_id)
  )
-- Chave exata primeiro (quem escreve "QUA-12" quer aquela tarefa), depois as mais recentes.
ORDER BY (upper(b.prefix || '-' || c.number) = upper(@q::text)) DESC, c.created_at DESC
LIMIT 20;
-- name: ListArchivedCards :many
SELECT * FROM cards WHERE board_id = $1 AND tenant_id = $2 AND archived_at IS NOT NULL ORDER BY archived_at DESC;
-- name: SetCardArchived :one
-- Arquiva (true) ou restaura (false) um card. Só no próprio tenant.
UPDATE cards SET archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN now() ELSE NULL END WHERE id = $1 AND tenant_id = $2 RETURNING *;
-- name: CreateCard :one
INSERT INTO cards (tenant_id, board_id, column_id, title, description, label, assignee, due,
                   source_email_id, position, contact_id, assignee_id, created_by, number, priority, start_date)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING *;
-- name: UpdateCard :one
-- contact_id só muda se o pedido trouxer o campo: quem edita a tarefa sem falar do contato não
-- pode desfazer o vínculo por omissão.
UPDATE cards SET title = $3, description = $4, label = $5, due = $6, assignee_id = $7,
    priority = sqlc.arg('priority'), start_date = sqlc.arg('start_date'), color = sqlc.arg('color'),
    contact_id = CASE WHEN sqlc.arg('set_contact')::boolean THEN sqlc.arg('contact_id') ELSE contact_id END
WHERE id = $1 AND tenant_id = $2 RETURNING *;
-- name: MoveCard :one
UPDATE cards SET column_id = $3, position = $4 WHERE id = $1 AND tenant_id = $2 RETURNING *;
-- name: ReorderColumn :exec
-- Reordena os cards de uma coluna e move para ela os de fora: define column_id e position (1..N) na
-- ordem exata do array, num único UPDATE atômico. Ordena por position em ListCardsByBoard.
UPDATE cards AS c
SET column_id = sqlc.arg(column_id), position = t.ord::int
FROM unnest(sqlc.arg(card_ids)::uuid[]) WITH ORDINALITY AS t(id, ord)
WHERE c.id = t.id AND c.tenant_id = sqlc.arg(tenant_id);
-- name: DeleteCard :exec
DELETE FROM cards WHERE id = $1 AND tenant_id = $2;
-- name: CardsDueForNotify :many
-- Cards com prazo (due 'YYYY-MM-DD') até a data limite, com responsável, ainda não avisados para
-- ESTE due. Datas ISO comparam lexicograficamente, então <= $1 pega vencidos e o que vence até lá.
SELECT * FROM cards
WHERE due <> '' AND due <= $1 AND assignee_id IS NOT NULL AND due_notified_for <> due
ORDER BY due LIMIT 200;
-- name: MarkCardDueNotified :exec
UPDATE cards SET due_notified_for = due WHERE id = $1;

-- ===== Calendário =====
-- ---- Fusão de duplicados: reatribui referências do contato "merge" para o "keep" ----
-- name: MergeReassignCards :exec
UPDATE cards SET contact_id = sqlc.arg(keep) WHERE contact_id = sqlc.arg(merge) AND tenant_id = sqlc.arg(tenant);
-- name: ListContactCards :many
-- Tarefas do contato = cards do Kanban ligados a ele. board_id serve para o link "abrir a tarefa".
SELECT c.id, c.title, c.description, c.assignee, c.due, c.column_id, c.board_id,
       b.name AS board_name, col.name AS column_name
FROM cards c JOIN boards b ON b.id = c.board_id JOIN board_columns col ON col.id = c.column_id
WHERE c.contact_id = $1 AND c.tenant_id = $2 ORDER BY c.created_at DESC;
-- name: UpdateBoard :one
UPDATE boards SET name = $3 WHERE id = $1 AND tenant_id = $2 RETURNING *;
-- name: DeleteBoard :exec
DELETE FROM boards WHERE id = $1 AND tenant_id = $2;
-- name: CountBoards :one
SELECT count(*) FROM boards WHERE tenant_id = $1;

-- ===== Grupos de utilizadores =====
-- name: ListCardComments :many
SELECT c.*, u.name AS author_name, u.email AS author_email
FROM card_comments c JOIN mail_users u ON u.id = c.author_id
WHERE c.card_id = $1 AND c.tenant_id = $2 ORDER BY c.created_at;
-- name: CreateCardComment :one
INSERT INTO card_comments (tenant_id, card_id, author_id, parent_id, body, mentions)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;
-- name: GetCardComment :one
SELECT * FROM card_comments WHERE id = $1 AND tenant_id = $2;
-- name: DeleteCardComment :exec
-- Só o autor apaga o próprio comentário.
DELETE FROM card_comments WHERE id = $1 AND author_id = $2;
-- name: EditCardComment :one
-- Só o autor edita o próprio comentário.
UPDATE card_comments SET body = $3 WHERE id = $1 AND author_id = $2 RETURNING *;
-- name: ListChecklist :many
SELECT * FROM card_checklist_items WHERE card_id = $1 AND tenant_id = $2 ORDER BY position, created_at;
-- name: NextChecklistPos :one
SELECT COALESCE(max(position), -1) + 1 FROM card_checklist_items WHERE card_id = $1 AND tenant_id = $2;
-- name: CreateChecklistItem :one
INSERT INTO card_checklist_items (tenant_id, card_id, text, position)
VALUES ($1, $2, $3, $4) RETURNING *;
-- name: SetChecklistDone :one
UPDATE card_checklist_items SET done = $3 WHERE id = $1 AND tenant_id = $2 RETURNING *;
-- name: DeleteChecklistItem :exec
DELETE FROM card_checklist_items WHERE id = $1 AND tenant_id = $2;
-- name: LogCardActivity :exec
INSERT INTO card_activity (tenant_id, card_id, actor_id, kind, detail) VALUES ($1, $2, $3, $4, $5);
-- name: AttachFileToCard :exec
INSERT INTO card_attachments (tenant_id, card_id, file_id, added_by)
VALUES ($1, $2, $3, $4) ON CONFLICT (card_id, file_id) DO NOTHING;
-- name: ListCardAttachments :many
SELECT ca.id, ca.file_id, f.name, f.size, ca.created_at
FROM card_attachments ca JOIN storage_files f ON f.id = ca.file_id
WHERE ca.card_id = $1 AND ca.tenant_id = $2 ORDER BY ca.created_at;
-- name: GetCardAttachmentBlob :one
-- Download tenant-scoped: o anexo tem de ser do tenant. Devolve o ficheiro para servir o blob.
SELECT f.* FROM card_attachments ca JOIN storage_files f ON f.id = ca.file_id
WHERE ca.id = $1 AND ca.tenant_id = $2;
-- name: ListCardActivity :many
SELECT a.*, COALESCE(u.name, NULLIF(u.email, ''), '') AS actor_name
FROM card_activity a LEFT JOIN mail_users u ON u.id = a.actor_id
WHERE a.card_id = $1 AND a.tenant_id = $2 ORDER BY a.created_at DESC LIMIT 100;
-- name: GetCard :one
SELECT * FROM cards WHERE id = $1 AND tenant_id = $2;

-- ---- Biblioteca de GIFs (pessoal) ----
-- ===== Kanban planning: WIP, múltiplos responsáveis, dependências =====
-- name: CountCardsInColumn :one
SELECT count(*) FROM cards WHERE column_id = $1 AND archived_at IS NULL;
-- name: AddCardAssignee :exec
INSERT INTO card_assignees (card_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;
-- name: RemoveCardAssignee :exec
DELETE FROM card_assignees WHERE card_id = $1 AND user_id = $2;
-- name: ListBoardAssignees :many
SELECT ca.card_id, u.id AS user_id, COALESCE(NULLIF(u.name, ''), u.email) AS name
FROM card_assignees ca JOIN cards c ON c.id = ca.card_id JOIN mail_users u ON u.id = ca.user_id
WHERE c.board_id = $1;
-- name: AddCardDep :exec
-- Só depende de card do mesmo quadro/tenant (o handler valida).
INSERT INTO card_deps (card_id, depends_on_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;
-- name: RemoveCardDep :exec
DELETE FROM card_deps WHERE card_id = $1 AND depends_on_id = $2;
-- name: ListBoardDeps :many
SELECT cd.card_id, cd.depends_on_id, dc.title AS dep_title, dc.column_id AS dep_column
FROM card_deps cd JOIN cards c ON c.id = cd.card_id JOIN cards dc ON dc.id = cd.depends_on_id
WHERE c.board_id = $1;
-- name: GetColumn :one
SELECT * FROM board_columns WHERE id = $1 AND tenant_id = $2;
-- name: AutomationsForColumn :many
-- Regras cujo gatilho é entrar nesta coluna (aplicadas quando um card se move para cá).
SELECT * FROM board_automations WHERE trigger_column_id = $1;
-- name: SetCardAssigneeOnly :exec
UPDATE cards SET assignee_id = $3 WHERE id = $1 AND tenant_id = $2;
-- name: SetCardLabelOnly :exec
UPDATE cards SET label = $3 WHERE id = $1 AND tenant_id = $2;
