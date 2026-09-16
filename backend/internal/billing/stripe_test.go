package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func sign(payload, secret string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, payload)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func TestWebhookSignatureValid(t *testing.T) {
	payload, secret := `{"type":"checkout.session.completed"}`, "whsec_test"
	hdr := sign(payload, secret, time.Now().Unix())
	if err := verifyStripeSignature([]byte(payload), hdr, secret, 5*time.Minute); err != nil {
		t.Errorf("assinatura válida rejeitada: %v", err)
	}
}

func TestWebhookSignatureTampered(t *testing.T) {
	secret := "whsec_test"
	hdr := sign(`{"a":1}`, secret, time.Now().Unix())
	// Payload diferente do assinado → deve falhar.
	if err := verifyStripeSignature([]byte(`{"a":2}`), hdr, secret, 5*time.Minute); err == nil {
		t.Error("payload adulterado deveria falhar")
	}
	// Segredo errado → deve falhar.
	if err := verifyStripeSignature([]byte(`{"a":1}`), hdr, "whsec_outro", 5*time.Minute); err == nil {
		t.Error("segredo errado deveria falhar")
	}
}

func TestWebhookSignatureExpired(t *testing.T) {
	payload, secret := `{}`, "whsec_test"
	hdr := sign(payload, secret, time.Now().Add(-10*time.Minute).Unix())
	if err := verifyStripeSignature([]byte(payload), hdr, secret, 5*time.Minute); err == nil {
		t.Error("timestamp velho deveria falhar (replay)")
	}
}

func TestWebhookMalformedHeader(t *testing.T) {
	if err := verifyStripeSignature([]byte(`{}`), "garbage", "s", time.Minute); err == nil {
		t.Error("cabeçalho malformado deveria falhar")
	}
}
