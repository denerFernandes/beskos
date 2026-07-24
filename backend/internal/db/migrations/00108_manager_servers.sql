-- +goose Up
-- Custo de infra POR SERVIDOR (pedido 22/07/2026): cada VPS/bare-metal com o seu custo mensal.
-- O custo fixo das finanças do manager = soma dos servidores ativos + "outros fixos" do
-- cost_model (que deixa de carregar os VPS — zerado abaixo para não contar em dobro).
CREATE TABLE manager_servers (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    provider      TEXT NOT NULL DEFAULT '',
    monthly_cents INT NOT NULL DEFAULT 0,
    notes         TEXT NOT NULL DEFAULT '',
    active        BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed: o layout do consulting (OVH, 22/07/2026) — 2×VPS-3 (core + media) + VPS-1 (mail) ≈ €65.
INSERT INTO manager_servers (name, provider, monthly_cents, notes) VALUES
    ('vps-core',  'OVH VPS-3', 2600, 'api + postgres + redis + manager'),
    ('vps-media', 'OVH VPS-3', 2600, 'livekit + egress + onlyoffice'),
    ('vps-mail',  'OVH VPS-1', 1300, 'stalwart (IP dedicado ao correio)');

-- O fixo genérico deixa de incluir os VPS (evita dupla contagem); fica para "outros fixos"
-- (domínios, backups, licenças…).
UPDATE manager_settings
SET value = jsonb_set(value, '{fixed_month_cents}', '0'::jsonb), updated_at = now()
WHERE key = 'cost_model';

-- +goose Down
DROP TABLE IF EXISTS manager_servers;
