package httpapi

// The contact form on the marketing site.
//
// It sends a message to the sales mailbox and stores nothing. That is deliberate: the whole product
// is mail, the sales address is a real mailbox somebody reads, and putting a second copy in a table
// would create an inbox nobody watches beside one they already do.
//
// The consequence is that a failed send must be REPORTED, not swallowed. A contact form that
// answers "thank you" while the message went nowhere loses the enquiry and the customer, and
// neither of them ever finds out. When it fails the page says so and shows the address, which is
// the one route that always works.

import (
	"log"
	"net/http"
	"strings"

	"mailin/backend/internal/notify"
)

type contactFormReq struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Company string `json:"company"`
	Message string `json:"message"`
	Locale  string `json:"locale"`
}

// handleContactForm relays an enquiry to the sales mailbox.
func (s *Server) handleContactForm(w http.ResponseWriter, r *http.Request) {
	var req contactFormReq
	if !decode(w, r, &req) {
		return
	}
	name := clip(req.Name, 120)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	message := clip(req.Message, 8000)
	if name == "" || message == "" || !looksLikeEmail(email) {
		writeErr(w, http.StatusBadRequest, "contact.missing_fields")
		return
	}
	if s.email == nil || s.salesEmail == "" {
		// Nothing can carry the message, so nothing is promised. The page falls back to showing the
		// address itself.
		writeErr(w, http.StatusServiceUnavailable, "contact.unavailable")
		return
	}

	msg := notify.ContactEnquiry(name, email, clip(req.Company, 160), clip(req.Locale, 12), message)
	// Reply-To is the enquirer, so answering is one click and the reply goes to them rather than to
	// the mailbox that relayed it.
	if err := s.email.SendReplyTo(s.salesEmail, email, msg.Subject, msg.HTML, msg.Text); err != nil {
		log.Printf("contact: enquiry from %s could not be delivered to %s: %v", email, s.salesEmail, err)
		writeErr(w, http.StatusBadGateway, "contact.send_failed")
		return
	}
	log.Printf("contact: enquiry from %s (%s) relayed to %s", email, req.Company, s.salesEmail)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
