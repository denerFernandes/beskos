package httpapi

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/billing"
	"mailin/backend/internal/db"
)

// handleUsage devolve uso vs. limites do plano do tenant (painel de uso, brief §9 Fase 4).
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	tid := u.TenantID
	plan := s.tenantPlan(r, tid)
	mailboxes, _ := s.q.CountMailboxesByTenant(r.Context(), tid)
	storageBytes, _ := s.q.SumStorageBytes(r.Context(), tid)
	sentToday, _ := s.q.MailSentToday(r.Context(), tid)
	writeJSON(w, http.StatusOK, map[string]any{
		"plan": plan,
		"usage": map[string]any{
			"mailboxes":       mailboxes,
			"storage_bytes":   storageBytes,
			"mail_sent_today": sentToday,
		},
		"limits": map[string]any{
			"max_mailboxes":     plan.MaxMailboxes,
			"max_storage_bytes": plan.MaxStorageBytes,
			"max_daily_send":    plan.MaxDailySend,
		},
	})
}

func (s *Server) handleGetBilling(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	plan := s.tenantPlan(r, u.TenantID)
	var subStatus string
	sub, subErr := s.q.GetSubscription(r.Context(), u.TenantID)
	if subErr == nil {
		subStatus = sub.Status
	}
	seats, _ := s.q.CountMailboxesByTenant(r.Context(), u.TenantID)
	cur := s.tenantCurrency(r.Context(), u.TenantID)
	localPlans := make([]billing.Plan, 0)
	for _, p := range s.activePlans(r.Context()) {
		localPlans = append(localPlans, s.localPlan(p, cur))
	}
	lp := s.localPlan(plan, cur)
	out := map[string]any{
		"provider":       s.billing.Name(),
		"current_plan":   lp,
		"status":         subStatus,
		"plans":          localPlans,
		"currency":       lp.Currency,
		"min_paid_users": s.billingCfg.MinPaidUsers,
		"seats":          seats, // usuários atuais (base do cálculo por usuário)
	}
	// Assinatura no provedor (Stripe): estado real para o painel gerir (cancelar/retomar/portal).
	if subErr == nil && sub.StripeID != nil && *sub.StripeID != "" && s.billing.Name() == "stripe" {
		if info, err := s.billing.Subscription(r.Context(), *sub.StripeID); err == nil {
			out["subscription"] = map[string]any{
				"manageable":           true,
				"status":               info.Status,
				"cancel_at_period_end": info.CancelAtPeriodEnd,
				"current_period_end":   info.CurrentPeriodEnd,
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// stripeSubID devolve o id da assinatura Stripe do tenant ("" se não há).
func (s *Server) stripeSubID(r *http.Request, tid uuid.UUID) string {
	if s.billing.Name() != "stripe" {
		return ""
	}
	sub, err := s.q.GetSubscription(r.Context(), tid)
	if err != nil || sub.StripeID == nil {
		return ""
	}
	return *sub.StripeID
}

// billableSeats = max(usuários atuais, mínimo pago). Preço por usuário × seats.
func (s *Server) billableSeats(r *http.Request, tid [16]byte) int {
	n, _ := s.q.CountMailboxesByTenant(r.Context(), tid)
	seats := int(n)
	if seats < s.billingCfg.MinPaidUsers {
		seats = s.billingCfg.MinPaidUsers
	}
	if seats < 1 {
		seats = 1
	}
	return seats
}

type checkoutReq struct {
	Plan  string `json:"plan"`
	Cycle string `json:"cycle"` // month (default) | year
}

// checkoutCustomer garante o cliente no provedor (razão social, morada, NIF, e-mail do financeiro)
// e devolve o id + o e-mail de faturação. Sem perfil preenchido devolve vazios — o checkout segue
// e o provedor pede o que precisar.
func (s *Server) checkoutCustomer(r *http.Request, tid uuid.UUID) (customerID, invoiceEmail string) {
	p, err := s.q.GetBillingProfile(r.Context(), tid)
	if err != nil || p.LegalName == "" {
		return "", ""
	}
	id, err := s.billing.EnsureCustomer(r.Context(), p.StripeCustomer, billingCustomer(uuid.UUID(tid).String(), p))
	if err != nil {
		log.Printf("billing: ensure customer for tenant %s: %v", tid, err)
		return "", p.InvoiceEmail // ao menos a fatura vai para o financeiro
	}
	if id != "" && id != p.StripeCustomer {
		_ = s.q.SetBillingStripeCustomer(r.Context(), db.SetBillingStripeCustomerParams{TenantID: tid, StripeCustomer: id})
	}
	return id, p.InvoiceEmail
}

// handleCheckout inicia a mudança de plano. No provedor Manual aplica na hora; no Stripe
// devolve a URL de checkout (a ativação real vem pelo webhook).
func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req checkoutReq
	if !decode(w, r, &req) {
		return
	}
	// Valida contra o CATÁLOGO DA BD (M2): plano tem de existir e estar ativo; a organização tem
	// de caber no teto do plano; o mínimo de seats cobrados é o piso do plano (faixa).
	plan, ok := s.catalogPlanByKey(r.Context(), req.Plan)
	if !ok || !plan.IsActive {
		writeErr(w, http.StatusBadRequest, "billing.invalid_plan")
		return
	}
	seats := s.billableSeats(r, u.TenantID)
	if plan.MaxMailboxes > 0 && seats > plan.MaxMailboxes {
		writeErr(w, http.StatusBadRequest, "billing.plan_too_small")
		return
	}
	if seats < plan.MinUsers {
		seats = plan.MinUsers
	}
	cycle := billing.Cycle(req.Cycle).Normalize()
	// Moeda por país: o preço/checkout usa a moeda do tenant (fallback: base).
	plan = s.localPlan(plan, s.tenantCurrency(r.Context(), u.TenantID))
	// Assinatura Stripe já ativa → UPGRADE/DOWNGRADE na própria assinatura (com rateio), sem
	// novo checkout (que criaria uma segunda assinatura).
	if subID := s.stripeSubID(r, u.TenantID); subID != "" {
		if err := s.billing.UpdatePlan(r.Context(), subID, plan, seats, cycle); err != nil {
			writeErr(w, http.StatusBadGateway, "billing.checkout_failed")
			return
		}
		s.applyPlan(r, u.TenantID, req.Plan, subID, "active")
		_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "billing.plan_change", Target: req.Plan})
		writeJSON(w, http.StatusOK, billing.CheckoutResult{Applied: true})
		return
	}
	// Cliente no provedor com os dados de faturação: o checkout fica só com o cartão e a fatura
	// sai com razão social, morada e NIF.
	cus, invEmail := s.checkoutCustomer(r, u.TenantID)
	res, err := s.billing.Checkout(r.Context(), uuid.UUID(u.TenantID).String(), plan, seats,
		billing.CheckoutOpts{Cycle: cycle, CustomerID: cus, InvoiceEmail: invEmail})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "billing.checkout_failed")
		return
	}
	if res.Applied { // Manual: aplica imediatamente
		s.applyPlan(r, u.TenantID, req.Plan, "", "active")
		_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "billing.plan_change", Target: req.Plan})
	}
	writeJSON(w, http.StatusOK, res)
}

