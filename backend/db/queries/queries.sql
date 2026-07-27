-- Queries do control plane. sqlc gera código tipado a partir daqui.
-- REGRA: toda query que toca dados de cliente filtra por tenant_id (brief §5/§10).

-- name: CreateTenant :one
INSERT INTO tenants (nome, plano) VALUES ($1, $2) RETURNING *;

-- name: GetTenant :one
SELECT * FROM tenants WHERE id = $1;

-- name: CreateAccount :one
-- Usuário com senha e SEM caixa (signup/convite: o tenant ainda não tem domínio).
INSERT INTO mail_users (tenant_id, email, password_hash, role)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetUserByID :one
SELECT * FROM mail_users WHERE id = $1;

-- name: UpdateUserLanguage :one
UPDATE mail_users SET language = $2 WHERE id = $1 RETURNING *;

-- name: UpdateUserPasswordByEmail :exec
UPDATE mail_users SET password_hash = $3 WHERE email = $1 AND tenant_id = $2;

-- name: CreateSession :one
INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3) RETURNING *;

-- name: GetSession :one
SELECT sqlc.embed(sessions), sqlc.embed(mail_users)
FROM sessions
JOIN mail_users ON mail_users.id = sessions.user_id
WHERE sessions.token = $1 AND sessions.expires_at > now();

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token = $1;

-- name: CreateDomain :one
INSERT INTO domains (tenant_id, dominio, provision_state)
VALUES ($1, $2, 'pending') RETURNING *;

-- name: ListDomainsByTenant :many
SELECT * FROM domains WHERE tenant_id = $1 ORDER BY created_at DESC;

-- name: CountMailUsersOnDomain :one
-- Utilizadores/caixas cujo e-mail pertence ao domínio (não há FK; o vínculo é o sufixo do e-mail).
SELECT count(*) FROM mail_users WHERE tenant_id = @tenant_id AND email LIKE '%@' || @dominio;

-- name: DeleteDomainRow :exec
DELETE FROM domains WHERE id = @id AND tenant_id = @tenant_id;

-- name: SetTenantMFAPolicy :exec
-- Política de MFA obrigatório da organização: ligada/desligada + método exigido (migração 00110).
UPDATE tenants SET mfa_required = @mfa_required, mfa_method = @mfa_method WHERE id = @id;

-- name: DeleteTenantWebmailSessions :exec
-- Mata TODAS as sessões do tenant menos a atual (ao ligar a política de MFA: todos são forçados
-- a re-entrar e caem no gate de enrollment).
DELETE FROM webmail_sessions s USING mail_users u
WHERE s.mail_user_id = u.id AND u.tenant_id = @tenant_id AND s.token <> @keep_token;

-- name: UpdateMyProfile :exec
UPDATE mail_users SET name = @name, job_title = @job_title, phone = @phone, avatar = @avatar
WHERE id = @id;

-- name: UpdateUserPassword :exec
UPDATE mail_users SET password_hash = @password_hash WHERE id = @id;

-- name: DeleteUserWebmailSessionsExcept :exec
-- Troca de senha: derruba as OUTRAS sessões do utilizador (a atual continua).
DELETE FROM webmail_sessions WHERE mail_user_id = @mail_user_id AND token <> @keep_token;

-- name: GetDomainByID :one
SELECT * FROM domains WHERE id = $1;

-- name: UpdateDomainProvision :one
UPDATE domains
SET provision_state = $2, stalwart_id = $3, dkim_status = $4, dns_status = $5, dns_zone = $6
WHERE id = $1 RETURNING *;

-- name: ListDomainsInState :many
SELECT * FROM domains WHERE provision_state = $1 ORDER BY created_at ASC LIMIT $2;

