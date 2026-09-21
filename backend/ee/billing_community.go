//go:build !pro

package ee

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// State and Account are the Community build's own account-state shape: this build links none of
// the real subscription evaluation (see billing_account_pro.go's Evaluate for what a Pro build
// actually computes from a subscription's status and period end). A Community install has no
// hosted subscription to evaluate in the first place, so there is only ever one state to report.
type State string

// StateOK is the only state a Community build ever reports.
const StateOK State = "ok"

// Account is what AccountState hands back.
type Account struct {
	State State     `json:"state"`
	Until time.Time `json:"until"`
}

// Blocked reports whether the organisation may still send. Always false: self-hosted access
// never depends on a hosted subscription or trial deadline.
func (a Account) Blocked() bool { return false }

// AccountState is the Community build's answer, matching today's
// scripts/public/billing_routes.go stub: an account state that's always OK.
func AccountState(context.Context, uuid.UUID) Account { return Account{State: StateOK} }

// SyncSeats is a no-op: there is no hosted subscription to keep a seat count in sync with.
func SyncSeats(uuid.UUID) {}
