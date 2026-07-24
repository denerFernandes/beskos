package ses

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestSignGolden congela a assinatura SigV4 num caso fixo: qualquer mudança acidental no processo
// canónico (ordem de headers, scope, hashing) muda a assinatura e faz o teste falhar.
func TestSignGolden(t *testing.T) {
	c := New("eu-west-1", "AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY")
	c.now = func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) }

	body := []byte(`{"EmailIdentity":"acme.com"}`)
	req, _ := http.NewRequest(http.MethodPost, "https://email.eu-west-1.amazonaws.com/v2/email/identities", strings.NewReader(string(body)))
	c.sign(req, body)

	auth := req.Header.Get("Authorization")
	for _, want := range []string{
		"AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20260722/eu-west-1/ses/aws4_request",
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date",
	} {
		if !strings.Contains(auth, want) {
			t.Errorf("Authorization missing %q: %s", want, auth)
		}
	}
	if req.Header.Get("X-Amz-Date") != "20260722T120000Z" {
		t.Errorf("X-Amz-Date = %s", req.Header.Get("X-Amz-Date"))
	}
	// Golden: assinatura estável para estas entradas exatas.
	const goldenSig = "Signature="
	i := strings.Index(auth, goldenSig)
	if i < 0 || len(auth[i+len(goldenSig):]) != 64 {
		t.Fatalf("signature malformed: %s", auth)
	}
	sig := auth[i+len(goldenSig):]
	if prev := "0"; sig == strings.Repeat(prev, 64) {
		t.Fatalf("degenerate signature")
	}
	// Regrava aqui se o processo canónico mudar DE PROPÓSITO.
	const expected = "bb65686bf9e2a8f321f1dacce892c5e14a0ce99dd37187adee7cd49dce4b6e4b"
	if sig != expected {
		t.Errorf("signature drifted: got %s, want %s", sig, expected)
	}
}
