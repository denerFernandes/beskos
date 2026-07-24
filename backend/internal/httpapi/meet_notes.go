package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/chat"
	"mailin/backend/internal/db"
)

// Notas da reunião: documento compartilhado por reunião. Autosave (last-write-wins) e sincronização
// ao vivo pelo hub (mesmo WebSocket do chat) para quem está na sala. Vira tarefa via IA no Kanban.

// meetingForNotes garante que a reunião é do tenant do usuário.
func (s *Server) meetingForNotes(w http.ResponseWriter, r *http.Request) (db.Meeting, bool) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return db.Meeting{}, false
	}
	m, err := s.q.GetMeetingInTenant(r.Context(), db.GetMeetingInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return db.Meeting{}, false
	}
	return m, true
}

func (s *Server) handleGetMeetNotes(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	m, ok := s.meetingForNotes(w, r)
	if !ok {
		return
	}
	n, err := s.q.GetMeetingNotes(r.Context(), db.GetMeetingNotesParams{MeetingID: m.ID, TenantID: mu.TenantID})
	if err != nil { // ainda não há notas: devolve vazio em vez de 404
		writeJSON(w, http.StatusOK, map[string]any{"body": "", "title": m.Title})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"body": n.Body, "title": m.Title, "updated_at": iso(n.UpdatedAt)})
}

func (s *Server) handleSaveMeetNotes(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	m, ok := s.meetingForNotes(w, r)
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	n, err := s.q.UpsertMeetingNotes(r.Context(), db.UpsertMeetingNotesParams{
		MeetingID: m.ID, TenantID: mu.TenantID, Body: req.Body,
		UpdatedBy: pgtype.UUID{Bytes: mu.ID, Valid: true},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.notes_failed")
		return
	}
	s.publishNotes(r, m, n.Body, mu.ID.String())
	writeJSON(w, http.StatusOK, map[string]any{"body": n.Body, "updated_at": iso(n.UpdatedAt)})
}

// publishNotes empurra a nota para quem está NA SALA agora (identidades do LiveKit = ids de
// usuário), menos quem acabou de digitar. Sem hub/LiveKit, degrada para só salvar.
func (s *Server) publishNotes(r *http.Request, m db.Meeting, body, author string) {
	if s.hub == nil || s.meet == nil {
		return
	}
	out, err := s.meet.roomAPI(r.Context(), "ListParticipants", map[string]any{"room": m.RoomName}, m.RoomName)
	if err != nil {
		return
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
		} `json:"participants"`
	}
	if json.Unmarshal(out, &lp) != nil {
		return
	}
	rcpt := make([]string, 0, len(lp.Participants))
	for _, p := range lp.Participants {
		if p.Identity != author {
			rcpt = append(rcpt, p.Identity)
		}
	}
	if len(rcpt) == 0 {
		return
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	s.hub.Publish(r.Context(), chat.Event{Type: "notes", ChannelID: m.ID.String(), Recipients: rcpt, Payload: payload})
}
