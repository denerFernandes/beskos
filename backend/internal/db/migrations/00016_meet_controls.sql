-- +goose Up
-- Controles do organizador (estilo Google Meet): o que os participantes podem fazer na sala.
-- Aplicado de verdade nos grants do token do LiveKit + UpdateParticipant (não é só UI).

ALTER TABLE meetings ADD COLUMN allow_mic   BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE meetings ADD COLUMN allow_cam   BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE meetings ADD COLUMN allow_share BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE meetings ADD COLUMN allow_chat  BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE meetings DROP COLUMN allow_mic;
ALTER TABLE meetings DROP COLUMN allow_cam;
ALTER TABLE meetings DROP COLUMN allow_share;
ALTER TABLE meetings DROP COLUMN allow_chat;
