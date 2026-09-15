package httpapi

import "net/http"

// handleGetEdition tells the frontend which capabilities this instance has
// unlocked, so it can gate Pro-only UI without guessing from feature flags.
// Unauthenticated on purpose: the signup and login pages need it too.
func (s *Server) handleGetEdition(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.edition)
}
