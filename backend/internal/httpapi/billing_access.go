package httpapi

import (
	"context"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/google/uuid"
	"time"
)

// accountState resolves the state of the organisation this user belongs to.
//
// Fails OPEN. If the subscription or the organisation cannot be read, the answer is "ok": a
// database hiccup must never be the reason a paying customer cannot send mail. The failure mode of
// the opposite choice is silent and total, and it arrives at the worst possible moment.
func (s *Server) accountState(ctx context.Context, tenant uuid.UUID) billing.Account {
	t, err := s.q.GetTenant(ctx, tenant)
	if err != nil {
		return billing.Account{State: billing.StateOK}
	}
	in := billing.Input{
		Now:       time.Now(),
		CreatedAt: t.CreatedAt,
		TrialDays: trialDays(),
	}
	if sub, err := s.q.GetSubscription(ctx, tenant); err == nil {
		in.Status = sub.Status
		if sub.CurrentPeriodEnd.Valid {
			in.PeriodEnd = sub.CurrentPeriodEnd.Time
		}
	}
	return billing.Evaluate(in)
}
