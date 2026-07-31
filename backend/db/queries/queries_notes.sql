-- Notes: personal notes and meeting notes (migration 00133).
--
-- Every query filters tenant_id (law 5.1). Access beyond the owner goes through NoteRole, which is
-- modelled on WikiPageRole so that both features answer "may this person see this" the same way.

-- name: NoteRole :one
-- Effective rank for a user on a note: 0 none, 1 viewer, 3 editor, 4 owner.
--
-- MAX over the sources, so the most generous grant wins: being in a group that has editor must not
-- be undone by a wider org grant of viewer. The owner short-circuits to 4 -- a note always has
-- someone who can delete it, even after every grant is removed.
SELECT COALESCE(MAX(rank), 0)::int AS role_rank FROM (
    SELECT CASE WHEN n.owner_user_id = @user_id THEN 4 ELSE 0 END AS rank
    FROM notes n WHERE n.id = @note_id AND n.tenant_id = @tenant_id
    UNION ALL
    SELECT CASE g.role WHEN 'editor' THEN 3 ELSE 1 END
    FROM note_grants g
    WHERE g.note_id = @note_id AND g.tenant_id = @tenant_id AND (
        g.subject_type = 'org'
        OR (g.subject_type = 'user' AND g.subject_id = @user_id)
        OR (g.subject_type = 'group' AND EXISTS (
            SELECT 1 FROM user_group_members ugm
            WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))
    )
) r;

-- name: ListNotes :many
-- Everything this person may see: their own, plus whatever was shared with them, directly or
-- through a group or the whole organisation. Pinned first, then most recently touched.
SELECT n.*,
       (p.user_id IS NOT NULL) AS pinned,
       u.name AS owner_name,
       m.title AS meeting_title
FROM notes n
LEFT JOIN note_pins p ON p.note_id = n.id AND p.user_id = @user_id
LEFT JOIN mail_users u ON u.id = n.owner_user_id
LEFT JOIN meetings m ON m.id = n.meeting_id
WHERE n.tenant_id = @tenant_id AND (
    n.owner_user_id = @user_id
    OR EXISTS (
        SELECT 1 FROM note_grants g
        WHERE g.note_id = n.id AND (
            g.subject_type = 'org'
            OR (g.subject_type = 'user' AND g.subject_id = @user_id)
            OR (g.subject_type = 'group' AND EXISTS (
                SELECT 1 FROM user_group_members ugm
                WHERE ugm.group_id = g.subject_id AND ugm.user_id = @user_id))
        )
    )
)
ORDER BY (p.user_id IS NOT NULL) DESC, p.pinned_at DESC NULLS LAST, n.updated_at DESC
LIMIT @lim;

-- name: GetNote :one
SELECT n.*, (p.user_id IS NOT NULL) AS pinned, u.name AS owner_name, m.title AS meeting_title
FROM notes n
LEFT JOIN note_pins p ON p.note_id = n.id AND p.user_id = @user_id
LEFT JOIN mail_users u ON u.id = n.owner_user_id
LEFT JOIN meetings m ON m.id = n.meeting_id
WHERE n.id = @note_id AND n.tenant_id = @tenant_id;

-- name: CreateNote :one
INSERT INTO notes (tenant_id, owner_user_id, title, body_json, body_text, meeting_id, updated_by)
VALUES (@tenant_id, @owner_user_id, @title, @body_json, @body_text, @meeting_id, @owner_user_id)
RETURNING *;

-- name: UpdateNote :one
UPDATE notes SET title = @title, body_json = @body_json, body_text = @body_text,
                 updated_at = now(), updated_by = @updated_by
WHERE id = @note_id AND tenant_id = @tenant_id
RETURNING *;

-- name: DeleteNote :exec
DELETE FROM notes WHERE id = @note_id AND tenant_id = @tenant_id;

-- name: GetNoteByMeeting :one
SELECT * FROM notes WHERE meeting_id = @meeting_id AND tenant_id = @tenant_id;

-- name: PinNote :exec
-- Pinning twice is pinning once: the click can arrive from two windows.
INSERT INTO note_pins (tenant_id, note_id, user_id) VALUES (@tenant_id, @note_id, @user_id)
ON CONFLICT (note_id, user_id) DO NOTHING;

-- name: UnpinNote :exec
DELETE FROM note_pins WHERE note_id = @note_id AND user_id = @user_id;

-- name: ListNoteGrants :many
SELECT g.*, u.name AS user_name, u.email AS user_email, gr.name AS group_name
FROM note_grants g
LEFT JOIN mail_users u ON u.id = g.subject_id AND g.subject_type = 'user'
LEFT JOIN user_groups gr ON gr.id = g.subject_id AND g.subject_type = 'group'
WHERE g.note_id = @note_id AND g.tenant_id = @tenant_id
ORDER BY g.subject_type, g.created_at;

-- name: UpsertNoteGrant :one
-- Sharing with someone who already has access CHANGES the role instead of adding a second row --
-- two grants for the same person would make "remove access" a trap that only removes one of them.
INSERT INTO note_grants (tenant_id, note_id, subject_type, subject_id, role)
VALUES (@tenant_id, @note_id, @subject_type, @subject_id, @role)
ON CONFLICT (note_id, subject_type, COALESCE(subject_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO UPDATE SET role = EXCLUDED.role
RETURNING *;

-- name: DeleteNoteGrant :exec
DELETE FROM note_grants WHERE id = @id AND tenant_id = @tenant_id;
