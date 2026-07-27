-- Metering (manager M3): rollup diário escrito pelo WORKER; o manager só lê.

-- name: RollupTenantDailyUsage :exec
-- Congela o uso de TODOS os tenants para um dia (idempotente: upsert). Contadores "de estado"
-- (users, storage, páginas, cards) refletem o momento do rollup; os "de fluxo" (ai_tokens,
-- meet_minutes, gravações) somam o que aconteceu NO dia.
INSERT INTO tenant_daily_usage (tenant_id, day, users, users_active, storage_bytes, files,
    ai_tokens, meet_minutes, recordings, recordings_bytes, wiki_pages, cards, mail_sent)
SELECT t.id, cast(@day as date),
    -- users inclui os ALIASES: um alias é cobrado como uma caixa (regra de negócio, 27/07/2026).
    (SELECT count(*) FROM mail_users u WHERE u.tenant_id = t.id)
        + (SELECT count(*) FROM email_aliases al WHERE al.tenant_id = t.id),
    (SELECT count(*) FROM mail_users u WHERE u.tenant_id = t.id
        AND u.mail_seen_at >= cast(@day as date) AND u.mail_seen_at < (cast(@day as date) + 1)),
    (SELECT coalesce(sum(f.tamanho), 0)::bigint FROM storage_files f
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

-- name: ManagerTenantUsage :many
SELECT * FROM tenant_daily_usage
WHERE tenant_id = @tenant_id AND day >= cast(@since as date)
ORDER BY day DESC;

-- name: ManagerUsageSeries :many
-- Série agregada de todos os tenants (dashboard/relatórios).
SELECT day,
    sum(users)::bigint AS users, sum(users_active)::bigint AS users_active,
    sum(storage_bytes)::bigint AS storage_bytes, sum(ai_tokens)::bigint AS ai_tokens,
    sum(meet_minutes)::bigint AS meet_minutes, sum(recordings)::bigint AS recordings,
    sum(mail_sent)::bigint AS mail_sent
FROM tenant_daily_usage
WHERE day >= cast(@since as date)
GROUP BY day ORDER BY day;

-- name: ManagerAlertRows :many
-- Base dos ALERTAS de uso exagerado (por tenant): números de HOJE (contadores ao vivo) + a média
-- de tokens IA dos 7 dias anteriores (baseline do spike) + os limites do plano.
SELECT t.id, t.nome, t.plano,
    ((SELECT count(*) FROM mail_users u WHERE u.tenant_id = t.id)
      + (SELECT count(*) FROM email_aliases al WHERE al.tenant_id = t.id))::int AS users,
    (SELECT coalesce(sum(f.tamanho), 0) FROM storage_files f
      WHERE f.tenant_id = t.id AND f.deleted_at IS NULL)::bigint AS storage_bytes,
    (SELECT coalesce(sum(a.tokens), 0) FROM assistant_usage a
      WHERE a.tenant_id = t.id AND a.created_at >= current_date)::bigint AS ai_today,
    (SELECT coalesce(avg(d.ai_tokens), 0) FROM tenant_daily_usage d
      WHERE d.tenant_id = t.id AND d.day >= current_date - 7 AND d.day < current_date)::bigint AS ai_prev_avg,
    COALESCE((SELECT m.sent FROM tenant_mail_sent m
      WHERE m.tenant_id = t.id AND m.day = current_date), 0)::int AS mail_today,
    COALESCE(p.max_users, 0)::int AS max_users,
    COALESCE(p.max_storage_gb, 0)::int AS max_storage_gb,
    COALESCE(p.max_daily_send, 0)::int AS max_daily_send
FROM tenants t
LEFT JOIN plans p ON p.key = t.plano
WHERE t.status = 'active'
ORDER BY t.nome;

-- name: ManagerTopAIUsers :many
-- Top utilizadores por tokens de IA nas últimas 24h (alerta de uso exagerado POR PESSOA).
SELECT u.email, u.name, t.nome AS tenant, sum(a.tokens)::bigint AS tokens
FROM assistant_usage a
JOIN mail_users u ON u.id = a.mail_user_id
JOIN tenants t ON t.id = a.tenant_id
WHERE a.created_at > now() - interval '24 hours'
GROUP BY u.email, u.name, t.nome
ORDER BY sum(a.tokens) DESC
LIMIT 10;

-- name: ManagerUsageAllRows :many
-- Export CSV (M5): rollup cru de todos os tenants no período, com o nome da organização.
SELECT t.nome, u.* FROM tenant_daily_usage u
JOIN tenants t ON t.id = u.tenant_id
WHERE u.day >= cast(@since as date)
ORDER BY u.day DESC, t.nome;

-- name: ExpiredRecordings :many
-- Gravações além da retenção do PLANO do tenant (fallback 30 dias sem plano na tabela).
SELECT r.id, r.tenant_id, r.blob_key
FROM recordings r
JOIN tenants t ON t.id = r.tenant_id
LEFT JOIN plans p ON p.key = t.plano
WHERE r.started_at < now() - make_interval(days => COALESCE(p.recording_retention_days, 30))
LIMIT 100;

-- name: DeleteRecordingRow :exec
DELETE FROM recordings WHERE id = @id;
