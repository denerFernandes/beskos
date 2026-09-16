package httpapi

import (
	"github.com/beskosorg/beskos/backend/internal/rbac"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountBilling(r chi.Router) {
	r.With(s.requirePerm(rbac.PermBillingView)).Get("/billing", s.handleGetBilling)
	r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/checkout", s.handleCheckout)
	r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/cancel", s.handleBillingCancel)
	r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/resume", s.handleBillingResume)
	r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/portal", s.handleBillingPortal)
	// Billing data (who's subscribing, where the invoice goes) + VAT simulation.
	r.With(s.requirePerm(rbac.PermBillingView)).Get("/billing/profile", s.handleGetBillingProfile)
	r.With(s.requirePerm(rbac.PermBillingManage)).Put("/billing/profile", s.handleSaveBillingProfile)
	r.With(s.requirePerm(rbac.PermBillingView)).Post("/billing/quote", s.handleBillingQuote)
}

func (s *Server) mountPublicBilling(r chi.Router) {
	r.Post("/billing/webhook", s.handleBillingWebhook)
	// Self-service signup (/signup): catalog, VAT simulation, and creating the organization.
	r.With(s.publicRateLimit).Get("/public/plans", s.handlePublicPlans)
	r.With(s.publicRateLimit).Post("/public/quote", s.handlePublicQuote)
	r.With(s.publicRateLimit).Post("/public/signup", s.handlePublicSignup)
}
