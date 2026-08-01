package stalwart

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Operações de provisionamento mapeadas 1:1 do brief §6/§7 para os métodos JMAP reais
// da v0.16.4. Ver deploy/DIVERGENCES.md §D4 para os payloads testados.

// CreateDomain cria um domínio (equivalente ao antigo type:domain). Com autoDKIM=true o Stalwart
// gere e assina o DKIM (dkimManagement=Automatic). Com autoDKIM=false (modo relay/white-label) o
// DKIM fica Manual — o Stalwart NÃO assina, deixando o relay (SES BYODKIM) ser o único assinante,
// evitando o cabeçalho DKIM-Signature duplicado. Retorna o stalwart_id.
func (c *Client) CreateDomain(ctx context.Context, name, description string, autoDKIM bool) (string, error) {
	create := map[string]any{
		"name":        name,
		"isEnabled":   true,
		"description": description, // guardamos "tenant_id=<n>" aqui, como no brief
	}
	if !autoDKIM {
		create["dkimManagement"] = map[string]any{"@type": "Manual"}
	}
	args := map[string]any{
		"create": map[string]any{"d1": create},
	}
	raw, err := c.call(ctx, "x:Domain/set", args)
	if err != nil {
		return "", err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode Domain/set: %w", err)
	}
	id, err := firstCreatedID(&r)
	if err == nil {
		return id, nil
	}
	// Idempotência: se o domínio já existe no Stalwart (primaryKeyViolation) — servidor recriado,
	// domínio default do bootstrap, ou re-tentativa de provisionamento — adota o existente em vez
	// de falhar em loop. Assim o fluxo genérico (cliente configura o mesmo domínio) é robusto.
	if isPrimaryKeyViolation(r.NotCreated) {
		existing, lookupErr := c.domainIDByName(ctx, name)
		if lookupErr != nil {
			return "", fmt.Errorf("adopt existing domain %q: %w", name, lookupErr)
		}
		return existing, nil
	}
	return "", err
}

// isPrimaryKeyViolation reporta se algum notCreated foi por o objeto já existir (chave duplicada).
func isPrimaryKeyViolation(nc map[string]json.RawMessage) bool {
	for _, v := range nc {
		var e struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(v, &e) == nil && e.Type == "primaryKeyViolation" {
			return true
		}
	}
	return false
}

// domainIDByName procura o stalwart_id de um domínio pelo nome (x:Domain/get devolve todos quando
// não se passam ids). Usado na adoção idempotente de um domínio já existente.
func (c *Client) domainIDByName(ctx context.Context, name string) (string, error) {
	raw, err := c.call(ctx, "x:Domain/get", map[string]any{"properties": []string{"name"}})
	if err != nil {
		return "", err
	}
	var g struct {
		List []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return "", fmt.Errorf("decode Domain/get: %w", err)
	}
	for _, d := range g.List {
		if strings.EqualFold(d.Name, name) {
			return d.ID, nil
		}
	}
	return "", fmt.Errorf("domain %q not found in stalwart", name)
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

// Alias é um endereço extra que entrega numa caixa: parte local + id do domínio NO STALWART.
type Alias struct {
	Local    string // "noreply"
	DomainID string // id do domínio no Stalwart (domains.stalwart_id)
}

// SetAccountAliases substitui a lista de aliases de uma conta. Manda a lista INTEIRA: a nossa base
// é a fonte de verdade, e assim a operação é idempotente — repetir não duplica nem deixa buracos.
//
// FORMATO (DIVERGENCES §D4b, confirmado na fonte do Stalwart, crates/registry/src/schema/structs.rs):
// `UserAccount.aliases` é `List<EmailAlias>`, o que em JSON significa um MAPA indexado
// (`{"0":…,"1":…}`) cujos VALORES são OBJETOS `{name, domainId, enabled}` — não endereços em texto.
// Um alias é descrito como uma caixa: parte local + domínio, sem campo `email`. Mandar strings dá
// `invalidPatch: "Invalid value type for object"`, que foi o que apanhámos em produção (28/07/2026).
func (c *Client) SetAccountAliases(ctx context.Context, accountID string, aliases []Alias) error {
	m := make(map[string]any, len(aliases))
	for i, a := range aliases {
		m[strconv.Itoa(i)] = map[string]any{
			"name":     a.Local,
			"domainId": a.DomainID,
			"enabled":  true, // por omissão seria false: o alias existiria mas não entregaria
		}
	}
	args := map[string]any{
		"update": map[string]any{accountID: map[string]any{"aliases": m}},
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
		sent, _ := json.Marshal(m)
		return fmt.Errorf("stalwart notUpdated: %s (we sent aliases=%s)", string(b), string(sent))
	}
	return nil
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
		err := fmt.Errorf("stalwart notUpdated: %s", string(b))
		if isWeakSecret(r.NotUpdated) {
			return fmt.Errorf("%w: %v", ErrWeakPassword, err)
		}
		return err
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
		return "", fmt.Errorf("stalwart: domain %s not found", domainID)
	}
	return g.List[0].DNSZoneFile, nil
}

// SuspendAccount blocks every way in without touching a single message: it removes the account's
// credentials. Mail keeps arriving, the mailbox keeps its contents, and IMAP, SMTP, JMAP and our
// own panel all stop accepting the password at once.
//
// WHY THIS AND NOT A FLAG: an account in Stalwart 0.16 has no "enabled" field -- the object carries
// name, quotas, roles, permissions, aliases and credentials, and nothing that says "off". Probed
// against a live account (01/08/2026): removing the credentials map denies the next login
// immediately, and writing a new secret lets it back in immediately. See deploy/DIVERGENCES.md.
//
// Reactivating therefore needs a NEW password (SetAccountPassword) -- the old one is gone. That is
// not a workaround dressed up as a feature: reinstating a suspended person's old password is the
// wrong default anyway, since suspension usually means somebody left or was compromised.
func (c *Client) SuspendAccount(ctx context.Context, accountID string) error {
	args := map[string]any{
		"update": map[string]any{accountID: map[string]any{"credentials": map[string]any{}}},
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

// RenameAccount changes the local part of an account, and with it the address it answers to.
//
// This is how a deleted mailbox stops accepting mail without a single message being destroyed. The
// account keeps everything it had; it simply no longer holds the address, so a sender to the old
// one is told the recipient does not exist -- which is the truth, and is what every other provider
// does. Renaming it back is the whole of "restore".
//
// Verified against a live account (01/08/2026): emailAddress follows the name in both directions
// and the stored mail is untouched.
func (c *Client) RenameAccount(ctx context.Context, accountID, newLocal string) error {
	args := map[string]any{
		"update": map[string]any{accountID: map[string]any{"name": newLocal}},
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
