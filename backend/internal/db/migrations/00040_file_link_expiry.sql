-- +goose Up
-- Links públicos passam a expirar (auditoria: link eterno e não-revogável). expires_at NULL = sem
-- expiração (compat.), mas novos links recebem um prazo; a resolução do token passa a checá-lo.
ALTER TABLE file_links ADD COLUMN expires_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE file_links DROP COLUMN expires_at;
