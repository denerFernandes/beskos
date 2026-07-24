-- +goose Up
-- Unifica a identidade: `mail_users` passa a ser a ÚNICA tabela de usuários — com ou sem caixa de
-- correio — e `panel_users` morre.
--
-- Contexto: existiam dois mundos. `panel_users` (login do /app, senha, MFA, RBAC) e `mail_users`
-- (webmail, caixa no Stalwart). Criar usuário criava os dois; trocar o papel atualizava só o
-- painel, deixando `mail_users.role` velho. Como o RBAC passa a ler ESSA coluna, o desalinhamento
-- viraria falha de autorização silenciosa.
--
-- Decisões (com o cliente):
--  * Admin é um usuário com role 'admin' — a capacidade "internal_admin" (operador global com
--    tenant nulo) é descontinuada. Por isso `tenant_id` segue NOT NULL e nenhum handler do webmail
--    precisa lidar com tenant nulo.
--  * Quem cria a conta é o admin (o signup já atribui 'owner' ao criador).
--  * Usuário sem caixa (signup, antes de existir domínio) é legítimo: `stalwart_account_id` nulo.

-- 1) mail_users absorve os campos de conta (senha, idioma, MFA).
ALTER TABLE mail_users ADD COLUMN password_hash    TEXT    NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN language         TEXT    NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN totp_secret      TEXT    NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN mfa_totp_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE mail_users ADD COLUMN phone            TEXT    NOT NULL DEFAULT '';
ALTER TABLE mail_users ADD COLUMN mfa_sms_enabled  BOOLEAN NOT NULL DEFAULT false;

-- 2) Onde o e-mail já existe nos dois lados, o mail_user absorve senha/MFA/idioma e o PAPEL do
--    painel — que é o autoritativo hoje (mail_users.role nunca autorizou nada).
UPDATE mail_users m SET
    password_hash    = p.password_hash,
    language         = p.language,
    totp_secret      = p.totp_secret,
    mfa_totp_enabled = p.mfa_totp_enabled,
    phone            = p.phone,
    mfa_sms_enabled  = p.mfa_sms_enabled,
    role             = p.role
FROM panel_users p
WHERE p.email = m.email;

-- 3) Traz os panel_users que não têm mail_user (contas de signup/convite, sem caixa).
--    Preserva o id: assim sessions/recovery_codes/mfa_challenges continuam apontando certo.
--    Linhas com tenant nulo (o antigo internal_admin) NÃO são migradas — capacidade descontinuada.
INSERT INTO mail_users (id, tenant_id, email, name, role, stalwart_account_id, quota_bytes,
                        password_hash, language, totp_secret, mfa_totp_enabled, phone,
                        mfa_sms_enabled, created_at)
SELECT p.id, p.tenant_id, p.email, '', p.role, NULL, 0,
       p.password_hash, p.language, p.totp_secret, p.mfa_totp_enabled, p.phone,
       p.mfa_sms_enabled, p.created_at
FROM panel_users p
WHERE p.tenant_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM mail_users m WHERE m.email = p.email);

-- 4) Repontar quem casou por e-mail: o id do mail_user é diferente do id do panel_user.
UPDATE sessions s        SET user_id = m.id FROM panel_users p JOIN mail_users m ON m.email = p.email WHERE s.user_id = p.id AND m.id <> p.id;
UPDATE recovery_codes rc SET user_id = m.id FROM panel_users p JOIN mail_users m ON m.email = p.email WHERE rc.user_id = p.id AND m.id <> p.id;
UPDATE mfa_challenges mc SET user_id = m.id FROM panel_users p JOIN mail_users m ON m.email = p.email WHERE mc.user_id = p.id AND m.id <> p.id;

-- 5) Descarta o que ficou órfão (ex.: sessão de um operador global não migrado).
DELETE FROM sessions       WHERE user_id NOT IN (SELECT id FROM mail_users);
DELETE FROM recovery_codes WHERE user_id NOT IN (SELECT id FROM mail_users);
DELETE FROM mfa_challenges WHERE user_id NOT IN (SELECT id FROM mail_users);

