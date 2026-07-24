-- +goose Up
-- Espelho do catálogo na Stripe (24/07/2026): cada plano tem um Product e um Price recorrente lá.
-- Criar/alterar plano no manager sincroniza; preço mudou → cria um Price novo (são imutáveis) e
-- desativa o antigo. Checkout/troca usam o price_id — a gestão financeira fica toda na Stripe.
ALTER TABLE plans ADD COLUMN stripe_product_id TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN stripe_price_id   TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE plans DROP COLUMN stripe_price_id;
ALTER TABLE plans DROP COLUMN stripe_product_id;
