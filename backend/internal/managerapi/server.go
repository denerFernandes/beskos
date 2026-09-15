// Package managerapi é a API HTTP do Manager — o painel do OPERADOR do SaaS (docs/manager.md).
// Totalmente separada da httpapi do produto: utilizadores, sessões, cookie e porta próprios.
// TOTP é obrigatório para todos os operadores; todo write gera linha em manager_audit.
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
	sessionTTL    = 8 * time.Hour // ferramenta de operação: sessão curta por desenho (doc §6)
)

type Server struct {
	q            *db.Queries
	box          *secretbox.Box // cifra o segredo TOTP em repouso (MFA_ENC_KEY)
	cookieSecure bool
	loginIP      *rateLimiter
	loginUser    *rateLimiter
	// billing espelha o catálogo de planos no provedor (Stripe): gravar plano → Product/Price lá.
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
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	r.Route("/api/manager", func(r chi.Router) {
		r.Post("/login", s.handleLogin)
		r.Group(func(r chi.Router) {
			r.Use(s.requireSession)
			r.Get("/me", s.handleMe)
			r.Post("/logout", s.handleLogout)
			// Visão read-only (M1).
			r.Get("/overview", s.handleOverview)
			r.Get("/tenants", s.handleListTenants)
			r.Get("/tenants/{id}", s.handleGetTenant)
			// Metering (M3): rollup diário escrito pelo worker.
			r.Get("/tenants/{id}/usage", s.handleTenantUsage)
			r.Get("/usage/series", s.handleUsageSeries)
			r.Get("/alerts", s.handleAlerts)
			r.Get("/deliverability", s.handleDeliverability)
			// Relatórios + exports (M5).
			r.Get("/reports/growth", s.handleGrowth)
			r.Get("/export/tenants.csv", s.handleExportTenants)
			r.Get("/export/usage.csv", s.handleExportUsage)
			r.Get("/export/finance.csv", s.handleExportFinance)
			// Operadores (M5). Escritas admin-only (checado nos handlers).
			// The waiting list: who asked to be told, and the invitation.
			r.Get("/waitlist", s.handleListWaitlist)
			r.Post("/waitlist/{id}/invite", s.handleInviteWaitlist)
			r.Post("/waitlist/{id}/status", s.handleSetWaitlistStatus)
			r.Delete("/waitlist/{id}", s.handleDeleteWaitlist)

			r.Get("/operators", s.handleListOperators)
			r.Post("/operators", s.handleCreateOperator)
			r.Post("/operators/{id}/active", s.handleSetOperatorActive)
			// Servidores (custo fixo por VPS). Escritas admin-only.
			r.Get("/servers", s.handleListServers)
			r.Put("/servers", s.handleSaveServer)
			r.Post("/servers/{id}/active", s.handleSetServerActive)
			r.Delete("/servers/{id}", s.handleDeleteServer)
			// Finanças (M4): resumo + margem por tenant; modelo de custos editável.
			r.Get("/finance", s.handleFinance)
			r.Get("/settings/costs", s.handleGetCosts)
			r.Put("/settings/costs", s.handleSaveCosts)
			// Planos (M2). Escritas exigem role=admin (checado no handler).
			r.Get("/plans", s.handleListPlans)
			r.Put("/plans", s.handleSavePlan)
			r.Post("/plans/{key}/active", s.handleSetPlanActive)
			r.Post("/tenants/{id}/plan", s.handleSetTenantPlan)
			r.Post("/tenants/{id}/currency", s.handleSetTenantCurrency)
		})
	})
	return r
}

// ---- sessão ----

type ctxKey string

const ctxManagerUser ctxKey = "manager_user"

// managerUserFrom devolve o operador autenticado posto no contexto pelo requireSession.
func managerUserFrom(ctx context.Context) db.GetManagerSessionRow {
	u, _ := ctx.Value(ctxManagerUser).(db.GetManagerSessionRow)
	return u
}

// requireSession valida o cookie de sessão e injeta o operador no contexto. 401 sem sessão válida.
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

// audit regista uma ação no manager_audit (best-effort: falha de audit não derruba a request,
// mas fica no log do servidor).
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

// ---- helpers HTTP (mesmo contrato da httpapi: error_code estável + fallback em inglês) ----

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
