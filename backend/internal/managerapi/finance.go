package managerapi

import (
	"context"
	"encoding/json"
	"net/http"

	"mailin/backend/internal/db"
)

// Finanças (M4, docs/manager.md §5): MRR/ARR/ARPU a partir do catálogo de planos × utilizadores
// (receita CONTRATADA — a conciliação com o billing real fica para quando o Stripe estiver vivo),
// custo variável estimado por driver (modelo editável) e margem por tenant. Estimativa ≠ fatura
// (doc §8): a UI marca como estimativa.

// costModel são as tarifas em CENTS. Billable seats = utilizadores atuais (sem mínimo por plano —
// registado no doc; o mínimo cobrado entra com o billing real).
type costModel struct {
	Currency            string `json:"currency"`
	FixedMonthCents     int64  `json:"fixed_month_cents"`
	StorageGbMonthCents int64  `json:"storage_gb_month_cents"`
	AiPerMtokCents      int64  `json:"ai_per_mtok_cents"`
	EmailPer1kCents     int64  `json:"email_per_1k_cents"`
}

var defaultCostModel = costModel{
	Currency: "EUR", FixedMonthCents: 6500, StorageGbMonthCents: 2,
	AiPerMtokCents: 30, EmailPer1kCents: 10,
}

func (s *Server) loadCostModel(ctx context.Context) costModel {
	raw, err := s.q.GetManagerSetting(ctx, "cost_model")
	if err != nil {
		return defaultCostModel
	}
	cm := defaultCostModel
	if err := json.Unmarshal(raw, &cm); err != nil {
		return defaultCostModel
	}
	return cm
}

func (s *Server) handleGetCosts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.loadCostModel(r.Context()))
}

func (s *Server) handleSaveCosts(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var cm costModel
	if !decode(w, r, &cm) {
		return
	}
	if cm.Currency == "" || cm.FixedMonthCents < 0 || cm.StorageGbMonthCents < 0 ||
		cm.AiPerMtokCents < 0 || cm.EmailPer1kCents < 0 {
		writeErr(w, http.StatusBadRequest, "manager.costs_invalid")
		return
	}
	before := s.loadCostModel(r.Context())
	raw, err := json.Marshal(cm)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	if err := s.q.UpsertManagerSetting(r.Context(), db.UpsertManagerSettingParams{Key: "cost_model", Value: raw}); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	s.audit(r.Context(), managerUserFrom(r.Context()), "cost_model_saved", "",
		map[string]any{"before": before, "after": cm})
	writeJSON(w, http.StatusOK, cm)
}

const gb = int64(1 << 30)

// tenantFinance é a linha da tabela de margem por tenant.
type tenantFinance struct {
	ID           string `json:"id"`
	Nome         string `json:"nome"`
	Plano        string `json:"plano"`
	Status       string `json:"status"`
	Users        int32  `json:"users"`
	MrrCents     int64  `json:"mrr_cents"`
	CostCents    int64  `json:"cost_cents"` // variável estimado (sem o fixo, que é global)
	MarginCents  int64  `json:"margin_cents"`
	StorageBytes int64  `json:"storage_bytes"`
	AiTokens30d  int64  `json:"ai_tokens_30d"`
}

// handleFinance calcula o resumo (MRR/ARR/ARPU, custos, margem) e a tabela por tenant.
func (s *Server) handleFinance(w http.ResponseWriter, r *http.Request) {
	summary, tenants, err := s.computeFinance(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": summary, "tenants": tenants})
}

// computeFinance é a conta em si (partilhada com o export CSV da M5).
func (s *Server) computeFinance(ctx context.Context) (map[string]any, []tenantFinance, error) {
	rows, err := s.q.ManagerFinanceRows(ctx)
	if err != nil {
		return nil, nil, err
	}
	plans, err := s.q.ListPlans(ctx)
	if err != nil {
		return nil, nil, err
	}
	priceByKey := map[string]int64{}
	for _, p := range plans {
		priceByKey[p.Key] = int64(p.PriceCentsPerUser)
	}
	cm := s.loadCostModel(ctx)
	// Custo fixo = soma dos SERVIDORES ativos (por VPS, migração 00108) + "outros fixos" do modelo.
	serversCents, _ := s.q.SumManagerServers(ctx)
	fixed := cm.FixedMonthCents + serversCents

	out := make([]tenantFinance, 0, len(rows))
	var totalMRR, totalVar int64
	var paidUsers int64
	for _, t := range rows {
		mrr := int64(0)
		if t.Status == "active" {
			mrr = priceByKey[t.Plano] * int64(t.Users)
		}
		// Custo variável estimado: storage (+ gravações) por GB-mês, IA por 1M tokens (30d),
		// e-mail por 1k envios (30d).
		cost := (t.StorageBytes+t.RecordingsBytes)/gb*cm.StorageGbMonthCents +
			t.AiTokens30d*cm.AiPerMtokCents/1_000_000 +
			t.MailSent30d*cm.EmailPer1kCents/1_000
		out = append(out, tenantFinance{
			ID: t.ID.String(), Nome: t.Nome, Plano: t.Plano, Status: t.Status, Users: t.Users,
			MrrCents: mrr, CostCents: cost, MarginCents: mrr - cost,
			StorageBytes: t.StorageBytes, AiTokens30d: t.AiTokens30d,
		})
		totalMRR += mrr
		totalVar += cost
		if mrr > 0 {
			paidUsers += int64(t.Users)
		}
	}
	arpu := int64(0)
	if paidUsers > 0 {
		arpu = totalMRR / paidUsers
	}
	summary := map[string]any{
		"currency":          cm.Currency,
		"mrr_cents":         totalMRR,
		"arr_cents":         totalMRR * 12,
		"arpu_cents":        arpu,
		"fixed_cents":       fixed,
		"servers_cents":     serversCents,
		"other_fixed_cents": cm.FixedMonthCents,
		"variable_cents":    totalVar,
		"margin_cents":      totalMRR - fixed - totalVar,
		"paid_users":        paidUsers,
	}
	return summary, out, nil
}
