-- Meet: meetings, participants, breakouts and co-hosts.
--
-- Split out of queries.sql, which the 600-line limit (law 1.4) had long outgrown. sqlc reads
-- every file in this directory, so a query changes file without changing the generated API.

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
-- name: MarkParticipantNotified :exec
UPDATE meeting_participants SET notified_at = now() WHERE meeting_id = $1 AND email = $2;
-- name: MarkParticipantJoined :exec
-- Records that someone is in the room, INSERTING the row when they were never invited.
--
-- It used to be an UPDATE, which quietly did nothing for anyone who joined from a link without an
-- invitation -- and those are precisely the people who then wonder why the meeting notes never
-- reached them. Cross-tenant joins are not recorded: the tenant check is in the join, not after it.
INSERT INTO meeting_participants (meeting_id, tenant_id, email, user_id, role, joined_at)
SELECT m.id, m.tenant_id, u.email, u.id, 'guest', now()
FROM meetings m JOIN mail_users u ON u.id = @user_id AND u.tenant_id = m.tenant_id
WHERE m.room_name = @room_name
ON CONFLICT (meeting_id, email) DO UPDATE SET
    joined_at = COALESCE(meeting_participants.joined_at, now()),
    user_id   = COALESCE(meeting_participants.user_id, EXCLUDED.user_id);
-- name: MarkParticipantLeft :exec
UPDATE meeting_participants SET left_at = now()
WHERE meeting_id = (SELECT id FROM meetings WHERE room_name = $1) AND user_id = $2;

-- ===== Notificações in-app =====
-- name: SetEventMeetingRoom :exec
UPDATE events SET meeting_room = $3 WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $4;

-- ===== Chat (estilo Slack) =====
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
