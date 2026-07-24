-- +goose Up
-- Múltiplos e-mails/telefones por contato. O e-mail/telefone principais (colunas email/phone) seguem
-- sendo a fonte para autocomplete, histórico, dedup e lista de distribuição — mexer nisso tocaria
-- meio sistema. Estes arrays guardam os SECUNDÁRIOS (ex.: e-mail pessoal + trabalho). Mesmo padrão de tags[].
ALTER TABLE contacts ADD COLUMN extra_emails TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE contacts ADD COLUMN extra_phones TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE contacts DROP COLUMN extra_emails;
ALTER TABLE contacts DROP COLUMN extra_phones;
