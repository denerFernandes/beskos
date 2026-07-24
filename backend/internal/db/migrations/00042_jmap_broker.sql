-- +goose Up
-- Brokering do JMAP (auditoria H11): a credencial de mailbox (Basic auth) deixa de viver no browser.
-- Guardamos aqui, CIFRADA (AES-GCM, prefixo enc:), a credencial do usuário; o proxy JMAP a injeta
-- server-to-server. O browser passa a autenticar só com a sessão (cookie/bearer). Vazio = sem mailbox
-- ou login anterior à feature (força re-login). Ver deploy/DIVERGENCES.md.
ALTER TABLE mail_users ADD COLUMN jmap_cred TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mail_users DROP COLUMN jmap_cred;
