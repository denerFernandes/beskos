package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
)

// Co-hosts + limite por plano. Co-host = utilizador do tenant que o dono promove a anfitrião: ganha
// política plena na sala e poderes de moderação (mutar/remover). O dono continua a gerir a lista.

// meetingAsHost devolve a reunião se o utilizador for o dono OU um co-host. Base do gate das ações
// de moderação (antes só o dono passava por GetMeetingOwned).
func (s *Server) meetingAsHost(ctx context.Context, id uuid.UUID, mu db.MailUser) (db.Meeting, bool) {
	m, err := s.q.GetMeetingInTenant(ctx, db.GetMeetingInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		return db.Meeting{}, false
	}
	if m.OwnerUserID == mu.ID {
		return m, true
	}
	if ok, _ := s.q.IsCohost(ctx, db.IsCohostParams{MeetingID: id, UserID: mu.ID}); ok {
		return m, true
	}
	return db.Meeting{}, false
}

// isHostOf diz se o utilizador é dono ou co-host da reunião (para grants no join).
func (s *Server) isHostOf(ctx context.Context, m db.Meeting, userID uuid.UUID) bool {
	if m.OwnerUserID == userID {
		return true
	}
	ok, _ := s.q.IsCohost(ctx, db.IsCohostParams{MeetingID: m.ID, UserID: userID})
	return ok
}

// participantCount conta quantos estão AGORA na sala do LiveKit (para o limite por plano). Erro do
// LiveKit → -1 (o chamador decide não bloquear: a sala existir importa mais que a contagem).
func (m *meet) participantCount(ctx context.Context, room string) int {
	out, err := m.roomAPI(ctx, "ListParticipants", map[string]any{"room": room}, room)
	if err != nil {
		return -1
	}
	var lp struct {
		Participants []json.RawMessage `json:"participants"`
	}
	if err := json.Unmarshal(out, &lp); err != nil {
		return -1
	}
	return len(lp.Participants)
}

// presentNonHost devolve as identidades (ids de utilizador) presentes na sala, menos o host — base
// da auto-distribuição dos breakouts.
func (m *meet) presentNonHost(ctx context.Context, room, hostIdentity string) []string {
	out, err := m.roomAPI(ctx, "ListParticipants", map[string]any{"room": room}, room)
	if err != nil {
		return nil
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
		} `json:"participants"`
	}
	if err := json.Unmarshal(out, &lp); err != nil {
		return nil
	}
	ids := make([]string, 0, len(lp.Participants))
	for _, p := range lp.Participants {
		if p.Identity != hostIdentity {
			ids = append(ids, p.Identity)
		}
	}
	return ids
}

// --- Gestão de co-hosts (SÓ O DONO) ---

func (s *Server) handleListCohosts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if _, err := s.q.GetMeetingOwned(r.Context(), db.GetMeetingOwnedParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	ids, err := s.q.ListCohostIDs(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	out := make([]string, 0, len(ids))
	for _, uid := range ids {
		out = append(out, uid.String())
	}
	writeJSON(w, http.StatusOK, map[string]any{"cohosts": out})
}

func (s *Server) handleAddCohost(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if _, err := s.q.GetMeetingOwned(r.Context(), db.GetMeetingOwnedParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	uid, err := uuid.Parse(req.UserID)
	if err != nil || uid == mu.ID {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	// O co-host tem de ser um utilizador do mesmo tenant (não promover alguém de fora).
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: uid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusBadRequest, "meet.cohost_not_found")
		return
	}
	if err := s.q.AddCohost(r.Context(), db.AddCohostParams{MeetingID: id, UserID: uid}); err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

func (s *Server) handleRemoveCohost(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if _, err := s.q.GetMeetingOwned(r.Context(), db.GetMeetingOwnedParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	uid, err := uuid.Parse(chi.URLParam(r, "uid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	if err := s.q.RemoveCohost(r.Context(), db.RemoveCohostParams{MeetingID: id, UserID: uid}); err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
