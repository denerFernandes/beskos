package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"mailin/backend/internal/billing"
	"mailin/backend/internal/stalwart"
)

// Rotas registadas. Escrever o handler não chega — se ninguém o liga ao router, a app responde 404
// e ninguém percebe até um cliente tentar (foi o que aconteceu com /api/aliases: handlers, queries,
// frontend e migração prontos, rota nenhuma). Este teste passa a apanhá-lo.
func TestRoutesRegistered(t *testing.T) {
	// Cliente Stalwart mínimo: o router monta o proxy JMAP e lê-lhe a URL base.
	srv := NewServer(nil, stalwart.New("http://stalwart.invalid", "", ""), nil, nil, nil,
		billing.ManualProvider{}, billing.Settings{Currency: "EUR", MinPaidUsers: 1})
	router, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatal("o router não expõe as rotas")
	}
	got := map[string]bool{}
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	})

	want := []string{
		// Aliases de e-mail (o caso que motivou o teste).
		"GET /api/aliases", "POST /api/aliases", "DELETE /api/aliases/{id}", "GET /api/me/addresses",
		// Registo self-service e o que a página pública precisa antes de haver conta.
		"POST /api/public/signup", "GET /api/public/plans", "POST /api/public/quote",
		// Faturação: perfil, simulação e checkout.
		"GET /api/billing/profile", "PUT /api/billing/profile", "POST /api/billing/quote",
		"POST /api/billing/checkout", "POST /api/billing/webhook",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("rota em falta: %s", w)
		}
	}
}
