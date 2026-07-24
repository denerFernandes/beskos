-- +goose Up
-- Co-hosts: outros utilizadores que o organizador promove a anfitrião. Recebem poderes de host
-- (mutar, remover, bloquear, gravar) e entram com política plena. O dono continua a gerir a lista.
CREATE TABLE meeting_cohosts (
    meeting_id UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (meeting_id, user_id)
);

-- +goose Down
DROP TABLE meeting_cohosts;
