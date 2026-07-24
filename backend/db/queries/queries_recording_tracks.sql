-- Pipeline de gravação offline (track egress + montagem ffmpeg no worker). Ver migração 00104.

-- name: CreateTracksRecording :one
INSERT INTO recordings (tenant_id, room_name, meeting_id, egress_id, blob_key, started_by, mode, priority)
VALUES (@tenant_id, @room_name, @meeting_id, '', '', @started_by, 'tracks', @priority)
RETURNING *;

-- name: InsertRecordingTrack :one
INSERT INTO recording_tracks (recording_id, egress_id, track_id, participant, kind)
VALUES (@recording_id, @egress_id, @track_id, @participant, @kind)
RETURNING *;

-- name: FinishRecordingTrack :execrows
-- Chamado pelo webhook egress_ended de um track egress; devolve 0 linhas se o egress não é de
-- track (aí o chamador cai na lógica de RoomComposite).
UPDATE recording_tracks SET status = @status, blob_key = @blob_key, ended_at = now()
WHERE egress_id = @egress_id AND status = 'recording';

-- name: ListRecordingTracks :many
SELECT * FROM recording_tracks WHERE recording_id = @recording_id ORDER BY started_at;

-- name: ListActiveRecordingTracks :many
SELECT * FROM recording_tracks WHERE recording_id = @recording_id AND status = 'recording';

-- name: GetActiveTracksRecordingForRoom :one
-- A gravação ativa em modo tracks de uma sala (para ligar egress a tracks publicadas depois).
SELECT * FROM recordings
WHERE room_name = @room_name AND status = 'recording' AND mode = 'tracks'
ORDER BY started_at DESC LIMIT 1;

-- name: MarkRecordingAssemblyPending :exec
UPDATE recordings SET assembly_status = 'pending' WHERE id = @id AND mode = 'tracks';

-- name: ClaimAssemblyJob :one
-- Claim atômico do próximo job: prioridade primeiro (offline_priority), depois FIFO. Só pega
-- gravações sem track ainda em 'recording'. Reanima processing órfão (>1h = worker morreu).
UPDATE recordings SET assembly_status = 'processing', assembly_claimed_at = now()
WHERE id = (
    SELECT r.id FROM recordings r
    WHERE r.mode = 'tracks'
      AND (r.assembly_status = 'pending'
           OR (r.assembly_status = 'processing' AND r.assembly_claimed_at < now() - interval '1 hour'))
      AND NOT EXISTS (SELECT 1 FROM recording_tracks t
                      WHERE t.recording_id = r.id AND t.status = 'recording')
    ORDER BY r.priority DESC, r.started_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: SetRecordingAssembled :exec
UPDATE recordings SET status = 'ready', assembly_status = 'done', blob_key = @blob_key,
    ended_at = now(), duration_seconds = @duration_seconds, size_bytes = @size_bytes
WHERE id = @id;

-- name: SetRecordingAssemblyFailed :exec
UPDATE recordings SET status = 'failed', assembly_status = 'failed', ended_at = now() WHERE id = @id;
