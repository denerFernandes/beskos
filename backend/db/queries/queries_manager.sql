-- Manager dashboard queries. These tables belong to the operator's control plane, not to a
-- tenant: the manager operates ON tenants rather than inside one. Used only by cmd/manager-api.

-- ---------- Utilizadores do manager ----------

-- name: CreateManagerUser :one
INSERT INTO manager_users (email, name, password_hash, role)
VALUES (@email, @name, @password_hash, @role)
RETURNING *;

-- name: GetManagerUserByEmail :one
SELECT * FROM manager_users WHERE lower(email) = lower(@email) AND active;

-- name: GetManagerUserByID :one
SELECT * FROM manager_users WHERE id = @id AND active;

-- name: ResetManagerUserCredentials :exec
-- Recuperação do acesso ao manager (MANAGER_BOOTSTRAP_RESET=1): repõe a senha e obriga a inscrever
-- o 2.º fator outra vez. Não é uma rota — só corre no arranque, com a variável explícita.
UPDATE manager_users
SET password_hash = @password_hash, totp_secret = '', totp_confirmed = false, active = true
WHERE lower(email) = lower(@email);

-- name: CountManagerUsers :one
SELECT count(*) FROM manager_users;

-- name: ListManagerUsers :many
SELECT id, email, name, role, totp_confirmed, active, last_login_at, created_at
FROM manager_users ORDER BY created_at;

-- name: SetManagerTOTPSecret :exec
-- Guarda o segredo (cifrado) ainda NÃO confirmado — o login só passa depois de confirmar.
UPDATE manager_users SET totp_secret = @totp_secret, totp_confirmed = false WHERE id = @id;

-- name: ConfirmManagerTOTP :exec
UPDATE manager_users SET totp_confirmed = true WHERE id = @id;

-- name: SetManagerTOTPLastStep :exec
-- Anti-replay: só avança (nunca aceita um passo já usado que regravaria para trás).
UPDATE manager_users SET last_totp_step = @last_totp_step
WHERE id = @id AND last_totp_step < @last_totp_step;

-- name: TouchManagerLastLogin :exec
UPDATE manager_users SET last_login_at = now() WHERE id = @id;

-- name: SetManagerUserActive :exec
-- Desativar corta o login E as sessões existentes (GetManagerSession filtra u.active).
UPDATE manager_users SET active = @active WHERE id = @id;

-- name: ManagerGrowth :many
-- Monthly growth: tenants and users created per month.
SELECT to_char(m.month, 'YYYY-MM') AS month,
    (SELECT count(*) FROM tenants t WHERE date_trunc('month', t.created_at) = m.month)::int AS tenants_new,
    (SELECT count(*) FROM mail_users u WHERE date_trunc('month', u.created_at) = m.month)::int AS users_new,
    (SELECT count(*) FROM tenants t WHERE t.created_at < m.month + interval '1 month')::int AS tenants_total,
    (SELECT count(*) FROM mail_users u WHERE u.created_at < m.month + interval '1 month')::int AS users_total
FROM (
    SELECT generate_series(
        date_trunc('month', (SELECT COALESCE(min(created_at), now()) FROM tenants)),
        date_trunc('month', now()), interval '1 month') AS month
) m
ORDER BY m.month;

-- ---------- Sessões ----------

-- name: InsertManagerSession :exec
INSERT INTO manager_sessions (token, manager_user_id, ip, user_agent, expires_at)
VALUES (@token, @manager_user_id, @ip, @user_agent, @expires_at);

-- name: GetManagerSession :one
SELECT s.token, s.expires_at, u.*
FROM manager_sessions s JOIN manager_users u ON u.id = s.manager_user_id
WHERE s.token = @token AND s.expires_at > now() AND u.active;

-- name: DeleteManagerSession :exec
DELETE FROM manager_sessions WHERE token = @token;

-- name: DeleteExpiredManagerSessions :exec
DELETE FROM manager_sessions WHERE expires_at <= now();

-- ---------- Read-only view: the manager reads tenant data, it never writes ----------

-- name: ManagerOverview :one
-- Totais do dashboard. Storage = soma dos ficheiros não apagados (lixeira ainda ocupa espaço no
-- MinIO, mas o que interessa ao operador é o uso "vivo"; a lixeira é rotativa).
SELECT
    (SELECT count(*) FROM tenants)                                              AS tenants,
    (SELECT count(*) FROM tenants WHERE status = 'active')                      AS tenants_active,
    (SELECT count(*) FROM mail_users)                                           AS users,
    (SELECT count(*) FROM mail_users WHERE mail_seen_at > now() - interval '7 days')  AS users_active_7d,
    (SELECT coalesce(sum(size), 0)::bigint FROM storage_files WHERE deleted_at IS NULL)    AS storage_bytes,
    (SELECT count(*) FROM domains)                                              AS domains;

