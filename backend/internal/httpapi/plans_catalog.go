package httpapi

import (
	"encoding/json"
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"mailin/backend/internal/billing"
	"mailin/backend/internal/db"
)

// Catálogo de planos com a BD como fonte de verdade (manager M2, docs/manager.md §4): o manager
// escreve na tabela `plans`; o produto LÊ daqui, com cache curto (evita uma query por request) e
// fallback para o catálogo em código de internal/billing quando a tabela está vazia ou a leitura
// falha (fail-safe: nunca deixa o produto sem limites definidos).

const planCacheTTL = time.Minute

type planCatalog struct {
	mu      sync.Mutex
	plans   []billing.Plan
	fetched time.Time
}

// catalogPlans devolve todos os planos (ativos e inativos) já convertidos para billing.Plan.
func (s *Server) catalogPlans(ctx context.Context) []billing.Plan {
	s.planCat.mu.Lock()
	defer s.planCat.mu.Unlock()
	if time.Since(s.planCat.fetched) < planCacheTTL && s.planCat.plans != nil {
		return s.planCat.plans
	}
	rows, err := s.q.ListPlans(ctx)
	if err != nil || len(rows) == 0 {
		// Fallback: catálogo em código. Não cacheia o fallback por muito tempo de propósito —
		// assim que a tabela responder, os valores da BD assumem.
		return billing.Plans()
	}
	out := make([]billing.Plan, 0, len(rows))
	for _, r := range rows {
		out = append(out, planFromRow(r))
	}
	s.planCat.plans = out
	s.planCat.fetched = time.Now()
	return out
}

// activePlans filtra o catálogo para exibição (checkout/billing só mostra planos ativos).
func (s *Server) activePlans(ctx context.Context) []billing.Plan {
	all := s.catalogPlans(ctx)
	out := make([]billing.Plan, 0, len(all))
	for _, p := range all {
		if p.IsActive {
			out = append(out, p)
		}
	}
	return out
}

// catalogPlanByKey resolve um plano pela chave SEM fallback (ok=false se não existe) — para
// validação de checkout/webhook, onde cair no trial silenciosamente seria errado.
func (s *Server) catalogPlanByKey(ctx context.Context, key string) (billing.Plan, bool) {
	for _, p := range s.catalogPlans(ctx) {
		if p.ID == key {
			return p, true
		}
	}
	return billing.Plan{}, false
}

// planByKey resolve um plano pela chave, com o mesmo fail-safe de billing.PlanOrDefault.
func (s *Server) planByKey(ctx context.Context, key string) billing.Plan {
	for _, p := range s.catalogPlans(ctx) {
		if p.ID == key {
			return p
		}
	}
	return billing.PlanOrDefault(key)
}

// planFromRow converte a linha da BD no tipo que o produto consome.
func planFromRow(r db.Plan) billing.Plan {
	return billing.Plan{
		ID:                  r.Key,
		Name:                r.Name,
		MailboxRange:        billing.RangeLabel(int(r.MinUsers), int(r.MaxUsers)),
		MaxMailboxes:        int(r.MaxUsers),
		MaxStorageBytes:     int64(r.MaxStorageGb) * billing.GB,
		MaxDailySend:        int(r.MaxDailySend),
		MaxMeetParticipants: int(r.MeetMaxParticipants),
		PriceCentsPerUser:   int(r.PriceCentsPerUser),
		MinUsers:            int(r.MinUsers),
		AppsEnabled:         r.AppsEnabled,
		AITokensPerWindow:   int(r.AiTokensPerWindow),
		MeetMaxMinutes:      int(r.MeetMaxMinutes),
		Recording:           r.Recording,
		RecordingDays:       int(r.RecordingRetentionDays),
		OnlyOfficeEdit:      r.OnlyofficeEdit,
		WikiPublicLinks:     r.WikiPublicLinks,
		IsActive:            r.Active,
		StripePriceID:       r.StripePriceID,
		Prices:              parsePlanPrices(r.Prices),
	}
}

// parsePlanPrices lê o JSONB {"USD":1900,...} (vazio/inválido = sem preços por moeda).
func parsePlanPrices(raw []byte) map[string]int {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]int
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}

// disabledAppsEffective une as apps desligadas pelo admin do tenant com as EXCLUÍDAS pelo plano
// (enforcement de apps_enabled — manager M2). Lista vazia de apps_enabled no plano = todas ligadas.
func (s *Server) disabledAppsEffective(ctx context.Context, tenantID uuid.UUID) []string {
	disabled, _ := s.q.GetTenantDisabledApps(ctx, tenantID)
	t, err := s.q.GetTenant(ctx, tenantID)
	if err != nil {
		return disabled
	}
	plan := s.planByKey(ctx, t.Plano)
	if len(plan.AppsEnabled) == 0 {
		return disabled
	}
	allowed := map[string]bool{}
	for _, a := range plan.AppsEnabled {
		allowed[a] = true
	}
	seen := map[string]bool{}
	for _, a := range disabled {
		seen[a] = true
	}
	for _, a := range toggleableApps {
		if !allowed[a] && !seen[a] {
			disabled = append(disabled, a)
			seen[a] = true
		}
	}
	return disabled
}
