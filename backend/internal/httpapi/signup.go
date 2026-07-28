package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/auth"
	"mailin/backend/internal/billing"
	"mailin/backend/internal/db"
	"mailin/backend/internal/provisioning"
	"mailin/backend/internal/rbac"
	"mailin/backend/internal/stalwart"
)

// Registo self-service (/signup): numa ida só cria a organização, o domínio, a CAIXA do dono e os
// dados de faturação, e devolve a URL do checkout com período de teste.
//
// A caixa é criada com o domínio ainda em `domain_created` (DNS por publicar), e não em `active`
// como exige o painel: quem acaba de se registar tem de conseguir entrar e usar chat, drive,
// calendário e reuniões no minuto seguinte. O e-mail de fora só entra quando o DNS estiver
// publicado — e a app diz isso no topo até lá.
//
// Se algo falhar a meio, desfaz-se o que já foi criado: meia organização (sem caixa, sem domínio)
// prendia o e-mail e o domínio para sempre, sem ninguém lá dentro para os libertar.

var (
	domainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	loginRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
)

// trialDays é o teste grátis do checkout. O cartão é pedido à partida (decisão do produto), mas só
// é cobrado no fim do período.
func trialDays() int {
	if v := atoiOr(os.Getenv("BILLING_TRIAL_DAYS"), 14); v > 0 {
		return v
	}
	return 14
}

