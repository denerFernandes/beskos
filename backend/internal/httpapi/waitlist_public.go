package httpapi

// The public end of the waiting list: the form on the marketing site.
//
// It is deliberately the smallest form that still produces a useful row. Every field beyond the
// address is one more reason to close the tab, and the address alone is already enough to come
// back to somebody. The note is optional and is the one field worth reading afterwards: it says
// which kind of company is actually arriving, which is not a thing we can guess.

import (
	"log"
	"net/http"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/db"
)

type waitlistReq struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Company string `json:"company"`
	Note    string `json:"note"`
	Locale  string `json:"locale"`
	Source  string `json:"source"`
}

// handleJoinWaitlist records an address. Rate-limited by IP like every other public route.
func (s *Server) handleJoinWaitlist(w http.ResponseWriter, r *http.Request) {
	var req waitlistReq
	if !decode(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !looksLikeEmail(email) {
		writeErr(w, http.StatusBadRequest, "waitlist.invalid_email")
		return
	}
	// The form requires a name, so the API does too. An endpoint that quietly accepts what the form
	// refuses is an endpoint whose rules nobody can state.
	name := clip(req.Name, 120)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "waitlist.missing_name")
		return
	}

	entry, err := s.q.AddToWaitlist(r.Context(), db.AddToWaitlistParams{
		Email:   email,
		Name:    name,
		Company: clip(req.Company, 160),
		Note:    clip(req.Note, 2000),
		Locale:  clip(req.Locale, 12),
		Source:  clip(req.Source, 40),
	})
	if err != nil {
		log.Printf("waitlist: %s could not be recorded: %v", email, err)
		writeErr(w, http.StatusInternalServerError, "waitlist.failed")
		return
	}

	// The same answer whether the address was new or already on the list. Telling somebody their
	// address is already registered confirms, to anybody who cares to ask, who our customers are.
	log.Printf("waitlist: %s (%s) from %q", entry.Email, entry.Company, entry.Source)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// looksLikeEmail is the shallowest check that rejects what is obviously not an address.
//
// No pattern decides whether an address exists -- only sending to it does, and the invitation is
// what will. Being stricter here only rejects the valid oddities: an apostrophe, a plus, one of the
// newer top-level domains.
func looksLikeEmail(s string) bool {
	at := strings.LastIndex(s, "@")
	if at < 1 || at == len(s)-1 || len(s) > 254 {
		return false
	}
	domain := s[at+1:]
	return strings.Contains(domain, ".") && !strings.Contains(s, " ") && !strings.HasSuffix(domain, ".")
}

// clip bounds a field somebody typed. The form is public, so every string arriving here is as long
// as whoever sent it decided.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
