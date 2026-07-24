-- +goose Up
-- Alinha o preço do Pro em EUR com a landing (€18.99, não €19.99): landing e cobrança têm de
-- mostrar/cobrar o mesmo. Correção pontual sobre a 00118 (que pode já ter aplicado com 1999).
UPDATE plans SET prices = jsonb_set(prices, '{EUR}', '1899'::jsonb), updated_at = now()
WHERE key = 'pro' AND prices ? 'EUR';

-- +goose Down
UPDATE plans SET prices = jsonb_set(prices, '{EUR}', '1999'::jsonb) WHERE key = 'pro' AND prices ? 'EUR';
