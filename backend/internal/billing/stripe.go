package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

func unknownProvider(name string) error { return fmt.Errorf("billing: unknown provider %q", name) }

// Checkout cria uma Checkout Session em modo assinatura usando price_data inline (não exige
// pré-cadastrar preços na Stripe). client_reference_id = tenant; metadata[plan] = plano.
// planCurrency: moeda efetiva da operação (a do tenant, resolvida pelo chamador) ou a global.
func (p *StripeProvider) planCurrency(plan Plan) string {
	if plan.Currency != "" {
		return strings.ToLower(plan.Currency)
	}
	return strings.ToLower(p.cfg.Settings().Currency)
}

func (p *StripeProvider) Checkout(ctx context.Context, tenantID string, plan Plan, seats int, opts CheckoutOpts) (CheckoutResult, error) {
	if seats < 1 {
		seats = 1
	}
	cycle := opts.Cycle.Normalize()
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("success_url", p.cfg.SuccessURL)
	form.Set("cancel_url", p.cfg.CancelURL)
	form.Set("client_reference_id", tenantID)
	form.Set("metadata[plan]", plan.ID)
	form.Set("metadata[tenant_id]", tenantID)
	form.Set("metadata[cycle]", string(cycle))
	form.Set("subscription_data[metadata][plan]", plan.ID)
	form.Set("subscription_data[metadata][tenant_id]", tenantID)
	form.Set("line_items[0][quantity]", strconv.Itoa(seats)) // preço por usuário × seats
	if opts.TrialDays > 0 {
		// Cartão pedido à partida, cobrança só no fim do teste. `cancel` no fim do período sem
		// método de pagamento: sem isto, um teste abandonado ficaria a tentar cobrar para sempre.
		form.Set("subscription_data[trial_period_days]", strconv.Itoa(opts.TrialDays))
		form.Set("subscription_data[trial_settings][end_behavior][missing_payment_method]", "cancel")
	}
	// Cliente já criado por nós (razão social, morada, NIF, e-mail do financeiro): o checkout só
	// pede o cartão e a fatura sai completa. Sem ele, ao menos o e-mail vai pré-preenchido.
	if opts.CustomerID != "" {
		form.Set("customer", opts.CustomerID)
		// A morada pode mudar no checkout (o cartão manda); guardá-la no cliente mantém a fatura certa.
		form.Set("customer_update[address]", "auto")
		form.Set("customer_update[name]", "auto")
	} else if opts.InvoiceEmail != "" {
		form.Set("customer_email", opts.InvoiceEmail)
	}
	// O Price espelhado é MENSAL; o anual é montado aqui (price_data com interval=year), senão
	// escolher "anual" cobrava na mesma todos os meses.
	if plan.StripePriceID != "" && cycle == CycleMonth {
		// Mirrored catalog: use the real Price so invoices and reports group by product.
		// currency selects the price's currency_option (per-country pricing).
		form.Set("line_items[0][price]", plan.StripePriceID)
		form.Set("currency", p.planCurrency(plan))
	} else {
		form.Set("line_items[0][price_data][currency]", p.planCurrency(plan))
		form.Set("line_items[0][price_data][recurring][interval]", string(cycle))
		form.Set("line_items[0][price_data][unit_amount]", strconv.Itoa(plan.UnitCents(cycle)))
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
				CurrentPeriodEnd  int64             `json:"current_period_end"`
				Metadata          map[string]string `json:"metadata"`
				AmountPaid        int64             `json:"amount_paid"` // invoice.paid
				Currency          string            `json:"currency"`    // invoice.paid
				HostedInvoiceURL  string            `json:"hosted_invoice_url"`
				Lines             struct {
					Data []struct {
						Metadata map[string]string `json:"metadata"`
					} `json:"data"`
				} `json:"lines"`
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
		return &WebhookEvent{EventID: ev.ID, Type: "activated", TenantID: tenant, Plan: o.Metadata["plan"],
			Status: "active", SubscriptionID: o.Subscription, Cycle: o.Metadata["cycle"]}, nil
	case "invoice.paid":
		// Dinheiro cobrado de facto (fim do teste ou renovação). O tenant vem da linha da fatura,
		// que herda os metadados da assinatura — a fatura em si não tem client_reference_id.
		if tenant == "" && len(o.Lines.Data) > 0 {
			tenant = o.Lines.Data[0].Metadata["tenant_id"]
		}
		if o.AmountPaid <= 0 {
			return nil, nil // fatura de 0 (início do teste): não é cobrança, não se avisa
		}
		plan := o.Metadata["plan"]
		if plan == "" && len(o.Lines.Data) > 0 {
			plan = o.Lines.Data[0].Metadata["plan"]
		}
		return &WebhookEvent{EventID: ev.ID, Type: "charged", TenantID: tenant, Plan: "", Status: "",
			SubscriptionID: o.Subscription, AmountCents: int(o.AmountPaid), Currency: strings.ToUpper(o.Currency),
			InvoiceURL: o.HostedInvoiceURL, PlanKey: plan}, nil
	case "customer.subscription.deleted":
		return &WebhookEvent{EventID: ev.ID, Type: "canceled", TenantID: tenant, Plan: DefaultPlanID, Status: "canceled",
			SubscriptionID: o.ID, PeriodEnd: o.CurrentPeriodEnd}, nil
	case "customer.subscription.updated":
		// Só sincroniza o STATUS (past_due, active…): Plan vazio = o handler não mexe no plano.
		return &WebhookEvent{EventID: ev.ID, Type: "status", TenantID: tenant, Plan: "", Status: o.Status,
			SubscriptionID: o.ID, PeriodEnd: o.CurrentPeriodEnd}, nil
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
		return fmt.Errorf("signature missing")
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp")
	}
	if tolerance > 0 && absDuration(time.Since(time.Unix(tsInt, 0))) > tolerance {
		return fmt.Errorf("signature outside the tolerance window")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(payload)))
	expected := hex.EncodeToString(mac.Sum(nil))
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(expected)) {
			return nil
		}
	}
	return fmt.Errorf("invalid signature")
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
func (p *StripeProvider) UpdatePlan(ctx context.Context, subID string, plan Plan, seats int, cycle Cycle) error {
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
	cycle = cycle.Normalize()
	if plan.StripePriceID != "" && cycle == CycleMonth {
		form.Set("items[0][price]", plan.StripePriceID)
	} else {
		form.Set("items[0][price_data][currency]", p.planCurrency(plan))
		form.Set("items[0][price_data][recurring][interval]", string(cycle))
		form.Set("items[0][price_data][unit_amount]", strconv.Itoa(plan.UnitCents(cycle)))
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

// PortalURL creates a Billing Portal session (payment method, invoices, history).
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

// EnsureCustomer cria (ou atualiza) o cliente na Stripe com os dados de faturação. É o que faz a
// fatura sair com razão social, morada e NIF em vez de só um e-mail — o que uma empresa precisa
// para deduzir o IVA. O NIF vai como tax_id à parte (a Stripe guarda-o numa lista própria).
func (p *StripeProvider) EnsureCustomer(ctx context.Context, customerID string, c Customer) (string, error) {
	form := url.Values{}
	if c.Name != "" {
		form.Set("name", c.Name)
	}
	if c.Email != "" {
		// É o e-mail do RESPONSÁVEL FINANCEIRO — é para aqui que a Stripe manda faturas e recibos,
		// e não para quem calhou de estar a assinar.
		form.Set("email", c.Email)
	}
	if c.Phone != "" {
		form.Set("phone", c.Phone)
	}
	if c.Country != "" {
		form.Set("address[country]", strings.ToUpper(c.Country))
	}
	if c.City != "" {
		form.Set("address[city]", c.City)
	}
	if c.PostalCode != "" {
		form.Set("address[postal_code]", c.PostalCode)
	}
	if c.AddressLine1 != "" {
		form.Set("address[line1]", c.AddressLine1)
	}
	if c.AddressLine2 != "" {
		form.Set("address[line2]", c.AddressLine2)
	}
	form.Set("metadata[tenant_id]", c.TenantID)
	form.Set("metadata[kind]", c.Kind)
	if c.TaxID != "" {
		form.Set("metadata[tax_id]", c.TaxID)
	}

	endpoint := "https://api.stripe.com/v1/customers"
	if customerID != "" {
		endpoint += "/" + url.PathEscape(customerID)
	}
	raw, err := p.doForm(ctx, http.MethodPost, endpoint, form)
	if err != nil {
		// Cliente apagado na Stripe (ou id de outra conta): recomeça em vez de ficar preso.
		if customerID != "" {
			return p.EnsureCustomer(ctx, "", c)
		}
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if c.TaxID != "" {
		p.syncTaxID(ctx, out.ID, c)
	}
	return out.ID, nil
}

// syncTaxID põe o NIF do cliente na Stripe (eu_vat para a UE). Best-effort: um número recusado não
// pode travar a assinatura — o valor a pagar é decidido por nós, não pela Stripe.
func (p *StripeProvider) syncTaxID(ctx context.Context, customerID string, c Customer) {
	kind := "eu_vat"
	if !IsEU(c.Country) {
		return // fora da UE cada país tem o seu tipo; não arriscamos adivinhar
	}
	base := "https://api.stripe.com/v1/customers/" + url.PathEscape(customerID) + "/tax_ids"
	// Já lá está o mesmo número? Não repete (a Stripe recusa duplicados).
	if raw, err := p.doForm(ctx, http.MethodGet, base, nil); err == nil {
		var list struct {
			Data []struct {
				ID    string `json:"id"`
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &list) == nil {
			for _, it := range list.Data {
				if strings.EqualFold(it.Value, c.TaxID) {
					return
				}
				if it.Type == kind { // número mudou: fora o antigo
					_, _ = p.doForm(ctx, http.MethodDelete, base+"/"+url.PathEscape(it.ID), nil)
				}
			}
		}
	}
	form := url.Values{}
	form.Set("type", kind)
	form.Set("value", c.TaxID)
	if _, err := p.doForm(ctx, http.MethodPost, base, form); err != nil {
		log.Printf("stripe: tax_id %q recusado para %s: %v", c.TaxID, customerID, err)
	}
}
