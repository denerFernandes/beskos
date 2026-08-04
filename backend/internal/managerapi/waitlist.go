package managerapi

// The waiting list, from the operator's side: who is queued, and inviting them in.
//
// The list is the closest thing there is to a demand signal at this stage. It is ordered oldest
// first and the pending ones float to the top, because the only fair way to run a queue is to
// invite the person who has waited longest -- and because any other order is how somebody gets
// quietly forgotten at the bottom of it.

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
	"mailin/backend/internal/notify"
)

// handleListWaitlist returns the queue and its counts.
func (s *Server) handleListWaitlist(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "", "pending", "invited", "joined":
	default:
		writeErr(w, http.StatusBadRequest, "waitlist.invalid_status")
		return
	}
	rows, err := s.q.ListWaitlist(r.Context(), status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	counts, cerr := s.q.CountWaitlist(r.Context())
	if cerr != nil {
		// The list is the point; the counters are decoration. Losing them must not lose the screen.
		log.Printf("waitlist: counts unavailable: %v", cerr)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": orEmpty(rows),
		"counts": map[string]int64{
			"total": counts.Total, "pending": counts.Pending,
			"invited": counts.Invited, "joined": counts.Joined,
		},
	})
}

// handleInviteWaitlist sends the invitation and records that it went.
//
// The row is marked invited ONLY after the message is accepted for delivery. Marking first would
// produce a list that says somebody was invited when nothing ever reached them -- and they would
// wait forever for a message that was never sent, with our own screen saying otherwise.
func (s *Server) handleInviteWaitlist(w http.ResponseWriter, r *http.Request) {
	me := managerUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "waitlist.not_found")
		return
	}
	entry, err := s.q.GetWaitlistEntry(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "waitlist.not_found")
		return
	}
	if s.email == nil {
		writeErr(w, http.StatusServiceUnavailable, "waitlist.email_unavailable")
		return
	}

	token, err := inviteToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	url := strings.TrimRight(s.appURL, "/") + "/signup?invite=" + token
	msg := notify.WaitlistInvite(entry.Locale, entry.Name, url)
	if err := s.email.SendHTML(entry.Email, msg.Subject, msg.HTML, msg.Text); err != nil {
		log.Printf("waitlist: inviting %s failed: %v", entry.Email, err)
		writeErr(w, http.StatusBadGateway, "waitlist.send_failed")
		return
	}

	updated, err := s.q.MarkWaitlistInvited(r.Context(), db.MarkWaitlistInvitedParams{
		ID: id, InviteToken: token, InvitedBy: me.Email,
	})
	if err != nil {
		// The message HAS gone. Saying so in the log is all that is left, and it matters: the row
		// still reads 'pending', so somebody will send a second invitation to a person who has one.
		log.Printf("waitlist: %s WAS INVITED but the row could not be updated: %v", entry.Email, err)
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), me, "waitlist_invited", entry.Email, nil)
	writeJSON(w, http.StatusOK, updated)
}

// handleSetWaitlistStatus moves an entry by hand -- marking somebody as joined, or putting them
// back in the queue after an invitation bounced.
func (s *Server) handleSetWaitlistStatus(w http.ResponseWriter, r *http.Request) {
	me := managerUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "waitlist.not_found")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch req.Status {
	case "pending", "invited", "joined":
	default:
		writeErr(w, http.StatusBadRequest, "waitlist.invalid_status")
		return
	}
	updated, err := s.q.SetWaitlistStatus(r.Context(), db.SetWaitlistStatusParams{ID: id, Status: req.Status})
	if err != nil {
		writeErr(w, http.StatusNotFound, "waitlist.not_found")
		return
	}
	s.audit(r.Context(), me, "waitlist_status_set", updated.Email, map[string]any{"status": req.Status})
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteWaitlist removes an entry: somebody asking to be forgotten, or an obvious robot.
func (s *Server) handleDeleteWaitlist(w http.ResponseWriter, r *http.Request) {
	me := managerUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "waitlist.not_found")
		return
	}
	entry, gerr := s.q.GetWaitlistEntry(r.Context(), id)
	if gerr != nil {
		writeErr(w, http.StatusNotFound, "waitlist.not_found")
		return
	}
	if err := s.q.DeleteWaitlistEntry(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), me, "waitlist_deleted", entry.Email, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// inviteToken is long enough to be worth nothing to guess. It identifies a click, not an
// entitlement: signup does not check it today.
func inviteToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
