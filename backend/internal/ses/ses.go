// Package ses talks to the Amazon SES v2 API over signed HTTP (SigV4) — no SDK, in the same
// spirit as the Stripe provider. Used to register a tenant's domain as an SES identity when it is
// provisioned, in WHITE-LABEL (BYODKIM) mode: WE generate the DKIM key and SES signs with it, so
// the client's DNS NEVER reveals Amazon.
package ses

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Client struct {
	region    string
	accessKey string
	secretKey string
	selector  string // neutral (brand) DKIM selector, e.g. "beskos" — never reveals SES
	accountID string // AWS account, only to build ARNs (tenants); empty disables tenants
	host      string
	http      *http.Client
	now       func() time.Time // injectable in signature tests
}

// NewFromEnv builds the client from SES_REGION + AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY.
// SES_DKIM_SELECTOR (optional) is the brand's selector in the customer's DNS (default "mail").
// Without the first three, returns nil (feature disabled — dev doesn't talk to AWS).
func NewFromEnv() *Client {
	region := os.Getenv("SES_REGION")
	key := os.Getenv("AWS_ACCESS_KEY_ID")
	secret := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if region == "" || key == "" || secret == "" {
		return nil
	}
	selector := os.Getenv("SES_DKIM_SELECTOR")
	if selector == "" {
		selector = "mail"
	}
	c := New(region, key, secret)
	c.selector = selector
	// Without SES_ACCOUNT_ID, tenants stay disabled and the rest keeps working: it is an isolation
	// improvement, not a requirement for sending mail.
	c.accountID = os.Getenv("SES_ACCOUNT_ID")
	return c
}

func New(region, accessKey, secretKey string) *Client {
	return &Client{
		region: region, accessKey: accessKey, secretKey: secretKey,
		selector: "mail",
		host:     "email." + region + ".amazonaws.com",
		http:     &http.Client{Timeout: 20 * time.Second},
		now:      time.Now,
	}
}

// CreateIdentity registers the domain as an SES identity in BYODKIM mode: generates our own RSA
// key, hands the PRIVATE one to SES (which then signs with it, d=customer-domain) and returns the
// SELECTOR and the PUBLIC KEY for the customer to publish `<selector>._domainkey.<domain>` IN TXT.
// This way the customer's DNS only shows a generic DKIM key — with no reference to amazonses.
// Idempotent: identity already exists -> (re)sets the BYODKIM signature with a new key.
func (c *Client) CreateIdentity(ctx context.Context, domain string) (selector, publicKeyB64 string, err error) {
	privB64, pubB64, err := generateDKIMKeypair()
	if err != nil {
		return "", "", err
	}
	body, _ := json.Marshal(map[string]any{
		"EmailIdentity": domain,
		"DkimSigningAttributes": map[string]any{
			"DomainSigningSelector":   c.selector,
			"DomainSigningPrivateKey": privB64,
		},
	})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/identities", body)
	if err != nil {
		return "", "", err
	}
	if status == http.StatusOK || status == http.StatusCreated {
		return c.selector, pubB64, nil
	}
	// Already exists: (re)configures the BYODKIM signature with the new key (idempotent).
	if status == http.StatusConflict || strings.Contains(string(out), "AlreadyExists") {
		putBody, _ := json.Marshal(map[string]any{
			"SigningAttributesOrigin": "EXTERNAL",
			"SigningAttributes": map[string]any{
				"DomainSigningSelector":   c.selector,
				"DomainSigningPrivateKey": privB64,
			},
		})
		st, o, e := c.do(ctx, http.MethodPut, "/v2/email/identities/"+domain+"/dkim/signing", putBody)
		if e != nil {
			return "", "", e
		}
		if st != http.StatusOK {
			return "", "", fmt.Errorf("ses set byodkim: HTTP %d: %s", st, o)
		}
		return c.selector, pubB64, nil
	}
	return "", "", fmt.Errorf("ses create identity: HTTP %d: %s", status, out)
}

// MailFromSubdomain is the white-label's MAIL FROM (Return-Path) subdomain: the recipient sees
// this domain in "sent by", never amazonses.
func (c *Client) MailFromSubdomain(domain string) string { return "bounce." + domain }

// MailFromMX is the MX target that SES requires on the MAIL FROM subdomain (receives the bounces).
func (c *Client) MailFromMX() string { return "feedback-smtp." + c.region + ".amazonses.com" }

// SetMailFrom configures the identity's custom MAIL FROM (Return-Path = bounce.<domain>), so the
// recipient's "sent by" shows the customer's domain. BehaviorOnMxFailure=USE_DEFAULT_VALUE: while
// the MX hasn't propagated, SES falls back to the default instead of refusing to send. Idempotent.
func (c *Client) SetMailFrom(ctx context.Context, domain string) error {
	body, _ := json.Marshal(map[string]any{
		"MailFromDomain":      c.MailFromSubdomain(domain),
		"BehaviorOnMxFailure": "USE_DEFAULT_VALUE",
	})
	status, out, err := c.do(ctx, http.MethodPut, "/v2/email/identities/"+domain+"/mail-from", body)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("ses set mail-from: HTTP %d: %s", status, out)
	}
	return nil
}

// IdentityVerified reports whether the identity can already send (DKIM verified on the SES side).
func (c *Client) IdentityVerified(ctx context.Context, domain string) (bool, error) {
	status, out, err := c.do(ctx, http.MethodGet, "/v2/email/identities/"+domain, nil)
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("ses get identity: HTTP %d: %s", status, out)
	}
	var resp struct {
		VerifiedForSendingStatus bool `json:"VerifiedForSendingStatus"`
		DkimAttributes           struct {
			Status string `json:"Status"` // PENDING | SUCCESS | FAILED
		} `json:"DkimAttributes"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return false, fmt.Errorf("ses get identity: parse: %w", err)
	}
	return resp.VerifiedForSendingStatus || resp.DkimAttributes.Status == "SUCCESS", nil
}

// DeleteIdentity removes the identity (when a domain is deleted). 404 is not an error (idempotent).
func (c *Client) DeleteIdentity(ctx context.Context, domain string) error {
	status, out, err := c.do(ctx, http.MethodDelete, "/v2/email/identities/"+domain, nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("ses delete identity: HTTP %d: %s", status, out)
}

// generateDKIMKeypair creates a 2048 RSA key and returns (private PKCS8 base64 for SES,
// public SubjectPublicKeyInfo base64 for the DKIM `p=` record). Neither has PEM armor.
func generateDKIMKeypair() (privB64, pubB64 string, err error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate DKIM key: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return "", "", err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(privDER), base64.StdEncoding.EncodeToString(pubDER), nil
}

// do signs (SigV4) and executes a call to the SESv2 API.
func (c *Client) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, "https://"+c.host+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	c.sign(req, body)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("ses %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, out, nil
}

// sign applies AWS Signature V4 (service "ses") via the generic signer below.
func (c *Client) sign(req *http.Request, body []byte) {
	SignRequest(req, body, c.region, "ses", c.accessKey, c.secretKey, c.now())
}

// SignRequest applies AWS Signature V4 to a request for ANY AWS service (ses, sns, ...) —
// a direct implementation of the canonical process. Also used by the SMS provider (internal/sms).
func SignRequest(req *http.Request, body []byte, region, service, accessKey, secretKey string, now time.Time) {
	t := now.UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")
	payloadHash := sha256Hex(body)
	host := req.URL.Host

	req.Header.Set("Host", host)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonicalHeaders := "host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method, req.URL.EscapedPath(), req.URL.RawQuery,
		canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")

	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, scope, sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, scope, signedHeaders, signature))
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