-- name: ManagerListTenants :many
-- List of tenants with counts, computed directly rather than from a rollup table.
SELECT t.id, t.name, t.plan, t.status, t.created_at,
    (SELECT count(*) FROM mail_users u WHERE u.tenant_id = t.id)                      AS users,
    (SELECT count(*) FROM domains d WHERE d.tenant_id = t.id)                         AS domains,
    (SELECT coalesce(sum(f.size), 0)::bigint FROM storage_files f
      WHERE f.tenant_id = t.id AND f.deleted_at IS NULL)                              AS storage_bytes
FROM tenants t
ORDER BY t.created_at DESC;

-- name: ManagerGetTenant :one
SELECT t.id, t.name, t.plan, t.status, t.created_at, t.disabled_apps,
    (SELECT count(*) FROM mail_users u WHERE u.tenant_id = t.id)                      AS users,
    (SELECT count(*) FROM domains d WHERE d.tenant_id = t.id)                         AS domains,
    (SELECT coalesce(sum(f.size), 0)::bigint FROM storage_files f
      WHERE f.tenant_id = t.id AND f.deleted_at IS NULL)                              AS storage_bytes
FROM tenants t WHERE t.id = @id;

-- name: ManagerListTenantUsers :many
SELECT u.id, u.email, u.name, u.role, u.created_at, u.mail_seen_at, u.quota_bytes,
    (u.mfa_totp_enabled OR u.mfa_sms_enabled)::boolean AS mfa_enabled,
    (SELECT coalesce(sum(f.size), 0)::bigint FROM storage_files f
      WHERE f.owner_user_id = u.id AND f.deleted_at IS NULL)                          AS storage_bytes
FROM mail_users u
WHERE u.tenant_id = @tenant_id
ORDER BY u.created_at;

-- ---------- Settings and billing ----------

-- name: GetManagerSetting :one
SELECT value FROM manager_settings WHERE key = @key;

-- name: UpsertManagerSetting :exec
INSERT INTO manager_settings (key, value) VALUES (@key, @value)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

-- name: ManagerFinanceRows :many
-- Base da margem por tenant: users atuais + storage do último rollup + fluxos de 30 dias.
SELECT t.id, t.name, t.plan, t.status,
    (SELECT count(*) FROM mail_users u WHERE u.tenant_id = t.id)::int AS users,
    COALESCE(lu.storage_bytes, 0)::bigint    AS storage_bytes,
    COALESCE(lu.recordings_bytes, 0)::bigint AS recordings_bytes,
    (SELECT COALESCE(sum(d.ai_tokens), 0) FROM tenant_daily_usage d
      WHERE d.tenant_id = t.id AND d.day >= current_date - 30)::bigint AS ai_tokens_30d,
    (SELECT COALESCE(sum(d.mail_sent), 0) FROM tenant_daily_usage d
      WHERE d.tenant_id = t.id AND d.day >= current_date - 30)::bigint AS mail_sent_30d
FROM tenants t
LEFT JOIN LATERAL (
    SELECT d.storage_bytes, d.recordings_bytes FROM tenant_daily_usage d
    WHERE d.tenant_id = t.id ORDER BY d.day DESC LIMIT 1
) lu ON true
ORDER BY t.created_at;

-- ---------- Audit ----------

-- name: InsertManagerAudit :exec
INSERT INTO manager_audit (actor_id, actor_email, action, target, payload)
VALUES (@actor_id, @actor_email, @action, @target, @payload);

-- ---------- Servidores (custo fixo por VPS — migração 00108) ----------

-- name: ListManagerServers :many
SELECT * FROM manager_servers ORDER BY created_at;

-- name: UpsertManagerServer :one
INSERT INTO manager_servers (id, name, provider, monthly_cents, notes)
VALUES (@id, @name, @provider, @monthly_cents, @notes)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name, provider = EXCLUDED.provider,
    monthly_cents = EXCLUDED.monthly_cents, notes = EXCLUDED.notes
RETURNING *;

-- name: SetManagerServerActive :exec
UPDATE manager_servers SET active = @active WHERE id = @id;

-- name: DeleteManagerServer :exec
DELETE FROM manager_servers WHERE id = @id;

-- name: SumManagerServers :one
SELECT COALESCE(sum(monthly_cents), 0)::bigint FROM manager_servers WHERE active;

-- name: UpdatePlanStripeIDs :exec
UPDATE plans SET stripe_product_id = $2, stripe_price_id = $3 WHERE key = $1;

-- name: SetTenantCurrency :exec
UPDATE tenants SET currency = $2 WHERE id = $1;
