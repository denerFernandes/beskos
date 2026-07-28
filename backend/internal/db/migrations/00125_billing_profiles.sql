-- +goose Up
-- Dados de faturação do cliente (quem assina, para onde vai a fatura). Antes o checkout ia para a
-- Stripe sem nada: a fatura saía sem razão social, sem morada e sem NIF, o que não serve a uma
-- empresa que precisa de deduzir o IVA. Guardamos cá porque a fatura é NOSSA obrigação legal — a
-- Stripe é só o meio de cobrança, e trocar de provedor não pode levar os dados fiscais com ele.
CREATE TABLE billing_profiles (
    tenant_id      UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    -- company | individual: mudam os campos obrigatórios (NIF/razão social) e a regra do IVA.
    kind           TEXT NOT NULL DEFAULT 'company',
    legal_name     TEXT NOT NULL DEFAULT '',
    -- NIF/VAT com prefixo do país (PL1234567890). Vazio no particular e na empresa sem número.
    tax_id         TEXT NOT NULL DEFAULT '',
    country        TEXT NOT NULL DEFAULT '',  -- ISO 3166-1 alfa-2, maiúsculas
    city           TEXT NOT NULL DEFAULT '',
    postal_code    TEXT NOT NULL DEFAULT '',
    address_line1  TEXT NOT NULL DEFAULT '',
    address_line2  TEXT NOT NULL DEFAULT '',
    -- Responsável financeiro: é para aqui que vai a fatura, e não para quem calhou de assinar.
    invoice_email  TEXT NOT NULL DEFAULT '',
    invoice_name   TEXT NOT NULL DEFAULT '',
    phone          TEXT NOT NULL DEFAULT '',
    -- Cliente espelhado na Stripe (cus_…): reutilizado a cada checkout para não duplicar clientes.
    stripe_customer TEXT NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE billing_profiles;
