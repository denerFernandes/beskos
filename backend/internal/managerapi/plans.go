package managerapi

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// Plan management. Only operators with role=admin can write; viewers can read. Every write is
// audited (before/after when applicable). The golden rule: the manager writes to `plans`, the
// product only reads (with a 1-minute cache — changes propagate on their own).

var planKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,31}$`)

// appsRail mirrors the product's toggleableApps (apps a plan can turn on/off).
var appsRail = []string{"today", "mail", "chat", "kanban", "cal", "files", "contacts", "meet", "wiki"}

// requireAdmin blocks viewer operators on write routes.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if managerUserFrom(r.Context()).Role != "admin" {
		writeErr(w, http.StatusForbidden, "manager.admin_only")
		return false
	}
	return true
}

// planView exposes the plan with the per-currency prices DECODED: db.Plan.Prices is []byte (JSONB)
// and encoding/json serialized it as base64 — the UI needs the object {"USD":1900,...}.
type planView struct {
	db.Plan
	Prices map[string]int `json:"prices"`
}

func planViewOf(p db.Plan) planView {
	m := map[string]int{}
	_ = json.Unmarshal(p.Prices, &m)
	return planView{Plan: p, Prices: m}
}

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListPlans(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	out := make([]planView, 0, len(rows))
	for _, p := range rows {
		out = append(out, planViewOf(p))
	}
	writeJSON(w, http.StatusOK, out)
}

type planReq struct {
	Key                    string         `json:"key"`
	Name                   string         `json:"name"`
	Description            string         `json:"description"`
	PriceCentsPerUser      int32          `json:"price_cents_per_user"`
	MinUsers               int32          `json:"min_users"`
	MaxUsers               int32          `json:"max_users"`
	AppsEnabled            []string       `json:"apps_enabled"`
	MaxStorageGb           int32          `json:"max_storage_gb"`
	AiTokensPerWindow      int32          `json:"ai_tokens_per_window"`
	MeetMaxParticipants    int32          `json:"meet_max_participants"`
	MeetMaxMinutes         int32          `json:"meet_max_minutes"`
	Recording              string         `json:"recording"`
	RecordingRetentionDays int32          `json:"recording_retention_days"`
	MaxDailySend           int32          `json:"max_daily_send"`
	OnlyofficeEdit         bool           `json:"onlyoffice_edit"`
	WikiPublicLinks        bool           `json:"wiki_public_links"`
	Sort                   int32          `json:"sort"`
	Prices                 map[string]int `json:"prices"` // price per currency (cents); keys BRL/USD/EUR
}

// handleSavePlan creates/updates a plan (upsert by key). Validates the key, the recording mode, and
// the apps (only those in the rail; empty = all on).
func (s *Server) handleSavePlan(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req planReq
	if !decode(w, r, &req) {
		return
	}
	if !planKeyRe.MatchString(req.Key) || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "manager.plan_invalid")
		return
	}
	if req.Recording != "none" && req.Recording != "offline" && req.Recording != "offline_priority" {
		writeErr(w, http.StatusBadRequest, "manager.plan_invalid")
		return
	}
	if req.MaxUsers > 0 && req.MinUsers > req.MaxUsers {
		writeErr(w, http.StatusBadRequest, "manager.plan_invalid")
		return
	}
	valid := map[string]bool{}
	for _, a := range appsRail {
		valid[a] = true
	}
	apps := []string{}
	for _, a := range req.AppsEnabled {
		if valid[a] {
			apps = append(apps, a)
		}
	}
	// All apps checked = no restriction; normalizes to empty (doesn't lock out future apps).
	if len(apps) == len(appsRail) {
		apps = []string{}
	}
	// Currency per country: supported currencies only, non-negative values; empty removes that currency's price.
	prices := map[string]int{}
	for cur, cents := range req.Prices {
		cur = strings.ToUpper(strings.TrimSpace(cur))
		if (cur == "BRL" || cur == "USD" || cur == "EUR" || cur == "PLN" || cur == "GBP") && cents > 0 {
			prices[cur] = cents
		}
	}
	pricesJSON, _ := json.Marshal(prices)
	before, _ := s.q.GetPlan(r.Context(), req.Key)
	p, err := s.q.UpsertPlan(r.Context(), db.UpsertPlanParams{
		Key: req.Key, Name: req.Name, Description: req.Description,
		PriceCentsPerUser: req.PriceCentsPerUser, MinUsers: req.MinUsers, MaxUsers: req.MaxUsers,
		AppsEnabled: apps, MaxStorageGb: req.MaxStorageGb, AiTokensPerWindow: req.AiTokensPerWindow,
		MeetMaxParticipants: req.MeetMaxParticipants, MeetMaxMinutes: req.MeetMaxMinutes,
		Recording: req.Recording, RecordingRetentionDays: req.RecordingRetentionDays,
		MaxDailySend: req.MaxDailySend, OnlyofficeEdit: req.OnlyofficeEdit,
		WikiPublicLinks: req.WikiPublicLinks, Sort: req.Sort, Prices: pricesJSON,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	// Mirrors to Stripe (Product + Price): financial management lives entirely there. Best-effort
	// with a warning — the plan is saved in the DB even if Stripe is down.
	stripeSync := ""
	if s.billing != nil {
		priceChanged := before.StripePriceID == "" || before.PriceCentsPerUser != p.PriceCentsPerUser ||
			!bytes.Equal(before.Prices, p.Prices)
		prodID, priceID, serr := s.billing.SyncPlan(r.Context(), billing.Plan{
			ID: p.Key, Name: p.Name, PriceCentsPerUser: int(p.PriceCentsPerUser), Prices: prices,
		}, before.StripeProductID, before.StripePriceID, priceChanged)
		if serr != nil {
			log.Printf("manager: stripe plan sync %s: %v", p.Key, serr)
			stripeSync = "failed"
		} else if prodID != "" {
			_ = s.q.UpdatePlanStripeIDs(r.Context(), db.UpdatePlanStripeIDsParams{Key: p.Key, StripeProductID: prodID, StripePriceID: priceID})
			p.StripeProductID, p.StripePriceID = prodID, priceID
			stripeSync = "ok"
		}
	}
	s.audit(r.Context(), managerUserFrom(r.Context()), "plan_saved", req.Key,
		map[string]any{"before": before, "after": p, "stripe_sync": stripeSync})
	out := map[string]any{"plan": planViewOf(p)}
	if stripeSync != "" {
		out["stripe_sync"] = stripeSync
	}
	writeJSON(w, http.StatusOK, out)
}

type activeReq struct {
	Active bool `json:"active"`
}

// handleSetPlanActive turns a plan on/off in the catalog. Deactivating doesn't touch tenants that
// already have it (they keep the plan's limits); it only blocks new assignments/checkout.
func (s *Server) handleSetPlanActive(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	key := chi.URLParam(r, "key")
	if _, err := s.q.GetPlan(r.Context(), key); err != nil {
		writeErr(w, http.StatusNotFound, "manager.plan_not_found")
		return
	}
	var req activeReq
	if !decode(w, r, &req) {
		return
	}
	if err := s.q.SetPlanActive(r.Context(), db.SetPlanActiveParams{Key: key, Active: req.Active}); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), managerUserFrom(r.Context()), "plan_active_set", key,
		map[string]any{"active": req.Active})
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "active": req.Active})
}

type tenantPlanReq struct {
	Plan string `json:"plan"`
}

// handleSetTenantPlan assigns an (active) plan to a tenant.
func (s *Server) handleSetTenantPlan(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
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
	var req tenantPlanReq
	if !decode(w, r, &req) {
		return
	}
	plan, err := s.q.GetPlan(r.Context(), req.Plan)
	if err != nil || !plan.Active {
		writeErr(w, http.StatusBadRequest, "manager.plan_not_found")
		return
	}
	if err := s.q.ManagerSetTenantPlan(r.Context(), db.ManagerSetTenantPlanParams{ID: id, Plan: plan.Key}); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), managerUserFrom(r.Context()), "tenant_plan_set", id.String(),
		map[string]any{"tenant": tenant.Name, "from": tenant.Plan, "to": plan.Key})
	writeJSON(w, http.StatusOK, map[string]any{"tenant_id": id, "plan": plan.Key})
}

// handleSetTenantCurrency sets the tenant's billing currency (” = global). The existing Stripe
// subscription keeps its original currency until the next plan change/checkout.
func (s *Server) handleSetTenantCurrency(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "manager.invalid_id")
		return
	}
	var req struct {
		Currency string `json:"currency"`
	}
	if !decode(w, r, &req) {
		return
	}
	cur := strings.ToUpper(strings.TrimSpace(req.Currency))
	if cur != "" && cur != "BRL" && cur != "USD" && cur != "EUR" && cur != "PLN" && cur != "GBP" {
		writeErr(w, http.StatusBadRequest, "manager.invalid_currency")
		return
	}
	if err := s.q.SetTenantCurrency(r.Context(), db.SetTenantCurrencyParams{ID: id, Currency: cur}); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), managerUserFrom(r.Context()), "tenant_currency", id.String(), map[string]any{"currency": cur})
	writeJSON(w, http.StatusOK, map[string]string{"currency": cur})
}
