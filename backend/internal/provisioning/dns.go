package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"strings"
)

// NetDNSVerifier confirma publicação checando resolução real de DNS: o domínio precisa ter
// pelo menos um registro MX e um TXT de DKIM (_domainkey). Verificação mínima da Fase 1;
// SPF/DMARC/MTA-STS podem ser adicionados depois.
type NetDNSVerifier struct {
	resolver *net.Resolver
}

func NewNetDNSVerifier() *NetDNSVerifier { return &NetDNSVerifier{resolver: net.DefaultResolver} }

func (v *NetDNSVerifier) Verify(ctx context.Context, domain, zone string) (bool, error) {
	mx, err := v.resolver.LookupMX(ctx, domain)
	if err != nil || len(mx) == 0 {
		return false, nil // ainda não publicado (não é erro fatal)
	}
	// Procura o selector DKIM esperado a partir do zonefile do Stalwart.
	selector := dkimHost(zone, domain)
	if selector == "" {
		return len(mx) > 0, nil // sem selector conhecido: MX basta na Fase 1
	}
	txts, err := v.resolver.LookupTXT(ctx, selector)
	if err != nil || len(txts) == 0 {
		return false, nil
	}
	for _, t := range txts {
		if strings.Contains(t, "DKIM1") || strings.Contains(t, "k=") {
			return true, nil
		}
	}
	return false, nil
}

// dkimHost extrai o host do primeiro registro _domainkey do zonefile (ex.: "sel._domainkey.dominio.").
func dkimHost(zone, domain string) string {
	for _, line := range strings.Split(zone, "\n") {
		if strings.Contains(line, "_domainkey."+domain) {
			return strings.TrimSuffix(strings.Fields(line)[0], ".")
		}
	}
	return ""
}

// AlwaysVerified pula a verificação de DNS (dev/local, onde domínios de teste não resolvem).
// Ligado por config (PROVISION_SKIP_DNS_VERIFY=true). NUNCA usar em produção.
type AlwaysVerified struct{}

func (AlwaysVerified) Verify(context.Context, string, string) (bool, error) { return true, nil }

// genInitialSecret gera um secret forte e DETERMINÍSTICO por domínio (idempotência em retry).
// Alta entropia (deriva do UUID do domínio) para passar no checador de força do Stalwart.
// O admin do tenant troca a senha depois.
func genInitialSecret(seed string) string {
	sum := sha256.Sum256([]byte("mailin-postmaster-v1:" + seed))
	return "Pm9!" + base64.RawURLEncoding.EncodeToString(sum[:18])
}
