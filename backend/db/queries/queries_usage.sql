-- Metering: daily rollup written by the worker; the manager only reads it.

-- name: RollupTenantDailyUsage :exec
-- Freezes usage for ALL tenants for one day (idempotent: upsert). "State" counters
-- (users, storage, pages, cards) reflect the moment of the rollup; "flow" counters (ai_tokens,
-- meet_minutes, recordings) sum what happened DURING the day.
INSERT INTO tenant_daily_usage (tenant_id, day, users, users_active, storage_bytes, files,
    ai_tokens, meet_minutes, recordings, recordings_bytes, wiki_pages, cards, mail_sent)
SELECT t.id, cast(@day as date),
    -- users includes ALIASES: an alias is billed as a mailbox (business rule, 2026-07-27).
    (SELECT count(*) FROM mail_users u WHERE u.tenant_id = t.id)
        + (SELECT count(*) FROM email_aliases al WHERE al.tenant_id = t.id),
    (SELECT count(*) FROM mail_users u WHERE u.tenant_id = t.id
        AND u.mail_seen_at >= cast(@day as date) AND u.mail_seen_at < (cast(@day as date) + 1)),
    (SELECT coalesce(sum(f.size), 0)::bigint FROM storage_files f
        WHERE f.tenant_id = t.id AND f.deleted_at IS NULL),
    (SELECT count(*) FROM storage_files f WHERE f.tenant_id = t.id AND f.deleted_at IS NULL),
    (SELECT coalesce(sum(a.tokens), 0)::bigint FROM assistant_usage a
        WHERE a.tenant_id = t.id AND a.created_at >= cast(@day as date) AND a.created_at < (cast(@day as date) + 1)),
    (SELECT coalesce(sum(extract(epoch FROM (coalesce(p.left_at, now()) - p.joined_at)) / 60), 0)::int
        FROM meeting_participants p WHERE p.tenant_id = t.id AND p.joined_at IS NOT NULL
        AND p.joined_at >= cast(@day as date) AND p.joined_at < (cast(@day as date) + 1)),
    (SELECT count(*) FROM recordings r WHERE r.tenant_id = t.id
        AND r.started_at >= cast(@day as date) AND r.started_at < (cast(@day as date) + 1)),
    (SELECT coalesce(sum(r.size_bytes), 0)::bigint FROM recordings r WHERE r.tenant_id = t.id
        AND r.started_at >= cast(@day as date) AND r.started_at < (cast(@day as date) + 1)),
    (SELECT count(*) FROM wiki_pages w WHERE w.tenant_id = t.id),
    (SELECT count(*) FROM cards c WHERE c.tenant_id = t.id),
    COALESCE((SELECT m.sent FROM tenant_mail_sent m
      WHERE m.tenant_id = t.id AND m.day = cast(@day as date)), 0)
FROM tenants t
ON CONFLICT (tenant_id, day) DO UPDATE SET
    users = EXCLUDED.users, users_active = EXCLUDED.users_active,
    storage_bytes = EXCLUDED.storage_bytes, files = EXCLUDED.files,
    ai_tokens = EXCLUDED.ai_tokens, meet_minutes = EXCLUDED.meet_minutes,
    recordings = EXCLUDED.recordings, recordings_bytes = EXCLUDED.recordings_bytes,
    wiki_pages = EXCLUDED.wiki_pages, cards = EXCLUDED.cards,
    mail_sent = EXCLUDED.mail_sent, updated_at = now();

-- name: ExpiredRecordings :many
-- Recordings past the tenant's PLAN retention (fallback: 30 days when the table has no plan).
SELECT r.id, r.tenant_id, r.blob_key
FROM recordings r
JOIN tenants t ON t.id = r.tenant_id
LEFT JOIN plans p ON p.key = t.plan
WHERE r.started_at < now() - make_interval(days => COALESCE(p.recording_retention_days, 30))
LIMIT 100;

-- name: DeleteRecordingRow :exec
DELETE FROM recordings WHERE id = @id AND tenant_id = @tenant_id;
