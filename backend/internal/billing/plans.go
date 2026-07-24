// Package billing define os planos (faixas de caixas) e seus limites de cota, e abstrai o
// provedor de pagamento (Stripe em produção; Manual em dev/testes) — trocável por config.
package billing

import "fmt"

// Plan é uma faixa de assinatura. O preço é POR USUÁRIO/mês (por seat).
// Desde a M2 do manager a fonte de verdade é a tabela `plans` (editável no painel do operador);
// o catálogo em código abaixo é o FALLBACK (tabela vazia/erro) e o seed da migração 00101.
type Plan struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	MailboxRange        string `json:"mailbox_range"`
	MaxMailboxes        int    `json:"max_mailboxes"`
	MaxStorageBytes     int64  `json:"max_storage_bytes"`
	MaxDailySend        int    `json:"max_daily_send"`
	MaxMeetParticipants int    `json:"max_meet_participants"` // teto de participantes por sala de vídeo
	PriceCentsPerUser   int    `json:"price_cents_per_user"`  // por usuário, por mês (na moeda configurada)

	// Knobs do manager (docs/manager.md §3.1). Zero/vazio = sem restrição/default do produto.
	MinUsers          int      `json:"min_users"`
	AppsEnabled       []string `json:"apps_enabled"`         // vazio = todas as apps do rail
	AITokensPerWindow int      `json:"ai_tokens_per_window"` // quota IA da janela 6h (0 = default)
	MeetMaxMinutes    int      `json:"meet_max_minutes"`
	Recording         string   `json:"recording"` // none | offline | offline_priority
	RecordingDays     int      `json:"recording_retention_days"`
	OnlyOfficeEdit    bool     `json:"onlyoffice_edit"`
	WikiPublicLinks   bool     `json:"wiki_public_links"`
	IsActive          bool     `json:"active"` // planos inativos não aceitam novos tenants

	// Espelho na Stripe (24/07/2026): quando preenchido, checkout/troca usam este price em vez de
	// price_data inline — a gestão (faturas, relatórios, portal) fica toda na Stripe.
	StripePriceID string `json:"stripe_price_id,omitempty"`
	// Prices: preço por moeda ({"USD":1900,...}, em cêntimos). PriceCentsPerUser é o da moeda BASE.
	Prices map[string]int `json:"prices,omitempty"`
	// Currency: moeda EFETIVA do preço apresentado (via PriceFor). Vai no JSON para a UI nunca
	// formatar um preço com a moeda de outro plano (bug 24/07/2026: Growth sem BRL mostrava
	// R$ com valor em USD).
	Currency string `json:"currency,omitempty"`
}

// PriceFor devolve o preço na moeda pedida e a moeda efetiva: cai na base quando o plano não tem
// preço específico para ela (nunca inventa câmbio).
func (p Plan) PriceFor(currency, baseCurrency string) (int, string) {
	if v, ok := p.Prices[currency]; ok && currency != "" {
		return v, currency
	}
	return p.PriceCentsPerUser, baseCurrency
}

// RangeLabel monta o rótulo da faixa a partir de min/max ("até 3", "1–10", "51+").
func RangeLabel(min, max int) string {
	switch {
	case max <= 0 && min <= 0:
		return ""
	case max <= 0:
		return fmt.Sprintf("%d+", min)
	case min <= 0:
		return fmt.Sprintf("até %d", max)
	default:
		return fmt.Sprintf("%d–%d", min, max)
	}
}

const GB = 1 << 30

// plans é o catálogo autoritativo. Preço por usuário decresce nas faixas maiores (desconto por volume).
var plans = []Plan{
	{ID: "trial", Name: "Trial", MailboxRange: "até 3", MaxMailboxes: 3, MaxStorageBytes: 5 * GB, MaxDailySend: 200, MaxMeetParticipants: 5, PriceCentsPerUser: 0,
		Recording: "none", RecordingDays: 30, OnlyOfficeEdit: true, WikiPublicLinks: true, IsActive: true},
	{ID: "starter", Name: "Starter", MailboxRange: "1–10", MaxMailboxes: 10, MaxStorageBytes: 50 * GB, MaxDailySend: 1000, MaxMeetParticipants: 25, PriceCentsPerUser: 1900,
		MinUsers: 1, Recording: "offline", RecordingDays: 30, OnlyOfficeEdit: true, WikiPublicLinks: true, IsActive: true},
	{ID: "growth", Name: "Growth", MailboxRange: "11–50", MaxMailboxes: 50, MaxStorageBytes: 250 * GB, MaxDailySend: 5000, MaxMeetParticipants: 50, PriceCentsPerUser: 1500,
		MinUsers: 11, Recording: "offline", RecordingDays: 30, OnlyOfficeEdit: true, WikiPublicLinks: true, IsActive: true},
	{ID: "business", Name: "Business", MailboxRange: "51–200", MaxMailboxes: 200, MaxStorageBytes: 1024 * GB, MaxDailySend: 20000, MaxMeetParticipants: 200, PriceCentsPerUser: 1200,
		MinUsers: 51, Recording: "offline_priority", RecordingDays: 30, OnlyOfficeEdit: true, WikiPublicLinks: true, IsActive: true},
}

// Settings globais de cobrança (por deploy, via .env).
type Settings struct {
	Currency     string `json:"currency"`       // BRL | EUR | USD (Intl no frontend)
	MinPaidUsers int    `json:"min_paid_users"` // mínimo de usuários cobrados em planos pagos
}

const DefaultPlanID = "trial"

// Plans devolve o catálogo (para a tela de billing).
func Plans() []Plan { return append([]Plan(nil), plans...) }

// PlanByID busca um plano; ok=false se desconhecido.
func PlanByID(id string) (Plan, bool) {
	for _, p := range plans {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

// PlanOrDefault devolve o plano do id, ou o trial (fail-safe para tenants com plano inesperado).
func PlanOrDefault(id string) Plan {
	if p, ok := PlanByID(id); ok {
		return p
	}
	d, _ := PlanByID(DefaultPlanID)
	return d
}

// IsValidPlan diz se o id é um plano conhecido (para validar checkout).
func IsValidPlan(id string) bool {
	_, ok := PlanByID(id)
	return ok
}
