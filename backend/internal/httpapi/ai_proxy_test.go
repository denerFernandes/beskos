package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// O token de IA é a "API key" que o plugin do OnlyOffice usa para chegar ao proxy (cross-origin, sem
// cookie). Tem de ida-e-voltar com user+tenant, recusar assinatura adulterada e recusar expirados —
// senão seria um buraco por onde entrar sem identidade.
func TestAIToken(t *testing.T) {
	o := &onlyoffice{tokenSecret: []byte("test-secret-32-bytes-long-enough!")}
	user, tenant := uuid.New(), uuid.New()

	tok := o.mintAIToken(user, tenant, time.Now().Add(time.Hour))
	gotUser, gotTenant, ok := o.parseAIToken(tok)
	if !ok || gotUser != user || gotTenant != tenant {
		t.Fatalf("ida-e-volta falhou: ok=%v user=%v tenant=%v", ok, gotUser, gotTenant)
	}

	// Assinatura adulterada → recusa.
	if _, _, ok := o.parseAIToken(tok + "x"); ok {
		t.Error("aceitou token adulterado")
	}

	// Outro segredo não valida o token deste servidor.
	other := &onlyoffice{tokenSecret: []byte("another-secret-different-key-here")}
	if _, _, ok := other.parseAIToken(tok); ok {
		t.Error("um segredo diferente validou o token")
	}

	// Expirado → recusa.
	expired := o.mintAIToken(user, tenant, time.Now().Add(-time.Minute))
	if _, _, ok := o.parseAIToken(expired); ok {
		t.Error("aceitou token expirado")
	}
}
