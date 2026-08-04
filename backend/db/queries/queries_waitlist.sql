-- The waiting list. See migration 00140 for why it exists.

-- name: AddToWaitlist :one
-- Idempotent by address. Asking twice is one person changing their mind about what to tell us, not
-- two rows -- and COALESCE keeps what they said the first time when the second is blank, so a
-- resubmission from a shorter form never erases the note that made the row worth reading.
INSERT INTO waitlist (email, name, company, note, locale, source)
VALUES (lower(@email), @name, @company, @note, @locale, @source)
ON CONFLICT (lower(email)) DO UPDATE SET
    name    = COALESCE(NULLIF(EXCLUDED.name, ''), waitlist.name),
    company = COALESCE(NULLIF(EXCLUDED.company, ''), waitlist.company),
    note    = COALESCE(NULLIF(EXCLUDED.note, ''), waitlist.note),
    locale  = COALESCE(NULLIF(EXCLUDED.locale, ''), waitlist.locale)
RETURNING *;

-- name: ListWaitlist :many
-- The queue, oldest first: the person who has waited longest is the person to invite next, and any
-- other order is a way of quietly losing them.
SELECT * FROM waitlist
WHERE (@status::text = '' OR status = @status)
ORDER BY (status = 'pending') DESC, created_at
LIMIT 500;

-- name: GetWaitlistEntry :one
SELECT * FROM waitlist WHERE id = $1;

-- name: MarkWaitlistInvited :one
UPDATE waitlist SET
    status = 'invited', invite_token = @invite_token, invited_at = now(), invited_by = @invited_by
WHERE id = @id
RETURNING *;

-- name: SetWaitlistStatus :one
UPDATE waitlist SET status = @status WHERE id = @id RETURNING *;

-- name: DeleteWaitlistEntry :exec
-- Somebody asking to be forgotten, or an address that was obviously a robot.
DELETE FROM waitlist WHERE id = $1;

-- name: CountWaitlist :one
SELECT
    COUNT(*) AS total,
    COUNT(*) FILTER (WHERE status = 'pending') AS pending,
    COUNT(*) FILTER (WHERE status = 'invited') AS invited,
    COUNT(*) FILTER (WHERE status = 'joined')  AS joined
FROM waitlist;
