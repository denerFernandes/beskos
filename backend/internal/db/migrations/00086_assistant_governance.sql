-- +goose Up
-- Governança do assistente (fase A7, §9 e §6.6): quota de uso e exclusão de pastas.

-- Uso registado por turno, para a quota da janela deslizante de 6 horas (§9.2). Uma linha por
-- execução; a quota soma as das últimas 6 horas. É o limite COMERCIAL — distinto do teto de passos
-- por execução (§9.1), que é segurança e vive no código desde a A0.
CREATE TABLE assistant_usage (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    tokens       INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- A soma da janela é sempre (utilizador, tempo recente): o índice serve exatamente essa consulta.
CREATE INDEX idx_assistant_usage_window ON assistant_usage (mail_user_id, created_at);

-- Pastas fora do alcance do assistente (§6.6). Escolher alcance amplo por omissão (decisão §12.1,
-- ponto 1) aumenta o que uma injeção poderia alcançar; esta é a válvula que deixa o administrador
-- tirar pastas sensíveis da vista do assistente. É por PASTA, e recursiva na aplicação.
CREATE TABLE assistant_excluded_folders (
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    folder_id    UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, folder_id)
);

-- +goose Down
DROP TABLE assistant_excluded_folders;
DROP TABLE assistant_usage;
