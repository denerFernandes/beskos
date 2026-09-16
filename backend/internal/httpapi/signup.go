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

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
	"github.com/beskosorg/beskos/backend/internal/rbac"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// Self-service signup (/signup): in a single pass, creates the organization, the domain, the
// owner's MAILBOX, and the billing data, and returns the checkout URL with a trial period.
//
// The mailbox is created with the domain still in `domain_created` (DNS pending publication),
// not in `active` as the panel requires: whoever just signed up has to be able to log in and use
// chat, drive, calendar, and meetings the next minute. External e-mail only starts arriving once
// DNS is published — and the app says so at the top until then.
//
// If something fails midway, whatever was already created gets undone: half an organization (no
// mailbox, no domain) would lock the e-mail and the domain forever, with no one inside to free them.

var (
	domainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	loginRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
)

// trialDays is the checkout's free trial. The card is requested upfront (a product decision), but
// is only charged at the end of the period.
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
	// Account
	OrgName  string `json:"org_name"`
	Domain   string `json:"domain"`   // acme.com
	Login    string `json:"login"`    // part before the @
	Name     string `json:"name"`     // name of whoever is signing up
	Password string `json:"password"` // mailbox password (Stalwart + panel)
	// Plan
	Plan  string `json:"plan"`
	Cycle string `json:"cycle"`
	// Billing (same fields as the profile; checkout uses them for the invoice)
	Billing billingProfileReq `json:"billing"`
}

func (s *Server) handlePublicSignup(w http.ResponseWriter, r *http.Request) {
	// Signup creates accounts and talks to Stalwart: without a limit, a script could create
	// organizations at network speed.
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
	// No password on signup (the normal path): we generate a random one and the user sets their
	// own on first login. It's one fewer field before the card, and the password never travels
	// unnecessarily through the form.
	if req.Password == "" {
		pw, err := auth.NewSessionToken() // 32 random bytes in base64url
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
	// Domain already registered (by another organization or this one) → never stolen, never duplicated.
	if _, err := s.q.GetDomainByName(r.Context(), req.Domain); err == nil {
		writeErr(w, http.StatusConflict, "signup.domain_taken")
		return
	}
	if _, err := s.q.GetMailUserByEmail(r.Context(), email); err == nil {
		writeErr(w, http.StatusConflict, "auth.email_taken")
		return
	}

	tenant, err := s.q.CreateTenant(r.Context(), db.CreateTenantParams{Name: req.OrgName, Plan: billing.DefaultPlanID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "tenant.create_failed")
		return
	}
	// From here on, any failure undoes everything (the tenant cascades away with domain and mailbox).
	rollback := func(reason string, err error) {
		log.Printf("signup: %s: %v (rolling back tenant %s)", reason, err, tenant.ID)
		_ = s.q.DeleteTenant(context.WithoutCancel(r.Context()), tenant.ID)
	}

	dom, err := s.q.CreateDomain(r.Context(), db.CreateDomainParams{TenantID: tenant.ID, Domain: req.Domain})
	if err != nil {
		rollback("create domain", err)
		writeErr(w, http.StatusConflict, "signup.domain_taken")
		return
	}
	// Create the domain in Stalwart right away (it's the 1st step of the provisioning machine):
	// without it there's nowhere to hang the mailbox. The worker picks up the rest — DKIM, DNS
	// zone, and activation.
	// autoDKIM follows the same rule as the worker: with an SES relay, SES does the signing (BYODKIM).
	autoDKIM := os.Getenv("SES_REGION") == "" || os.Getenv("AWS_ACCESS_KEY_ID") == ""
	swDomainID, err := s.sw.CreateDomain(r.Context(), req.Domain, "tenant_id="+tenant.ID.String(), autoDKIM)
	if err != nil {
		rollback("create domain in Stalwart", err)
		writeErr(w, http.StatusBadGateway, "signup.provision_failed")
		return
	}
	if _, err := s.q.UpdateDomainProvision(r.Context(), db.UpdateDomainProvisionParams{
		ID: dom.ID, ProvisionState: provisioning.StateDomainCreated, StalwartID: &swDomainID,
		DkimStatus: dom.DkimStatus, DnsStatus: dom.DnsStatus, DnsZone: dom.DnsZone,
	}); err != nil {
		rollback("save domain state", err)
		writeErr(w, http.StatusInternalServerError, "signup.provision_failed")
		return
	}

	// Owner's mailbox in Stalwart + user (unified identity: mailbox, password, and role in one).
	acctID, err := s.sw.CreateAccount(r.Context(), stalwart.AccountInput{
		Login: req.Login, DomainID: swDomainID, DisplayName: req.Name, Password: req.Password, QuotaBytes: 5 << 30,
	})
	if err != nil {
		rollback("create mailbox in Stalwart", err)
		if errors.Is(err, stalwart.ErrWeakPassword) {
			writeErr(w, http.StatusBadRequest, "mailbox.password_weak")
			return
		}
		writeErr(w, http.StatusBadGateway, "signup.provision_failed")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		rollback("hash password", err)
		writeErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
		return
	}
	owner, err := s.q.CreateMailUser(r.Context(), db.CreateMailUserParams{
		TenantID: tenant.ID, Email: email, Name: req.Name, Role: string(rbac.RoleOwner),
		StalwartAccountID: &acctID, QuotaBytes: 5 << 30, PasswordHash: hash,
	})
	if err != nil {
		rollback("create user", err)
		writeErr(w, http.StatusConflict, "auth.email_taken")
		return
	}

	// Billing data: saved before checkout, so the invoice comes out complete from the first one.
	b := req.Billing
	if _, err := s.q.UpsertBillingProfile(r.Context(), db.UpsertBillingProfileParams{
		TenantID: tenant.ID, Kind: b.Kind, LegalName: b.LegalName, TaxID: b.TaxID, Country: b.Country,
		City: b.City, PostalCode: b.PostalCode, AddressLine1: b.AddressLine1, AddressLine2: b.AddressLine2,
		InvoiceEmail: b.InvoiceEmail, InvoiceName: b.InvoiceName, Phone: b.Phone,
	}); err != nil {
		log.Printf("signup: save billing for tenant %s: %v", tenant.ID, err) // doesn't block signup
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: tenant.ID, Valid: true}, Actor: email, Action: "signup", Target: req.Domain,
	})

	// Session: whoever signs up enters the app right after payment, without re-entering the password.
	token, ok := s.issueSession(w, r.Context(), owner)
	if !ok {
		return
	}
	// JMAP credential (mailbox's Basic auth) encrypted server-side, same as login: it's what the
	// proxy injects to talk to Stalwart. Without this the webmail would answer 401 right after signup.
	basic := base64.StdEncoding.EncodeToString([]byte(email + ":" + req.Password))
	_ = s.q.SetJmapCred(r.Context(), db.SetJmapCredParams{ID: owner.ID, JmapCred: s.encTOTP(basic)})

	// Alerts: the account exists (with the address and what's still missing in DNS) and the trial
	// is running. They go to the address just created; the billing address gets the money alerts.
	s.alertAccountCreated(owner.Language, email, req.Domain)
	s.alertTrialStarted(owner.Language, email, plan.Name, trialDays(), planAmount(s.localPlan(plan, s.tenantCurrency(r.Context(), tenant.ID)), req.Cycle))

	out := map[string]any{"tenant_id": tenant.ID, "email": email, "token": token, "trial_days": trialDays()}
	if url := s.signupCheckoutURL(r, tenant.ID, plan, req.Cycle); url != "" {
		out["checkout_url"] = url
	}
	writeJSON(w, http.StatusCreated, out)
}

