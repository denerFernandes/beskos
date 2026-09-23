package ses

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// Signs an SNS message with a test cert and verifies the full path (injected fetch).
func TestSNSVerify(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "sns.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	m := SNSMessage{
		Type: "Notification", MessageID: "mid-1", TopicArn: "arn:aws:sns:eu-west-1:1:ses-events",
		Message: `{"notificationType":"Bounce"}`, Timestamp: "2026-07-22T12:00:00.000Z",
		SignatureVersion: "1",
		SigningCertURL:   "https://sns.eu-west-1.amazonaws.com/cert.pem",
	}
	sum := sha1.Sum([]byte(canonicalString(&m)))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA1, sum[:])
	m.Signature = base64.StdEncoding.EncodeToString(sig)

	v := &SNSVerifier{fetch: func(string) ([]byte, error) { return certPEM, nil },
		cache: map[string]*rsa.PublicKey{}}
	body, _ := json.Marshal(m)
	out, err := v.Verify(body)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if out.Type != "Notification" {
		t.Fatalf("type = %s", out.Type)
	}

	// Tampered signature → rejected.
	m.Message = `{"notificationType":"Complaint"}`
	body, _ = json.Marshal(m)
	if _, err := v.Verify(body); err == nil {
		t.Fatal("tampered message should fail")
	}

	// Cert outside AWS → rejected without even fetching it.
	m.SigningCertURL = "https://evil.example.com/cert.pem"
	body, _ = json.Marshal(m)
	if _, err := v.Verify(body); err == nil {
		t.Fatal("non-AWS cert URL should fail")
	}
}

func TestSESEventKind(t *testing.T) {
	for raw, want := range map[string]string{
		`{"notificationType":"Bounce"}`:    "bounce",
		`{"notificationType":"Complaint"}`: "complaint",
		`{"eventType":"Delivery"}`:         "delivery",
		`{"eventType":"Open"}`:             "",
	} {
		var e SESEvent
		_ = json.Unmarshal([]byte(raw), &e)
		if got := e.Kind(); got != want {
			t.Errorf("Kind(%s) = %q, want %q", raw, got, want)
		}
	}
}
