package stalwart

import (
	"context"
	"os"
	"testing"
	"time"
)

// Teste de integração contra um Stalwart de teste em container (brief §10).
// Rode com:
//   STALWART_TEST_URL=http://localhost:18080 STALWART_TEST_USER=admin \
//   STALWART_TEST_PWD=probe-secret go test ./internal/stalwart -run TestIntegration -v
// Sem as envs, o teste é pulado (não quebra `go test ./...` em CI sem container).

func testClient(t *testing.T) *Client {
	t.Helper()
	url := os.Getenv("STALWART_TEST_URL")
	if url == "" {
		t.Skip("STALWART_TEST_URL não definido; pulando teste de integração")
	}
	return New(url, os.Getenv("STALWART_TEST_USER"), os.Getenv("STALWART_TEST_PWD"))
}

func TestIntegrationProvisioning(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ok, err := c.IsBootstrapped(ctx)
	if err != nil {
		t.Fatalf("IsBootstrapped: %v", err)
	}
	if !ok {
		t.Skip("servidor em bootstrap mode; rode Bootstrap()+restart antes deste teste")
	}

	// Nome único-ish sem depender de time.Now() no domínio (base32 curto do unix nano).
	domain := "it-" + time.Now().Format("150405") + ".test"

	domID, err := c.CreateDomain(ctx, domain, "tenant_id=integration")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	t.Logf("domínio criado id=%s", domID)

	// DNS zonefile deve conter registros essenciais.
	zone, err := c.GetDomainDNSZone(ctx, domID)
	if err != nil {
		t.Fatalf("GetDomainDNSZone: %v", err)
	}
	// Garantidos no momento do create. O DKIM TXT (_domainkey) NÃO aparece imediatamente
	// no zonefile de um domínio recém-criado (geração das assinaturas não é instantânea);
	// a tela de DNS deve fazer polling até o registro DKIM surgir. Ver DIVERGENCES §D4.
	for _, want := range []string{"MX", "spf1", "DMARC1"} {
		if !contains(zone, want) {
			t.Errorf("dnsZoneFile não contém %q. zona=%s", want, zone)
		}
	}

	acctID, err := c.CreateAccount(ctx, AccountInput{
		Login:       "joao",
		DomainID:    domID,
		DisplayName: "João Integração",
		Password:    "correct-horse-battery-staple-9273",
		QuotaBytes:  5 << 30,
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	t.Logf("conta criada id=%s", acctID)

	// Teardown na ordem correta (conta → domínio) por causa da integridade referencial.
	if err := c.DeleteAccount(ctx, acctID); err != nil {
		t.Errorf("DeleteAccount: %v", err)
	}
	if err := c.DeleteDomain(ctx, domID); err != nil {
		t.Errorf("DeleteDomain: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
