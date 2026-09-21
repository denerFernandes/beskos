//go:build pro

package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
	"github.com/go-chi/chi/v5"
)

func TestHostedBillingRoutesRegistered(t *testing.T) {
	srv := NewServer(nil, stalwart.New("http://stalwart.invalid", "", ""), nil, nil, nil,
		billing.ManualProvider{}, billing.Settings{}, edition.CommunityCapabilities())
	got := map[string]bool{}
	err := chi.Walk(srv.Router().(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{
		"POST /api/public/signup", "GET /api/public/plans", "POST /api/public/quote",
		"GET /api/billing", "GET /api/billing/profile", "PUT /api/billing/profile",
		"POST /api/billing/quote", "POST /api/billing/checkout", "POST /api/billing/webhook",
		"POST /api/billing/cancel", "POST /api/billing/resume", "POST /api/billing/portal",
	} {
		if !got[route] {
			t.Errorf("missing hosted billing route: %s", route)
		}
	}
}
