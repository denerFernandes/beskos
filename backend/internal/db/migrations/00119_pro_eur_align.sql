-- +goose Up
-- Aligns the Pro price in EUR with the landing page (EUR 18.99, not EUR 19.99): the landing page
-- and billing must show/charge the same. One-off fix over 00118 (which may already have applied 1999).
UPDATE plans SET prices = jsonb_set(prices, '{EUR}', '1899'::jsonb), updated_at = now()
WHERE key = 'pro' AND prices ? 'EUR';

-- +goose Down
UPDATE plans SET prices = jsonb_set(prices, '{EUR}', '1999'::jsonb) WHERE key = 'pro' AND prices ? 'EUR';
