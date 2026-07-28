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

// Cycle é a periodicidade da assinatura. Mensal é o padrão; anual cobra AnnualMonthsCharged meses
// de uma vez (os restantes são o desconto de quem paga adiantado).
type Cycle string

const (
	CycleMonth Cycle = "month"
	CycleYear  Cycle = "year"
	// AnnualMonthsCharged: 10 meses pelo ano = 2 meses grátis (~17% de desconto).
	AnnualMonthsCharged = 10
)

// Normalize devolve um ciclo válido (qualquer coisa que não seja "year" é mensal).
func (c Cycle) Normalize() Cycle {
	if c == CycleYear {
		return CycleYear
	}
	return CycleMonth
}

// CheckoutOpts são os extras do checkout: periodicidade e o cliente já criado no provedor (com
// morada/NIF/e-mail de faturação), para a fatura sair completa.
type CheckoutOpts struct {
	Cycle        Cycle
	CustomerID   string // cus_… (Stripe). Vazio = o provedor cria um no checkout.
	InvoiceEmail string // usado só quando não há CustomerID
	// TrialDays > 0 pede o cartão mas só cobra no fim do período de teste (registo self-service).
	TrialDays int
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
	Checkout(ctx context.Context, tenantID string, plan Plan, seats int, opts CheckoutOpts) (CheckoutResult, error)
	// ParseWebhook valida a assinatura e normaliza o evento. (nil, nil) = ignorar/sem efeito.
	ParseWebhook(payload []byte, sigHeader string) (*WebhookEvent, error)
	// Subscription lê a assinatura no provedor (status, fim do período, item para trocar preço).
	Subscription(ctx context.Context, subID string) (SubscriptionInfo, error)
	// UpdatePlan troca o preço/quantidade da assinatura ATIVA (upgrade/downgrade com rateio).
	UpdatePlan(ctx context.Context, subID string, plan Plan, seats int, cycle Cycle) error
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
	// EnsureCustomer cria/atualiza o cliente no provedor com os dados de faturação (razão social,
	// morada, NIF, e-mail do responsável financeiro). Devolve o id do cliente ("" = não suportado),
	// que o checkout reutiliza — assim a fatura sai completa e não se pede tudo outra vez no cartão.
	EnsureCustomer(ctx context.Context, customerID string, c Customer) (string, error)
}

// Customer são os dados de faturação enviados ao provedor. Espelha o que a fatura tem de mostrar.
type Customer struct {
	TenantID     string
	Kind         string // company | individual
	Name         string // razão social ou nome
	Email        string // responsável financeiro (para onde vai a fatura)
	TaxID        string // NIF/VAT já normalizado ("" = sem número)
	Country      string // ISO alfa-2
	City         string
	PostalCode   string
	AddressLine1 string
	AddressLine2 string
	Phone        string
}

// ManualProvider: sem gateway externo. O plano é aplicado direto (dev, self-serve interno,
// e é o caminho exercitado nos testes). Não há webhooks.
type ManualProvider struct{}

func (ManualProvider) Name() string { return "manual" }

func (ManualProvider) Checkout(_ context.Context, _ string, _ Plan, _ int, _ CheckoutOpts) (CheckoutResult, error) {
	return CheckoutResult{Applied: true}, nil
}

func (ManualProvider) ParseWebhook([]byte, string) (*WebhookEvent, error) { return nil, nil }

func (ManualProvider) Subscription(context.Context, string) (SubscriptionInfo, error) {
	return SubscriptionInfo{}, nil
}
func (ManualProvider) UpdatePlan(context.Context, string, Plan, int, Cycle) error { return nil }
func (ManualProvider) SetCancelAtPeriodEnd(context.Context, string, bool) error   { return nil }
func (ManualProvider) PortalURL(context.Context, string, string) (string, error)  { return "", nil }
func (ManualProvider) UpdateQuantity(context.Context, string, int) error          { return nil }
func (ManualProvider) EnsureCustomer(context.Context, string, Customer) (string, error) {
	return "", nil
}
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
		Currency:      strings.ToUpper(getenv("BILLING_CURRENCY", "USD")),
		MinPaidUsers:  atoiDefault(os.Getenv("BILLING_MIN_PAID_USERS"), 1),
	}
}

// Settings expõe as configurações globais (moeda + mínimo de usuários) para os handlers/UI.
func (c Config) Settings() Settings {
	cur := c.Currency
	if cur != "BRL" && cur != "EUR" && cur != "USD" && cur != "PLN" && cur != "GBP" {
		cur = "USD"
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
