-- +goose Up
-- Notificação de prazo: marca qual valor de `due` já foi avisado. O worker avisa quando due está
-- vencendo/vencido e due_notified_for ainda não bate com o due atual; depois grava due aqui. Se o
-- prazo mudar (UpdateCard grava outro due), due_notified_for deixa de bater e re-arma o aviso.
ALTER TABLE cards ADD COLUMN due_notified_for TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cards DROP COLUMN due_notified_for;
