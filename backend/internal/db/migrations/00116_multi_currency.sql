-- +goose Up
-- Moeda por país (24/07/2026): cada plano pode ter preço por moeda ({"USD":1900,"BRL":9900,...});
-- price_cents_per_user continua a ser o preço na moeda BASE (fallback). Cada tenant tem uma moeda
-- ('' = a global BILLING_CURRENCY); checkout/assinatura usam o preço dessa moeda via
-- currency_options do Price na Stripe.
ALTER TABLE plans   ADD COLUMN prices   JSONB NOT NULL DEFAULT '{}';
ALTER TABLE tenants ADD COLUMN currency TEXT  NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tenants DROP COLUMN currency;
ALTER TABLE plans   DROP COLUMN prices;