-- 6) Aponta as FKs para mail_users.
ALTER TABLE sessions       DROP CONSTRAINT sessions_user_id_fkey;
ALTER TABLE sessions       ADD  CONSTRAINT sessions_user_id_fkey       FOREIGN KEY (user_id) REFERENCES mail_users(id) ON DELETE CASCADE;
ALTER TABLE recovery_codes DROP CONSTRAINT recovery_codes_user_id_fkey;
ALTER TABLE recovery_codes ADD  CONSTRAINT recovery_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES mail_users(id) ON DELETE CASCADE;
ALTER TABLE mfa_challenges DROP CONSTRAINT mfa_challenges_user_id_fkey;
ALTER TABLE mfa_challenges ADD  CONSTRAINT mfa_challenges_user_id_fkey FOREIGN KEY (user_id) REFERENCES mail_users(id) ON DELETE CASCADE;

-- 7) Normaliza o vocabulário de papéis. `mail_users.role` usava 'user', que NÃO existe no RBAC
--    (owner|admin|member|billing) — e papel desconhecido é negado (fail-closed). Sem isso, esses
--    usuários ficariam sem nenhuma permissão ao ligarmos o RBAC nessa coluna.
UPDATE mail_users SET role = 'member' WHERE role NOT IN ('owner', 'admin', 'member', 'billing');

-- 8) panel_users morre.
DROP TABLE panel_users;

-- +goose Down
-- Recria a tabela e devolve as contas COM senha (as que vieram do painel). Volta as FKs.
-- ASSIMÉTRICO DE PROPÓSITO: as contas migradas permanecem também em mail_users, e 'user' não é
-- restaurado (virou 'member'). Apagá-las cascatearia para contatos/quadros/arquivos que elas
-- tenham criado — num rollback de emergência, resíduo inofensivo é melhor que perda de dados.
CREATE TABLE panel_users (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID REFERENCES tenants(id) ON DELETE CASCADE,
    email            TEXT NOT NULL UNIQUE,
    password_hash    TEXT NOT NULL,
    role             TEXT NOT NULL DEFAULT 'tenant_admin',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    language         TEXT NOT NULL DEFAULT '',
    totp_secret      TEXT NOT NULL DEFAULT '',
    mfa_totp_enabled BOOLEAN NOT NULL DEFAULT false,
    phone            TEXT NOT NULL DEFAULT '',
    mfa_sms_enabled  BOOLEAN NOT NULL DEFAULT false
);
INSERT INTO panel_users (id, tenant_id, email, password_hash, role, created_at, language,
                         totp_secret, mfa_totp_enabled, phone, mfa_sms_enabled)
SELECT id, tenant_id, email, password_hash, role, created_at, language,
       totp_secret, mfa_totp_enabled, phone, mfa_sms_enabled
FROM mail_users WHERE password_hash <> '';

DELETE FROM sessions       WHERE user_id NOT IN (SELECT id FROM panel_users);
DELETE FROM recovery_codes WHERE user_id NOT IN (SELECT id FROM panel_users);
DELETE FROM mfa_challenges WHERE user_id NOT IN (SELECT id FROM panel_users);

ALTER TABLE sessions       DROP CONSTRAINT sessions_user_id_fkey;
ALTER TABLE sessions       ADD  CONSTRAINT sessions_user_id_fkey       FOREIGN KEY (user_id) REFERENCES panel_users(id) ON DELETE CASCADE;
ALTER TABLE recovery_codes DROP CONSTRAINT recovery_codes_user_id_fkey;
ALTER TABLE recovery_codes ADD  CONSTRAINT recovery_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES panel_users(id) ON DELETE CASCADE;
ALTER TABLE mfa_challenges DROP CONSTRAINT mfa_challenges_user_id_fkey;
ALTER TABLE mfa_challenges ADD  CONSTRAINT mfa_challenges_user_id_fkey FOREIGN KEY (user_id) REFERENCES panel_users(id) ON DELETE CASCADE;

ALTER TABLE mail_users DROP COLUMN password_hash;
ALTER TABLE mail_users DROP COLUMN language;
ALTER TABLE mail_users DROP COLUMN totp_secret;
ALTER TABLE mail_users DROP COLUMN mfa_totp_enabled;
ALTER TABLE mail_users DROP COLUMN phone;
ALTER TABLE mail_users DROP COLUMN mfa_sms_enabled;
