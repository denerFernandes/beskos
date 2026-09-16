-- +goose Up
-- Manager admin operator (requested 2026-07-22): dener.php@gmail.com with an initial password
-- (bcrypt). Mandatory TOTP completes at first login (the Manager issues no session before that).
-- Idempotent: does nothing if the email already exists. The initial password was delivered
-- outside the repository; only the hash lives here (a secret in code is a bug, law 2.6 — a bcrypt
-- hash is not a secret).
INSERT INTO manager_users (email, name, password_hash, role)
VALUES ('dener.php@gmail.com', 'Dener', '$2a$10$zMyaShaUZSOYZIYJRpPkGuR8qcqrlqzYlZ3BiOtkgCjWY5uG8YOAm', 'admin')
ON CONFLICT (email) DO NOTHING;

-- +goose Down
DELETE FROM manager_users WHERE email = 'dener.php@gmail.com' AND totp_confirmed = false;
