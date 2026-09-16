package ses

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Verification of Amazon SNS messages (the SES bounces/complaints webhook). Follows the official
// process: canonical string per message type, RSA signature (SHA1 on SignatureVersion 1, SHA256
// on 2) with the SigningCertURL certificate — which MUST come from a *.amazonaws.com host over
// HTTPS (otherwise anyone could sign with their own cert and forge bounces).

// SNSMessage is the SNS envelope (fields used in verification + processing).
type SNSMessage struct {
	Type             string `json:"Type"`
	MessageID        string `json:"MessageId"`
	TopicArn         string `json:"TopicArn"`
	Subject          string `json:"Subject"`
	Message          string `json:"Message"`
	Timestamp        string `json:"Timestamp"`
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`
	SubscribeURL     string `json:"SubscribeURL"`
	Token            string `json:"Token"`
}

// SNSVerifier validates SNS signatures. fetch is injectable in tests (fixture cert).
type SNSVerifier struct {
	fetch func(url string) ([]byte, error)
	mu    sync.Mutex
	cache map[string]*rsa.PublicKey
}

func NewSNSVerifier() *SNSVerifier {
	return &SNSVerifier{fetch: fetchHTTPS, cache: map[string]*rsa.PublicKey{}}
}

// Verify decodes and validates the message; error = reject the webhook.
func (v *SNSVerifier) Verify(body []byte) (*SNSMessage, error) {
	var m SNSMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("sns: parse: %w", err)
	}
	if err := validCertURL(m.SigningCertURL); err != nil {
		return nil, err
	}
	key, err := v.publicKey(m.SigningCertURL)
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return nil, fmt.Errorf("sns: signature decode: %w", err)
	}
	canonical := canonicalString(&m)
	switch m.SignatureVersion {
	case "2":
		sum := sha256.Sum256([]byte(canonical))
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
			return nil, fmt.Errorf("sns: signature invalid: %w", err)
		}
	default: // "1"
		sum := sha1.Sum([]byte(canonical))
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA1, sum[:], sig); err != nil {
			return nil, fmt.Errorf("sns: signature invalid: %w", err)
		}
	}
	return &m, nil
}

// canonicalString builds the signed string (ordered key\nvalue pairs, per message type).
func canonicalString(m *SNSMessage) string {
	var pairs [][2]string
	if m.Type == "Notification" {
		pairs = [][2]string{{"Message", m.Message}, {"MessageId", m.MessageID}}
		if m.Subject != "" {
			pairs = append(pairs, [2]string{"Subject", m.Subject})
		}
		pairs = append(pairs, [2]string{"Timestamp", m.Timestamp}, [2]string{"TopicArn", m.TopicArn}, [2]string{"Type", m.Type})
	} else { // SubscriptionConfirmation / UnsubscribeConfirmation
		pairs = [][2]string{
			{"Message", m.Message}, {"MessageId", m.MessageID}, {"SubscribeURL", m.SubscribeURL},
			{"Timestamp", m.Timestamp}, {"Token", m.Token}, {"TopicArn", m.TopicArn}, {"Type", m.Type},
		}
	}
	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(p[0] + "\n" + p[1] + "\n")
	}
	return b.String()
}

// validCertURL requires HTTPS + an AWS host (sns.<region>.amazonaws.com).
func validCertURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("sns: cert URL must be https: %q", raw)
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".amazonaws.com") {
		return fmt.Errorf("sns: cert URL host not allowed: %q", host)
	}
	return nil
}

func (v *SNSVerifier) publicKey(certURL string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	if k, ok := v.cache[certURL]; ok {
		v.mu.Unlock()
		return k, nil
	}
	v.mu.Unlock()
	raw, err := v.fetch(certURL)
	if err != nil {
		return nil, fmt.Errorf("sns: fetch cert: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("sns: cert not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("sns: parse cert: %w", err)
	}
	key, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("sns: cert key is not RSA")
	}
	v.mu.Lock()
	v.cache[certURL] = key
	v.mu.Unlock()
	return key, nil
}

func fetchHTTPS(url string) ([]byte, error) {
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// ConfirmSubscription visits the SubscribeURL (same host allowlist) to activate the subscription.
func ConfirmSubscription(subscribeURL string) error {
	if err := validCertURL(subscribeURL); err != nil {
		return err
	}
	_, err := fetchHTTPS(subscribeURL)
	return err
}

// SESEvent is the inner Message of an SES Notification (fields used).
type SESEvent struct {
	NotificationType string `json:"notificationType"`
	EventType        string `json:"eventType"` // the "event publishing" format uses eventType
	Mail             struct {
		Source      string   `json:"source"`
		Destination []string `json:"destination"`
	} `json:"mail"`
	Bounce struct {
		BounceType        string `json:"bounceType"` // Permanent | Transient
		BouncedRecipients []struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"bouncedRecipients"`
	} `json:"bounce"`
	Complaint struct {
		ComplaintFeedbackType string `json:"complaintFeedbackType"` // abuse | fraud | ...
		ComplainedRecipients  []struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"complainedRecipients"`
	} `json:"complaint"`
}

// Kind normalizes the event type ("" = ignore).
func (e *SESEvent) Kind() string {
	t := e.NotificationType
	if t == "" {
		t = e.EventType
	}
	switch strings.ToLower(t) {
	case "bounce":
		return "bounce"
	case "complaint":
		return "complaint"
	case "delivery":
		return "delivery"
	}
	return ""
}
