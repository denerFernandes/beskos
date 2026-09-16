-- ---------- Delivery events (SNS -> webhook; see migration 00107) ----------

-- name: InsertMailEvent :exec
INSERT INTO mail_events (tenant_id, type, subtype, recipient, source)
VALUES (@tenant_id, @type, @subtype, @recipient, @source);

-- name: TenantByEmailDomain :one
-- Resolves the tenant that owns the sender's domain (mail.source from the SES event).
SELECT tenant_id FROM domains WHERE domain = @domain LIMIT 1;
