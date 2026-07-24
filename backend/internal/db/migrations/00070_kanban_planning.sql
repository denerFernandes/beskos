-- +goose Up
-- Kanban: limite WIP por coluna, data de início do card, múltiplos responsáveis e dependências.
ALTER TABLE board_columns ADD COLUMN wip_limit INT NOT NULL DEFAULT 0; -- 0 = sem limite
ALTER TABLE cards ADD COLUMN start_date TEXT NOT NULL DEFAULT '';

-- Responsáveis adicionais (além do assignee_id "principal"): M:N.
CREATE TABLE card_assignees (
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, user_id)
);

-- Dependências: card depende de (é bloqueado por) outro card do mesmo quadro.
CREATE TABLE card_deps (
    card_id       UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    depends_on_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, depends_on_id),
    CHECK (card_id <> depends_on_id)
);

-- +goose Down
DROP TABLE card_deps;
DROP TABLE card_assignees;
ALTER TABLE cards DROP COLUMN start_date;
ALTER TABLE board_columns DROP COLUMN wip_limit;
