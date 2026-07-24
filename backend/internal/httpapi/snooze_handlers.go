package httpapi

import (
	"net/http"
	"time"

	"mailin/backend/internal/db"
)

// Snooze: o cliente move a mensagem para a pasta "Snoozed" (JMAP, com a própria sessão) e regista aqui
// a hora de despertar. O worker devolve-a à Caixa de entrada em wake_at, mesmo com o app fechado.
func (s *Server) handleSnoozeEmail(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		EmailID string `json:"email_id"`
		WakeAt  string `json:"wake_at"` // RFC3339
	}
	if !decode(w, r, &req) {
		return
	}
	wake, err := time.Parse(time.RFC3339, req.WakeAt)
	if err != nil || req.EmailID == "" {
		writeErr(w, http.StatusBadRequest, "snooze.invalid")
		return
	}
	if wake.Before(time.Now()) {
		writeErr(w, http.StatusBadRequest, "snooze.past")
		return
	}
	if _, err := s.q.CreateSnooze(r.Context(), db.CreateSnoozeParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, EmailID: req.EmailID, WakeAt: wake,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "snooze.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "snoozed"})
}