// signupCheckoutURL creates the trial checkout (card requested upfront, charged only at the end).
// Failing here doesn't undo the signup: the account exists and the subscription can be done
// later in Billing.
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
		log.Printf("signup: checkout for tenant %s: %v", tid, err)
		return ""
	}
	return res.URL
}

// --- Public catalog and simulation endpoints (the signup page has no session yet) ---

// handlePublicPlans returns the active plans with the price in the base currency. It doesn't
// expose anything that isn't already on the site's pricing page.
func (s *Server) handlePublicPlans(w http.ResponseWriter, r *http.Request) {
	cur := s.billingCfg.Currency
	plans := make([]billing.Plan, 0)
	for _, p := range s.activePlans(r.Context()) {
		if p.PriceCentsPerUser <= 0 {
			continue // free/enterprise plans aren't subscribed to through here
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

// handlePublicQuote is the same computation as /api/billing/quote for someone who doesn't have an
// account yet: the signup page has to show the total with VAT before the organization exists.
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

// planAmount formats a price with its billing-cycle suffix (e.g. "12.00 EUR" plus per-month or
// per-year) for the alerts — the per-user amount, as in checkout.
func planAmount(plan billing.Plan, cycle string) string {
	c := billing.Cycle(cycle).Normalize()
	unit := float64(plan.UnitCents(c)) / 100
	per := "/mês"
	if c == billing.CycleYear {
		per = "/ano"
	}
	return fmt.Sprintf("%.2f %s%s", unit, plan.Currency, per)
}
