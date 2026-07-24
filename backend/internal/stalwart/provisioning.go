package stalwart

import (
	"context"
	"encoding/json"
	"fmt"
)

// Operações de provisionamento mapeadas 1:1 do brief §6/§7 para os métodos JMAP reais
// da v0.16.4. Ver deploy/DIVERGENCES.md §D4 para os payloads testados.

// CreateDomain cria um domínio (equivalente ao antigo type:domain). DKIM é auto-gerado
// (dkimManagement=Automatic por default). Retorna o stalwart_id a guardar em domains.stalwart_id.
func (c *Client) CreateDomain(ctx context.Context, name, description string) (string, error) {
	args := map[string]any{
		"create": map[string]any{
			"d1": map[string]any{
				"name":        name,
				"isEnabled":   true,
				"description": description, // guardamos "tenant_id=<n>" aqui, como no brief
			},
		},
	}
	raw, err := c.call(ctx, "x:Domain/set", args)
	if err != nil {
		return "", err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode Domain/set: %w", err)
	}
	return firstCreatedID(&r)
}

// AccountInput são os campos mínimos para criar uma mailbox individual.
type AccountInput struct {
	Login       string // vira o local-part; NÃO existe campo "email" na 0.16 (endereço = Login@domínio)
	DomainID    string // stalwart_id do domínio (ou "#d1" para ref na mesma request)
	DisplayName string
	Password    string // passa por checador de força (zxcvbn); senhas fracas são rejeitadas
	QuotaBytes  int64
}

// CreateAccount cria uma mailbox individual (type User). Retorna stalwart_account_id.
func (c *Client) CreateAccount(ctx context.Context, in AccountInput) (string, error) {
	acct := map[string]any{
		"@type":       "User",
		"name":        in.Login,
		"domainId":    in.DomainID,
		"description": in.DisplayName,
		// credentials/aliases/quotas são MAPAS ("0","1",...), não arrays (DIVERGENCES §D4).
		"credentials": map[string]any{
			"0": map[string]any{"@type": "Password", "secret": in.Password},
		},
	}
	if in.QuotaBytes > 0 {
		acct["quotas"] = map[string]any{"maxDiskQuota": in.QuotaBytes}
	}
	args := map[string]any{"create": map[string]any{"u1": acct}}
	raw, err := c.call(ctx, "x:Account/set", args)
	if err != nil {
		return "", err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode Account/set: %w", err)
	}
	return firstCreatedID(&r)
}

// SetAccountPassword troca a senha de uma mailbox (x:Account/set update credentials/0/secret).
// Ver DIVERGENCES §D4. A senha passa pelo checador de força do Stalwart (zxcvbn).
func (c *Client) SetAccountPassword(ctx context.Context, accountID, newPassword string) error {
	args := map[string]any{
		"update": map[string]any{
			accountID: map[string]any{"credentials/0/secret": newPassword},
		},
	}
	raw, err := c.call(ctx, "x:Account/set", args)
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode Account/set: %w", err)
	}
	if len(r.NotUpdated) > 0 {
		b, _ := json.Marshal(r.NotUpdated)
		return fmt.Errorf("stalwart notUpdated: %s", string(b))
	}
	return nil
}

// DeleteDomain / DeleteAccount — destroy. Atenção à integridade referencial:
// destruir domínio com contas/DKIM ligados falha com objectIsLinked (DIVERGENCES §D4).
func (c *Client) DeleteAccount(ctx context.Context, id string) error {
	_, err := c.call(ctx, "x:Account/set", map[string]any{"destroy": []string{id}})
	return err
}

func (c *Client) DeleteDomain(ctx context.Context, id string) error {
	_, err := c.call(ctx, "x:Domain/set", map[string]any{"destroy": []string{id}})
	return err
}

// GetDomainDNSZone devolve o trecho de zona BIND pronto (dnsZoneFile) com MX/SPF/DKIM/DMARC/MTA-STS/…
// É a fonte para a tela de DNS do estado dns_pending (§7). Parseamos isto, não montamos à mão.
func (c *Client) GetDomainDNSZone(ctx context.Context, domainID string) (string, error) {
	args := map[string]any{"ids": []string{domainID}, "properties": []string{"dnsZoneFile"}}
	raw, err := c.call(ctx, "x:Domain/get", args)
	if err != nil {
		return "", err
	}
	var g struct {
		List []struct {
			DNSZoneFile string `json:"dnsZoneFile"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return "", fmt.Errorf("decode Domain/get: %w", err)
	}
	if len(g.List) == 0 {
		return "", fmt.Errorf("stalwart: domínio %s não encontrado", domainID)
	}
	return g.List[0].DNSZoneFile, nil
}
