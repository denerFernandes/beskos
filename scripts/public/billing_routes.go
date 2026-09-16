package httpapi

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/billing"
)

// Self-hosted installations have no hosted billing or checkout routes.
func (s *Server) mountBilling(chi.Router)       {}
func (s *Server) mountPublicBilling(chi.Router) {}
func (s *Server) syncSeats(uuid.UUID)           {}

// Community access does not depend on a hosted subscription or trial deadline.
func (s *Server) accountState(context.Context, uuid.UUID) billing.Account {
	return billing.Account{State: billing.StateOK}
}
