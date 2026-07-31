package stalwart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Servidor novo sobe em bootstrap mode: qualquer x:* (exceto x:Bootstrap) retorna forbidden.
// É preciso completar o bootstrap UMA vez e reiniciar o processo (DIVERGENCES §D3).
// Modelamos como passo de setup idempotente do control plane (não confundir com provisionar tenant).

type BootstrapInput struct {
	ServerHostname string // ex.: mail.mailin.app
	DefaultDomain  string // ex.: mailin.app
}

// BootstrapResult traz o admin permanente gerado (secret mostrado só uma vez).
type BootstrapResult struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

// Bootstrap completa o setup inicial. Após chamar, é OBRIGATÓRIO reiniciar o processo Stalwart.
func (c *Client) Bootstrap(ctx context.Context, in BootstrapInput) (*BootstrapResult, error) {
	args := map[string]any{
		"update": map[string]any{
			"singleton": map[string]any{
				"serverHostname":        in.ServerHostname,
				"defaultDomain":         in.DefaultDomain,
				"requestTlsCertificate": false, // evita ACME no dev; produção liga isto
				"generateDkimKeys":      true,
			},
		},
	}
	raw, err := c.call(ctx, "x:Bootstrap/set", args)
	if err != nil {
		return nil, err
	}
	var r struct {
		Updated map[string]json.RawMessage `json:"updated"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode Bootstrap/set: %w", err)
	}
	upd, ok := r.Updated["singleton"]
	if !ok {
		return nil, errors.New("stalwart: bootstrap returned no updated singleton")
	}
	var res BootstrapResult
	if err := json.Unmarshal(upd, &res); err != nil {
		return nil, fmt.Errorf("decode bootstrap result: %w", err)
	}
	return &res, nil
}

// IsBootstrapped tenta uma leitura administrativa leve; se o servidor ainda está em bootstrap
// mode a chamada falha com forbidden. Usado pelo control plane para decidir se precisa bootstrapar.
func (c *Client) IsBootstrapped(ctx context.Context) (bool, error) {
	_, err := c.call(ctx, "x:Domain/query", map[string]any{})
	if err == nil {
		return true, nil
	}
	// Bootstrap mode responde forbidden — pode vir como erro de request (APIError) ou de método.
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Type != "" {
		return false, nil
	}
	var methErr *MethodError
	if errors.As(err, &methErr) && methErr.Type == "forbidden" {
		return false, nil
	}
	return false, err
}
