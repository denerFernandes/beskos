package ses

import (
	"fmt"
	"strings"
	"testing"
)

// FuzzSNSVerify exercises the whole untrusted-body parsing path for the SNS webhook: JSON
// decoding, the cert-URL host allowlist (validCertURL), the canonical-string builder
// (canonicalString), and the signature's base64 decoding. The HTTP fetch is stubbed out so the
// fuzzer never touches the network — it only has to find a panic, not a valid signature (that is
// already covered by the golden test in ses_test.go).
func FuzzSNSVerify(f *testing.F) {
	seeds := []string{
		``,
		`{}`,
		`not json`,
		`{"Type":"Notification","Message":"m","MessageId":"1","TopicArn":"t","Timestamp":"2024-01-01T00:00:00Z","SignatureVersion":"1","Signature":"AA==","SigningCertURL":"https://sns.us-east-1.amazonaws.com/cert.pem"}`,
		`{"Type":"Notification","Message":"m","MessageId":"1","Subject":"s","TopicArn":"t","Timestamp":"2024-01-01T00:00:00Z","SignatureVersion":"2","Signature":"AA==","SigningCertURL":"https://sns.us-east-1.amazonaws.com/cert.pem"}`,
		`{"Type":"SubscriptionConfirmation","Token":"tok","SubscribeURL":"https://sns.us-east-1.amazonaws.com/?Action=ConfirmSubscription","SigningCertURL":"https://sns.us-east-1.amazonaws.com/cert.pem","Signature":"AA=="}`,
		`{"SigningCertURL":"http://evil.example.com/cert.pem"}`,
		`{"SigningCertURL":"https://notamazonaws.com/cert.pem"}`,
		`{"SigningCertURL":"https://sns.us-east-1.amazonaws.com.evil.com/cert.pem"}`,
		`{"SigningCertURL":"https://sns.us-east-1.amazonaws.com/../../../etc/passwd"}`,
		`{"SigningCertURL":""}`,
		`{"SigningCertURL":"ftp://sns.us-east-1.amazonaws.com/cert.pem"}`,
		`{"SigningCertURL":"https://SNS.US-EAST-1.AMAZONAWS.COM/cert.pem","Signature":"not-base64!!"}`,
		`{"Type":"Notification","Signature":"====","SigningCertURL":"https://sns.us-east-1.amazonaws.com/cert.pem"}`,
		strings.Repeat(`{"a":"`+strings.Repeat("x", 1000)+`"}`, 50),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	v := NewSNSVerifier()
	v.fetch = func(string) ([]byte, error) { return nil, fmt.Errorf("fuzz: network disabled") }

	f.Fuzz(func(t *testing.T, body string) {
		// Must never panic on adversarial input, and a message this fabricated (no real fetched
		// cert, arbitrary signature) must never be reported as valid.
		_, err := v.Verify([]byte(body))
		if err == nil {
			t.Fatalf("Verify unexpectedly accepted forged input: %q", body)
		}
	})
}

// FuzzValidCertURL targets the cert/subscribe-URL host allowlist directly: it must reject
// anything that is not HTTPS on a *.amazonaws.com host, and never panic regardless of how the URL
// is malformed.
func FuzzValidCertURL(f *testing.F) {
	seeds := []string{
		"",
		"https://sns.us-east-1.amazonaws.com/cert.pem",
		"http://sns.us-east-1.amazonaws.com/cert.pem",
		"https://evil.com/cert.pem",
		"https://sns.us-east-1.amazonaws.com.evil.com/cert.pem",
		"https://amazonaws.com/cert.pem",
		"HTTPS://SNS.US-EAST-1.AMAZONAWS.COM/cert.pem",
		"https://user:pass@sns.us-east-1.amazonaws.com/cert.pem",
		"https://[::1]/cert.pem",
		"https://sns.us-east-1.amazonaws.com:99999/cert.pem",
		"not a url at all %%%",
		"https://",
		"://missing-scheme",
		strings.Repeat("a", 5000) + ".amazonaws.com",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		_ = validCertURL(raw)
	})
}