-- name: CreateMailUser :one
INSERT INTO mail_users (tenant_id, email, name, role, stalwart_account_id, quota_bytes, password_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: ListMailUsersByTenant :many
SELECT * FROM mail_users WHERE tenant_id = $1 ORDER BY created_at DESC;

-- name: GetMailUserByEmail :one
SELECT * FROM mail_users WHERE email = $1;

-- name: GetMailUserInTenant :one
SELECT * FROM mail_users WHERE id = $1 AND tenant_id = $2;

-- name: InsertAudit :exec
INSERT INTO audit_log (tenant_id, actor, acao, alvo) VALUES ($1, $2, $3, $4);

-- ===== Webmail session (usuário final na nossa API) =====

-- name: CreateWebmailSession :one
INSERT INTO webmail_sessions (token, mail_user_id, expires_at) VALUES ($1, $2, $3) RETURNING *;

-- name: GetWebmailSession :one
SELECT sqlc.embed(mail_users) FROM webmail_sessions
JOIN mail_users ON mail_users.id = webmail_sessions.mail_user_id
WHERE webmail_sessions.token = $1 AND webmail_sessions.expires_at > now();

-- name: DeleteWebmailSession :exec
DELETE FROM webmail_sessions WHERE token = $1;

-- ===== Marcadores =====

-- name: ListLabels :many
SELECT * FROM labels WHERE tenant_id = $1 ORDER BY name;

-- name: CreateLabel :one
INSERT INTO labels (tenant_id, name, color) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateLabel :one
UPDATE labels SET name = $3, color = $4 WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: DeleteLabel :exec
DELETE FROM labels WHERE id = $1 AND tenant_id = $2;

-- ===== Kanban =====

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
INSERT INTO boards (tenant_id, name, owner_user_id) VALUES ($1, $2, $3) RETURNING *;

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

-- name: ListEventsRange :many
SELECT * FROM events
WHERE owner_user_id = $1 AND starts_at < $3 AND ends_at > $2 AND deleted_at IS NULL
ORDER BY starts_at;

-- name: CreateEvent :one
INSERT INTO events (tenant_id, owner_user_id, title, description, location, color, starts_at, ends_at, source_email_id, tags, meeting_room, remind_minutes, all_day, recur, recur_until, calendar_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING *;

-- name: UpdateEvent :one
-- reminded = false: editar re-arma o lembrete (ex.: mudou a hora, avisa de novo).
UPDATE events SET title = $3, description = $4, location = $5, color = $6, starts_at = $7, ends_at = $8, tags = $9, remind_minutes = $11, all_day = $12, recur = $13, recur_until = $14, calendar_id = $15, reminded = false
WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $10 RETURNING *;

-- name: ListRecurringByOwner :many
-- Mestres recorrentes do dono (recur <> ''); o backend expande as ocorrências no intervalo pedido.
SELECT * FROM events WHERE owner_user_id = $1 AND recur <> '' AND deleted_at IS NULL ORDER BY starts_at;

-- name: EventsToRemind :many
-- Eventos cujo lembrete deve disparar agora: início dentro da antecedência (remind_minutes), ainda
-- futuros, com lembrete ligado e não avisados. Traz o dono para notificar.
SELECT id, owner_user_id, tenant_id, title, starts_at FROM events
WHERE remind_minutes > 0 AND NOT reminded AND owner_user_id IS NOT NULL AND deleted_at IS NULL
  AND starts_at > now() AND starts_at - make_interval(mins => remind_minutes) <= now()
ORDER BY starts_at LIMIT 200;

-- name: MarkEventReminded :exec
UPDATE events SET reminded = true WHERE id = $1;

-- name: DeleteEvent :exec
-- Soft-delete: marca deleted_at (permite desfazer via RestoreEvent). Só o dono, no seu tenant.
UPDATE events SET deleted_at = now() WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $3 AND deleted_at IS NULL;

-- name: RestoreEvent :one
-- Desfaz a exclusão dentro da janela de undo. Devolve o evento restaurado (ou nada se não achar).
UPDATE events SET deleted_at = NULL WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $3 RETURNING *;

-- ===== RBAC: convites e membros =====

-- name: CreateInvite :one
INSERT INTO invites (tenant_id, email, role, token, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetValidInvite :one
SELECT * FROM invites WHERE token = $1 AND accepted_at IS NULL AND expires_at > now();

-- name: AcceptInvite :exec
UPDATE invites SET accepted_at = now() WHERE token = $1;

-- name: ConsumeInvite :one
-- Marca o convite como aceito ATOMICAMENTE e devolve-o só se ainda estava válido (uso único, sem
-- corrida): dois aceites concorrentes com o mesmo token — só um recebe a linha.
UPDATE invites SET accepted_at = now()
WHERE token = $1 AND accepted_at IS NULL AND expires_at > now()
RETURNING *;

-- name: ListPendingInvites :many
SELECT * FROM invites WHERE tenant_id = $1 AND accepted_at IS NULL AND expires_at > now() ORDER BY created_at DESC;

-- name: RevokeInvite :exec
DELETE FROM invites WHERE id = $1 AND tenant_id = $2;

-- name: ListMembers :many
SELECT id, email, name, role, created_at, (stalwart_account_id IS NOT NULL) AS has_mailbox
FROM mail_users WHERE tenant_id = $1 ORDER BY created_at;

-- name: UpdateUserRole :one
UPDATE mail_users SET role = $3 WHERE id = $1 AND tenant_id = $2 RETURNING id, email, role, created_at;

-- name: DeleteUserInTenant :exec
DELETE FROM mail_users WHERE id = $1 AND tenant_id = $2;

-- name: CountOwners :one
SELECT count(*) FROM mail_users WHERE tenant_id = $1 AND role = 'owner';

-- ===== MFA =====

-- name: SetTOTPSecret :exec
UPDATE mail_users SET totp_secret = $2 WHERE id = $1;

-- name: EnableTOTP :exec
UPDATE mail_users SET mfa_totp_enabled = true WHERE id = $1;

-- name: SetTOTPLastStep :exec
-- Regista o passo de tempo aceito (anti-replay): só avança, nunca recua.
UPDATE mail_users SET last_totp_step = $2 WHERE id = $1 AND $2 > last_totp_step;

-- name: SetJmapCred :exec
-- Guarda a credencial JMAP (Basic) cifrada, para o proxy injetar server-to-server (brokering H11).
UPDATE mail_users SET jmap_cred = $2 WHERE id = $1;

-- name: SetPhone :exec
UPDATE mail_users SET phone = $2 WHERE id = $1;

-- name: SetPresence :exec
UPDATE mail_users SET presence = $2 WHERE id = $1;

-- name: EnableSMS :exec
UPDATE mail_users SET mfa_sms_enabled = true WHERE id = $1;

-- name: DisableMFA :exec
UPDATE mail_users SET mfa_totp_enabled = false, mfa_sms_enabled = false, totp_secret = '', phone = '' WHERE id = $1;

-- name: InsertRecoveryCode :exec
INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2);

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = $1;

-- name: ListActiveRecoveryCodes :many
SELECT id, code_hash FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL;

-- name: UseRecoveryCode :exec
UPDATE recovery_codes SET used_at = now() WHERE id = $1;

-- name: CreateMFAChallenge :exec
INSERT INTO mfa_challenges (token, user_id, sms_code_hash, expires_at) VALUES ($1, $2, $3, $4);

-- name: GetMFAChallenge :one
SELECT * FROM mfa_challenges WHERE token = $1 AND expires_at > now();

-- name: IncrMFAChallengeAttempts :one
UPDATE mfa_challenges SET attempts = attempts + 1 WHERE token = $1 RETURNING attempts;

-- name: DeleteMFAChallenge :exec
DELETE FROM mfa_challenges WHERE token = $1;

-- ===== Armazenamento (MinIO) =====

-- name: CreateStorageFile :one
INSERT INTO storage_files (tenant_id, owner_user_id, nome, blob_key, hash, tamanho, versao, folder_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: ListStorageFilesByTenant :many
SELECT * FROM storage_files WHERE tenant_id = $1 ORDER BY created_at DESC;

-- name: ListFilesInFolder :many
SELECT * FROM storage_files WHERE owner_user_id = $1 AND folder_id IS NOT DISTINCT FROM $2 AND deleted_at IS NULL ORDER BY nome;

-- name: ListTrashedFiles :many
-- Lixeira do dono: só os soft-deleted, mais recentes primeiro.
SELECT * FROM storage_files WHERE owner_user_id = $1 AND deleted_at IS NOT NULL ORDER BY deleted_at DESC;

-- name: CreateDeviceToken :one
INSERT INTO device_tokens (tenant_id, mail_user_id, name, token_hash, token_prefix, expires_at)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, name, token_prefix, scope, expires_at, created_at;

-- name: GetDeviceTokenUser :one
-- Autentica o daemon: casa o hash do token, ignora revogados/expirados, e devolve a mail_user
-- (para injetar no contexto, tal como a sessão do webmail) + o id do token (para o touch).
SELECT sqlc.embed(mail_users), device_tokens.id AS token_id
FROM device_tokens
JOIN mail_users ON mail_users.id = device_tokens.mail_user_id
WHERE device_tokens.token_hash = $1
  AND device_tokens.revoked_at IS NULL
  AND (device_tokens.expires_at IS NULL OR device_tokens.expires_at > now());

-- name: TouchDeviceToken :exec
UPDATE device_tokens SET last_used_at = now() WHERE id = $1;

-- name: ListDeviceTokens :many
-- Dispositivos do próprio utilizador (a UI de Definições lista/revoga). Nunca expõe o hash.
SELECT id, name, token_prefix, scope, last_used_at, expires_at, revoked_at, created_at
FROM device_tokens WHERE mail_user_id = $1 ORDER BY created_at DESC;

-- name: RevokeDeviceToken :exec
-- Revoga um dispositivo do próprio utilizador (owner-scoped). Idempotente.
UPDATE device_tokens SET revoked_at = now()
WHERE id = $1 AND mail_user_id = $2 AND revoked_at IS NULL;

-- name: ListFileChanges :many
-- Feed incremental para o cliente de sync (daemon). Keyset por (updated_at, id): o cursor é opaco
-- no cliente e nunca salta nem repete linhas no limite da página. Inclui os da lixeira (deleted_at
-- != NULL) como tombstones — o cliente remove-os localmente. `hash` serve de ETag do conteúdo.
-- Inclui os MEUS ficheiros e os que me foram PARTILHADOS (principal = o meu e-mail), num único
-- keyset — assim o cursor continua a ser um só. `owned` diz ao cliente se pode escrever: um
-- partilhado é só-leitura no sync (vai para uma pasta reservada e não sobe alterações).
SELECT f.id, f.folder_id, f.nome, f.hash, f.tamanho, f.versao, f.deleted_at, f.updated_at,
       (f.owner_user_id = $2) AS owned
FROM storage_files f
WHERE f.tenant_id = $1
  AND (f.owner_user_id = $2 OR EXISTS (
        SELECT 1 FROM storage_shares sh
        WHERE sh.file_id = f.id AND sh.tenant_id = $1 AND sh.principal = $3))
  AND (f.updated_at > $4 OR (f.updated_at = $4 AND f.id > $5))
ORDER BY f.updated_at, f.id
LIMIT $6;

-- name: TrashFile :one
-- Soft delete: manda para a lixeira (não apaga blob). now() marca quando foi.
UPDATE storage_files SET deleted_at = now() WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: RestoreFile :one
-- Restaura da lixeira: volta a ser ativo.
UPDATE storage_files SET deleted_at = NULL WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: GetStorageFile :one
SELECT * FROM storage_files WHERE id = $1 AND owner_user_id = $2;

-- name: SetFileStarred :one
-- Marca/desmarca favorito. Só o dono (multi-tenant via owner_user_id).
UPDATE storage_files SET starred = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: SearchStorageFiles :many
-- Busca global (recursiva): casa o nome em TODAS as pastas do dono, não só na atual. LIMIT p/ teto.
-- Ignora a lixeira (deleted_at IS NULL).
SELECT * FROM storage_files WHERE owner_user_id = $1 AND nome ILIKE $2 AND deleted_at IS NULL ORDER BY nome LIMIT 200;

-- name: GetStorageFileInTenant :one
SELECT * FROM storage_files WHERE id = $1 AND tenant_id = $2;

-- name: UpdateStorageFileContent :one
UPDATE storage_files SET blob_key = $3, hash = $4, tamanho = $5, versao = versao + 1
WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: DeleteStorageFile :exec
DELETE FROM storage_files WHERE id = $1 AND owner_user_id = $2;

-- name: MoveStorageFile :one
UPDATE storage_files SET folder_id = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: RenameStorageFile :one
UPDATE storage_files SET nome = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: MoveRenameStorageFile :one
-- Move E renomeia num só UPDATE (o daemon de sync trata mover/renomear local como uma operação).
UPDATE storage_files SET folder_id = $3, nome = $4 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- ===== Pastas =====

-- name: ListFolders :many
SELECT * FROM storage_folders WHERE owner_user_id = $1 AND parent_id IS NOT DISTINCT FROM $2 ORDER BY name;

-- name: GetFolder :one
SELECT * FROM storage_folders WHERE id = $1 AND owner_user_id = $2;

-- name: ListAllFoldersByOwner :many
-- Todas as pastas do dono (qualquer nível) — o backend monta a árvore em memória (ex.: zip recursivo).
SELECT * FROM storage_folders WHERE owner_user_id = $1;

-- name: SearchStorageFolders :many
-- Pastas cujo nome casa a busca, em qualquer nível (para a busca global também achar pastas).
SELECT * FROM storage_folders WHERE owner_user_id = $1 AND name ILIKE $2 ORDER BY name LIMIT 100;

-- name: CreateFolder :one
INSERT INTO storage_folders (tenant_id, owner_user_id, parent_id, name) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: RenameFolder :one
UPDATE storage_folders SET name = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: MoveFolder :one
-- Move a pasta para outro pai (null = raiz). O handler barra ciclo (mover para dentro de si/descendente).
UPDATE storage_folders SET parent_id = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: DeleteFolder :exec
DELETE FROM storage_folders WHERE id = $1 AND owner_user_id = $2;

-- name: DeleteShare :exec
DELETE FROM storage_shares WHERE id = $1 AND tenant_id = $2;

-- name: CountBlobRefs :one
SELECT count(*) FROM storage_files WHERE tenant_id = $1 AND blob_key = $2;

-- name: CountBlobRefsAll :one
-- Blob em uso por QUALQUER registo: ficheiro atual OU versão histórica. A purga só remove o blob do
-- MinIO quando isto é 0 (senão apagaria conteúdo ainda referenciado por outra versão/ficheiro dedup).
SELECT (SELECT count(*) FROM storage_files WHERE storage_files.tenant_id = $1 AND storage_files.blob_key = $2)
     + (SELECT count(*) FROM file_versions WHERE file_versions.tenant_id = $1 AND file_versions.blob_key = $2) AS total;

-- name: SnapshotFileVersion :exec
-- Fotografa uma versão no histórico (o estado corrente, antes de sobrescrever).
INSERT INTO file_versions (file_id, tenant_id, blob_key, hash, tamanho, versao, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListFileVersions :many
SELECT v.*, COALESCE(u.name, NULLIF(u.email, ''), '') AS author
FROM file_versions v LEFT JOIN mail_users u ON u.id = v.created_by
WHERE v.file_id = $1 AND v.tenant_id = $2 ORDER BY v.versao DESC;

-- name: GetFileVersion :one
SELECT * FROM file_versions WHERE id = $1 AND file_id = $2 AND tenant_id = $3;

-- name: ListVersionBlobs :many
SELECT DISTINCT blob_key FROM file_versions WHERE file_id = $1 AND tenant_id = $2;

-- name: MaxVersionByName :one
SELECT COALESCE(max(versao), 0)::int FROM storage_files WHERE tenant_id = $1 AND nome = $2;

-- name: CreateShare :one
INSERT INTO storage_shares (file_id, tenant_id, principal, nivel) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListSharesByFile :many
SELECT * FROM storage_shares WHERE file_id = $1 AND tenant_id = $2;

-- name: SharedFileIDs :many
SELECT DISTINCT file_id FROM storage_shares WHERE tenant_id = $1;

-- ===== Contatos (grupos N:N + tags) =====

-- REGRA DE VISIBILIDADE (repetida em toda leitura de contato):
--   tenant  → todos da organização veem
--   private → só o dono
--   groups  → o dono + quem está num grupo com quem o contato foi compartilhado
-- Ler SEMPRE por aqui; nunca por owner_user_id sozinho (esconderia o que é compartilhado) nem
-- por tenant_id sozinho (vazaria o que é privado).

-- name: ListContacts :many
-- VISIBILIDADE PELO GRUPO: vejo o contato se sou o dono, ou se estou numa EQUIPA com que algum
-- GRUPO dele foi partilhado. Uma pessoa herda da empresa: ver a Acme é ver quem trabalha nela —
-- senão a lista mostraria a empresa e escondia as pessoas dela (o bug "de —" de volta).
WITH vis AS (
    SELECT c.id FROM contacts c
    WHERE c.tenant_id = $2 AND (
        c.owner_user_id = $1
        OR EXISTS (
            SELECT 1 FROM contact_group_members m
            JOIN contact_group_shares s ON s.group_id = m.group_id
            JOIN user_group_members ugm ON ugm.group_id = s.user_group_id
            WHERE m.contact_id = c.id AND ugm.user_id = $1)
        OR EXISTS (
            SELECT 1 FROM contact_group_members m
            JOIN contact_group_user_shares us ON us.group_id = m.group_id
            WHERE m.contact_id = c.id AND us.user_id = $1)
    )
)
SELECT c.*, co.name AS company_name
FROM contacts c
LEFT JOIN contacts co ON co.id = c.company_id
WHERE c.tenant_id = $2
  AND (c.id IN (SELECT id FROM vis) OR c.company_id IN (SELECT id FROM vis))
ORDER BY c.name;

-- name: GetContact :one
WITH vis AS (
    SELECT c.id FROM contacts c
    WHERE c.tenant_id = $3 AND (
        c.owner_user_id = $2
        OR EXISTS (
            SELECT 1 FROM contact_group_members m
            JOIN contact_group_shares s ON s.group_id = m.group_id
            JOIN user_group_members ugm ON ugm.group_id = s.user_group_id
            WHERE m.contact_id = c.id AND ugm.user_id = $2)
        OR EXISTS (
            SELECT 1 FROM contact_group_members m
            JOIN contact_group_user_shares us ON us.group_id = m.group_id
            WHERE m.contact_id = c.id AND us.user_id = $2)
    )
)
SELECT c.*, co.name AS company_name
FROM contacts c
LEFT JOIN contacts co ON co.id = c.company_id
WHERE c.id = $1 AND c.tenant_id = $3
  AND (c.id IN (SELECT id FROM vis) OR c.company_id IN (SELECT id FROM vis));

-- name: ListCompanyPeople :many
-- Pessoas de uma empresa. Sem filtro de visibilidade: quem chegou à ficha da empresa já passou
-- pelo GetContact — e as pessoas dela fazem parte do registo.
SELECT * FROM contacts WHERE company_id = $1 AND tenant_id = $2 ORDER BY name;

-- name: FindCompanyByDomain :one
-- Empresa (kind='company') do dono com este domínio — base do vínculo pessoa↔empresa automático.
SELECT * FROM contacts
WHERE tenant_id = @tenant_id AND owner_user_id = @owner_user_id
  AND kind = 'company' AND lower(domain) = lower(@domain) AND domain <> ''
ORDER BY created_at LIMIT 1;

-- name: LinkPeopleByDomain :execrows
-- Liga pessoas sem empresa a uma empresa pelo domínio (idempotente). Usado no "enriquecer agora".
UPDATE contacts p SET company_id = c.id
FROM contacts c
WHERE p.tenant_id = @tenant_id AND p.owner_user_id = @owner_user_id
  AND p.kind = 'person' AND p.company_id IS NULL AND p.domain <> ''
  AND c.tenant_id = p.tenant_id AND c.owner_user_id = p.owner_user_id
  AND c.kind = 'company' AND lower(c.domain) = lower(p.domain);

-- name: CreateContact :one
INSERT INTO contacts (tenant_id, owner_user_id, kind, name, email, phone, company, domain, notes,
                      tags, title, mobile, is_whatsapp, allow_phone, allow_email, company_id,
                      extra_emails, extra_phones, custom_fields)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19) RETURNING *;

-- name: UpdateContact :one
-- EDITAR é só do dono (ver ≠ mexer).
UPDATE contacts SET kind = $3, name = $4, email = $5, phone = $6, company = $7, domain = $8,
    notes = $9, tags = $10, title = $11, mobile = $12, is_whatsapp = $13, allow_phone = $14,
    allow_email = $15, company_id = $16, extra_emails = $17, extra_phones = $18, custom_fields = $19
WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: SetContactAvatar :one
-- Define/remove a foto do contato (aponta para um storage_files do dono). Só o dono.
UPDATE contacts SET avatar_file_id = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: DeleteContact :exec
DELETE FROM contacts WHERE id = $1 AND owner_user_id = $2;

-- ---- Fusão de duplicados: reatribui referências do contato "merge" para o "keep" ----
-- name: MergeReassignCards :exec
UPDATE cards SET contact_id = sqlc.arg(keep) WHERE contact_id = sqlc.arg(merge) AND tenant_id = sqlc.arg(tenant);

-- name: MergeReassignNotes :exec
UPDATE crm_notes SET contact_id = sqlc.arg(keep) WHERE contact_id = sqlc.arg(merge) AND tenant_id = sqlc.arg(tenant);

-- name: MergeReassignGroups :exec
-- Move as filiações de grupo (evita duplicar a PK contact_id+group_id com ON CONFLICT).
INSERT INTO contact_group_members (contact_id, group_id)
SELECT sqlc.arg(keep), m.group_id FROM contact_group_members m WHERE m.contact_id = sqlc.arg(merge)
ON CONFLICT DO NOTHING;

-- name: FindContactByEmail :one
SELECT * FROM contacts WHERE tenant_id = $2 AND $1 <> '' AND lower(email) = lower($1) LIMIT 1;

-- ---- Partilha de GRUPO DE CONTATOS com EQUIPA ----

-- name: ListGroupShares :many
SELECT s.user_group_id, u.name FROM contact_group_shares s
JOIN user_groups u ON u.id = s.user_group_id WHERE s.group_id = $1 ORDER BY u.name;

-- name: SetGroupShare :exec
INSERT INTO contact_group_shares (group_id, user_group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ClearGroupShares :exec
DELETE FROM contact_group_shares WHERE group_id = $1;

-- Partilha de GRUPO DE CONTATOS com um UTILIZADOR específico (além de equipas).
-- name: ListGroupUserShares :many
SELECT s.user_id, u.name FROM contact_group_user_shares s
JOIN mail_users u ON u.id = s.user_id WHERE s.group_id = $1 ORDER BY u.name;

-- name: SetGroupUserShare :exec
INSERT INTO contact_group_user_shares (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ClearGroupUserShares :exec
DELETE FROM contact_group_user_shares WHERE group_id = $1;

-- Equipas do tenant (id+nome) para o PICKER de partilha — sem exigir permissão de admin, ao
-- contrário de ListUserGroups (que é para a gestão de membros). Escolher com quem partilhar o meu
-- próprio grupo não é uma operação de administração.
-- name: ListTeamsForShare :many
SELECT id, name FROM user_groups WHERE tenant_id = $1 ORDER BY name;

-- name: ListContactNotes :many
SELECT n.id, n.body, n.created_at, u.name AS author_name, u.email AS author_email
FROM crm_notes n JOIN mail_users u ON u.id = n.author_id
WHERE n.contact_id = $1 AND n.tenant_id = $2 ORDER BY n.created_at DESC;

-- name: CreateContactNote :one
INSERT INTO crm_notes (tenant_id, contact_id, author_id, body)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: DeleteCrmNote :exec
DELETE FROM crm_notes WHERE id = $1 AND author_id = $2;

-- name: ListContactCards :many
-- Tarefas do contato = cards do Kanban ligados a ele. board_id serve para o link "abrir a tarefa".
SELECT c.id, c.title, c.description, c.assignee, c.due, c.column_id, c.board_id,
       b.name AS board_name, col.name AS column_name
FROM cards c JOIN boards b ON b.id = c.board_id JOIN board_columns col ON col.id = c.column_id
WHERE c.contact_id = $1 AND c.tenant_id = $2 ORDER BY c.created_at DESC;

-- name: ListContactGroups :many
SELECT * FROM contact_groups WHERE owner_user_id = $1 ORDER BY name;

-- name: CreateContactGroup :one
INSERT INTO contact_groups (tenant_id, owner_user_id, name, color) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdateContactGroup :one
UPDATE contact_groups SET name = $3, color = $4 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: DeleteContactGroup :exec
DELETE FROM contact_groups WHERE id = $1 AND owner_user_id = $2;

-- name: ClearContactGroups :exec
DELETE FROM contact_group_members WHERE contact_id = $1;

-- name: ListContactGroupEmails :many
-- E-mails dos contatos de um grupo (lista de distribuição): escrever ao grupo inteiro de uma vez.
-- Só contatos do próprio dono e com e-mail preenchido.
SELECT c.name, c.email FROM contact_group_members m
JOIN contacts c ON c.id = m.contact_id
WHERE m.group_id = $1 AND c.owner_user_id = $2 AND c.email <> '' ORDER BY c.name;

-- name: AddContactToGroup :exec
INSERT INTO contact_group_members (contact_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ListContactMemberships :many
SELECT m.contact_id, m.group_id FROM contact_group_members m
JOIN contacts c ON c.id = m.contact_id
WHERE c.owner_user_id = $1;

-- ===== Billing e cotas =====

-- name: CountMailboxesByTenant :one
SELECT count(*) FROM mail_users WHERE tenant_id = $1;

-- name: SumStorageBytes :one
SELECT COALESCE(sum(tamanho), 0)::bigint
FROM (SELECT DISTINCT ON (blob_key) tamanho FROM storage_files WHERE tenant_id = $1) t;

-- name: UpdateTenantPlan :exec
UPDATE tenants SET plano = $2 WHERE id = $1;

-- name: UpsertSubscription :exec
INSERT INTO subscriptions (tenant_id, stripe_id, faixa_de_caixas, status)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id) DO UPDATE SET stripe_id = EXCLUDED.stripe_id, faixa_de_caixas = EXCLUDED.faixa_de_caixas, status = EXCLUDED.status;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE tenant_id = $1;

-- name: UpdateSubscriptionStatus :exec
-- Sincroniza só o status (customer.subscription.updated) sem tocar no plano/faixa.
UPDATE subscriptions SET status = @status WHERE tenant_id = @tenant_id;

-- name: MarkWebhookProcessed :one
INSERT INTO processed_webhooks (event_id) VALUES ($1) ON CONFLICT DO NOTHING RETURNING event_id;

-- ===== Meet (reuniões + participantes) =====

-- name: CreateMeeting :one
INSERT INTO meetings (tenant_id, owner_user_id, title, room_name, scheduled_start, scheduled_end, event_id, access)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: GetMeetingInTenant :one
SELECT * FROM meetings WHERE id = $1 AND tenant_id = $2;

-- name: GetMeetingByRoom :one
SELECT * FROM meetings WHERE room_name = $1;

-- name: CancelMeeting :exec
UPDATE meetings SET status = 'canceled' WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $3;

-- name: UpdateMeeting :one
-- Editar/reagendar: só o dono, e só reunião não cancelada. Devolve a linha para sincronizar o evento.
UPDATE meetings SET title = $4, scheduled_start = $5, scheduled_end = $6
WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $3 AND status <> 'canceled' RETURNING *;

-- name: SetMeetingStarted :exec
UPDATE meetings SET actual_start = COALESCE(actual_start, now()), status = 'live'
WHERE room_name = $1 AND status <> 'canceled';

-- name: SetMeetingEnded :exec
UPDATE meetings SET actual_end = now(), status = 'ended'
WHERE room_name = $1 AND status <> 'canceled';

-- name: ListMyMeetings :many
SELECT m.*, COALESCE(p.rsvp, '') AS my_rsvp, (m.owner_user_id = $2) AS is_owner
FROM meetings m
LEFT JOIN meeting_participants p ON p.meeting_id = m.id AND p.email = $3
WHERE m.tenant_id = $1 AND (m.owner_user_id = $2 OR p.id IS NOT NULL)
ORDER BY COALESCE(m.scheduled_start, m.created_at) DESC;

-- name: ListParticipants :many
SELECT * FROM meeting_participants WHERE meeting_id = $1 ORDER BY role DESC, email;

-- name: AddParticipant :one
INSERT INTO meeting_participants (meeting_id, tenant_id, email, user_id, role)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (meeting_id, email) DO UPDATE SET role = EXCLUDED.role
RETURNING *;

-- name: SetRSVP :one
UPDATE meeting_participants SET rsvp = $3
WHERE meeting_id = $1 AND email = $2 RETURNING *;

-- name: MarkParticipantNotified :exec
UPDATE meeting_participants SET notified_at = now() WHERE meeting_id = $1 AND email = $2;

-- name: MarkParticipantJoined :exec
UPDATE meeting_participants SET joined_at = COALESCE(joined_at, now())
WHERE meeting_id = (SELECT id FROM meetings WHERE room_name = $1) AND user_id = $2;

-- name: MarkParticipantLeft :exec
UPDATE meeting_participants SET left_at = now()
WHERE meeting_id = (SELECT id FROM meetings WHERE room_name = $1) AND user_id = $2;

-- ===== Notificações in-app =====

-- name: CreateNotification :one
INSERT INTO notifications (tenant_id, user_id, kind, title, body, link, meeting_id, args)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: ListNotifications :many
SELECT * FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationRead :exec
UPDATE notifications SET read_at = now() WHERE id = $1 AND user_id = $2;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationsReadByKind :exec
UPDATE notifications SET read_at = now() WHERE user_id = $1 AND kind = $2 AND read_at IS NULL;

-- ===== Gravações de reunião (LiveKit Egress) =====

-- name: CreateRecording :one
INSERT INTO recordings (tenant_id, room_name, meeting_id, egress_id, blob_key, started_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetRecordingByEgress :one
SELECT * FROM recordings WHERE egress_id = $1;

-- name: SetRecordingReady :exec
UPDATE recordings SET status = 'ready', ended_at = now(), duration_seconds = $2, size_bytes = $3
WHERE egress_id = $1;

-- name: SetRecordingFailed :exec
UPDATE recordings SET status = 'failed', ended_at = now() WHERE egress_id = $1;

-- name: ListRecordingsForTenant :many
SELECT * FROM recordings WHERE tenant_id = $1 ORDER BY started_at DESC LIMIT $2;

-- name: GetRecordingInTenant :one
SELECT * FROM recordings WHERE id = $1 AND tenant_id = $2;

-- name: DeleteRecording :exec
DELETE FROM recordings WHERE id = $1 AND tenant_id = $2;

-- name: GetActiveRecordingForRoom :one
SELECT * FROM recordings WHERE room_name = $1 AND status = 'recording' ORDER BY started_at DESC LIMIT 1;

-- ===== Meet: lobby (sala de espera / aprovação) =====

-- name: CreateKnock :one
INSERT INTO meeting_lobby (meeting_id, tenant_id, room_name, name, identity)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: GetKnock :one
SELECT * FROM meeting_lobby WHERE id = $1;

-- name: ListPendingKnocks :many
SELECT * FROM meeting_lobby WHERE meeting_id = $1 AND status = 'pending' ORDER BY created_at;

-- name: SetKnockStatus :one
UPDATE meeting_lobby SET status = $2 WHERE id = $1 RETURNING *;

-- ===== Tags (catálogo compartilhado) =====

-- name: ListTags :many
SELECT * FROM tags WHERE tenant_id = $1 ORDER BY name;

-- name: UpsertTag :one
INSERT INTO tags (tenant_id, name, color) VALUES ($1, $2, $3)
ON CONFLICT (tenant_id, name) DO UPDATE SET color = EXCLUDED.color RETURNING *;

-- name: DeleteTag :exec
DELETE FROM tags WHERE id = $1 AND tenant_id = $2;

-- name: EventsOverlapping :many
SELECT * FROM events WHERE owner_user_id = $1 AND starts_at < $3 AND ends_at > $2 ORDER BY starts_at;

-- name: EventsOverlappingExcept :many
-- Como EventsOverlapping, mas ignora um evento (o que está sendo editado) para não conflitar consigo.
SELECT * FROM events WHERE owner_user_id = $1 AND starts_at < $3 AND ends_at > $2 AND id <> $4 ORDER BY starts_at;

-- name: SetEventMeetingRoom :exec
UPDATE events SET meeting_room = $3 WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $4;

-- ===== Chat (estilo Slack) =====

-- name: CreateChannel :one
INSERT INTO channels (tenant_id, kind, name, topic, dm_key, created_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetChannel :one
SELECT * FROM channels WHERE id = $1 AND tenant_id = $2;

-- name: GetGeneralChannel :one
SELECT * FROM channels WHERE tenant_id = $1 AND is_general;

-- name: CreateGeneralChannel :one
-- Canal por defeito do tenant. O índice parcial garante um só por tenant; numa corrida, o segundo
-- INSERT não devolve linha e o chamador volta a ler o que ficou.
INSERT INTO channels (tenant_id, kind, name, topic, dm_key, created_by, is_general)
VALUES ($1, 'public', $2, '', '', $3, true)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetDMByKey :one
SELECT * FROM channels WHERE tenant_id = $1 AND kind = 'dm' AND dm_key = $2;

-- name: ListChannelsForUser :many
SELECT c.id, c.tenant_id, c.kind, c.name, c.topic, c.dm_key, c.created_by, c.created_at,
       c.listed, c.add_policy, c.is_general, cm.role, cm.notif_pref, cm.last_read_at,
       (SELECT count(*) FROM messages m
          WHERE m.channel_id = c.id AND m.created_at > cm.last_read_at
            AND m.user_id <> $1 AND m.deleted_at IS NULL) AS unread
FROM channels c
JOIN channel_members cm ON cm.channel_id = c.id AND cm.user_id = $1
WHERE c.tenant_id = $2 AND c.archived_at IS NULL
ORDER BY c.kind, c.name;

-- name: ListPublicChannels :many
-- Só canais públicos E listados aparecem na descoberta. Não listados = fechados (só por convite).
SELECT c.* FROM channels c
WHERE c.tenant_id = $1 AND c.kind = 'public' AND c.listed = true AND c.archived_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = $2)
ORDER BY c.name;

-- name: SetChannelArchived :one
-- Arquiva (true) ou restaura (false). Arquivado sai das listas e fica só de leitura; o histórico
-- fica intacto, ao contrário do apagar.
UPDATE channels SET archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN now() ELSE NULL END
WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: ListArchivedChannels :many
-- Só lista a quem PODE restaurar: admin da organização ou dono do canal. Assim tudo o que aparece
-- é acionável, e para os restantes o canal arquivado desaparece (é o efeito que se quer).
SELECT c.* FROM channels c
WHERE c.tenant_id = @tenant_id AND c.archived_at IS NOT NULL AND c.kind <> 'dm'
  AND (
      sqlc.arg(is_admin)::boolean
      OR EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = @user_id AND cm.role = 'owner')
  )
ORDER BY c.archived_at DESC;

-- name: UpdateChannelAccess :exec
UPDATE channels SET listed = $3, add_policy = $4 WHERE id = $1 AND tenant_id = $2;

-- name: UpdateChannelSettings :exec
UPDATE channels SET name = $3, topic = $4, listed = $5, add_policy = $6 WHERE id = $1 AND tenant_id = $2;

-- name: UpdateMemberRole :exec
UPDATE channel_members SET role = $3 WHERE channel_id = $1 AND user_id = $2;

-- name: AddChannelMember :exec
INSERT INTO channel_members (channel_id, tenant_id, user_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (channel_id, user_id) DO NOTHING;

-- name: RemoveChannelMember :exec
DELETE FROM channel_members WHERE channel_id = $1 AND user_id = $2;

-- name: GetMembership :one
SELECT * FROM channel_members WHERE channel_id = $1 AND user_id = $2;

-- name: ListChannelMembers :many
SELECT cm.user_id, cm.role, cm.last_read_at, u.email, u.name, u.status_emoji, u.status_text
FROM channel_members cm JOIN mail_users u ON u.id = cm.user_id
WHERE cm.channel_id = $1 ORDER BY u.name;

-- name: GetChannelMeta :one
-- Metadados do canal p/ decidir a notificação de mensagem (DM sempre avisa; canal pelo nome).
SELECT kind, name FROM channels WHERE id = $1;

-- name: ListChannelMemberPrefs :many
-- Membros do canal + preferência de notificação, para o fan-out de "mensagem nova".
SELECT user_id, notif_pref FROM channel_members WHERE channel_id = $1;

-- name: ListMemberUserIDs :many
SELECT user_id FROM channel_members WHERE channel_id = $1;

-- name: UpdateLastRead :exec
UPDATE channel_members SET last_read_at = now() WHERE channel_id = $1 AND user_id = $2;

-- name: CreateMessage :one
INSERT INTO messages (tenant_id, channel_id, user_id, parent_id, body)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListMessagesByChannel :many
SELECT m.id, m.channel_id, m.user_id, m.parent_id, m.body, m.edited_at, m.deleted_at, m.created_at,
       u.email AS author_email, u.name AS author_name,
       (SELECT count(*) FROM messages r WHERE r.parent_id = m.id AND r.deleted_at IS NULL) AS reply_count, m.bot_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.parent_id IS NULL
ORDER BY m.created_at DESC LIMIT $2;

-- name: ListMessagesBeforeByChannel :many
-- Página de histórico ANTERIOR a um instante (cursor por created_at) — carrega mensagens antigas.
SELECT m.id, m.channel_id, m.user_id, m.parent_id, m.body, m.edited_at, m.deleted_at, m.created_at,
       u.email AS author_email, u.name AS author_name,
       (SELECT count(*) FROM messages r WHERE r.parent_id = m.id AND r.deleted_at IS NULL) AS reply_count, m.bot_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.parent_id IS NULL AND m.created_at < $2
ORDER BY m.created_at DESC LIMIT $3;

-- name: SaveMessage :exec
INSERT INTO saved_messages (user_id, message_id, tenant_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- name: UnsaveMessage :exec
DELETE FROM saved_messages WHERE user_id = $1 AND message_id = $2;

-- name: ListSavedIDsInChannel :many
-- Ids das mensagens salvas pelo usuário DENTRO de um canal (marca o estado na lista de mensagens).
SELECT sm.message_id FROM saved_messages sm JOIN messages m ON m.id = sm.message_id
WHERE sm.user_id = $1 AND m.channel_id = $2;

-- name: ListSavedMessages :many
-- Mensagens salvas do usuário (painel "Salvos"), com autor e canal, mais recentes primeiro.
SELECT m.id, m.channel_id, m.body, m.created_at, c.kind AS channel_kind, c.name AS channel_name,
       u.email AS author_email, u.name AS author_name, sm.created_at AS saved_at
FROM saved_messages sm
JOIN messages m ON m.id = sm.message_id
JOIN channels c ON c.id = m.channel_id
JOIN mail_users u ON u.id = m.user_id
WHERE sm.user_id = $1 AND m.deleted_at IS NULL
ORDER BY sm.created_at DESC LIMIT 50;

-- name: ListMessagesAfterByChannel :many
-- Mensagens POSTERIORES a um instante (lado novo da janela "saltar para a mensagem"), em ordem.
SELECT m.id, m.channel_id, m.user_id, m.parent_id, m.body, m.edited_at, m.deleted_at, m.created_at,
       u.email AS author_email, u.name AS author_name,
       (SELECT count(*) FROM messages r WHERE r.parent_id = m.id AND r.deleted_at IS NULL) AS reply_count, m.bot_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.parent_id IS NULL AND m.created_at > $2
ORDER BY m.created_at ASC LIMIT $3;

-- name: ListThreadReplies :many
SELECT m.id, m.channel_id, m.user_id, m.parent_id, m.body, m.edited_at, m.deleted_at, m.created_at,
       u.email AS author_email, u.name AS author_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.parent_id = $1
ORDER BY m.created_at ASC;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = $1 AND tenant_id = $2;

-- name: EditMessage :one
UPDATE messages SET body = $3, edited_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL RETURNING *;

-- name: DeleteMessage :exec
UPDATE messages SET deleted_at = now(), body = '' WHERE id = $1 AND user_id = $2;

-- name: DeleteMessageAny :exec
-- Apaga (soft) por id, sem exigir autoria — para moderação por gestor/owner do canal.
UPDATE messages SET deleted_at = now(), body = '' WHERE id = $1 AND tenant_id = $2;

-- name: DeleteChannel :exec
-- Exclui o canal (cascata remove membros/mensagens). Só o owner, verificado no handler.
DELETE FROM channels WHERE id = $1 AND tenant_id = $2;

-- name: SetLastReadAt :exec
-- Define o marcador de leitura num instante específico (usado por "marcar como não lido").
UPDATE channel_members SET last_read_at = $3 WHERE channel_id = $1 AND user_id = $2;

-- name: AddReaction :exec
INSERT INTO message_reactions (message_id, user_id, emoji)
VALUES ($1, $2, $3) ON CONFLICT (message_id, user_id, emoji) DO NOTHING;

-- name: RemoveReaction :exec
DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3;

-- name: ListReactionsByChannel :many
SELECT r.message_id, r.emoji, r.user_id
FROM message_reactions r JOIN messages m ON m.id = r.message_id
WHERE m.channel_id = $1;

-- name: AddMessageAttachment :exec
INSERT INTO message_attachments (message_id, file_id, name, size)
VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING;

-- name: ListAttachmentsByChannel :many
-- Anexos de mensagem apagada não voltam (a mensagem vira só o marcador "apagada").
SELECT a.message_id, a.file_id, a.name, a.size
FROM message_attachments a JOIN messages m ON m.id = a.message_id
WHERE m.channel_id = $1 AND m.deleted_at IS NULL;

-- name: GetChatAttachment :one
SELECT sf.blob_key, a.name
FROM message_attachments a
JOIN messages m ON m.id = a.message_id
JOIN channel_members cm ON cm.channel_id = m.channel_id AND cm.user_id = $2
JOIN storage_files sf ON sf.id = a.file_id
WHERE a.file_id = $1 AND m.deleted_at IS NULL
LIMIT 1;

-- name: SearchMessages :many
-- Busca de mensagens (FTS via body_tsv) com filtros opcionais estilo Slack: from:<autor>,
-- before:/after:<data>. Os flags ligam cada filtro; termo vazio = só filtros.
SELECT m.id, m.channel_id, c.kind AS channel_kind, c.name AS channel_name,
       m.user_id, m.body, m.created_at, u.name AS author_name, u.email AS author_email
FROM messages m
JOIN channel_members cm ON cm.channel_id = m.channel_id AND cm.user_id = sqlc.arg(user_id)
JOIN channels c ON c.id = m.channel_id
JOIN mail_users u ON u.id = m.user_id
WHERE m.tenant_id = sqlc.arg(tenant_id) AND m.deleted_at IS NULL
  AND (sqlc.arg(term)::text = '' OR m.body_tsv @@ plainto_tsquery('simple', sqlc.arg(term)))
  AND (NOT sqlc.arg(has_from)::boolean OR m.user_id = sqlc.arg(from_user))
  AND (NOT sqlc.arg(has_before)::boolean OR m.created_at < sqlc.arg(before_ts))
  AND (NOT sqlc.arg(has_after)::boolean OR m.created_at > sqlc.arg(after_ts))
ORDER BY m.created_at DESC LIMIT sqlc.arg(lim);

-- name: UpdateMeetingControls :one
-- Só o dono (organizador) altera; o filtro por owner_user_id é a checagem de permissão.
UPDATE meetings SET allow_mic = $3, allow_cam = $4, allow_share = $5, allow_chat = $6,
    lock_until_host = $7, locked = $8
WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: UpdateMeetingAccess :one
UPDATE meetings SET access = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: GetMeetingOwned :one
-- A reunião, mas só se quem pede for o dono — usado antes de expulsar alguém ou encerrar.
SELECT * FROM meetings WHERE id = $1 AND owner_user_id = $2;

-- name: GetMeetingNotes :one
SELECT * FROM meeting_notes WHERE meeting_id = $1 AND tenant_id = $2;

-- name: UpsertMeetingNotes :one
INSERT INTO meeting_notes (meeting_id, tenant_id, body, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (meeting_id) DO UPDATE
  SET body = EXCLUDED.body, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- name: UpdateBoard :one
UPDATE boards SET name = $3 WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: DeleteBoard :exec
DELETE FROM boards WHERE id = $1 AND tenant_id = $2;

-- name: CountBoards :one
SELECT count(*) FROM boards WHERE tenant_id = $1;

-- ===== Grupos de utilizadores =====

-- name: CreateUserGroup :one
INSERT INTO user_groups (tenant_id, name, created_by) VALUES ($1, $2, $3) RETURNING *;

-- name: SetUserGroupChannel :exec
UPDATE user_groups SET channel_id = $2 WHERE id = $1;

-- name: ListUserGroups :many
SELECT g.*, (SELECT count(*) FROM user_group_members m WHERE m.group_id = g.id) AS members
FROM user_groups g WHERE g.tenant_id = $1 ORDER BY g.name;

-- name: GetUserGroup :one
SELECT * FROM user_groups WHERE id = $1 AND tenant_id = $2;

-- name: UpdateUserGroup :one
UPDATE user_groups SET name = $3 WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: DeleteUserGroup :exec
DELETE FROM user_groups WHERE id = $1 AND tenant_id = $2;

-- name: AddUserToGroup :exec
INSERT INTO user_group_members (group_id, user_id, tenant_id) VALUES ($1, $2, $3)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveUserFromGroup :exec
DELETE FROM user_group_members WHERE group_id = $1 AND user_id = $2;

-- name: ListGroupMembers :many
SELECT u.id, u.email, u.name, u.role
FROM user_group_members m JOIN mail_users u ON u.id = m.user_id
WHERE m.group_id = $1 ORDER BY u.name, u.email;

-- name: ListGroupsForUser :many
SELECT g.id, g.name FROM user_group_members m JOIN user_groups g ON g.id = m.group_id
WHERE m.user_id = $1 ORDER BY g.name;

-- name: UpdateChannelName :one
UPDATE channels SET name = $3 WHERE id = $1 AND tenant_id = $2 RETURNING *;

-- name: ListContactGroupsOf :many
SELECT group_id FROM contact_group_members WHERE contact_id = $1;

-- name: ListColleagues :many
-- Colegas do mesmo tenant: só o mínimo para atribuir trabalho (id/nome/e-mail). NÃO expõe papel,
-- MFA nem nada do painel — por isso não precisa de members.view.
SELECT id, email, name, avatar FROM mail_users WHERE tenant_id = $1 ORDER BY name, email;

-- name: GetContactGroupOwned :one
-- Grupo de contatos do próprio dono — usado antes de mexer na partilha dele.
SELECT * FROM contact_groups WHERE id = $1 AND owner_user_id = $2;

-- ---- Comentários da tarefa ----

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

-- name: ListCommentAuthors :many
-- Quem já comentou nesta tarefa — são os interessados na conversa.
SELECT DISTINCT author_id FROM card_comments WHERE card_id = $1 AND tenant_id = $2;

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
SELECT ca.id, ca.file_id, f.nome, f.tamanho, ca.created_at
FROM card_attachments ca JOIN storage_files f ON f.id = ca.file_id
WHERE ca.card_id = $1 AND ca.tenant_id = $2 ORDER BY ca.created_at;

-- name: DetachFile :exec
DELETE FROM card_attachments WHERE id = $1 AND tenant_id = $2;

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

-- name: ListGifs :many
SELECT g.id, g.file_id, g.created_at,
       COALESCE(NULLIF(g.name, ''), f.nome) AS name, f.tamanho AS size
FROM gif_library g JOIN storage_files f ON f.id = g.file_id
WHERE g.owner_user_id = $1 ORDER BY g.created_at DESC LIMIT 60;

-- name: AddGif :one
-- ON CONFLICT: guardar duas vezes o mesmo GIF não é erro nem duplicado — é a mesma gaveta.
INSERT INTO gif_library (tenant_id, owner_user_id, file_id, name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (owner_user_id, file_id) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: DeleteGif :exec
DELETE FROM gif_library WHERE id = $1 AND owner_user_id = $2;

-- name: GetUserSettings :one
SELECT settings FROM mail_users WHERE id = $1;

-- name: UpdateUserSettings :exec
-- Merge no nível de topo (||): gravar {"email":{…}} não apaga {"chat":{…}}. Cada app grava a sua
-- secção sem pisar as outras.
UPDATE mail_users SET settings = settings || $2 WHERE id = $1;

-- name: UpsertPushSubscription :exec
-- ON CONFLICT: reactivar o push no mesmo browser reaproveita a linha (as chaves podem rodar).
INSERT INTO push_subscriptions (tenant_id, user_id, endpoint, p256dh, auth)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (endpoint) DO UPDATE SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth;

-- name: ListPushSubscriptionsByUser :many
SELECT * FROM push_subscriptions WHERE user_id = $1;

-- name: DeletePushSubscriptionByEndpoint :exec
DELETE FROM push_subscriptions WHERE endpoint = $1;

-- name: GetFileLinkByFile :one
-- Reaproveita só um link ainda válido (não expirado); expirado conta como inexistente.
SELECT * FROM file_links WHERE file_id = $1 AND (expires_at IS NULL OR expires_at > now());

-- name: CreateFileLink :one
INSERT INTO file_links (tenant_id, file_id, token, created_by, expires_at, password_hash, permission, allow_download)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateFileLink :one
-- Reconfigura um link existente (validade, senha, permissão, descarga) sem trocar o token. Só do dono.
UPDATE file_links SET expires_at = $2, password_hash = $3, permission = $5, allow_download = $6
WHERE file_id = $1 AND tenant_id = $4 RETURNING *;

-- name: DeleteFileLinkByFile :exec
-- Revoga o link público de um ficheiro (do dono).
DELETE FROM file_links WHERE file_id = $1 AND tenant_id = $2;

-- name: GetFileByLinkToken :one
-- Resolve o token público → ficheiro (+ hash da senha do link). Sem filtro de tenant/dono: o token
-- É a autorização. Mas o link precisa estar dentro da validade (expires_at futuro ou NULL).
SELECT f.*, l.password_hash, l.permission, l.allow_download FROM file_links l JOIN storage_files f ON f.id = l.file_id
WHERE l.token = $1 AND (l.expires_at IS NULL OR l.expires_at > now());

-- name: ListFilesSharedWithMe :many
-- Ficheiros que outra pessoa do tenant partilhou comigo (principal = o meu e-mail). DISTINCT porque
-- a mesma pessoa pode ter partilhado o ficheiro mais de uma vez.
SELECT DISTINCT f.* FROM storage_shares sh
JOIN storage_files f ON f.id = sh.file_id
WHERE sh.tenant_id = $1 AND sh.principal = $2
ORDER BY f.created_at DESC;

-- name: GetFileIfSharedWithMe :one
-- O ficheiro, se me foi partilhado (principal = o meu e-mail, mesmo tenant). Autoriza o download
-- de quem não é dono mas recebeu a partilha.
SELECT f.* FROM storage_shares sh JOIN storage_files f ON f.id = sh.file_id
WHERE f.id = $1 AND sh.tenant_id = $2 AND sh.principal = $3;

-- name: UpdateTenantBranding :exec
UPDATE tenants SET brand_color = $2, brand_logo_white_dark = @brand_logo_white_dark, brand_logo = $3 WHERE id = $1;

-- name: GetTenantDisabledApps :one
SELECT disabled_apps FROM tenants WHERE id = $1;

-- name: SetTenantDisabledApps :exec
UPDATE tenants SET disabled_apps = $2 WHERE id = $1;

-- name: GetTenantBrandingByEmail :one
-- Marca da organização a partir do e-mail de um utilizador (para o ecrã de login, pré-auth).
SELECT t.nome, t.brand_color, t.brand_logo
FROM mail_users u JOIN tenants t ON t.id = u.tenant_id
WHERE u.email = $1;

-- name: ListAnnouncements :many
-- Avisos do tenant: busca (título/corpo), paginação e visibilidade. $5 = ver também os não
-- publicados (só admin); membros veem apenas os publicados.
SELECT * FROM announcements
WHERE tenant_id = $1
  AND ($2 = '' OR title ILIKE '%' || $2 || '%' OR body ILIKE '%' || $2 || '%')
  AND (published OR $5)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountAnnouncements :one
SELECT count(*) FROM announcements
WHERE tenant_id = $1
  AND ($2 = '' OR title ILIKE '%' || $2 || '%' OR body ILIKE '%' || $2 || '%')
  AND (published OR $3);

-- name: CreateAnnouncement :one
INSERT INTO announcements (tenant_id, author_id, author_name, title, body, published)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: SetAnnouncementPublished :exec
UPDATE announcements SET published = $3 WHERE id = $1 AND tenant_id = $2;

-- name: DeleteAnnouncement :exec
DELETE FROM announcements WHERE id = $1 AND tenant_id = $2;

-- name: SearchChannelMembers :many
-- Membros de um canal, com busca opcional por nome/e-mail e paginação (para canais grandes).
SELECT cm.user_id, cm.role, cm.last_read_at, u.email, u.name, u.status_emoji, u.status_text
FROM channel_members cm JOIN mail_users u ON u.id = cm.user_id
WHERE cm.channel_id = $1
  AND ($2 = '' OR u.name ILIKE '%' || $2 || '%' OR u.email ILIKE '%' || $2 || '%')
ORDER BY u.name
LIMIT $3 OFFSET $4;

-- name: CountChannelMembers :one
SELECT count(*) FROM channel_members cm JOIN mail_users u ON u.id = cm.user_id
WHERE cm.channel_id = $1
  AND ($2 = '' OR u.name ILIKE '%' || $2 || '%' OR u.email ILIKE '%' || $2 || '%');

-- name: UpdateChannelCanvas :exec
UPDATE channels SET canvas = $3, canvas_updated_at = now() WHERE id = $1 AND tenant_id = $2;

-- name: SetMessagePinned :exec
UPDATE messages SET pinned_at = CASE WHEN $2::boolean THEN now() ELSE NULL END WHERE id = $1;

-- name: ListPinnedByChannel :many
SELECT m.id, m.body, m.created_at, m.pinned_at, u.email AS author_email, u.name AS author_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.pinned_at IS NOT NULL AND m.deleted_at IS NULL
ORDER BY m.pinned_at DESC;

-- name: ListChannelBookmarks :many
SELECT * FROM channel_bookmarks WHERE channel_id = $1 ORDER BY created_at DESC;

-- name: CreateChannelBookmark :one
INSERT INTO channel_bookmarks (channel_id, tenant_id, kind, title, url, file_id, file_name, file_size, added_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING *;

-- name: GetChannelBookmark :one
SELECT b.*, sf.blob_key FROM channel_bookmarks b
LEFT JOIN storage_files sf ON sf.id = b.file_id
WHERE b.id = $1 AND b.channel_id = $2;

-- name: DeleteChannelBookmark :exec
DELETE FROM channel_bookmarks WHERE id = $1 AND channel_id = $2;

-- name: SetChannelNotifPref :exec
UPDATE channel_members SET notif_pref = $3 WHERE channel_id = $1 AND user_id = $2;

-- name: SearchMessagesInChannel :many
SELECT m.id, m.body, m.created_at, u.email AS author_email, u.name AS author_name
FROM messages m JOIN mail_users u ON u.id = m.user_id
WHERE m.channel_id = $1 AND m.deleted_at IS NULL AND m.body_tsv @@ plainto_tsquery('simple', $2)
ORDER BY m.created_at DESC LIMIT 40;

-- name: SetUserStatus :exec
UPDATE mail_users SET status_emoji = $2, status_text = $3 WHERE id = $1;

-- name: GetUserStatusByEmail :one
SELECT status_emoji, status_text FROM mail_users WHERE email = $1 AND tenant_id = $2;

-- name: CreateSnooze :one
INSERT INTO snoozed_emails (tenant_id, mail_user_id, email_id, wake_at) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: DueSnoozes :many
-- Snoozes vencidos (wake_at passou) ainda não processados, com a credencial JMAP cifrada do dono
-- para o worker mover a mensagem de volta à Caixa de entrada.
SELECT s.id, s.email_id, u.jmap_cred FROM snoozed_emails s
JOIN mail_users u ON u.id = s.mail_user_id
WHERE NOT s.done AND s.wake_at <= now() ORDER BY s.wake_at LIMIT 200;

-- name: MarkSnoozeDone :exec
UPDATE snoozed_emails SET done = true WHERE id = $1;

-- name: UsersForMailPush :many
-- Utilizadores elegíveis para push de e-mail novo: têm credencial JMAP (brokering), pelo menos uma
-- subscrição de push, e não estão em Não Perturbe. mail_seen_at é a base da última verificação.
SELECT DISTINCT u.id, u.tenant_id, u.jmap_cred, u.mail_seen_at
FROM mail_users u JOIN push_subscriptions p ON p.user_id = u.id
WHERE u.jmap_cred <> '' AND u.presence <> 'dnd';

-- name: UpdateMailSeen :exec
UPDATE mail_users SET mail_seen_at = $2 WHERE id = $1;

-- name: AddEventInvite :exec
INSERT INTO event_invites (event_id, tenant_id, user_id) VALUES ($1, $2, $3) ON CONFLICT (event_id, user_id) DO NOTHING;

-- name: ClearEventInvites :exec
DELETE FROM event_invites WHERE event_id = $1;

-- name: ListEventInviteesForEvents :many
-- Convidados de VÁRIOS eventos numa ida só (avatares nos cards do calendário — 23/07/2026).
SELECT i.event_id, i.status, u.name, u.email, u.avatar
FROM event_invites i JOIN mail_users u ON u.id = i.user_id
WHERE i.event_id = ANY(@event_ids::uuid[]) ORDER BY u.name;

-- name: ListEventInvites :many
SELECT i.user_id, i.status, u.name, u.email FROM event_invites i JOIN mail_users u ON u.id = i.user_id WHERE i.event_id = $1 ORDER BY u.name;

-- name: SetInviteStatus :exec
UPDATE event_invites SET status = $3 WHERE event_id = $1 AND user_id = $2;

-- name: GetEventInvite :one
SELECT * FROM event_invites WHERE event_id = $1 AND user_id = $2;

-- name: ListInvitedEvents :many
-- Eventos a que o utilizador foi convidado (com o seu status), para entrarem na agenda dele.
SELECT e.*, i.status AS my_rsvp FROM event_invites i JOIN events e ON e.id = i.event_id WHERE i.user_id = $1 AND e.deleted_at IS NULL;

-- name: GetEventInTenant :one
SELECT * FROM events WHERE id = $1 AND tenant_id = $2;

-- name: SetEventTimes :exec
-- Sincroniza o evento de calendário ligado à reunião quando ela é reagendada (re-arma o lembrete).
UPDATE events SET starts_at = $2, ends_at = $3, reminded = false WHERE id = $1;

-- ===== Emojis customizados do workspace =====
-- name: ListWorkspaceEmojis :many
SELECT e.id, e.name, e.file_id, e.created_by FROM workspace_emojis e WHERE e.tenant_id = $1 ORDER BY e.name;

-- name: CreateWorkspaceEmoji :one
INSERT INTO workspace_emojis (tenant_id, name, file_id, created_by) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: DeleteWorkspaceEmoji :exec
DELETE FROM workspace_emojis WHERE id = $1 AND tenant_id = $2;

-- ===== Mensagens agendadas + lembretes de mensagem =====
-- name: CreateScheduledMessage :one
INSERT INTO scheduled_messages (tenant_id, channel_id, user_id, body, send_at)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListScheduledMessages :many
SELECT sm.*, c.name AS channel_name FROM scheduled_messages sm JOIN channels c ON c.id = sm.channel_id
WHERE sm.user_id = $1 ORDER BY sm.send_at;

-- name: DeleteScheduledMessage :exec
DELETE FROM scheduled_messages WHERE id = $1 AND user_id = $2;

-- name: DueScheduledMessages :many
SELECT sm.*, COALESCE(NULLIF(u.name, ''), u.email) AS author_name, u.email AS author_email
FROM scheduled_messages sm JOIN mail_users u ON u.id = sm.user_id
WHERE sm.send_at <= now() ORDER BY sm.send_at LIMIT 100;

-- name: PurgeScheduledMessage :exec
DELETE FROM scheduled_messages WHERE id = $1;

-- name: CreateMessageReminder :one
INSERT INTO message_reminders (tenant_id, user_id, channel_id, message_id, note, remind_at)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: DueMessageReminders :many
SELECT mr.*, c.name AS channel_name FROM message_reminders mr JOIN channels c ON c.id = mr.channel_id
WHERE mr.remind_at <= now() ORDER BY mr.remind_at LIMIT 100;

-- name: PurgeMessageReminder :exec
DELETE FROM message_reminders WHERE id = $1;

-- ===== Múltiplos calendários + free-busy =====
-- name: ListCalendars :many
SELECT * FROM calendars WHERE owner_user_id = $1 ORDER BY created_at;

-- name: CreateCalendar :one
INSERT INTO calendars (tenant_id, owner_user_id, name, color) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdateCalendar :one
UPDATE calendars SET name = $3, color = $4 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- name: DeleteCalendar :exec
DELETE FROM calendars WHERE id = $1 AND owner_user_id = $2;

-- name: FreeBusy :many
-- Blocos ocupados de um colega (só início/fim, sem detalhes) num intervalo. O handler valida que o
-- alvo é do mesmo tenant. Ignora eventos na lixeira.
SELECT starts_at, ends_at FROM events
WHERE owner_user_id = $1 AND tenant_id = $2 AND deleted_at IS NULL AND all_day = false
  AND starts_at < $4 AND ends_at > $3 ORDER BY starts_at;

-- name: GetCalendar :one
SELECT * FROM calendars WHERE id = $1 AND owner_user_id = $2;

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

-- ===== CRM: edição de nota + transferência de dono =====
-- name: EditCrmNote :one
-- Só o autor edita a própria nota.
UPDATE crm_notes SET body = $3 WHERE id = $1 AND author_id = $2 RETURNING *;

-- name: TransferContactOwner :one
-- Transfere a posse do contato: só o dono atual pode, e para um colega do mesmo tenant (validado no handler).
UPDATE contacts SET owner_user_id = $3 WHERE id = $1 AND owner_user_id = $2 RETURNING *;

-- ===== Automações do quadro =====
-- name: ListAutomations :many
SELECT * FROM board_automations WHERE board_id = $1 ORDER BY created_at;

-- name: CreateAutomation :one
INSERT INTO board_automations (board_id, tenant_id, trigger_column_id, action, action_value)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: DeleteAutomation :exec
DELETE FROM board_automations WHERE id = $1 AND tenant_id = $2;

-- name: AutomationsForColumn :many
-- Regras cujo gatilho é entrar nesta coluna (aplicadas quando um card se move para cá).
SELECT * FROM board_automations WHERE trigger_column_id = $1;

-- name: SetCardAssigneeOnly :exec
UPDATE cards SET assignee_id = $3 WHERE id = $1 AND tenant_id = $2;

-- name: SetCardLabelOnly :exec
UPDATE cards SET label = $3 WHERE id = $1 AND tenant_id = $2;

-- ===== Webhooks de entrada (bots) =====
-- name: CreateBotMessage :one
INSERT INTO messages (tenant_id, channel_id, user_id, body, bot_name)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: CreateWebhook :one
INSERT INTO chat_webhooks (tenant_id, channel_id, token, name, created_by)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListWebhooks :many
SELECT * FROM chat_webhooks WHERE channel_id = $1 ORDER BY created_at;

-- name: DeleteWebhook :exec
DELETE FROM chat_webhooks WHERE id = $1 AND tenant_id = $2;

-- name: GetWebhookByToken :one
SELECT * FROM chat_webhooks WHERE token = $1;

-- ===== Outbox de envio server-side (undo-send + agendados) =====

-- name: CreateOutbox :one
INSERT INTO outbox_emails (tenant_id, user_id, email_id, identity_id, from_email, rcpts, subject, send_at)
VALUES (@tenant_id, @user_id, @email_id, @identity_id, @from_email, @rcpts, @subject, @send_at)
RETURNING *;

-- name: ListPendingOutbox :many
-- E-mails ainda por enviar (agendados) do dono — para a UI de gestão. Ordena pelo horário de envio.
SELECT id, email_id, from_email, rcpts, subject, send_at, status, created_at
FROM outbox_emails
WHERE user_id = @user_id AND tenant_id = @tenant_id AND status = 'pending'
ORDER BY send_at;

-- name: CancelOutbox :execrows
-- Cancela um envio pendente do dono (undo ou "cancelar agendado"). Só afeta pendentes (idempotente).
UPDATE outbox_emails SET status = 'canceled'
WHERE id = @id AND user_id = @user_id AND tenant_id = @tenant_id AND status = 'pending';

-- name: DueOutbox :many
-- Envios vencidos (send_at passou) ainda pendentes, com a credencial JMAP cifrada do dono para o
-- worker submeter (EmailSubmission/set). Limite protege o tick de uma fila gigante.
SELECT o.id, o.tenant_id, o.email_id, o.identity_id, o.from_email, o.rcpts, u.jmap_cred
FROM outbox_emails o
JOIN mail_users u ON u.id = o.user_id
WHERE o.status = 'pending' AND o.send_at <= now()
ORDER BY o.send_at LIMIT 200;

-- name: MarkOutboxSent :exec
UPDATE outbox_emails SET status = 'sent' WHERE id = $1;

-- name: MarkOutboxFailed :exec
UPDATE outbox_emails SET status = 'failed' WHERE id = $1;

-- ===== Templates de resposta (canned responses) =====

-- name: ListTemplates :many
SELECT * FROM mail_templates
WHERE owner_user_id = @owner_user_id AND tenant_id = @tenant_id
ORDER BY name;

-- name: CreateTemplate :one
INSERT INTO mail_templates (tenant_id, owner_user_id, name, subject, body_html)
VALUES (@tenant_id, @owner_user_id, @name, @subject, @body_html)
RETURNING *;

-- name: UpdateTemplate :one
UPDATE mail_templates SET name = @name, subject = @subject, body_html = @body_html, updated_at = now()
WHERE id = @id AND owner_user_id = @owner_user_id AND tenant_id = @tenant_id
RETURNING *;

-- name: DeleteTemplate :execrows
DELETE FROM mail_templates WHERE id = @id AND owner_user_id = @owner_user_id AND tenant_id = @tenant_id;

-- ===== Co-hosts de reunião =====

-- name: AddCohost :exec
INSERT INTO meeting_cohosts (meeting_id, user_id) VALUES (@meeting_id, @user_id)
ON CONFLICT DO NOTHING;

-- name: RemoveCohost :exec
DELETE FROM meeting_cohosts WHERE meeting_id = @meeting_id AND user_id = @user_id;

-- name: IsCohost :one
SELECT EXISTS(SELECT 1 FROM meeting_cohosts WHERE meeting_id = @meeting_id AND user_id = @user_id);

-- name: ListCohostIDs :many
SELECT user_id FROM meeting_cohosts WHERE meeting_id = @meeting_id;

-- ===== Breakout rooms =====

-- name: CreateBreakout :one
INSERT INTO meeting_breakouts (meeting_id, tenant_id, idx, name, room_name)
VALUES (@meeting_id, @tenant_id, @idx, @name, @room_name)
RETURNING *;

-- name: ListBreakouts :many
SELECT * FROM meeting_breakouts WHERE meeting_id = @meeting_id ORDER BY idx;

-- name: ListBreakoutAssignments :many
SELECT user_id, breakout_id FROM breakout_assignments WHERE meeting_id = @meeting_id;

-- name: GetBreakout :one
SELECT * FROM meeting_breakouts WHERE id = @id AND meeting_id = @meeting_id;

-- name: AssignBreakout :exec
INSERT INTO breakout_assignments (meeting_id, user_id, breakout_id)
VALUES (@meeting_id, @user_id, @breakout_id)
ON CONFLICT (meeting_id, user_id) DO UPDATE SET breakout_id = EXCLUDED.breakout_id;

-- name: GetMyBreakout :one
SELECT b.* FROM breakout_assignments a
JOIN meeting_breakouts b ON b.id = a.breakout_id
WHERE a.meeting_id = @meeting_id AND a.user_id = @user_id;

-- name: DeleteBreakouts :exec
DELETE FROM meeting_breakouts WHERE meeting_id = @meeting_id;

-- ===== Assistente de IA =====

-- name: RecordAssistantToolCall :exec
-- Auditoria de uma chamada de ferramenta (§6.4). Escrita em todos os casos: sucesso e erro.
-- conversation_id é anulável: a A0 registava execuções sem conversa nenhuma.
INSERT INTO assistant_tool_calls
    (tenant_id, mail_user_id, run_id, conversation_id, agent, tool, args, result, error, duration_ms)
VALUES (@tenant_id, @mail_user_id, @run_id, @conversation_id, @agent, @tool, @args, @result, @error, @duration_ms);

-- name: ListAssistantToolCallsByRun :many
-- Cartões de ação de uma execução, por ordem cronológica. Filtra por tenant (lei 5.1).
SELECT * FROM assistant_tool_calls
WHERE run_id = @run_id AND tenant_id = @tenant_id
ORDER BY created_at;

-- name: CreateAssistantConversation :one
-- Abre uma conversa. O par (tenant, utilizador) vem da sessão, nunca do pedido (lei 5.1).
INSERT INTO assistant_conversations (tenant_id, mail_user_id, agent, title)
VALUES (@tenant_id, @mail_user_id, @agent, @title)
RETURNING *;

-- name: GetAssistantConversation :one
-- Uma conversa DO utilizador autenticado. O filtro duplo (tenant + utilizador) é deliberado: a
-- conversa é pessoal e não é partilhada nem dentro da mesma organização (§3.5).
SELECT * FROM assistant_conversations
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: ListAssistantConversations :many
-- Histórico do próprio utilizador, mais recente primeiro.
SELECT * FROM assistant_conversations
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id
ORDER BY updated_at DESC
LIMIT @lim;

-- name: TouchAssistantConversation :exec
-- Sobe a conversa na lista quando lhe chega mensagem nova.
UPDATE assistant_conversations SET updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: DeleteAssistantConversation :exec
-- Apagar leva as mensagens atrás (ON DELETE CASCADE).
DELETE FROM assistant_conversations
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: AppendAssistantMessage :one
-- Acrescenta uma mensagem ao fio. `steps` são os cartões de ação da resposta (§3.3); `agent` é quem
-- respondeu — a escolha do encaminhamento é sempre visível (§3.2).
INSERT INTO assistant_messages (tenant_id, conversation_id, role, content, steps, stopped, run_id, agent)
VALUES (@tenant_id, @conversation_id, @role, @content, @steps, @stopped, @run_id, @agent)
RETURNING *;

-- name: PinAssistantConversationAgent :exec
-- Fixa (ou desfixa, com vazio) o agente para o resto do fio. Só o dono da conversa o faz.
UPDATE assistant_conversations SET pinned_agent = @pinned_agent
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: ListAssistantMessages :many
-- Mensagens de uma conversa, por ordem. O JOIN garante que a conversa é mesmo do utilizador — sem
-- ele, um conversation_id adivinhado leria o fio de outra pessoa.
SELECT m.* FROM assistant_messages m
JOIN assistant_conversations c ON c.id = m.conversation_id
WHERE m.conversation_id = @conversation_id
  AND c.tenant_id = @tenant_id AND c.mail_user_id = @mail_user_id
ORDER BY m.created_at
LIMIT @lim;

-- name: ListClientAgents :many
-- Perfis criados pelo cliente, por ordem de nome. Filtra por tenant (lei 5.1).
SELECT * FROM assistant_agents WHERE tenant_id = @tenant_id ORDER BY name;

-- name: GetClientAgent :one
SELECT * FROM assistant_agents WHERE id = @id AND tenant_id = @tenant_id;

-- name: CreateClientAgent :one
INSERT INTO assistant_agents (tenant_id, name, purpose, instructions, tools)
VALUES (@tenant_id, @name, @purpose, @instructions, @tools)
RETURNING *;

-- name: UpdateClientAgent :one
UPDATE assistant_agents
SET name = @name, purpose = @purpose, instructions = @instructions, tools = @tools, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id
RETURNING *;

-- name: DeleteClientAgent :exec
DELETE FROM assistant_agents WHERE id = @id AND tenant_id = @tenant_id;

-- name: ListDisabledBuiltinAgents :many
-- Só o que diverge do padrão: ausência = ligado.
SELECT agent_key FROM assistant_builtin_settings
WHERE tenant_id = @tenant_id AND enabled = FALSE;

-- name: SetBuiltinAgentEnabled :exec
-- Liga ou desliga um agente de origem para a organização. Controla a DISPONIBILIDADE, nunca o
-- comportamento: as instruções e as ferramentas continuam a ser nossas (§3.2).
INSERT INTO assistant_builtin_settings (tenant_id, agent_key, enabled)
VALUES (@tenant_id, @agent_key, @enabled)
ON CONFLICT (tenant_id, agent_key) DO UPDATE SET enabled = EXCLUDED.enabled;

-- name: CreateAssistantTask :one
-- Enfileira uma tarefa em fundo. O par (tenant, utilizador) vem da sessão (lei 5.1).
INSERT INTO assistant_tasks (tenant_id, mail_user_id, conversation_id, agent, message)
VALUES (@tenant_id, @mail_user_id, @conversation_id, @agent, @message)
RETURNING *;

-- name: ListAssistantTasks :many
-- As tarefas do próprio utilizador, mais recentes primeiro.
SELECT * FROM assistant_tasks
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id
ORDER BY created_at DESC
LIMIT @lim;

-- name: GetAssistantTask :one
SELECT * FROM assistant_tasks
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: RequestAssistantTaskCancel :exec
-- Cancelar é um PEDIDO: o worker vê a bandeira no passo seguinte. O que já foi feito fica.
UPDATE assistant_tasks SET cancel_requested = TRUE, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id
  AND status IN ('queued', 'running', 'awaiting_confirmation');

-- name: ClaimAssistantTask :one
-- Tira UMA tarefa da fila para este worker. SKIP LOCKED deixa vários workers correr sem se
-- pisarem; o arrendamento (claimed_at) é o que permite recuperar de um worker que morra.
--
-- Uma tarefa 'running' com arrendamento mais velho do que o limite volta a ser elegível: é
-- exatamente o caso do reinício a meio (§7.5).
UPDATE assistant_tasks SET status = 'running', claimed_at = now(), updated_at = now()
WHERE id = (
    SELECT id FROM assistant_tasks
    WHERE status = 'queued'
       OR (status IN ('running', 'awaiting_confirmation') AND claimed_at < now() - @lease::interval)
    ORDER BY created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: HeartbeatAssistantTask :one
-- Renova o arrendamento e devolve a tarefa, para o worker ver o pedido de cancelamento sem uma
-- segunda consulta.
UPDATE assistant_tasks SET claimed_at = now(), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SaveAssistantTaskProgress :exec
-- Grava o progresso a cada passo: é isto que permite retomar e é isto que a bolha mostra.
UPDATE assistant_tasks SET steps = @steps, status = @status, updated_at = now()
WHERE id = @id;

-- name: FinishAssistantTask :exec
UPDATE assistant_tasks
SET status = @status, answer = @answer, error_code = @error_code, stopped = @stopped,
    steps = @steps, run_id = @run_id, updated_at = now()
WHERE id = @id;

-- name: CreateAssistantPendingAction :one
-- Regista uma ação à espera de aprovação. Durável: sobrevive a um reinício do servidor (§6.3).
INSERT INTO assistant_pending_actions (task_id, tenant_id, mail_user_id, tool, args)
VALUES (@task_id, @tenant_id, @mail_user_id, @tool, @args)
RETURNING *;

-- name: GetAssistantPendingAction :one
SELECT * FROM assistant_pending_actions WHERE id = @id;

-- name: ListAssistantPendingActions :many
-- O que está à espera da decisão DESTE utilizador.
SELECT * FROM assistant_pending_actions
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id AND decision = ''
ORDER BY created_at;

-- name: DecideAssistantPendingAction :one
-- Decide UMA ação. A condição `decision = ''` torna-a de uso único: aprovar duas vezes não envia
-- duas vezes. Filtra por utilizador — a aprovação é de quem tem a ação (lei 5.1).
UPDATE assistant_pending_actions SET decision = @decision, decided_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id AND decision = ''
RETURNING *;

-- name: GetAssistantTaskEffect :one
-- Um efeito já produzido por esta tarefa. É a guarda de idempotência da retoma (lei 5.3).
SELECT summary FROM assistant_task_effects
WHERE task_id = @task_id AND fingerprint = @fingerprint;

-- name: RecordAssistantTaskEffect :exec
-- Marca o efeito como feito. ON CONFLICT DO NOTHING porque uma corrida entre dois workers não pode
-- transformar-se em erro: o que importa é que fique marcado uma vez.
INSERT INTO assistant_task_effects (task_id, fingerprint, summary)
VALUES (@task_id, @fingerprint, @summary)
ON CONFLICT (task_id, fingerprint) DO NOTHING;

-- name: UpdateStorageFileContentIfVersion :one
-- Grava conteúdo novo SÓ SE a versão corrente for a esperada (§5.1: editar em cima da versão que se
-- leu). Se outra pessoa gravou entretanto, a versão já não bate e nenhuma linha é atualizada — a
-- escrita é recusada em vez de perder trabalho alheio em silêncio.
UPDATE storage_files SET blob_key = @blob_key, hash = @hash, tamanho = @tamanho, versao = versao + 1
WHERE id = @id AND tenant_id = @tenant_id AND owner_user_id = @owner_user_id AND versao = @expected_version
RETURNING *;

-- name: ListAssistantMemory :many
-- Todos os factos do próprio utilizador. Pequena por natureza, cabe inteira (§8.3).
SELECT * FROM assistant_memory
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id
ORDER BY updated_at DESC;

-- name: CreateAssistantMemory :one
-- Guarda um facto. A origem é sempre uma mensagem do utilizador (§6.5).
INSERT INTO assistant_memory (tenant_id, mail_user_id, fact, category, source_message_id)
VALUES (@tenant_id, @mail_user_id, @fact, @category, @source_message_id)
RETURNING *;

-- name: UpdateAssistantMemory :one
-- Edita um facto. Factos que se contradizem substituem o anterior (§8.2) — o cliente da UI faz isso
-- com este update; a origem passa a ser a nova mensagem.
UPDATE assistant_memory
SET fact = @fact, category = @category, source_message_id = @source_message_id, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id
RETURNING *;

-- name: DeleteAssistantMemory :exec
DELETE FROM assistant_memory
WHERE id = @id AND tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: DeleteAllAssistantMemory :exec
-- Apagar tudo de uma vez (§8.4). Direito do utilizador (RGPD).
DELETE FROM assistant_memory
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id;

-- name: TouchAssistantMemoryUsed :exec
-- Marca factos como usados quando entram numa execução — alimenta a proposta de remoção do que
-- nunca é usado (§8.4).
UPDATE assistant_memory SET last_used_at = now()
WHERE mail_user_id = @mail_user_id AND id = ANY(@ids::uuid[]);

-- name: RecordAssistantUsage :exec
-- Regista os tokens de um turno para a quota (§9.2).
INSERT INTO assistant_usage (tenant_id, mail_user_id, tokens)
VALUES (@tenant_id, @mail_user_id, @tokens);

-- name: SumAssistantUsageWindow :one
-- Soma os tokens do utilizador na janela deslizante. @since é o início da janela (agora menos 6h).
SELECT COALESCE(SUM(tokens), 0)::bigint AS total,
       MIN(created_at) AS oldest
FROM assistant_usage
WHERE mail_user_id = @mail_user_id AND created_at >= @since;

-- name: DeleteOldAssistantUsage :exec
-- Limpa o que já saiu da janela — a tabela não precisa de crescer para sempre.
DELETE FROM assistant_usage WHERE created_at < @before;

-- name: ListExcludedFolders :many
SELECT folder_id FROM assistant_excluded_folders WHERE tenant_id = @tenant_id;

-- name: AddExcludedFolder :exec
INSERT INTO assistant_excluded_folders (tenant_id, folder_id)
VALUES (@tenant_id, @folder_id)
ON CONFLICT (tenant_id, folder_id) DO NOTHING;

-- name: RemoveExcludedFolder :exec
DELETE FROM assistant_excluded_folders WHERE tenant_id = @tenant_id AND folder_id = @folder_id;

-- name: ListAssistantToolCallsByUser :many
-- Registo de auditoria do PRÓPRIO utilizador, mais recente primeiro (§6.4). Legível por ele, não só
-- pelo administrador — daí filtrar por mail_user_id e não só por tenant.
SELECT * FROM assistant_tool_calls
WHERE tenant_id = @tenant_id AND mail_user_id = @mail_user_id
ORDER BY created_at DESC
LIMIT @lim;

-- name: AddEventExternal :exec
INSERT INTO event_externals (event_id, tenant_id, email) VALUES ($1, $2, $3)
ON CONFLICT (event_id, email) DO NOTHING;

-- name: DeleteEventExternals :exec
DELETE FROM event_externals WHERE event_id = $1;

-- name: ListEventExternals :many
SELECT email FROM event_externals WHERE event_id = $1 ORDER BY email;

-- name: ListEventExternalsForEvents :many
SELECT event_id, email FROM event_externals WHERE event_id = ANY(@event_ids::uuid[]) ORDER BY email;

-- ===== Booking (agenda pública estilo Calendly) =====
-- name: UpsertBookingPage :one
INSERT INTO booking_pages (tenant_id, user_id, org_slug, user_slug, enabled, title, duration_min, phone_field, company_field, windows)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (user_id) DO UPDATE SET enabled = $5, title = $6, duration_min = $7, phone_field = $8, company_field = $9, windows = $10
RETURNING *;

-- name: GetBookingPageByUser :one
SELECT * FROM booking_pages WHERE user_id = $1;

-- name: GetBookingPageBySlugs :one
SELECT * FROM booking_pages WHERE org_slug = $1 AND user_slug = $2;

-- name: CreateBooking :one
INSERT INTO bookings (tenant_id, page_id, user_id, name, email, phone, company, note, starts_at, ends_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING *;

-- name: SetEventBooking :exec
UPDATE events SET booking_id = $2 WHERE id = $1;

-- name: BookingBusy :many
-- Ocupação COMPLETA de um colaborador para o booking: eventos próprios (com hora E dia inteiro)
-- + eventos onde é convidado e não recusou. Recorrentes entram via expansão no handler.
SELECT ev.starts_at, ev.ends_at FROM events ev
WHERE ev.owner_user_id = sqlc.arg(owner_id) AND ev.tenant_id = sqlc.arg(tenant_id) AND ev.deleted_at IS NULL
  AND ev.starts_at < sqlc.arg(to_ts) AND ev.ends_at > sqlc.arg(from_ts)
UNION ALL
SELECT e.starts_at, e.ends_at FROM events e
JOIN event_invites i ON i.event_id = e.id
WHERE i.user_id = sqlc.arg(invitee_id) AND e.tenant_id = sqlc.arg(tenant_id) AND e.deleted_at IS NULL
  AND i.status <> 'declined' AND e.starts_at < sqlc.arg(to_ts) AND e.ends_at > sqlc.arg(from_ts);

-- name: SetEventKind :exec
UPDATE events SET kind = $2 WHERE id = $1 AND tenant_id = $3;

-- name: TimeOffConflicts :many
-- Eventos que colidem com uma ausência a marcar: os PRÓPRIOS (own=true, canceláveis) e os em que
-- sou convidado e não recusei (own=false, só dá para recusar). Ausências (kind<>'') não contam.
SELECT ev.id, ev.title, ev.starts_at, ev.ends_at, TRUE AS own FROM events ev
WHERE ev.owner_user_id = sqlc.arg(owner_id) AND ev.tenant_id = sqlc.arg(tenant_id) AND ev.deleted_at IS NULL
  AND ev.kind = '' AND ev.starts_at < sqlc.arg(to_ts) AND ev.ends_at > sqlc.arg(from_ts)
UNION ALL
SELECT e.id, e.title, e.starts_at, e.ends_at, FALSE AS own FROM events e
JOIN event_invites i ON i.event_id = e.id
WHERE i.user_id = sqlc.arg(invitee_id) AND e.tenant_id = sqlc.arg(tenant_id) AND e.deleted_at IS NULL
  AND e.kind = '' AND i.status <> 'declined' AND e.starts_at < sqlc.arg(to_ts) AND e.ends_at > sqlc.arg(from_ts)
ORDER BY 3;

-- name: ListMyMeetingsPage :many
-- Página de reuniões do utilizador por balde (upcoming/past), com o total na própria linha
-- (COUNT OVER) — paginação server-side da home do Meet (24/07/2026).
SELECT m.*, COALESCE(p.rsvp, '') AS my_rsvp, (m.owner_user_id = sqlc.arg(owner_user_id)) AS is_owner,
       COUNT(*) OVER() AS total
FROM meetings m
LEFT JOIN meeting_participants p ON p.meeting_id = m.id AND p.email = sqlc.arg(email)
WHERE m.tenant_id = sqlc.arg(tenant_id) AND (m.owner_user_id = sqlc.arg(owner_user_id) OR p.id IS NOT NULL)
  AND CASE WHEN sqlc.arg(bucket)::text = 'upcoming'
    THEN m.status <> 'canceled' AND NOT (m.status = 'ended' OR (m.scheduled_end IS NOT NULL AND m.scheduled_end < now() AND m.status <> 'live'))
    ELSE (m.status = 'ended' OR (m.scheduled_end IS NOT NULL AND m.scheduled_end < now() AND m.status <> 'live'))
  END
ORDER BY CASE WHEN sqlc.arg(bucket)::text = 'upcoming' THEN COALESCE(m.scheduled_start, m.created_at) END ASC,
         COALESCE(m.scheduled_start, m.created_at) DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: ListRecordingsPage :many
SELECT *, COUNT(*) OVER() AS total FROM recordings WHERE tenant_id = $1
ORDER BY started_at DESC LIMIT $2 OFFSET $3;

-- name: GetMailUserByEmailInTenant :one
-- Variante tenant-scoped de GetMailUserByEmail: para caminhos onde o alvo TEM de ser do mesmo
-- tenant (assistente: convidar/partilhar), sem depender de um post-check no chamador.
SELECT * FROM mail_users WHERE email = $1 AND tenant_id = $2;

-- name: GetFolderInTenant :one
SELECT * FROM storage_folders WHERE id = $1 AND tenant_id = $2;

-- name: NextOutboxDue :one
-- O send_at mais próximo ainda pendente — o worker dorme exatamente até aí (sem polling). NULL se
-- não há nada em fila.
SELECT MIN(send_at)::timestamptz AS next_at FROM outbox_emails WHERE status = 'pending';

-- name: NotifyOutbox :exec
-- Acorda o worker de envio: a API chama isto ao enfileirar um e-mail (undo-send/agendado).
SELECT pg_notify('outbox_new', '');
