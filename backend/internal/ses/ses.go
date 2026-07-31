// Package ses fala com a API do Amazon SES v2 por HTTP assinado (SigV4) — sem SDK (lei 2.5),
// no mesmo espírito do provider Stripe. Uso: registar o domínio de um tenant como identidade SES
// quando ele é provisionado, em modo WHITE-LABEL (BYODKIM): NÓS geramos a chave DKIM e o SES assina
// com ela, para que o DNS do cliente NUNCA revele o Amazon (docs/manager.md §4).
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
	selector  string // seletor DKIM neutro (marca), ex.: "beskos" — nunca revela o SES
	accountID string // conta AWS, só para montar ARNs (tenants); vazio desliga os tenants
	host      string
	http      *http.Client
	now       func() time.Time // injetável nos testes de assinatura
}

// NewFromEnv monta o cliente a partir de SES_REGION + AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY.
// SES_DKIM_SELECTOR (opcional) é o seletor da marca no DNS do cliente (default "mail").
// Sem as três primeiras, devolve nil (feature desligada — dev não fala com a AWS).
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
	// Sem SES_ACCOUNT_ID os tenants ficam desligados e o resto continua a funcionar: é uma melhoria
	// de isolamento, não um requisito para enviar correio.
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

// CreateIdentity regista o domínio como identidade SES em modo BYODKIM: gera uma chave RSA nossa,
// entrega a PRIVADA ao SES (que passa a assinar com ela, d=domínio-do-cliente) e devolve o SELETOR
// e a CHAVE PÚBLICA para o cliente publicar `<selector>._domainkey.<domínio>` IN TXT. Assim o DNS
// do cliente só mostra uma chave DKIM genérica — sem qualquer referência ao amazonses.
// Idempotente: identidade já existente → (re)define a assinatura BYODKIM com uma chave nova.
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
	// Já existe: (re)configura a assinatura BYODKIM com a chave nova (idempotente).
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

// MailFromSubdomain é o subdomínio de MAIL FROM (Return-Path) do white-label: o destinatário vê
// este domínio em "enviado por", nunca o amazonses.
func (c *Client) MailFromSubdomain(domain string) string { return "bounce." + domain }

// MailFromMX é o alvo do MX que o SES exige no subdomínio de MAIL FROM (recebe os bounces).
func (c *Client) MailFromMX() string { return "feedback-smtp." + c.region + ".amazonses.com" }

// SetMailFrom configura o custom MAIL FROM da identidade (Return-Path = bounce.<domínio>), para o
// "enviado por" do destinatário mostrar o domínio do cliente. BehaviorOnMxFailure=USE_DEFAULT_VALUE:
// enquanto o MX não propaga, o SES cai no default em vez de recusar o envio. Idempotente.
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

// IdentityVerified diz se a identidade já pode enviar (DKIM verificado do lado do SES).
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

// generateDKIMKeypair cria uma chave RSA 2048 e devolve (privada PKCS8 base64 p/ o SES,
// pública SubjectPublicKeyInfo base64 p/ o registo DKIM `p=`). Ambas sem armadura PEM.
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
