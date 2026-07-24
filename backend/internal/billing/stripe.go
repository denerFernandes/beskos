package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// StripeProvider fala com a API da Stripe via HTTP (sem SDK). Checkout Session (assinatura) +
// verificação de assinatura do webhook (HMAC-SHA256, esquema oficial da Stripe).
type StripeProvider struct {
	cfg  Config
	http *http.Client
}

func NewStripe(cfg Config) *StripeProvider {
	return &StripeProvider{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}
}

func (p *StripeProvider) Name() string { return "stripe" }

func unknownProvider(name string) error { return fmt.Errorf("billing: provedor desconhecido %q", name) }

// Checkout cria uma Checkout Session em modo assinatura usando price_data inline (não exige
// pré-cadastrar preços na Stripe). client_reference_id = tenant; metadata[plan] = plano.
// planCurrency: moeda efetiva da operação (a do tenant, resolvida pelo chamador) ou a global.
func (p *StripeProvider) planCurrency(plan Plan) string {
	if plan.Currency != "" {
		return strings.ToLower(plan.Currency)
	}
	return strings.ToLower(p.cfg.Settings().Currency)
}

func (p *StripeProvider) Checkout(ctx context.Context, tenantID string, plan Plan, seats int) (CheckoutResult, error) {
	if seats < 1 {
		seats = 1
	}
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("success_url", p.cfg.SuccessURL)
	form.Set("cancel_url", p.cfg.CancelURL)
	form.Set("client_reference_id", tenantID)
	form.Set("metadata[plan]", plan.ID)
	form.Set("metadata[tenant_id]", tenantID)
	form.Set("line_items[0][quantity]", strconv.Itoa(seats)) // preço por usuário × seats
	if plan.StripePriceID != "" {
		// Catálogo espelhado na Stripe: usa o Price real — faturas/relatórios agrupam por produto.
		// currency seleciona a currency_option do price (moeda por país, 24/07/2026).
		form.Set("line_items[0][price]", plan.StripePriceID)
		form.Set("currency", p.planCurrency(plan))
	} else {
		form.Set("line_items[0][price_data][currency]", p.planCurrency(plan))
		form.Set("line_items[0][price_data][recurring][interval]", "month")
		form.Set("line_items[0][price_data][unit_amount]", strconv.Itoa(plan.PriceCentsPerUser))
		form.Set("line_items[0][price_data][product_data][name]", "Beskos "+plan.Name)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.stripe.com/v1/checkout/sessions", strings.NewReader(form.Encode()))
	req.Header.Set("Authorization", "Bearer "+p.cfg.SecretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.http.Do(req)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("stripe checkout: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return CheckoutResult{}, fmt.Errorf("stripe checkout HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return CheckoutResult{}, err
	}
	return CheckoutResult{URL: out.URL}, nil
}

// ParseWebhook verifica a assinatura (Stripe-Signature: t=...,v1=...) e normaliza o evento.
func (p *StripeProvider) ParseWebhook(payload []byte, sigHeader string) (*WebhookEvent, error) {
	// Falha fechado se o segredo não está configurado: com secret vazio o HMAC é reproduzível por
	// qualquer um, e um webhook forjado concederia plano pago a qualquer tenant (auditoria M).
	if p.cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("stripe webhook secret not configured")
	}
	if err := verifyStripeSignature(payload, sigHeader, p.cfg.WebhookSecret, 5*time.Minute); err != nil {
		return nil, err
	}
	var ev struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID                string            `json:"id"`
				ClientReferenceID string            `json:"client_reference_id"`
				Subscription      string            `json:"subscription"`
				Status            string            `json:"status"`
				Metadata          map[string]string `json:"metadata"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil, err
	}
	o := ev.Data.Object
	tenant := o.ClientReferenceID
	if tenant == "" {
		tenant = o.Metadata["tenant_id"]
	}
	switch ev.Type {
	case "checkout.session.completed":
		return &WebhookEvent{EventID: ev.ID, Type: "activated", TenantID: tenant, Plan: o.Metadata["plan"], Status: "active", SubscriptionID: o.Subscription}, nil
	case "customer.subscription.deleted":
		return &WebhookEvent{EventID: ev.ID, Type: "canceled", TenantID: tenant, Plan: DefaultPlanID, Status: "canceled", SubscriptionID: o.ID}, nil
	case "customer.subscription.updated":
		// Só sincroniza o STATUS (past_due, active…): Plan vazio = o handler não mexe no plano.
		return &WebhookEvent{EventID: ev.ID, Type: "status", TenantID: tenant, Plan: "", Status: o.Status, SubscriptionID: o.ID}, nil
	default:
		return nil, nil // evento sem efeito
	}
}

// verifyStripeSignature valida o cabeçalho Stripe-Signature. Exportado indiretamente para teste.
func verifyStripeSignature(payload []byte, header, secret string, tolerance time.Duration) error {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			sigs = append(sigs, kv[1])
		}
	}
	if ts == "" || len(sigs) == 0 {
		return fmt.Errorf("assinatura ausente")
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("timestamp inválido")
	}
	if tolerance > 0 && absDuration(time.Since(time.Unix(tsInt, 0))) > tolerance {
		return fmt.Errorf("assinatura fora da janela de tolerância")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(payload)))
	expected := hex.EncodeToString(mac.Sum(nil))
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(expected)) {
			return nil
		}
	}
	return fmt.Errorf("assinatura inválida")
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// doForm executa uma chamada form-encoded à API da Stripe e devolve o corpo (erro em não-2xx).
func (p *StripeProvider) doForm(ctx context.Context, method, url string, form url.Values) ([]byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.SecretKey)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("stripe HTTP %d: %s", resp.StatusCode, raw)
	}
	return raw, nil
}

// Subscription lê a assinatura (status, cliente, item e fim do período).
func (p *StripeProvider) Subscription(ctx context.Context, subID string) (SubscriptionInfo, error) {
	raw, err := p.doForm(ctx, http.MethodGet, "https://api.stripe.com/v1/subscriptions/"+url.PathEscape(subID), nil)
	if err != nil {
		return SubscriptionInfo{}, err
	}
	var out struct {
		Customer          string `json:"customer"`
		Status            string `json:"status"`
		CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
		CurrentPeriodEnd  int64  `json:"current_period_end"`
		Items             struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return SubscriptionInfo{}, err
	}
	info := SubscriptionInfo{Customer: out.Customer, Status: out.Status, CancelAtPeriodEnd: out.CancelAtPeriodEnd, CurrentPeriodEnd: out.CurrentPeriodEnd}
	if len(out.Items.Data) > 0 {
		info.ItemID = out.Items.Data[0].ID
	}
	return info, nil
}

// UpdatePlan troca o preço (price_data inline, como no checkout) e a quantidade da assinatura
// ativa, com rateio — upgrade/downgrade sem novo checkout.
func (p *StripeProvider) UpdatePlan(ctx context.Context, subID string, plan Plan, seats int) error {
	info, err := p.Subscription(ctx, subID)
	if err != nil {
		return err
	}
	if info.ItemID == "" {
		return fmt.Errorf("stripe: subscription %s has no items", subID)
	}
	if seats < 1 {
		seats = 1
	}
	form := url.Values{}
	form.Set("items[0][id]", info.ItemID)
	form.Set("items[0][quantity]", strconv.Itoa(seats))
	if plan.StripePriceID != "" {
		form.Set("items[0][price]", plan.StripePriceID)
	} else {
		form.Set("items[0][price_data][currency]", p.planCurrency(plan))
		form.Set("items[0][price_data][recurring][interval]", "month")
		form.Set("items[0][price_data][unit_amount]", strconv.Itoa(plan.PriceCentsPerUser))
		form.Set("items[0][price_data][product_data][name]", "Beskos "+plan.Name)
	}
	form.Set("metadata[plan]", plan.ID)
	form.Set("proration_behavior", "create_prorations")
	form.Set("cancel_at_period_end", "false") // trocar de plano retoma uma assinatura a caminho do cancelamento
	_, err = p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/subscriptions/"+url.PathEscape(subID), form)
	return err
}

func (p *StripeProvider) SetCancelAtPeriodEnd(ctx context.Context, subID string, cancel bool) error {
	form := url.Values{}
	form.Set("cancel_at_period_end", strconv.FormatBool(cancel))
	_, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/subscriptions/"+url.PathEscape(subID), form)
	return err
}

// PortalURL cria uma sessão do Billing Portal (forma de pagamento, faturas, histórico).
func (p *StripeProvider) PortalURL(ctx context.Context, customerID, returnURL string) (string, error) {
	form := url.Values{}
	form.Set("customer", customerID)
	if returnURL != "" {
		form.Set("return_url", returnURL)
	}
	raw, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/billing_portal/sessions", form)
	if err != nil {
		return "", err
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// SyncPlan espelha o plano na Stripe: garante o Product (cria/renomeia) e o Price recorrente.
// Prices são imutáveis — mudou o valor, cria um novo, marca-o default e desativa o antigo.
func (p *StripeProvider) SyncPlan(ctx context.Context, plan Plan, productID, priceID string, priceChanged bool) (string, string, error) {
	// 1) Product
	if productID == "" {
		form := url.Values{}
		form.Set("name", "Beskos "+plan.Name)
		form.Set("metadata[plan]", plan.ID)
		raw, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/products", form)
		if err != nil {
			return "", "", err
		}
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", "", err
		}
		productID = out.ID
	} else {
		form := url.Values{}
		form.Set("name", "Beskos "+plan.Name)
		if _, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/products/"+url.PathEscape(productID), form); err != nil {
			return "", "", err
		}
	}
	// 2) Price — só cria quando não existe ou o valor mudou.
	if priceID == "" || priceChanged {
		base := strings.ToLower(p.cfg.Settings().Currency)
		form := url.Values{}
		form.Set("product", productID)
		form.Set("currency", base)
		form.Set("unit_amount", strconv.Itoa(plan.PriceCentsPerUser))
		form.Set("recurring[interval]", "month")
		form.Set("metadata[plan]", plan.ID)
		// Moeda por país: as demais moedas entram como currency_options do MESMO price.
		for cur, cents := range plan.Prices {
			lc := strings.ToLower(cur)
			if lc == base {
				continue
			}
			form.Set("currency_options["+lc+"][unit_amount]", strconv.Itoa(cents))
		}
		raw, err := p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/prices", form)
		if err != nil {
			return productID, priceID, err
		}
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return productID, priceID, err
		}
		if priceID != "" { // desativa o preço antigo (não afeta assinaturas existentes)
			off := url.Values{}
			off.Set("active", "false")
			_, _ = p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/prices/"+url.PathEscape(priceID), off)
		}
		priceID = out.ID
	}
	return productID, priceID, nil
}

// UpdateQuantity ajusta os seats da assinatura com rateio (create_prorations): quem entra a meio
// do período paga só a fração restante; quem sai gera crédito na fatura seguinte.
func (p *StripeProvider) UpdateQuantity(ctx context.Context, subID string, seats int) error {
	info, err := p.Subscription(ctx, subID)
	if err != nil {
		return err
	}
	if info.ItemID == "" {
		return fmt.Errorf("stripe: subscription %s has no items", subID)
	}
	if seats < 1 {
		seats = 1
	}
	form := url.Values{}
	form.Set("items[0][id]", info.ItemID)
	form.Set("items[0][quantity]", strconv.Itoa(seats))
	form.Set("proration_behavior", "create_prorations")
	_, err = p.doForm(ctx, http.MethodPost, "https://api.stripe.com/v1/subscriptions/"+url.PathEscape(subID), form)
	return err
}