func atoiOr(s string, def int) int {
	n := 0
	if s == "" {
		return def
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}

type publicSignupReq struct {
	// Conta
	OrgName  string `json:"org_name"`
	Domain   string `json:"domain"`   // acme.com
	Login    string `json:"login"`    // parte antes do @
	Name     string `json:"name"`     // nome de quem se regista
	Password string `json:"password"` // senha da caixa (Stalwart + painel)
	// Plano
	Plan  string `json:"plan"`
	Cycle string `json:"cycle"`
	// Faturação (mesmos campos do perfil; o checkout usa-os para a fatura)
	Billing billingProfileReq `json:"billing"`
}

func (s *Server) handlePublicSignup(w http.ResponseWriter, r *http.Request) {
	// O registo cria contas e fala com o Stalwart: sem limite, um script criava organizações à
	// velocidade da rede.
	if !s.loginIP.allow("signup:"+clientIP(r), time.Now()) {
		writeErr(w, http.StatusTooManyRequests, "auth.rate_limited")
		return
	}
	if !s.signupsOpen {
		writeErr(w, http.StatusForbidden, "signup.closed")
		return
	}
	var req publicSignupReq
	if !decode(w, r, &req) {
		return
	}
	req.OrgName = strings.TrimSpace(req.OrgName)
	req.Domain = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(req.Domain, "https://"), "http://")))
	req.Domain = strings.TrimSuffix(strings.TrimPrefix(req.Domain, "www."), "/")
	req.Login = strings.ToLower(strings.TrimSpace(req.Login))
	req.Name = strings.TrimSpace(req.Name)

	switch {
	case req.OrgName == "":
		writeErr(w, http.StatusBadRequest, "signup.org_required")
		return
	case !domainRe.MatchString(req.Domain):
		writeErr(w, http.StatusBadRequest, "signup.domain_invalid")
		return
	case !loginRe.MatchString(req.Login):
		writeErr(w, http.StatusBadRequest, "signup.login_invalid")
		return
	case req.Password != "" && len(req.Password) < 10:
		writeErr(w, http.StatusBadRequest, "signup.password_weak")
		return
	}
	// Sem senha no registo (o caminho normal): geramos uma aleatória e quem entra define a sua no
	// primeiro acesso. É menos um campo antes do cartão, e a senha nunca viaja de graça no formulário.
	if req.Password == "" {
		pw, err := auth.NewSessionToken() // 32 bytes aleatórios em base64url
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
			return
		}
		req.Password = pw
	}
	if code := validateProfile(&req.Billing); code != "" {
		writeErr(w, http.StatusBadRequest, code)
		return
	}
	plan, ok := s.catalogPlanByKey(r.Context(), req.Plan)
	if !ok || !plan.IsActive {
		writeErr(w, http.StatusBadRequest, "billing.invalid_plan")
		return
	}
	email := req.Login + "@" + req.Domain
	// Domínio já registado (por outra organização ou por esta) → não se rouba nem se duplica.
	if _, err := s.q.GetDomainByName(r.Context(), req.Domain); err == nil {
		writeErr(w, http.StatusConflict, "signup.domain_taken")
		return
	}
	if _, err := s.q.GetMailUserByEmail(r.Context(), email); err == nil {
		writeErr(w, http.StatusConflict, "auth.email_taken")
		return
	}

	tenant, err := s.q.CreateTenant(r.Context(), db.CreateTenantParams{Nome: req.OrgName, Plano: billing.DefaultPlanID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "tenant.create_failed")
		return
	}
	// A partir daqui, qualquer falha desfaz tudo (o tenant cai em cascata com domínio e caixa).
	rollback := func(reason string, err error) {
		log.Printf("signup: %s: %v (a desfazer o tenant %s)", reason, err, tenant.ID)
		_ = s.q.DeleteTenant(context.WithoutCancel(r.Context()), tenant.ID)
	}

	dom, err := s.q.CreateDomain(r.Context(), db.CreateDomainParams{TenantID: tenant.ID, Dominio: req.Domain})
	if err != nil {
		rollback("criar domínio", err)
		writeErr(w, http.StatusConflict, "signup.domain_taken")
		return
	}
	// Cria o domínio no Stalwart já (é o 1º passo da máquina de provisionamento): sem ele não há
	// onde pendurar a caixa. O worker apanha o resto — DKIM, zona DNS e ativação.
	// autoDKIM segue a mesma regra do worker: com relay SES quem assina é o SES (BYODKIM).
	autoDKIM := os.Getenv("SES_REGION") == "" || os.Getenv("AWS_ACCESS_KEY_ID") == ""
	swDomainID, err := s.sw.CreateDomain(r.Context(), req.Domain, "tenant_id="+tenant.ID.String(), autoDKIM)
	if err != nil {
		rollback("criar domínio no Stalwart", err)
		writeErr(w, http.StatusBadGateway, "signup.provision_failed")
		return
	}
	if _, err := s.q.UpdateDomainProvision(r.Context(), db.UpdateDomainProvisionParams{
		ID: dom.ID, ProvisionState: provisioning.StateDomainCreated, StalwartID: &swDomainID,
		DkimStatus: dom.DkimStatus, DnsStatus: dom.DnsStatus, DnsZone: dom.DnsZone,
	}); err != nil {
		rollback("guardar o estado do domínio", err)
		writeErr(w, http.StatusInternalServerError, "signup.provision_failed")
		return
	}

	// Caixa do dono no Stalwart + utilizador (identidade unificada: caixa, senha e papel num só).
	acctID, err := s.sw.CreateAccount(r.Context(), stalwart.AccountInput{
		Login: req.Login, DomainID: swDomainID, DisplayName: req.Name, Password: req.Password, QuotaBytes: 5 << 30,
	})
	if err != nil {
		rollback("criar a caixa no Stalwart", err)
		if errors.Is(err, stalwart.ErrWeakPassword) {
			writeErr(w, http.StatusBadRequest, "mailbox.password_weak")
			return
		}
		writeErr(w, http.StatusBadGateway, "signup.provision_failed")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		rollback("cifrar a senha", err)
		writeErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
		return
	}
	owner, err := s.q.CreateMailUser(r.Context(), db.CreateMailUserParams{
		TenantID: tenant.ID, Email: email, Name: req.Name, Role: string(rbac.RoleOwner),
		StalwartAccountID: &acctID, QuotaBytes: 5 << 30, PasswordHash: hash,
	})
	if err != nil {
		rollback("criar o utilizador", err)
		writeErr(w, http.StatusConflict, "auth.email_taken")
		return
	}

	// Dados de faturação: guardados antes do checkout, para a fatura sair completa desde a primeira.
	b := req.Billing
	if _, err := s.q.UpsertBillingProfile(r.Context(), db.UpsertBillingProfileParams{
		TenantID: tenant.ID, Kind: b.Kind, LegalName: b.LegalName, TaxID: b.TaxID, Country: b.Country,
		City: b.City, PostalCode: b.PostalCode, AddressLine1: b.AddressLine1, AddressLine2: b.AddressLine2,
		InvoiceEmail: b.InvoiceEmail, InvoiceName: b.InvoiceName, Phone: b.Phone,
	}); err != nil {
		log.Printf("signup: guardar faturação do tenant %s: %v", tenant.ID, err) // não trava o registo
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: tenant.ID, Valid: true}, Actor: email, Acao: "signup", Alvo: req.Domain,
	})

	// Sessão: quem se regista entra na app a seguir ao pagamento, sem repetir a senha.
	token, ok := s.issueSession(w, r.Context(), owner)
	if !ok {
		return
	}
	// Credencial JMAP (Basic da caixa) cifrada server-side, como no login: é o que o proxy injeta
	// para falar com o Stalwart. Sem isto o webmail respondia 401 logo a seguir ao registo.
	basic := base64.StdEncoding.EncodeToString([]byte(email + ":" + req.Password))
	_ = s.q.SetJmapCred(r.Context(), db.SetJmapCredParams{ID: owner.ID, JmapCred: s.encTOTP(basic)})

	// Avisos: a conta existe (com o endereço e o que falta no DNS) e o teste está a correr. Vão para
	// o endereço criado; o de faturação recebe os avisos de dinheiro.
	s.alertAccountCreated(owner.Language, email, req.Domain)
	s.alertTrialStarted(owner.Language, email, plan.Name, trialDays(), planAmount(s.localPlan(plan, s.tenantCurrency(r.Context(), tenant.ID)), req.Cycle))

	out := map[string]any{"tenant_id": tenant.ID, "email": email, "token": token, "trial_days": trialDays()}
	if url := s.signupCheckoutURL(r, tenant.ID, plan, req.Cycle); url != "" {
		out["checkout_url"] = url
	}
	writeJSON(w, http.StatusCreated, out)
}

