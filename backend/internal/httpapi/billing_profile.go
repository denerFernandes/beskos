package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// Dados de faturação do cliente + simulação com IVA. A fatura é obrigação nossa, por isso os dados
// vivem cá; a Stripe recebe uma CÓPIA (cliente com morada/NIF/e-mail do financeiro) para o checkout
// só ter de pedir o cartão.

type billingProfileReq struct {
	Kind         string `json:"kind"` // company | individual
	LegalName    string `json:"legal_name"`
	TaxID        string `json:"tax_id"`
	Country      string `json:"country"`
	City         string `json:"city"`
	PostalCode   string `json:"postal_code"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2"`
	InvoiceEmail string `json:"invoice_email"`
	InvoiceName  string `json:"invoice_name"`
	Phone        string `json:"phone"`
}

func (s *Server) handleGetBillingProfile(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	p, err := s.q.GetBillingProfile(r.Context(), u.TenantID)
	if err != nil {
		// Ainda não preenchido: devolve o esqueleto (o formulário abre vazio, não em erro).
		writeJSON(w, http.StatusOK, map[string]any{"kind": "company", "filled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind": p.Kind, "legal_name": p.LegalName, "tax_id": p.TaxID, "country": p.Country,
		"city": p.City, "postal_code": p.PostalCode, "address_line1": p.AddressLine1,
		"address_line2": p.AddressLine2, "invoice_email": p.InvoiceEmail, "invoice_name": p.InvoiceName,
		"phone": p.Phone, "filled": p.LegalName != "" && p.InvoiceEmail != "",
	})
}

// validateProfile devolve o código de erro do primeiro campo em falta ("" = tudo bem).
func validateProfile(req *billingProfileReq) string {
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	if req.Kind != "individual" {
		req.Kind = "company"
	}
	req.Country = strings.ToUpper(strings.TrimSpace(req.Country))
	req.LegalName = strings.TrimSpace(req.LegalName)
	req.InvoiceEmail = strings.TrimSpace(req.InvoiceEmail)
	req.TaxID = billing.NormalizeVATID(req.TaxID, req.Country)

	switch {
	case req.LegalName == "":
		return "billing.legal_name_required"
	case len(req.Country) != 2:
		return "billing.country_required"
	case req.City == "" || req.AddressLine1 == "":
		return "billing.address_required"
	case !strings.Contains(req.InvoiceEmail, "@"):
		return "billing.invoice_email_required"
	// Empresa polaca sem NIF não consegue deduzir o IVA — e a fatura polaca exige-o.
	case req.Kind == "company" && req.Country == billing.SellerCountry && req.TaxID == "":
		return "billing.tax_id_required"
	case req.Kind == "company" && req.TaxID != "" && billing.IsEU(req.Country) && !billing.ValidVATID(req.TaxID, req.Country):
		return "billing.tax_id_invalid"
	}
	return ""
}

func (s *Server) handleSaveBillingProfile(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req billingProfileReq
	if !decode(w, r, &req) {
		return
	}
	if code := validateProfile(&req); code != "" {
		writeErr(w, http.StatusBadRequest, code)
		return
	}
	if _, err := s.q.UpsertBillingProfile(r.Context(), db.UpsertBillingProfileParams{
		TenantID: u.TenantID, Kind: req.Kind, LegalName: req.LegalName, TaxID: req.TaxID,
		Country: req.Country, City: req.City, PostalCode: req.PostalCode,
		AddressLine1: req.AddressLine1, AddressLine2: req.AddressLine2,
		InvoiceEmail: req.InvoiceEmail, InvoiceName: req.InvoiceName, Phone: req.Phone,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "billing.profile_save_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgUUID(u.TenantID), Actor: u.Email, Action: "billing.profile", Target: req.LegalName})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type quoteReq struct {
	Plan    string `json:"plan"`
	Cycle   string `json:"cycle"`
	Kind    string `json:"kind"`
	Country string `json:"country"`
	TaxID   string `json:"tax_id"`
}

// handleBillingQuote simula o valor a pagar: preço × utilizadores + IVA (ou autoliquidação). É o
// mesmo cálculo que vai para a fatura — a página de subscrição não repete a regra em JavaScript,
// senão o total mostrado e o cobrado divergiam ao primeiro ajuste.
func (s *Server) handleBillingQuote(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req quoteReq
	if !decode(w, r, &req) {
		return
	}
	plan, ok := s.catalogPlanByKey(r.Context(), req.Plan)
	if !ok || !plan.IsActive {
		writeErr(w, http.StatusBadRequest, "billing.invalid_plan")
		return
	}
	plan = s.localPlan(plan, s.tenantCurrency(r.Context(), u.TenantID))
	cycle := billing.Cycle(req.Cycle).Normalize()
	seats := s.checkoutSeats(r, u.TenantID, plan)
	q := billing.QuoteFor(plan.Currency, plan.UnitCents(cycle), seats, req.Kind, req.Country, req.TaxID)

	writeJSON(w, http.StatusOK, map[string]any{
		"quote":            q,
		"cycle":            string(cycle),
		"plan":             plan.ID,
		"plan_name":        plan.Name,
		"monthly_cents":    plan.PriceCentsPerUser,
		"months_charged":   billing.AnnualMonthsCharged,
		"seller_country":   billing.SellerCountry,
		"vat_rate_percent": float64(q.VATRateBps) / 100,
	})
}

// checkoutSeats é o nº de utilizadores cobrados: os atuais, nunca abaixo do mínimo global nem do
// piso do plano.
func (s *Server) checkoutSeats(r *http.Request, tid uuid.UUID, plan billing.Plan) int {
	seats := s.billableSeats(r, tid)
	if seats < plan.MinUsers {
		seats = plan.MinUsers
	}
	return seats
}

// billingCustomer monta os dados do cliente para o provedor a partir do perfil guardado.
func billingCustomer(tenantID string, p db.BillingProfile) billing.Customer {
	return billing.Customer{
		TenantID: tenantID, Kind: p.Kind, Name: p.LegalName, Email: p.InvoiceEmail,
		TaxID: p.TaxID, Country: p.Country, City: p.City, PostalCode: p.PostalCode,
		AddressLine1: p.AddressLine1, AddressLine2: p.AddressLine2, Phone: p.Phone,
	}
}
