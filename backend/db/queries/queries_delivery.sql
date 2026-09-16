-- ---------- Eventos de entrega (SNS → webhook; ver migração 00107) ----------

-- name: InsertMailEvent :exec
INSERT INTO mail_events (tenant_id, type, subtype, recipient, source)
VALUES (@tenant_id, @type, @subtype, @recipient, @source);

-- name: TenantByEmailDomain :one
-- Resolve o tenant dono do domínio do remetente (mail.source do evento SES).
SELECT tenant_id FROM domains WHERE domain = @domain LIMIT 1;
