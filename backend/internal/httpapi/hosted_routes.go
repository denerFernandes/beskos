package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountHostedPublic(r chi.Router) {
	// The waiting list, posted from the marketing site. Same cross-origin shape as mail-audit above,
	// including the registered preflight -- for exactly the reason written there.
	r.With(publicCORS, s.publicRateLimit).Post("/public/waitlist", s.handleJoinWaitlist)
	r.With(publicCORS).Options("/public/waitlist", func(http.ResponseWriter, *http.Request) {})
	// The contact form, relayed to the sales mailbox. Same cross-origin shape, same registered
	// preflight.
	r.With(publicCORS, s.publicRateLimit).Post("/public/contact", s.handleContactForm)
	r.With(publicCORS).Options("/public/contact", func(http.ResponseWriter, *http.Request) {})
	// SES events require an SNS signature, verified by the handler.
	r.With(s.publicRateLimit).Post("/webhooks/ses", s.handleSESWebhook)
}
