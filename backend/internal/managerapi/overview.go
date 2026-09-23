package managerapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Read-only overview: dashboard totals, the tenant list with counts, and a single tenant's detail
// with its users. The manager only READS tenant data — it never writes it.

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	row, err := s.q.ManagerOverview(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ManagerListTenants(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "manager.invalid_id")
		return
	}
	tenant, err := s.q.ManagerGetTenant(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "manager.tenant_not_found")
		return
	}
	users, err := s.q.ManagerListTenantUsers(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenant": tenant, "users": orEmpty(users)})
}

// handleTenantUsage returns the tenant's daily rollup for the last N days (default 30, max 365).
func (s *Server) handleTenantUsage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "manager.invalid_id")
		return
	}
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 365 {
		days = v
	}
	since := pgtype.Date{Time: time.Now().AddDate(0, 0, -days), Valid: true}
	rows, err := s.q.ManagerTenantUsage(r.Context(), db.ManagerTenantUsageParams{TenantID: id, Since: since})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

// handleUsageSeries returns the usage series aggregated across all tenants, for the dashboard.
func (s *Server) handleUsageSeries(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 365 {
		days = v
	}
	since := pgtype.Date{Time: time.Now().AddDate(0, 0, -days), Valid: true}
	rows, err := s.q.ManagerUsageSeries(r.Context(), since)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

// handleDeliverability returns sent/bounces/complaints over 30 days per tenant (the Analytics table).
func (s *Server) handleDeliverability(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ManagerDeliverability(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}
