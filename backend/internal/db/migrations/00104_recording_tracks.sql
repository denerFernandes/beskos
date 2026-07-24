-- +goose Up
-- Pipeline de gravação offline (decisão 22/07/2026, docs/manager.md §1): nos planos pagos o
-- padrão é TRACK EGRESS (tracks crus por participante saem baratos durante a reunião) +
-- MONTAGEM OFFLINE (o worker compõe o MP4 com ffmpeg fora do caminho quente). O modo
-- 'composite' (RoomComposite ao vivo) continua existindo como fallback/legado.
ALTER TABLE recordings ADD COLUMN mode TEXT NOT NULL DEFAULT 'composite'
    CHECK (mode IN ('composite', 'tracks'));
-- Ciclo do modo tracks: '' (composite) | pending (parou, à espera de montagem) |
-- processing (worker montando) | done | failed. O status geral da gravação continua em
-- recordings.status ('recording' → 'ready'/'failed').
ALTER TABLE recordings ADD COLUMN assembly_status TEXT NOT NULL DEFAULT ''
    CHECK (assembly_status IN ('', 'pending', 'processing', 'done', 'failed'));
-- Prioridade da fila de montagem: planos offline_priority montam primeiro.
ALTER TABLE recordings ADD COLUMN priority INT NOT NULL DEFAULT 0;
-- Carimbo do claim do worker: um processing mais velho que 1h volta a pending (worker morreu).
ALTER TABLE recordings ADD COLUMN assembly_claimed_at TIMESTAMPTZ;

-- Um egress de track por faixa publicada na sala durante a gravação.
CREATE TABLE recording_tracks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_id UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    egress_id    TEXT NOT NULL,
    track_id     TEXT NOT NULL,
    participant  TEXT NOT NULL DEFAULT '',
    kind         TEXT NOT NULL CHECK (kind IN ('audio', 'video')),
    -- blob_key definitivo vem do webhook egress_ended (o LiveKit escolhe a extensão pelo codec).
    blob_key     TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'recording' CHECK (status IN ('recording', 'done', 'failed')),
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at     TIMESTAMPTZ
);
CREATE INDEX idx_recording_tracks_rec ON recording_tracks(recording_id);
CREATE INDEX idx_recording_tracks_egress ON recording_tracks(egress_id);

-- +goose Down
DROP TABLE IF EXISTS recording_tracks;
ALTER TABLE recordings DROP COLUMN IF EXISTS assembly_claimed_at;
ALTER TABLE recordings DROP COLUMN IF EXISTS priority;
ALTER TABLE recordings DROP COLUMN IF EXISTS assembly_status;
ALTER TABLE recordings DROP COLUMN IF EXISTS mode;
