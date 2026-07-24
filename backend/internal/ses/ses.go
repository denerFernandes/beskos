// Package ses fala com a API do Amazon SES v2 por HTTP assinado (SigV4) — sem SDK (lei 2.5),
// no mesmo espírito do provider Stripe. Uso: registar automaticamente o domínio de um tenant
// como identidade SES (Easy DKIM) quando ele é provisionado (docs/manager.md §4).
package ses

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
	host      string
	http      *http.Client
	now       func() time.Time // injetável nos testes de assinatura
}

// NewFromEnv monta o cliente a partir de SES_REGION + AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY.
// Sem as três, devolve nil (feature desligada — dev não fala com a AWS).
func NewFromEnv() *Client {
	region := os.Getenv("SES_REGION")
	key := os.Getenv("AWS_ACCESS_KEY_ID")
	secret := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if region == "" || key == "" || secret == "" {
		return nil
	}
	return New(region, key, secret)
}

func New(region, accessKey, secretKey string) *Client {
	return &Client{
		region: region, accessKey: accessKey, secretKey: secretKey,
		host: "email." + region + ".amazonaws.com",
		http: &http.Client{Timeout: 20 * time.Second},
		now:  time.Now,
	}
}

// CreateIdentity regista o domínio como identidade SES (Easy DKIM) e devolve os 3 tokens DKIM
// (CNAMEs <token>._domainkey.<domínio> → <token>.dkim.amazonses.com). Idempotente: identidade
// já existente não é erro — busca os tokens.
func (c *Client) CreateIdentity(ctx context.Context, domain string) ([]string, error) {
	body, _ := json.Marshal(map[string]any{"EmailIdentity": domain})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/identities", body)
	if err != nil {
		return nil, err
	}
	if status == http.StatusOK || status == http.StatusCreated {
		var resp struct {
			DkimAttributes struct {
				Tokens []string `json:"Tokens"`
			} `json:"DkimAttributes"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, fmt.Errorf("ses create identity: parse: %w", err)
		}
		return resp.DkimAttributes.Tokens, nil
	}
	if status == http.StatusConflict || strings.Contains(string(out), "AlreadyExists") {
		tokens, _, err := c.GetIdentity(ctx, domain)
		return tokens, err
	}
	return nil, fmt.Errorf("ses create identity: HTTP %d: %s", status, out)
}

// GetIdentity devolve os tokens DKIM e se a identidade já está verificada para envio.
func (c *Client) GetIdentity(ctx context.Context, domain string) (tokens []string, verified bool, err error) {
	status, out, err := c.do(ctx, http.MethodGet, "/v2/email/identities/"+domain, nil)
	if err != nil {
		return nil, false, err
	}
	if status != http.StatusOK {
		return nil, false, fmt.Errorf("ses get identity: HTTP %d: %s", status, out)
	}
	var resp struct {
		VerifiedForSendingStatus bool `json:"VerifiedForSendingStatus"`
		DkimAttributes           struct {
			Status string   `json:"Status"` // PENDING | SUCCESS | FAILED | ...
			Tokens []string `json:"Tokens"`
		} `json:"DkimAttributes"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, false, fmt.Errorf("ses get identity: parse: %w", err)
	}
	ok := resp.VerifiedForSendingStatus || resp.DkimAttributes.Status == "SUCCESS"
	return resp.DkimAttributes.Tokens, ok, nil
}

// SetupMailFrom configura o custom MAIL FROM da identidade (tem de ser SUBDOMÍNIO dela — regra
// do SES) e devolve o alvo do MX que o cliente publica (feedback-smtp da região). Com falha de MX
// o SES cai no envelope default (USE_DEFAULT_VALUE) em vez de recusar o envio (fail-safe).
func (c *Client) SetupMailFrom(ctx context.Context, identity, mailFromDomain string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"MailFromDomain":      mailFromDomain,
		"BehaviorOnMxFailure": "USE_DEFAULT_VALUE",
	})
	status, out, err := c.do(ctx, http.MethodPut, "/v2/email/identities/"+identity+"/mail-from", body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", fmt.Errorf("ses mail-from: HTTP %d: %s", status, out)
	}
	return "feedback-smtp." + c.region + ".amazonses.com", nil
}

// IdentityVerified diz se a identidade já pode enviar (satisfaz provisioning.EmailRelay).
func (c *Client) IdentityVerified(ctx context.Context, domain string) (bool, error) {
	_, ok, err := c.GetIdentity(ctx, domain)
	return ok, err
}

// DeleteIdentity remove a identidade (quando um domínio é apagado). 404 não é erro (idempotente).
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

// do assina (SigV4) e executa uma chamada à API SESv2.
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

// sign aplica AWS Signature V4 (serviço "ses") via o assinador genérico abaixo.
func (c *Client) sign(req *http.Request, body []byte) {
	SignRequest(req, body, c.region, "ses", c.accessKey, c.secretKey, c.now())
}

// SignRequest aplica AWS Signature V4 a um pedido para QUALQUER serviço AWS (ses, sns, …) —
// implementação direta do processo canónico. Usado também pelo provider de SMS (internal/sms).
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
