-- +goose Up
-- Operador admin do Manager (pedido 22/07/2026): dener.php@gmail.com com senha inicial (bcrypt).
-- O TOTP obrigatório completa no primeiro login (o Manager não emite sessão antes disso).
-- Idempotente: se o e-mail já existir, não mexe. A senha inicial foi entregue fora do repositório;
-- aqui vive apenas o hash (segredo no código = bug, lei 2.6 — hash bcrypt não é segredo).
INSERT INTO manager_users (email, name, password_hash, role)
VALUES ('dener.php@gmail.com', 'Dener', '$2a$10$zMyaShaUZSOYZIYJRpPkGuR8qcqrlqzYlZ3BiOtkgCjWY5uG8YOAm', 'admin')
ON CONFLICT (email) DO NOTHING;

-- +goose Down
DELETE FROM manager_users WHERE email = 'dener.php@gmail.com' AND totp_confirmed = false;
