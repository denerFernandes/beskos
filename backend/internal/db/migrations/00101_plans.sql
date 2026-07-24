-- +goose Up
-- Catálogo de planos em BD (manager M2, docs/manager.md §3.1). Substitui o catálogo hard-coded de
-- internal/billing/plans.go como fonte de verdade: o produto passa a ler daqui (com cache e
-- fallback para o catálogo em código se a tabela estiver vazia). O vínculo tenant→plano continua
-- sendo tenants.plano (chave do plano) — já usado pelo billing/webhook; NÃO criamos tenant_plans
-- (divergência registada no doc: seats/overrides ficam para quando o billing precisar).
CREATE TABLE plans (
    key                      TEXT PRIMARY KEY,
    name                     TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    price_cents_per_user     INT NOT NULL DEFAULT 0,
    min_users                INT NOT NULL DEFAULT 0,  -- tamanho mínimo da organização (0 = sem mínimo)
    max_users                INT NOT NULL DEFAULT 0,  -- teto de caixas/utilizadores (0 = sem teto)
    -- Apps do rail permitidas no plano. Vazio = todas (o gating soma com tenants.disabled_apps).
    apps_enabled             TEXT[] NOT NULL DEFAULT '{}',
    max_storage_gb           INT NOT NULL DEFAULT 0,  -- pool de storage do tenant (0 = sem teto)
    ai_tokens_per_window     INT NOT NULL DEFAULT 0,  -- quota IA janela 6h (0 = default do produto)
    meet_max_participants    INT NOT NULL DEFAULT 0,
    meet_max_minutes         INT NOT NULL DEFAULT 0,  -- duração máxima de sala (0 = sem teto)
    -- Gravação de reuniões (decisão 22/07/2026): none | offline (track egress + montagem ffmpeg)
    -- | offline_priority (mesma coisa, fila prioritária).
    recording                TEXT NOT NULL DEFAULT 'none'
        CHECK (recording IN ('none', 'offline', 'offline_priority')),
    recording_retention_days INT NOT NULL DEFAULT 30,
    max_daily_send           INT NOT NULL DEFAULT 0,  -- envios/dia do tenant (0 = sem teto)
    onlyoffice_edit          BOOLEAN NOT NULL DEFAULT true,
    wiki_public_links        BOOLEAN NOT NULL DEFAULT true,
    active                   BOOLEAN NOT NULL DEFAULT true,
    sort                     INT NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed: os 4 planos do catálogo em código, com os MESMOS limites (zero mudança de comportamento).
-- Gravação conforme a decisão: plano de entrada sem gravação; pagos = offline; topo = prioridade.
INSERT INTO plans (key, name, price_cents_per_user, min_users, max_users, max_storage_gb,
                   ai_tokens_per_window, meet_max_participants, recording, max_daily_send, sort)
VALUES
    ('trial',    'Trial',    0,    0, 3,   5,    0, 5,   'none',             200,   0),
    ('starter',  'Starter',  1900, 1, 10,  50,   0, 25,  'offline',          1000,  1),
    ('growth',   'Growth',   1500, 11, 50, 250,  0, 50,  'offline',          5000,  2),
    ('business', 'Business', 1200, 51, 200, 1024, 0, 200, 'offline_priority', 20000, 3);

-- +goose Down
DROP TABLE IF EXISTS plans;
