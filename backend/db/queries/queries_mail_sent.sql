-- Send metering and the daily sending limit.

-- name: IncrementMailSent :exec
INSERT INTO tenant_mail_sent (tenant_id, day, sent)
VALUES (@tenant_id, current_date, @n)
ON CONFLICT (tenant_id, day) DO UPDATE SET sent = tenant_mail_sent.sent + EXCLUDED.sent;

-- name: MailSentToday :one
SELECT COALESCE((SELECT sent FROM tenant_mail_sent
    WHERE tenant_id = @tenant_id AND day = current_date), 0)::int;

-- name: TenantDailySendLimit :one
-- Limite do plano direto da BD (para o worker, que não tem o cache do catálogo). 0 = sem teto.
SELECT COALESCE(p.max_daily_send, 0)::int
FROM tenants t LEFT JOIN plans p ON p.key = t.plan
WHERE t.id = @tenant_id;
