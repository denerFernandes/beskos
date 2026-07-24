-- Medição + limite diário de envio (ver migração 00105 e docs/manager.md §4).

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
FROM tenants t LEFT JOIN plans p ON p.key = t.plano
WHERE t.id = @tenant_id;

-- ---------- Eventos de entrega (SNS → webhook; ver migração 00107) ----------

-- name: InsertMailEvent :exec
INSERT INTO mail_events (tenant_id, type, subtype, recipient, source)
VALUES (@tenant_id, @type, @subtype, @recipient, @source);

-- name: TenantByEmailDomain :one
-- Resolve o tenant dono do domínio do remetente (mail.source do evento SES).
SELECT tenant_id FROM domains WHERE dominio = @dominio LIMIT 1;

-- name: ManagerDeliverability :many
-- Deliverability 30d por tenant: enviados (contador) vs bounces/complaints (eventos SES).
SELECT t.id, t.nome,
    (SELECT COALESCE(sum(m.sent), 0) FROM tenant_mail_sent m
      WHERE m.tenant_id = t.id AND m.day >= current_date - 30)::bigint AS sent_30d,
    (SELECT count(*) FROM mail_events e WHERE e.tenant_id = t.id
      AND e.type = 'bounce' AND e.created_at > now() - interval '30 days')::bigint AS bounces_30d,
    (SELECT count(*) FROM mail_events e WHERE e.tenant_id = t.id
      AND e.type = 'complaint' AND e.created_at > now() - interval '30 days')::bigint AS complaints_30d
FROM tenants t
WHERE t.status = 'active'
ORDER BY t.nome;
