// Package managerapi is the Manager HTTP API — the SaaS OPERATOR's admin panel backend.
// Fully separate from the product's httpapi: its own users, sessions, cookie, and port.
// TOTP is mandatory for all operators; every write logs a row in manager_audit.
package managerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/notify"
	"github.com/beskosorg/beskos/backend/internal/secretbox"
)

const (
	sessionCookie = "mgr_session"
	sessionTTL    = 8 * time.Hour // operations tool: short session by design (doc §6)
)

type Server struct {
	q            *db.Queries
	box          *secretbox.Box // encrypts the TOTP secret at rest (MFA_ENC_KEY)
	cookieSecure bool
	loginIP      *rateLimiter
	loginUser    *rateLimiter
	// billing mirrors the plan catalog to the provider (Stripe): saving a plan → Product/Price there.
	billing billing.Provider
	// email sends the waiting-list invitation. nil = not configured, and the endpoint says so
	// rather than reporting somebody invited who was never written to.
	email *notify.Email
	// appURL is where the invitation link points: the product, not the manager.
	appURL string
}

// EnableEmail wires the sender used for waiting-list invitations.
func (s *Server) EnableEmail(e *notify.Email, appURL string) { s.email, s.appURL = e, appURL }

func NewServer(q *db.Queries, encKey string, cookieSecure bool, bp billing.Provider) *Server {
	return &Server{
		q:            q,
		box:          secretbox.New(encKey),
		cookieSecure: cookieSecure,
		billing:      bp,
		loginIP:      newRateLimiter(30, 5*time.Minute),
		loginUser:    newRateLimiter(8, 5*time.Minute),
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// ClientIPFromXFFTrustedProxies(1): exactly one reverse proxy (Caddy) sits in front of this
	// service. The deprecated middleware.RealIP trusted the leftmost X-Forwarded-For entry, which
	// a client's own header can forge; this trusts only the entry Caddy itself appended.
	r.Use(middleware.ClientIPFromXFFTrustedProxies(1))
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	r.Route("/api/manager", func(r chi.Router) {
		r.Post("/login", s.handleLogin)
		r.Group(func(r chi.Router) {
			r.Use(s.requireSession)
			r.Get("/me", s.handleMe)
			r.Post("/logout", s.handleLogout)
			// Read-only overview.
			r.Get("/overview", s.handleOverview)
			r.Get("/tenants", s.handleListTenants)
			r.Get("/tenants/{id}", s.handleGetTenant)
			// Metering: daily rollup written by the worker.
			r.Get("/tenants/{id}/usage", s.handleTenantUsage)
			r.Get("/usage/series", s.handleUsageSeries)
			r.Get("/alerts", s.handleAlerts)
			r.Get("/deliverability", s.handleDeliverability)
			// Reports and CSV exports.
			r.Get("/reports/growth", s.handleGrowth)
			r.Get("/export/tenants.csv", s.handleExportTenants)
			r.Get("/export/usage.csv", s.handleExportUsage)
			r.Get("/export/finance.csv", s.handleExportFinance)
			// The waiting list: who asked to be told, and the invitation.
			r.Get("/waitlist", s.handleListWaitlist)
			r.Post("/waitlist/{id}/invite", s.handleInviteWaitlist)
			r.Post("/waitlist/{id}/status", s.handleSetWaitlistStatus)
			r.Delete("/waitlist/{id}", s.handleDeleteWaitlist)

			// Operators. Writes are admin-only, checked in the handlers.
			r.Get("/operators", s.handleListOperators)
			r.Post("/operators", s.handleCreateOperator)
			r.Post("/operators/{id}/active", s.handleSetOperatorActive)
			// Infra servers (fixed cost per VPS). Writes are admin-only.
			r.Get("/servers", s.handleListServers)
			r.Put("/servers", s.handleSaveServer)
			r.Post("/servers/{id}/active", s.handleSetServerActive)
			r.Delete("/servers/{id}", s.handleDeleteServer)
			// Finance: summary and margin per tenant; the cost model is editable.
			r.Get("/finance", s.handleFinance)
			r.Get("/settings/costs", s.handleGetCosts)
			r.Put("/settings/costs", s.handleSaveCosts)
			// Plans. Writes require role=admin, checked in the handler.
			r.Get("/plans", s.handleListPlans)
			r.Put("/plans", s.handleSavePlan)
			r.Post("/plans/{key}/active", s.handleSetPlanActive)
			r.Post("/tenants/{id}/plan", s.handleSetTenantPlan)
			r.Post("/tenants/{id}/currency", s.handleSetTenantCurrency)
		})
	})
	return r
}

// ---- session ----

type ctxKey string

const ctxManagerUser ctxKey = "manager_user"

// managerUserFrom returns the authenticated operator placed in the context by requireSession.
func managerUserFrom(ctx context.Context) db.GetManagerSessionRow {
	u, _ := ctx.Value(ctxManagerUser).(db.GetManagerSessionRow)
	return u
}

// requireSession validates the session cookie and injects the operator into the context.
// Responds 401 without a valid session.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			writeErr(w, http.StatusUnauthorized, "manager.unauthorized")
			return
		}
		row, err := s.q.GetManagerSession(r.Context(), c.Value)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "manager.unauthorized")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxManagerUser, row)))
	})
}

// audit records an action in manager_audit (best-effort: an audit failure doesn't bring down the
// request, but it lands in the server log).
func (s *Server) audit(ctx context.Context, u db.GetManagerSessionRow, action, target string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil || payload == nil {
		raw = []byte("{}")
	}
	_ = s.q.InsertManagerAudit(ctx, db.InsertManagerAuditParams{
		ActorID: pgtype.UUID{Bytes: u.ID, Valid: true}, ActorEmail: u.Email,
		Action: action, Target: target, Payload: raw,
	})
}

// ---- HTTP helpers (same contract as httpapi: stable error_code + English fallback) ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error_code": code, "error": humanizeCode(code)})
}

// humanizeCode turns "manager.invalid_credentials" into "Invalid credentials" (English fallback).
func humanizeCode(code string) string {
	s := code
	if i := strings.LastIndex(code, "."); i >= 0 {
		s = code[i+1:]
	}
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return "Error"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "request.invalid_json")
		return false
	}
	return true
}
