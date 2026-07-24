-- +goose Up
-- Branding: opção de forçar o logo a BRANCO no tema escuro (pedido 23/07/2026) — logos escuros
-- desapareciam sobre o fundo escuro; com a flag, a UI aplica um filtro que o torna silhueta branca.
ALTER TABLE tenants ADD COLUMN brand_logo_white_dark BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE tenants DROP COLUMN IF EXISTS brand_logo_white_dark;
