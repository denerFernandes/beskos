package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
)

// Gravação de reunião via LiveKit Egress (RoomComposite) → MinIO. Opcional: só quando o usuário
// inicia. Chamamos a API Twirp do LiveKit; o Egress compõe a sala e sobe o MP4.

const recordTokenTTL = 6 * time.Hour

// twirp chama um método da API do LiveKit (Egress) com token roomRecord.
func (m *meet) twirp(ctx context.Context, method string, req any) ([]byte, error) {
	tok, err := m.signVideo("egress", map[string]any{"roomRecord": true}, recordTokenTTL)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.httpURL+"/twirp/livekit.Egress/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("egress %s: HTTP %d: %s", method, resp.StatusCode, out)
	}
	return out, nil
}

// handleStartRecording inicia a gravação da sala e registra a gravação.
func (s *Server) handleStartRecording(w http.ResponseWriter, r *http.Request) {
	if !s.meet.recordingEnabled() {
		writeErr(w, http.StatusServiceUnavailable, "meet.recording_disabled")
		return
	}
	var req struct {
		Room   string `json:"room"`
		Layout string `json:"layout"` // grid (default) | speaker | single-speaker
	}
	if !decode(w, r, &req) || req.Room == "" {
		writeErr(w, http.StatusBadRequest, "meet.invalid_room")
		return
	}
	mu := mailUserFrom(r.Context())
	// A sala precisa ser uma reunião DESTE tenant e o solicitante ser o host. Sem isto, qualquer
	// usuário inicia o egress de uma sala de outro tenant e a gravação é guardada sob o próprio
	// tenant (e depois reproduzível por ele) — captura cross-tenant (auditoria H4).
	mtg, err := s.q.GetMeetingByRoom(r.Context(), req.Room)
	if err != nil || mtg.TenantID != mu.TenantID {
		writeErr(w, http.StatusNotFound, "meet.meeting_not_found")
		return
	}
	if mtg.OwnerUserID != mu.ID {
		writeErr(w, http.StatusForbidden, "meet.host_only")
		return
	}
	// Gravação por plano: o plano de entrada não grava; "offline"/"offline_priority" usam o
	// PIPELINE DE TRACKS (padrão — barato ao vivo, MP4 montado offline pelo worker); qualquer
	// outro valor cai no RoomComposite legado.
	recording := s.tenantPlanByID(r.Context(), mu.TenantID).Recording
	if recording == "none" {
		writeErr(w, http.StatusForbidden, "meet.recording_not_in_plan")
		return
	}
	if recording == "offline" || recording == "offline_priority" {
		priority := int32(0)
		if recording == "offline_priority" {
			priority = 1
		}
		rec, err := s.startTracksRecording(r.Context(), mu, pgtype.UUID{Bytes: mtg.ID, Valid: true}, req.Room, priority)
		if err != nil {
			log.Printf("meet: tracks recording start failed: %v", err)
			writeErr(w, http.StatusInternalServerError, "meet.record_start_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"recording_id": rec.ID, "mode": "tracks"})
		return
	}
	// Prefixo de tenant na chave: é o que faz a gravação cair na cifra POR TENANT em vez de ser
	// embrulhada pela KEK. Apagar a chave de um tenant tem de tornar ilegível tudo o que é dele,
	// gravações incluídas. As gravações antigas ficam onde estão — leem-se pela chave guardada na
	// base de dados — e serão seladas com a KEK pela passagem de selagem.
	filepath := mu.TenantID.String() + "/recordings/" + req.Room + "-" +
		strconv.FormatInt(time.Now().Unix(), 10) + ".mp4"

	egReq := map[string]any{
		"roomName": req.Room,
		"layout":   recordLayout(req.Layout),
		"fileOutputs": []map[string]any{{
			"fileType": "MP4",
			"filepath": filepath,
			"s3": map[string]any{
				"accessKey": s.meet.s3key, "secret": s.meet.s3secret, "bucket": s.meet.s3bucket,
				"endpoint": s.meet.s3endpoint, "forcePathStyle": true,
			},
		}},
	}
	out, err := s.meet.twirp(r.Context(), "StartRoomCompositeEgress", egReq)
	if err != nil {
		log.Printf("meet: egress start failed: %v", err)
		writeErr(w, http.StatusBadGateway, "meet.record_start_failed")
		return
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	egressID, _ := firstString(m, "egressId", "egress_id").(string)
	if egressID == "" {
		log.Printf("meet: egress start: no egressId in response: %s", out)
		writeErr(w, http.StatusBadGateway, "meet.record_start_failed")
		return
	}
	// Vincula à reunião já validada acima (mesmo tenant, host).
	meetingID := pgtype.UUID{Bytes: mtg.ID, Valid: true}
	rec, err := s.q.CreateRecording(r.Context(), db.CreateRecordingParams{
		TenantID: mu.TenantID, RoomName: req.Room, MeetingID: meetingID,
		EgressID: egressID, BlobKey: filepath, StartedBy: mu.Email,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.record_persist_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recording_id": rec.ID, "egress_id": egressID})
}

// handleStopRecording encerra a gravação ativa da sala.
func (s *Server) handleStopRecording(w http.ResponseWriter, r *http.Request) {
	if !s.meet.recordingEnabled() {
		writeErr(w, http.StatusServiceUnavailable, "meet.recording_disabled")
		return
	}
	var req struct {
		Room string `json:"room"`
	}
	if !decode(w, r, &req) || req.Room == "" {
		writeErr(w, http.StatusBadRequest, "meet.invalid_room")
		return
	}
	mu := mailUserFrom(r.Context())
	rec, err := s.q.GetActiveRecordingForRoom(r.Context(), req.Room)
	if err != nil || rec.TenantID != mu.TenantID {
		// Gravação inexistente OU de outro tenant → 404 (não confirma a existência cross-tenant).
		writeErr(w, http.StatusNotFound, "meet.no_active_recording")
		return
	}
	// Só o host da reunião para a gravação (auditoria H4).
	if mtg, err := s.q.GetMeetingByRoom(r.Context(), req.Room); err != nil || mtg.OwnerUserID != mu.ID {
		writeErr(w, http.StatusForbidden, "meet.host_only")
		return
	}
	if rec.Mode == "tracks" {
		s.stopTracksRecording(w, r, rec)
		return
	}
	if _, err := s.meet.twirp(r.Context(), "StopEgress", map[string]any{"egressId": rec.EgressID}); err != nil {
		writeErr(w, http.StatusBadGateway, "meet.record_stop_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
}

// handleListRecordings lista as gravações do tenant, paginadas no servidor (home do Meet).
func (s *Server) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	per, _ := strconv.Atoi(r.URL.Query().Get("per"))
	if per < 1 || per > 50 {
		per = 6
	}
	rows, err := s.q.ListRecordingsPage(r.Context(), db.ListRecordingsPageParams{
		TenantID: mu.TenantID, Limit: int32(per), Offset: int32((page - 1) * per),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.recordings_list_failed")
		return
	}
	total := int64(0)
	recs := make([]db.Recording, 0, len(rows))
	for _, rc := range rows {
		total = rc.Total
		recs = append(recs, db.Recording{
			ID: rc.ID, TenantID: rc.TenantID, MeetingID: rc.MeetingID, RoomName: rc.RoomName,
			EgressID: rc.EgressID, Status: rc.Status, BlobKey: rc.BlobKey, SizeBytes: rc.SizeBytes,
			DurationSeconds: rc.DurationSeconds, StartedBy: rc.StartedBy, StartedAt: rc.StartedAt,
			EndedAt: rc.EndedAt, Mode: rc.Mode, AssemblyStatus: rc.AssemblyStatus, Priority: rc.Priority,
			AssemblyClaimedAt: rc.AssemblyClaimedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"recordings": recs, "total": total, "page": page, "per": per})
}

// handlePlayRecording faz streaming do MP4 do MinIO (inline) para o player.
func (s *Server) handlePlayRecording(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	rec, err := s.q.GetRecordingInTenant(r.Context(), db.GetRecordingInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil || rec.Status != "ready" || rec.BlobKey == "" {
		writeErr(w, http.StatusNotFound, "meet.recording_not_ready")
		return
	}
	obj, err := s.storage.GetSeek(r.Context(), rec.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "video/mp4")
	// ServeContent honra o Range: o player pode avançar/retroceder (seek), em vez de baixar tudo.
	http.ServeContent(w, r, "recording.mp4", rec.StartedAt, obj)
}

// handleDeleteRecording apaga uma gravação: remove o blob do MinIO e a linha. Só quem iniciou a
// gravação ou o dono da reunião pode apagar (posse do conteúdo).
func (s *Server) handleDeleteRecording(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	rec, err := s.q.GetRecordingInTenant(r.Context(), db.GetRecordingInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.recording_not_found")
		return
	}
	// Posse: quem iniciou, OU o dono da reunião ligada.
	allowed := rec.StartedBy == mu.Email
	if !allowed && rec.MeetingID.Valid {
		if m, merr := s.q.GetMeetingOwned(r.Context(), db.GetMeetingOwnedParams{ID: uuid.UUID(rec.MeetingID.Bytes), OwnerUserID: mu.ID}); merr == nil {
			allowed = m.ID == uuid.UUID(rec.MeetingID.Bytes)
		}
	}
	if !allowed {
		writeErr(w, http.StatusForbidden, "meet.not_owner")
		return
	}
	if rec.BlobKey != "" && s.storage != nil {
		_ = s.storage.Remove(r.Context(), rec.BlobKey)
	}
	if err := s.q.DeleteRecording(r.Context(), db.DeleteRecordingParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.recording_delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleEgressEvent trata os eventos de gravação vindos no webhook do LiveKit. Primeiro tenta a
// tabela de TRACK egresses (pipeline offline); se o egress_id não é de track, cai no legado
// (RoomComposite em recordings.egress_id).
func (s *Server) handleEgressEvent(ctx context.Context, event string, raw []byte) {
	var ev struct {
		EgressInfo struct {
			EgressID    string `json:"egressId"`
			Status      string `json:"status"`
			FileResults []struct {
				Filename string `json:"filename"`
				Size     string `json:"size"`
				Duration string `json:"duration"`
			} `json:"fileResults"`
		} `json:"egressInfo"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil || ev.EgressInfo.EgressID == "" {
		return
	}
	eg := ev.EgressInfo
	// Sem arquivo (abortada) ou status de falha → marca como falha.
	failed := event == "egress_failed" || len(eg.FileResults) == 0 || eg.Status == "EGRESS_ABORTED" || eg.Status == "EGRESS_FAILED"
	if event != "egress_ended" && event != "egress_failed" {
		return
	}
	// Track egress? O filename final (extensão escolhida pela codec) chega aqui.
	tStatus, blob := "done", ""
	if failed {
		tStatus = "failed"
	} else {
		blob = blobKeyFromFilename(eg.FileResults[0].Filename)
	}
	if n, err := s.q.FinishRecordingTrack(ctx, db.FinishRecordingTrackParams{
		EgressID: eg.EgressID, Status: tStatus, BlobKey: blob,
	}); err == nil && n > 0 {
		return
	}
	// Legado: RoomComposite.
	switch {
	case !failed:
		size, _ := strconv.ParseInt(eg.FileResults[0].Size, 10, 64)
		ns, _ := strconv.ParseInt(eg.FileResults[0].Duration, 10, 64)
		dur := int32(ns / 1_000_000_000) // ns → s
		_ = s.q.SetRecordingReady(ctx, db.SetRecordingReadyParams{EgressID: eg.EgressID, DurationSeconds: &dur, SizeBytes: &size})
	default:
		_ = s.q.SetRecordingFailed(ctx, eg.EgressID)
	}
}

// blobKeyFromFilename normaliza o filename do EgressInfo para a chave no bucket (o LiveKit pode
// devolver o caminho absoluto local ou com o nome do bucket à frente).
//
// As chaves novas trazem o tenant à frente (`<tenant>/recordings/…`), por isso o corte é feito no
// UUID quando ele existe — cortar em "recordings/" perderia o prefixo e a gravação deixaria de ser
// cifrada com a chave do tenant.
func blobKeyFromFilename(name string) string {
	i := strings.Index(name, "recordings/")
	if i < 0 {
		return name
	}
	// Recua até ao início do segmento anterior, se for um UUID de tenant.
	if i > 0 {
		anterior := strings.LastIndex(name[:i-1], "/")
		candidato := name[anterior+1 : i-1]
		if _, err := uuid.Parse(candidato); err == nil {
			return name[anterior+1:]
		}
	}
	return name[i:]
}

// recordLayout valida o layout do RoomComposite pedido pelo host contra a lista suportada pelo
// LiveKit Egress; qualquer valor desconhecido (ou vazio) cai no "grid" para nunca falhar o egress.
func recordLayout(v string) string {
	switch v {
	case "speaker", "single-speaker", "grid":
		return v
	default:
		return "grid"
	}
}

// firstString devolve o primeiro valor não-vazio dentre as chaves dadas de um mapa JSON.
func firstString(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
