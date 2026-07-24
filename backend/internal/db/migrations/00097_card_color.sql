-- +goose Up
-- Cor do card do Kanban (pedido do utilizador): tinta suave escolhida pelo utilizador; '' = default.
-- O contraste do conteúdo é garantido no frontend (paleta de tons claros + tinta de texto fixa).
ALTER TABLE cards ADD COLUMN color TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cards DROP COLUMN IF EXISTS color;
