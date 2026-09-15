package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Meet v1.5: "organizador entra primeiro", expulsar participante, e notas por e-mail ao encerrar.
// Em todos: só o DONO da sala manda — a query filtra por owner_user_id, que é a checagem real.

// hostPresent diz se o organizador está conectado na sala AGORA. É o LiveKit que sabe: a nossa
// base só sabe quem foi convidado, não quem está lá dentro.
func (m *meet) hostPresent(ctx context.Context, room string, hostIdentities ...string) (bool, error) {
	out, err := m.roomAPI(ctx, "ListParticipants", map[string]any{"room": room}, room)
	if err != nil {
		return false, err
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
		} `json:"participants"`
	}
	if err := json.Unmarshal(out, &lp); err != nil {
		return false, err
	}
	hosts := make(map[string]bool, len(hostIdentities))
	for _, h := range hostIdentities {
		hosts[h] = true
	}
	for _, p := range lp.Participants {
		if hosts[p.Identity] {
			return true, nil
		}
	}
	return false, nil
}

// kickReq identifica quem sai pela identity do LiveKit (que é o nosso user id).
type kickReq struct {
	Identity string `json:"identity"`
}

// handleKickParticipant expulsa alguém da sala. Só o dono; e ele não pode expulsar-se a si mesmo
// (sairia da própria sala por engano, e com a tranca ligada ninguém mais entrava).
func (s *Server) handleKickParticipant(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	var req kickReq
	if !decode(w, r, &req) {
		return
	}
	m, ok := s.meetingAsHost(r.Context(), id, mu)
	if !ok {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	if req.Identity == "" || req.Identity == mu.ID.String() {
		writeErr(w, http.StatusBadRequest, "meet.cannot_kick_self")
		return
	}
	if _, err := s.meet.roomAPI(r.Context(), "RemoveParticipant",
		map[string]any{"room": m.RoomName, "identity": req.Identity}, m.RoomName); err != nil {
		slog.Error("meet: remove participant", "room", m.RoomName, "identity", req.Identity, "err", err)
		writeErr(w, http.StatusInternalServerError, "meet.kick_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleMuteParticipant silencia (server-side) o microfone de um participante. O host manda; o
// LiveKit exige o track_sid do áudio, que buscamos na lista de participantes da sala.
func (s *Server) handleMuteParticipant(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	var req struct {
		Identity string `json:"identity"`
	}
	if !decode(w, r, &req) {
		return
	}
	m, ok := s.meetingAsHost(r.Context(), id, mu)
	if !ok {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	if req.Identity == "" {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	// Acha o track de microfone do participante na sala.
	out, err := s.meet.roomAPI(r.Context(), "ListParticipants", map[string]any{"room": m.RoomName}, m.RoomName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
			Tracks   []struct {
				Sid    string `json:"sid"`
				Type   string `json:"type"`
				Source string `json:"source"`
			} `json:"tracks"`
		} `json:"participants"`
	}
	_ = json.Unmarshal(out, &lp)
	trackSid := ""
	for _, p := range lp.Participants {
		if p.Identity != req.Identity {
			continue
		}
		for _, tr := range p.Tracks {
			if tr.Type == "AUDIO" || tr.Source == "MICROPHONE" {
				trackSid = tr.Sid
				break
			}
		}
	}
	if trackSid == "" {
		writeErr(w, http.StatusNotFound, "meet.no_audio_track")
		return
	}
	if _, err := s.meet.roomAPI(r.Context(), "MutePublishedTrack",
		map[string]any{"room": m.RoomName, "identity": req.Identity, "track_sid": trackSid, "muted": true}, m.RoomName); err != nil {
		slog.Error("meet: mute participant", "room", m.RoomName, "identity", req.Identity, "err", err)
		writeErr(w, http.StatusInternalServerError, "meet.mute_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "muted"})
}

// handleMeetParticipants lista quem está NA SALA agora (via LiveKit), para o painel do host poder
// expulsar. A lista da nossa base é de convidados — não é a mesma coisa.
func (s *Server) handleMeetParticipants(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	m, err := s.q.GetMeetingInTenant(r.Context(), db.GetMeetingInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	out, err := s.meet.roomAPI(r.Context(), "ListParticipants", map[string]any{"room": m.RoomName}, m.RoomName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
			Name     string `json:"name"`
		} `json:"participants"`
	}
	_ = json.Unmarshal(out, &lp)
	// Co-hosts também são "host" na lista (badge + poderes na UI).
	hosts := map[string]bool{m.OwnerUserID.String(): true}
	if cohosts, cerr := s.q.ListCohostIDs(r.Context(), m.ID); cerr == nil {
		for _, c := range cohosts {
			hosts[c.String()] = true
		}
	}
	list := make([]map[string]any, 0, len(lp.Participants))
	for _, p := range lp.Participants {
		list = append(list, map[string]any{
			"identity": p.Identity, "name": p.Name,
			"is_host":    p.Identity == m.OwnerUserID.String(),
			"is_cohost":  hosts[p.Identity] && p.Identity != m.OwnerUserID.String(),
			"can_manage": hosts[p.Identity],
		})
	}
	writeJSON(w, http.StatusOK, list)
}

// handleSendMeetNotes manda as notas da reunião por e-mail aos participantes. Só o dono dispara —
// as notas são da sala dele.
func (s *Server) handleSendMeetNotes(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	m, ok := s.meetingAsHost(r.Context(), id, mu)
	if !ok {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	notes, err := s.q.GetMeetingNotes(r.Context(), db.GetMeetingNotesParams{MeetingID: m.ID, TenantID: mu.TenantID})
	if err != nil || strings.TrimSpace(notes.Body) == "" {
		// Sem notas não há o que enviar — e um e-mail vazio é pior que nenhum.
		writeErr(w, http.StatusBadRequest, "meet.no_notes")
		return
	}
	if s.email == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.email_disabled")
		return
	}
	people, _ := s.q.ListParticipants(r.Context(), m.ID)
	sent, failed := s.mailNotes(r.Context(), m, notes.Body, people)
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "failed": failed})
}

// mailNotes envia a cada participante. Best-effort e contado: quem chamou fica a saber quantos
// saíram e quantos falharam, em vez de um "ok" que esconde metade dos e-mails perdidos.
func (s *Server) mailNotes(ctx context.Context, m db.Meeting, body string, people []db.MeetingParticipant) (int, int) {
	subject := fmt.Sprintf("[%s] %s", m.Title, "Notas da reunião")
	text := strings.TrimSpace(fmt.Sprintf("Notas da reunião \"%s\"\n\n%s\n\n---\nBeskos", m.Title, body))
	var sent, failed int
	seen := map[string]bool{}
	for _, p := range people {
		if p.Email == "" || seen[strings.ToLower(p.Email)] {
			continue
		}
		seen[strings.ToLower(p.Email)] = true
		if err := s.email.Send(p.Email, subject, text); err != nil {
			slog.Error("meet notes email failed", "to", p.Email, "err", err)
			failed++
			continue
		}
		sent++
	}
	return sent, failed
}

// lockedOut: com "organizador entra primeiro", quem não é o host espera até ele chegar.
func (s *Server) lockedOut(ctx context.Context, m db.Meeting, isHost bool) bool {
	if isHost || s.meet == nil {
		return false
	}
	// Sala trancada a novas entradas: ninguém novo entra (só o host, já tratado acima).
	if m.Locked {
		return true
	}
	if !m.LockUntilHost {
		return false
	}
	// Qualquer host abre a sala: o dono ou um co-host presente já libera a entrada dos demais.
	hostIDs := []string{m.OwnerUserID.String()}
	if cohosts, cerr := s.q.ListCohostIDs(ctx, m.ID); cerr == nil {
		for _, c := range cohosts {
			hostIDs = append(hostIDs, c.String())
		}
	}
	present, err := s.meet.hostPresent(ctx, m.RoomName, hostIDs...)
	if err != nil {
		// Se o LiveKit não responde, não trancamos ninguém de fora: a sala existir é mais
		// importante que a tranca, e o erro é nosso, não do convidado.
		slog.Error("meet: host presence check failed", "room", m.RoomName, "err", err)
		return false
	}
	return !present
}
