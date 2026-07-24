package totp

import (
	"testing"
	"time"
)

func TestGenerateAndValidate(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	c, err := code(secret, uint64(now.Unix())/period)
	if err != nil {
		t.Fatal(err)
	}
	if !Validate(secret, c, now) {
		t.Errorf("código válido rejeitado")
	}
	if Validate(secret, "000000", now) && c != "000000" {
		t.Errorf("código errado aceito")
	}
}

func TestWindowTolerance(t *testing.T) {
	secret, _ := GenerateSecret()
	now := time.Unix(1_700_000_000, 0)
	prev, _ := code(secret, uint64(now.Unix())/period-1) // passo anterior
	if !Validate(secret, prev, now) {
		t.Errorf("passo anterior (±1) deveria ser aceito")
	}
	old, _ := code(secret, uint64(now.Unix())/period-5) // fora da janela
	if Validate(secret, old, now) {
		t.Errorf("passo distante NÃO deveria ser aceito")
	}
}

func TestRFC6238Vector(t *testing.T) {
	// Vetor de teste do RFC 6238 (SHA1): secret ASCII "12345678901234567890".
	// Base32 do ASCII acima:
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	// Em T=59s (contador 1) o código de 8 dígitos é 94287082; 6 dígitos = 287082.
	got, err := code(secret, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "287082" {
		t.Errorf("vetor RFC 6238: got %s, quer 287082", got)
	}
}

func TestBadInputs(t *testing.T) {
	secret, _ := GenerateSecret()
	now := time.Now()
	for _, in := range []string{"", "12345", "1234567", "abcdef"} {
		if Validate(secret, in, now) {
			t.Errorf("entrada inválida %q aceita", in)
		}
	}
}

func TestProvisioningURI(t *testing.T) {
	uri := ProvisioningURI("ABC", "user@x.com")
	if !contains(uri, "otpauth://totp/") || !contains(uri, "secret=ABC") || !contains(uri, "issuer=Beskos") {
		t.Errorf("URI inesperada: %s", uri)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
