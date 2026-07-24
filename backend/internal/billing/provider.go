package billing

import (
	"context"
	"os"
	"strconv"
	"strings"
)

// CheckoutResult: Manual aplica o plano na hora (Applied=true, sem redirect); Stripe devolve URL.
type CheckoutResult struct {
	URL     string `json:"url,omitempty"`
	Applied bool   `json:"applied"`
}

// WebhookEvent é o evento normalizado (independente de provedor) que o control plane aplica.
type WebhookEvent struct {
	EventID        string // id do provedor — usado para idempotência (brief §10)
	Type           string // "activated" | "canceled"
	TenantID       string
	Plan           string
	Status         string
	SubscriptionID string
}

// SubscriptionInfo é o retrato da assinatura no provedor — o painel gere sem sair da app.
type SubscriptionInfo struct {
	Customer          string
	ItemID            string
	Status            string
	CancelAtPeriodEnd bool
	CurrentPeriodEnd  int64 // unix
}

// Provider abstrai o pagamento. Trocar Stripe↔Manual↔outro = só config (como ai/sms).
type Provider interface {
	Name() string
	// Checkout inicia/atualiza a assinatura do tenant. seats = nº de usuários cobrados.
	// Checkout recebe o Plan JÁ resolvido pelo chamador (catálogo da BD desde a M2 do manager) —
	// o provedor nunca consulta o catálogo em código, senão o preço divergiria do editado no painel.
	Checkout(ctx context.Context, tenantID string, plan Plan, seats int) (CheckoutResult, error)
	// ParseWebhook valida a assinatura e normaliza o evento. (nil, nil) = ignorar/sem efeito.
	ParseWebhook(payload []byte, sigHeader string) (*WebhookEvent, error)
	// Subscription lê a assinatura no provedor (status, fim do período, item para trocar preço).
	Subscription(ctx context.Context, subID string) (SubscriptionInfo, error)
	// UpdatePlan troca o preço/quantidade da assinatura ATIVA (upgrade/downgrade com rateio).
	UpdatePlan(ctx context.Context, subID string, plan Plan, seats int) error
	// SetCancelAtPeriodEnd cancela no fim do período (true) ou retoma (false).
	SetCancelAtPeriodEnd(ctx context.Context, subID string, cancel bool) error
	// PortalURL abre o portal do cliente (trocar forma de pagamento, faturas). "" = não suportado.
	PortalURL(ctx context.Context, customerID, returnURL string) (string, error)
	// UpdateQuantity ajusta o nº de seats da assinatura ativa, com rateio — utilizador novo a
	// meio do período entra proporcional na fatura.
	UpdateQuantity(ctx context.Context, subID string, seats int) error
	// SyncPlan espelha o plano no provedor (Product + Price). Preço mudou → Price novo (imutáveis)
	// e o antigo é desativado. Devolve os ids atuais ("" no provedor manual).
	SyncPlan(ctx context.Context, plan Plan, productID, priceID string, priceChanged bool) (newProductID, newPriceID string, err error)
}

// ManualProvider: sem gateway externo. O plano é aplicado direto (dev, self-serve interno,
// e é o caminho exercitado nos testes). Não há webhooks.
type ManualProvider struct{}

func (ManualProvider) Name() string { return "manual" }

func (ManualProvider) Checkout(_ context.Context, _ string, _ Plan, _ int) (CheckoutResult, error) {
	return CheckoutResult{Applied: true}, nil
}

func (ManualProvider) ParseWebhook([]byte, string) (*WebhookEvent, error) { return nil, nil }

func (ManualProvider) Subscription(context.Context, string) (SubscriptionInfo, error) {
	return SubscriptionInfo{}, nil
}
func (ManualProvider) UpdatePlan(context.Context, string, Plan, int) error       { return nil }
func (ManualProvider) SetCancelAtPeriodEnd(context.Context, string, bool) error  { return nil }
func (ManualProvider) PortalURL(context.Context, string, string) (string, error) { return "", nil }
func (ManualProvider) UpdateQuantity(context.Context, string, int) error { return nil }
func (ManualProvider) SyncPlan(_ context.Context, _ Plan, productID, priceID string, _ bool) (string, string, error) {
	return productID, priceID, nil
}

// Config seleciona o provedor por ambiente.
type Config struct {
	Provider      string // "manual" (default) | "stripe"
	SecretKey     string
	WebhookSecret string
	SuccessURL    string
	CancelURL     string
	Currency      string
	MinPaidUsers  int
}

func FromEnv() Config {
	return Config{
		Provider:      getenv("BILLING_PROVIDER", "manual"),
		SecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		SuccessURL:    getenv("BILLING_SUCCESS_URL", "https://app.mailin/billing?ok=1"),
		CancelURL:     getenv("BILLING_CANCEL_URL", "https://app.mailin/billing?canceled=1"),
		Currency:      strings.ToUpper(getenv("BILLING_CURRENCY", "BRL")),
		MinPaidUsers:  atoiDefault(os.Getenv("BILLING_MIN_PAID_USERS"), 1),
	}
}

// Settings expõe as configurações globais (moeda + mínimo de usuários) para os handlers/UI.
func (c Config) Settings() Settings {
	cur := c.Currency
	if cur != "BRL" && cur != "EUR" && cur != "USD" && cur != "PLN" {
		cur = "BRL"
	}
	min := c.MinPaidUsers
	if min < 1 {
		min = 1
	}
	return Settings{Currency: cur, MinPaidUsers: min}
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// New constrói o Provider. Único ponto que conhece provedores concretos.
func New(cfg Config) (Provider, error) {
	switch cfg.Provider {
	case "manual", "":
		return ManualProvider{}, nil
	case "stripe":
		return NewStripe(cfg), nil
	default:
		return nil, unknownProvider(cfg.Provider)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
