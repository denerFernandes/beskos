package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestSelfHostedRoutesExcludeHostedBilling(t *testing.T) {
	srv := NewServer(nil, stalwart.New("http://stalwart.invalid", "", ""), nil, nil, nil,
		billing.ManualProvider{}, billing.Settings{}, edition.CommunityCapabilities())
	usage := false
	err := chi.Walk(srv.Router().(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/billing") || route == "/api/public/signup" || route == "/api/public/plans" || route == "/api/public/quote" || route == "/api/public/waitlist" || route == "/api/public/contact" || route == "/api/webhooks/ses" {
			t.Errorf("hosted route exported: %s %s", method, route)
		}
		if method == "GET" && route == "/api/usage" {
			usage = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !usage {
		t.Fatal("usage route was removed with billing")
	}
}

func TestSelfHostedAccessDoesNotRequireSubscription(t *testing.T) {
	// A nil database proves this decision does not read hosted trial or subscription state.
	srv := &Server{}
	account := srv.accountState(context.Background(), uuid.New())
	if account.State != billing.StateOK || account.Blocked() || !account.Until.IsZero() {
		t.Fatalf("self-hosted account is restricted: %+v", account)
	}
}
