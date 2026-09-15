package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Breakout rooms: salas paralelas dentro de uma reunião. O host cria N salas (e pode auto-distribuir
// os presentes), atribui pessoas e fecha tudo quando acabam. Cada breakout é uma sala LiveKit própria
// (room_name = <sala>-b<idx>); entrar num breakout = pedir um token dessa sala.

const maxBreakouts = 20

type breakoutDTO struct {
	ID       string   `json:"id"`
	Idx      int32    `json:"idx"`
	Name     string   `json:"name"`
	RoomName string   `json:"room_name"`
	Members  []string `json:"members"` // ids dos utilizadores atribuídos
}

// handleCreateBreakouts cria salas de breakout (host). Aceita `count` (nomes automáticos) e um
// opcional `auto_assign` para distribuir os participantes presentes em rodízio.
func (s *Server) handleCreateBreakouts(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Count      int  `json:"count"`
		AutoAssign bool `json:"auto_assign"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Count < 1 || req.Count > maxBreakouts {
		writeErr(w, http.StatusBadRequest, "meet.breakout_count")
		return
	}
	// Recria do zero: fechar os antigos evita salas órfãs e mantém a numeração previsível.
	_ = s.q.DeleteBreakouts(r.Context(), m.ID)
	created := make([]db.MeetingBreakout, 0, req.Count)
	for i := 1; i <= req.Count; i++ {
		b, cerr := s.q.CreateBreakout(r.Context(), db.CreateBreakoutParams{
			MeetingID: m.ID, TenantID: mu.TenantID, Idx: int32(i),
			Name: fmt.Sprintf("Sala %d", i), RoomName: fmt.Sprintf("%s-b%d", m.RoomName, i),
		})
		if cerr != nil {
			writeErr(w, http.StatusInternalServerError, "meet.failed")
			return
		}
		created = append(created, b)
	}
	if req.AutoAssign && s.meet != nil {
		s.autoAssign(r.Context(), m, created)
	}
	s.writeBreakouts(w, r, m.ID)
}

// autoAssign distribui os participantes presentes (menos o host) pelas salas, em rodízio.
func (s *Server) autoAssign(ctx context.Context, m db.Meeting, rooms []db.MeetingBreakout) {
	ids := s.meet.presentNonHost(ctx, m.RoomName, m.OwnerUserID.String())
	for i, uid := range ids {
		room := rooms[i%len(rooms)]
		if pid, err := uuid.Parse(uid); err == nil {
			_ = s.q.AssignBreakout(ctx, db.AssignBreakoutParams{MeetingID: m.ID, UserID: pid, BreakoutID: room.ID})
		}
	}
}

func (s *Server) handleListBreakouts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	// Qualquer membro do tenant que veja a reunião pode listar (para saber a sua própria atribuição).
	if _, err := s.q.GetMeetingInTenant(r.Context(), db.GetMeetingInTenantParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	s.writeBreakouts(w, r, id)
}

// handleAssignBreakout move um participante para uma sala (host).
func (s *Server) handleAssignBreakout(w http.ResponseWriter, r *http.Request) {
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
	bid, err := uuid.Parse(chi.URLParam(r, "bid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if _, err := s.q.GetBreakout(r.Context(), db.GetBreakoutParams{ID: bid, MeetingID: m.ID}); err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	uid, err := uuid.Parse(strings.TrimSpace(req.UserID))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if err := s.q.AssignBreakout(r.Context(), db.AssignBreakoutParams{MeetingID: m.ID, UserID: uid, BreakoutID: bid}); err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
}

// handleCloseBreakouts fecha todas as salas (host). Os clientes detetam (poll) e voltam à principal.
func (s *Server) handleCloseBreakouts(w http.ResponseWriter, r *http.Request) {
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
	_ = s.q.DeleteBreakouts(r.Context(), m.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

// handleBreakoutToken devolve um token da sala de breakout. O utilizador tem de estar atribuído a
// ela — ou ser host (que pode visitar qualquer breakout).
func (s *Server) handleBreakoutToken(w http.ResponseWriter, r *http.Request) {
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
	bid, err := uuid.Parse(chi.URLParam(r, "bid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	b, err := s.q.GetBreakout(r.Context(), db.GetBreakoutParams{ID: bid, MeetingID: m.ID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.not_found")
		return
	}
	isHost := s.isHostOf(r.Context(), m, mu.ID)
	if !isHost {
		mine, merr := s.q.GetMyBreakout(r.Context(), db.GetMyBreakoutParams{MeetingID: m.ID, UserID: mu.ID})
		if merr != nil || mine.ID != b.ID {
			writeErr(w, http.StatusForbidden, "meet.breakout_not_yours")
			return
		}
	}
	// Todos publicam plenamente dentro do breakout (é uma sala de trabalho pequena).
	tok, err := s.meet.token(mu.ID.String(), mu.Name, b.RoomName, meetTokenTTL, fullPolicy())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.token_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url": s.meet.url, "token": tok, "room": b.RoomName, "name": b.Name, "breakout_id": b.ID.String(),
	})
}

// writeBreakouts serializa as salas + membros atribuídos.
func (s *Server) writeBreakouts(w http.ResponseWriter, r *http.Request, meetingID uuid.UUID) {
	rooms, err := s.q.ListBreakouts(r.Context(), meetingID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	assigns, _ := s.q.ListBreakoutAssignments(r.Context(), meetingID)
	byRoom := map[uuid.UUID][]string{}
	for _, a := range assigns {
		byRoom[a.BreakoutID] = append(byRoom[a.BreakoutID], a.UserID.String())
	}
	out := make([]breakoutDTO, 0, len(rooms))
	for _, b := range rooms {
		out = append(out, breakoutDTO{
			ID: b.ID.String(), Idx: b.Idx, Name: b.Name, RoomName: b.RoomName, Members: byRoom[b.ID],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"breakouts": out})
}