// signupCheckoutURL cria o checkout do teste (cartão pedido à partida, cobrança só no fim). Falhar
// aqui não desfaz o registo: a conta existe e a subscrição pode ser feita depois em Faturação.
func (s *Server) signupCheckoutURL(r *http.Request, tid uuid.UUID, plan billing.Plan, cycle string) string {
	plan = s.localPlan(plan, s.tenantCurrency(r.Context(), tid))
	seats := plan.MinUsers
	if seats < 1 {
		seats = 1
	}
	cus, invEmail := s.checkoutCustomer(r, tid)
	res, err := s.billing.Checkout(r.Context(), tid.String(), plan, seats, billing.CheckoutOpts{
		Cycle: billing.Cycle(cycle).Normalize(), CustomerID: cus, InvoiceEmail: invEmail, TrialDays: trialDays(),
	})
	if err != nil {
		log.Printf("signup: checkout do tenant %s: %v", tid, err)
		return ""
	}
	return res.URL
}

// --- Catálogo e simulação PÚBLICOS (a página de registo ainda não tem sessão) ---

// handlePublicPlans devolve os planos ativos com o preço na moeda base. Não expõe nada que já não
// esteja na página de preços do site.
func (s *Server) handlePublicPlans(w http.ResponseWriter, r *http.Request) {
	cur := s.billingCfg.Currency
	plans := make([]billing.Plan, 0)
	for _, p := range s.activePlans(r.Context()) {
		if p.PriceCentsPerUser <= 0 {
			continue // grátis/enterprise não se assinam por aqui
		}
		plans = append(plans, s.localPlan(p, cur))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plans": plans, "currency": cur, "trial_days": trialDays(), "signups_open": s.signupsOpen,
		"months_charged": billing.AnnualMonthsCharged, "seller_country": billing.SellerCountry,
	})
}

type publicQuoteReq struct {
	Plan    string `json:"plan"`
	Cycle   string `json:"cycle"`
	Kind    string `json:"kind"`
	Country string `json:"country"`
	TaxID   string `json:"tax_id"`
	Seats   int    `json:"seats"`
}

// handlePublicQuote é a mesma conta do /api/billing/quote para quem ainda não tem conta: a página
// de registo tem de mostrar o total com IVA antes de existir organização.
func (s *Server) handlePublicQuote(w http.ResponseWriter, r *http.Request) {
	var req publicQuoteReq
	if !decode(w, r, &req) {
		return
	}
	plan, ok := s.catalogPlanByKey(r.Context(), req.Plan)
	if !ok || !plan.IsActive {
		writeErr(w, http.StatusBadRequest, "billing.invalid_plan")
		return
	}
	plan = s.localPlan(plan, s.billingCfg.Currency)
	cycle := billing.Cycle(req.Cycle).Normalize()
	seats := req.Seats
	if seats < plan.MinUsers {
		seats = plan.MinUsers
	}
	if seats < 1 {
		seats = 1
	}
	q := billing.QuoteFor(plan.Currency, plan.UnitCents(cycle), seats, req.Kind, req.Country, req.TaxID)
	writeJSON(w, http.StatusOK, map[string]any{
		"quote": q, "cycle": string(cycle), "plan": plan.ID, "plan_name": plan.Name,
		"monthly_cents": plan.PriceCentsPerUser, "months_charged": billing.AnnualMonthsCharged,
		"seller_country": billing.SellerCountry, "vat_rate_percent": float64(q.VATRateBps) / 100,
		"trial_days": trialDays(),
	})
}

// planAmount formata "12,00 EUR/mês" para os avisos — o valor por utilizador, como no checkout.
func planAmount(plan billing.Plan, cycle string) string {
	c := billing.Cycle(cycle).Normalize()
	unit := float64(plan.UnitCents(c)) / 100
	per := "/mês"
	if c == billing.CycleYear {
		per = "/ano"
	}
	return fmt.Sprintf("%.2f %s%s", unit, plan.Currency, per)
}
