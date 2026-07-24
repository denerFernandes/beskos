-- +goose Up
-- Prioridade da tarefa (voltou dos tickets, removida na 00024). Escala fixa; a UI mostra cor/ícone.
-- 'normal' como default preserva os cards existentes sem tocar em nada.
ALTER TABLE cards ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal'
    CONSTRAINT cards_priority_ck CHECK (priority IN ('low', 'normal', 'high', 'urgent'));

-- +goose Down
ALTER TABLE cards DROP COLUMN priority;
