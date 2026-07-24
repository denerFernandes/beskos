// Package stalwart é o nosso client tipado para o Stalwart v0.16.4.
//
// A REST Management API foi REMOVIDA na 0.16 — não há OpenAPI para gerar código.
// Toda administração é via JMAP, capability "urn:stalwart:jmap", métodos no namespace "x:".
// Ver deploy/DIVERGENCES.md para o mapeamento completo brief §6 → realidade.
//
// Restrição de licença (brief §2): só falamos com o Stalwart pela rede. Nada aqui
// linka, embute ou modifica o binário do Stalwart.
package stalwart

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Capabilities usadas em toda request administrativa.
var adminUsing = []string{"urn:ietf:params:jmap:core", "urn:stalwart:jmap"}

// Client fala JMAP com um Stalwart. A credencial é o admin GLOBAL de recuperação
// (STALWART_RECOVERY_ADMIN). Só o backend Go a carrega — nunca o frontend (brief §2).
type Client struct {
	baseURL  string // ex.: http://stalwart:8080
	adminID  string // basic-auth user, ex.: "admin"
	adminPwd string
	http     *http.Client
}

func New(baseURL, adminUser, adminPwd string) *Client {
	return &Client{
		baseURL:  baseURL,
		adminID:  adminUser,
		adminPwd: adminPwd,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// BaseURL devolve a URL base do Stalwart (ex.: http://stalwart:8080). Usada pelo proxy JMAP do
// control plane (o app nativo não tem reverse proxy same-origin; ver jmap_proxy.go).
func (c *Client) BaseURL() string { return c.baseURL }

// --- Envelope JMAP -----------------------------------------------------------

type methodCall [3]any // [ "x:Object/fn", args, callId ]

type jmapRequest struct {
	Using       []string     `json:"using"`
	MethodCalls []methodCall `json:"methodCalls"`
}

type jmapResponse struct {
	MethodResponses []json.RawMessage `json:"methodResponses"`
	SessionState    string            `json:"sessionState"`
}

// APIError é um erro JMAP de nível de request (ex.: notRequest, forbidden em bootstrap).
type APIError struct {
	Type   string `json:"type"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("stalwart jmap error: status=%d type=%s detail=%s", e.Status, e.Type, e.Detail)
}

// call executa uma única chamada de método e devolve o payload cru da resposta desse método.
// Idempotência (brief §10): o caller usa creationIds/updates determinísticos; retry é seguro
// para operações set desde que a checagem de existência preceda o create no control plane.
func (c *Client) call(ctx context.Context, method string, args any) (json.RawMessage, error) {
	body, err := json.Marshal(jmapRequest{
		Using:       adminUsing,
		MethodCalls: []methodCall{{method, args, "c0"}},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jmap/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.adminID, c.adminPwd)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stalwart request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Erros de nível de request vêm como objeto de erro JMAP (status != 200).
	if resp.StatusCode != http.StatusOK {
		var apiErr APIError
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Type != "" {
			return nil, &apiErr
		}
		return nil, fmt.Errorf("stalwart http %d: %s", resp.StatusCode, string(raw))
	}

	var jr jmapResponse
	if err := json.Unmarshal(raw, &jr); err != nil {
		return nil, fmt.Errorf("decode jmap response: %w (body=%s)", err, string(raw))
	}
	if len(jr.MethodResponses) == 0 {
		return nil, fmt.Errorf("stalwart: resposta sem methodResponses")
	}

	// methodResponses[0] = [ method, payload, callId ]. Extraímos o payload (índice 1).
	var triple []json.RawMessage
	if err := json.Unmarshal(jr.MethodResponses[0], &triple); err != nil || len(triple) < 2 {
		return nil, fmt.Errorf("stalwart: methodResponse malformado: %s", string(jr.MethodResponses[0]))
	}
	// Detecta erro de nível de método (ex.: {"type":"...","description":"..."} sob "error").
	var maybeName string
	_ = json.Unmarshal(triple[0], &maybeName)
	if maybeName == "error" {
		me := &MethodError{Raw: string(triple[1])}
		_ = json.Unmarshal(triple[1], me)
		return nil, me
	}
	return triple[1], nil
}

// MethodError é um erro JMAP de nível de método (ex.: forbidden em bootstrap mode, invalidPatch).
type MethodError struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Raw         string `json:"-"`
}

func (e *MethodError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("stalwart method error: %s (%s)", e.Type, e.Description)
	}
	return "stalwart method error: " + e.Raw
}

// setResult é a resposta padrão de um x:Object/set.
type setResult struct {
	Created map[string]struct {
		ID string `json:"id"`
	} `json:"created"`
	Updated    map[string]json.RawMessage `json:"updated"`
	Destroyed  []string                   `json:"destroyed"`
	NotCreated map[string]json.RawMessage `json:"notCreated"`
	NotUpdated map[string]json.RawMessage `json:"notUpdated"`
}

func firstCreatedID(r *setResult) (string, error) {
	for _, v := range r.Created {
		return v.ID, nil
	}
	if len(r.NotCreated) > 0 {
		b, _ := json.Marshal(r.NotCreated)
		return "", fmt.Errorf("stalwart notCreated: %s", string(b))
	}
	return "", fmt.Errorf("stalwart: nenhum objeto criado na resposta")
}
