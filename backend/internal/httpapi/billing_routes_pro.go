//go:build pro

package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/beskosorg/beskos/backend/ee"
	"github.com/beskosorg/beskos/backend/internal/rbac"
)

func (s *Server) mountBilling(r chi.Router) {
	r.With(s.requirePerm(rbac.PermBillingView)).Get("/billing", func(w http.ResponseWriter, r *http.Request) {
		ee.GetBilling(w, r, s.tenantPlan(r, mailUserFrom(r.Context()).TenantID), s.billingDeps())
	})
	r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/checkout", func(w http.ResponseWriter, r *http.Request) { ee.Checkout(w, r, s.billingDeps()) })
	r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/cancel", func(w http.ResponseWriter, r *http.Request) { ee.BillingCancel(w, r, s.billingDeps()) })
	r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/resume", func(w http.ResponseWriter, r *http.Request) { ee.BillingResume(w, r, s.billingDeps()) })
	r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/portal", func(w http.ResponseWriter, r *http.Request) { ee.BillingPortal(w, r, s.billingDeps()) })
	// Billing data (who's subscribing, where the invoice goes) + VAT simulation.
	r.With(s.requirePerm(rbac.PermBillingView)).Get("/billing/profile", func(w http.ResponseWriter, r *http.Request) { ee.GetBillingProfile(w, r, s.billingDeps()) })
	r.With(s.requirePerm(rbac.PermBillingManage)).Put("/billing/profile", func(w http.ResponseWriter, r *http.Request) { ee.SaveBillingProfile(w, r, s.billingDeps()) })
	r.With(s.requirePerm(rbac.PermBillingView)).Post("/billing/quote", func(w http.ResponseWriter, r *http.Request) { ee.BillingQuote(w, r, s.billingDeps()) })
}

func (s *Server) mountPublicBilling(r chi.Router) {
	r.Post("/billing/webhook", func(w http.ResponseWriter, r *http.Request) { ee.BillingWebhook(w, r, s.billingDeps()) })
	// Self-service signup (/signup): catalog, VAT simulation, and creating the organization.
	r.With(s.publicRateLimit).Get("/public/plans", func(w http.ResponseWriter, r *http.Request) { ee.PublicPlans(w, r, s.billingDeps()) })
	r.With(s.publicRateLimit).Post("/public/quote", func(w http.ResponseWriter, r *http.Request) { ee.PublicQuote(w, r, s.billingDeps()) })
	r.With(s.publicRateLimit).Post("/public/signup", func(w http.ResponseWriter, r *http.Request) { ee.Signup(w, r, s.billingDeps()) })
}
