-- +goose Up
-- Meet v1.5: "organizador entra primeiro" (a sala fica trancada até o host chegar).
--
-- Default false: ligar a tranca é decisão consciente do dono. Ligada por omissão, toda sala ad-hoc
-- (ex.: chamada iniciada no chat) barraria quem entrasse antes de quem convidou — o oposto do útil.
ALTER TABLE meetings ADD COLUMN lock_until_host BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE meetings DROP COLUMN lock_until_host;
