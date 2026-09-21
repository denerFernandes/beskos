package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

func TestHostedRoutesRegistered(t *testing.T) {
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
		"POST /api/public/waitlist", "OPTIONS /api/public/waitlist",
		"POST /api/public/contact", "OPTIONS /api/public/contact", "POST /api/webhooks/ses",
	} {
		if !got[route] {
			t.Errorf("missing hosted route: %s", route)
		}
	}
}