// handleBillingCancel agenda o cancelamento para o FIM do período pago (padrão SaaS). No provedor
// manual, volta ao plano default imediatamente.
func (s *Server) handleBillingCancel(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	if subID := s.stripeSubID(r, u.TenantID); subID != "" {
		if err := s.billing.SetCancelAtPeriodEnd(r.Context(), subID, true); err != nil {
			writeErr(w, http.StatusBadGateway, "billing.cancel_failed")
			return
		}
	} else {
		// Manual: sem provedor externo — aplica o plano default na hora.
		s.applyPlan(r, u.TenantID, billing.DefaultPlanID, "", "canceled")
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "billing.cancel", Target: ""})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleBillingResume desfaz um cancelamento agendado (antes do fim do período).
func (s *Server) handleBillingResume(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	subID := s.stripeSubID(r, u.TenantID)
	if subID == "" {
		writeErr(w, http.StatusBadRequest, "billing.no_subscription")
		return
	}
	if err := s.billing.SetCancelAtPeriodEnd(r.Context(), subID, false); err != nil {
		writeErr(w, http.StatusBadGateway, "billing.resume_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "billing.resume", Target: ""})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleBillingPortal abre o Billing Portal do Stripe (trocar forma de pagamento, faturas).
func (s *Server) handleBillingPortal(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	subID := s.stripeSubID(r, u.TenantID)
	if subID == "" {
		writeErr(w, http.StatusBadRequest, "billing.no_subscription")
		return
	}
	info, err := s.billing.Subscription(r.Context(), subID)
	if err != nil || info.Customer == "" {
		writeErr(w, http.StatusBadGateway, "billing.portal_failed")
		return
	}
	ret := s.publicURL + "/mail/admin/billing"
	link, err := s.billing.PortalURL(r.Context(), info.Customer, ret)
	if err != nil || link == "" {
		writeErr(w, http.StatusBadGateway, "billing.portal_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": link})
}

// handleBillingWebhook aplica eventos do provedor. Idempotente por event_id (brief §10).
func (s *Server) handleBillingWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "billing.bad_body")
		return
	}
	ev, err := s.billing.ParseWebhook(body, r.Header.Get("Stripe-Signature"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "billing.bad_signature")
		return
	}
	if ev == nil { // evento sem efeito
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	// Idempotência: se o evento já foi processado, não reaplica.
	if ev.EventID != "" {
		if _, err := s.q.MarkWebhookProcessed(r.Context(), ev.EventID); err != nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
			return
		}
	}
	if tid, err := uuid.Parse(ev.TenantID); err == nil {
		// What they have paid up to, whenever the provider mentions it. Written before the switch
		// because every branch below wants it to already be true, and none of them owns it.
		if ev.PeriodEnd > 0 {
			_ = s.q.SetSubscriptionPeriodEnd(r.Context(), db.SetSubscriptionPeriodEndParams{
				TenantID:         tid,
				CurrentPeriodEnd: pgtype.Timestamptz{Time: time.Unix(ev.PeriodEnd, 0).UTC(), Valid: true},
			})
		}
		switch {
		case ev.Type == "charged":
			// Dinheiro cobrado: avisa quem paga, com o valor e o link da fatura.
			s.alertCharge(r.Context(), tid, s.planLabel(r.Context(), ev.PlanKey), money(ev.AmountCents, ev.Currency), ev.InvoiceURL)
		case ev.Plan == "":
			// Evento só de STATUS (ex.: customer.subscription.updated → past_due): não mexe no plano.
			_ = s.q.UpdateSubscriptionStatus(r.Context(), db.UpdateSubscriptionStatusParams{TenantID: tid, Status: ev.Status})
		default:
			s.applyPlan(r, tid, ev.Plan, ev.SubscriptionID, ev.Status)
			if ev.Type == "activated" {
				plan := s.planByKey(r.Context(), ev.Plan)
				lp := s.localPlan(plan, s.tenantCurrency(r.Context(), tid))
				cycle := billing.Cycle(ev.Cycle).Normalize()
				s.alertSubscription(r.Context(), tid, plan.Name, cycleLabel(cycle),
					money(lp.UnitCents(cycle), lp.Currency)+perLabel(cycle))
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

// syncSeats espelha o nº de utilizadores na QUANTIDADE da assinatura Stripe, com rateio: um
// utilizador adicionado a meio do período entra proporcional na próxima fatura; um removido gera
// crédito. Chamado após criar/remover utilizador/caixa (24/07/2026). Best-effort em goroutine —
// nunca bloqueia nem falha a operação de origem.
func (s *Server) syncSeats(tid uuid.UUID) {
	if s.billing.Name() != "stripe" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sub, err := s.q.GetSubscription(ctx, tid)
		if err != nil || sub.StripeID == nil || *sub.StripeID == "" {
			return
		}
		plan := s.tenantPlanByID(ctx, tid)
		n, _ := s.q.CountMailboxesByTenant(ctx, tid)
		seats := int(n)
		if seats < s.billingCfg.MinPaidUsers {
			seats = s.billingCfg.MinPaidUsers
		}
		if seats < plan.MinUsers {
			seats = plan.MinUsers
		}
		if seats < 1 {
			seats = 1
		}
		if err := s.billing.UpdateQuantity(ctx, *sub.StripeID, seats); err != nil {
			log.Printf("billing: seat sync for tenant %s: %v", tid, err)
		}
	}()
}

// tenantCurrency devolve a moeda do tenant (” → a global BILLING_CURRENCY).
func (s *Server) tenantCurrency(ctx context.Context, tid uuid.UUID) string {
	if t, err := s.q.GetTenant(ctx, tid); err == nil && t.Currency != "" {
		return t.Currency
	}
	return s.billingCfg.Currency
}

// localPlan devolve o plano com o PREÇO na moeda do tenant (PriceFor; cai na base sem inventar
// câmbio) e a moeda efetiva anotada — o frontend continua a ver um preço único.
func (s *Server) localPlan(p billing.Plan, currency string) billing.Plan {
	cents, cur := p.PriceFor(currency, s.billingCfg.Currency)
	p.PriceCentsPerUser = cents
	p.Currency = cur
	p.Prices = nil
	return p
}

// --- helpers ---

func (s *Server) tenantPlan(r *http.Request, tid uuid.UUID) billing.Plan {
	return s.tenantPlanByID(r.Context(), tid)
}

// tenantPlanByID é a mesma coisa sem depender de um *http.Request — serve quem corre fora de um
// pedido HTTP, como a geração de ficheiros pelo assistente.
func (s *Server) tenantPlanByID(ctx context.Context, tid uuid.UUID) billing.Plan {
	t, err := s.q.GetTenant(ctx, tid)
	if err != nil {
		return s.planByKey(ctx, billing.DefaultPlanID)
	}
	return s.planByKey(ctx, t.Plan)
}

func (s *Server) applyPlan(r *http.Request, tid uuid.UUID, plan, stripeID, status string) {
	// Valida contra o catálogo da BD (M2); plano desconhecido cai no default (fail-safe).
	p, ok := s.catalogPlanByKey(r.Context(), plan)
	if !ok {
		p = s.planByKey(r.Context(), billing.DefaultPlanID)
		plan = p.ID
	}
	_ = s.q.UpdateTenantPlan(r.Context(), db.UpdateTenantPlanParams{ID: tid, Plan: plan})
	_ = s.q.UpsertSubscription(r.Context(), db.UpsertSubscriptionParams{
		TenantID: tid, StripeID: &stripeID, MailboxRange: p.MailboxRange, Status: status,
	})
}

// money formata um valor em cêntimos para os avisos por e-mail ("12.00 EUR").
func money(cents int, currency string) string {
	if currency == "" {
		currency = "EUR"
	}
	return fmt.Sprintf("%.2f %s", float64(cents)/100, strings.ToUpper(currency))
}

func cycleLabel(c billing.Cycle) string {
	if c == billing.CycleYear {
		return "anual"
	}
	return "mensal"
}

func perLabel(c billing.Cycle) string {
	if c == billing.CycleYear {
		return "/ano por utilizador"
	}
	return "/mês por utilizador"
}

// planLabel devolve o nome do plano a partir da chave (para o texto do aviso).
func (s *Server) planLabel(ctx context.Context, key string) string {
	if key == "" {
		return "Beskos"
	}
	if p, ok := s.catalogPlanByKey(ctx, key); ok {
		return p.Name
	}
	return key
}
