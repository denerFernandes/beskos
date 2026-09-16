package provisioning

import (
	"context"
	"strings"
	"testing"
)

type fakeRelay struct {
	created      int
	mailFrom     int
	verified     bool
	tenants      []string // names created
	associados   []string // "tenant/domain"
	configSets   []string // configuration sets created
	predefinidos []string // "domain/set"
}

func (f *fakeRelay) CreateIdentity(context.Context, string) (string, string, error) {
	f.created++
	return "mail", "PUBKEYbase64", nil // brand selector + our own public key (BYODKIM)
}
func (f *fakeRelay) IdentityVerified(context.Context, string) (bool, error) {
	return f.verified, nil
}
func (f *fakeRelay) SetMailFrom(context.Context, string) error { f.mailFrom++; return nil }
func (f *fakeRelay) MailFromSubdomain(domain string) string    { return "bounce." + domain }
func (f *fakeRelay) MailFromMX() string                        { return "feedback-smtp.eu-west-1.amazonses.com" }
func (f *fakeRelay) CreateTenant(_ context.Context, name string) error {
	f.tenants = append(f.tenants, name)
	return nil
}
func (f *fakeRelay) AssociateIdentity(_ context.Context, tenant, domain string) error {
	f.associados = append(f.associados, tenant+"/"+domain)
	return nil
}
func (f *fakeRelay) CreateConfigurationSet(_ context.Context, name string) error {
	f.configSets = append(f.configSets, name)
	return nil
}
func (f *fakeRelay) SetIdentityConfigurationSet(_ context.Context, domain, name string) error {
	f.predefinidos = append(f.predefinidos, domain+"/"+name)
	return nil
}

// With relay (white-label): the zone gains DKIM under OUR OWN key (no amazonses), SPF via
// _spf.<brand>, and a DMARC; Stalwart's DKIM is removed; and dns_verified only fires once SES confirms.
func TestRelaySES(t *testing.T) {
	sw := &fakeMailserver{zone: "cliente.com.br. IN TXT \"v=spf1 a:mail.x -all\"\nsel._domainkey.cliente.com.br. IN TXT \"v=DKIM1; p=abc\""}
	store := &fakeStore{domain: newDomain(StatePending)}
	relay := &fakeRelay{}
	m := NewMachine(sw, AlwaysVerified{}, store).WithRelay(relay, "beskos.com")

	final := runToActive(t, m, store)
	if sw.autoDKIM {
		t.Fatal("relay domains must not enable Stalwart DKIM")
	}
	// SES hasn't verified yet → stuck at dns_pending.
	if final.ProvisionState != StateDNSPending {
		t.Fatalf("expected dns_pending while SES hasn't verified, got %s", final.ProvisionState)
	}
	zone := strval(final.DnsZone)
	for _, want := range []string{
		"mail._domainkey.cliente.com.br. IN TXT \"v=DKIM1; k=rsa; p=PUBKEYbase64\"",
		"include:_spf.beskos.com", "_dmarc.cliente.com.br.", "rua=mailto:dmarc@beskos.com",
		// Custom MAIL FROM (the recipient's "sent by" shows bounce.<domain>, not amazonses).
		"bounce.cliente.com.br. IN MX 10 feedback-smtp.eu-west-1.amazonses.com.",
		"bounce.cliente.com.br. IN TXT \"v=spf1 include:amazonses.com ~all\"",
	} {
		if !containsStr(zone, want) {
			t.Errorf("zone missing %q:\n%s", want, zone)
		}
	}
	// Reputation isolation: the domain must get its own SES tenant and stay associated with it.
	// Without this, one customer's bounces penalize the whole account.
	if len(relay.tenants) != 1 || relay.tenants[0] != "cliente-com-br" {
		t.Errorf("expected SES tenant [cliente-com-br], got %v", relay.tenants) // lang:ok
	}
	if len(relay.associados) != 1 || relay.associados[0] != "cliente-com-br/cliente.com.br" {
		t.Errorf("expected association [cliente-com-br/cliente.com.br], got %v", relay.associados) // lang:ok
	}
	// The configuration set must become the DEFAULT on the identity: that's what dispenses with
	// the per-message header and keeps metrics separated without touching the mail path.
	if len(relay.predefinidos) != 1 || relay.predefinidos[0] != "cliente.com.br/cliente-com-br" {
		t.Errorf("expected default set [cliente.com.br/cliente-com-br], got %v", relay.predefinidos) // lang:ok
	}
	if relay.mailFrom != 1 {
		t.Errorf("SetMailFrom should be 1x, got %d", relay.mailFrom)
	}
	// White-label: amazonses can only appear in the MAIL FROM subdomain (bounce.<domain>) — never
	// in the apex domain's records, which is what the customer and recipient see first.
	for _, line := range strings.Split(zone, "\n") {
		if strings.Contains(line, "amazonses") || strings.Contains(line, "feedback-smtp") {
			if !strings.HasPrefix(strings.TrimSpace(line), "bounce.cliente.com.br.") {
				t.Errorf("amazonses outside the bounce subdomain:\n%s", line)
			}
		}
	}
	// Stalwart's DKIM must be removed (only our key via SES).
	if containsStr(zone, "sel._domainkey.cliente.com.br") {
		t.Errorf("Stalwart's DKIM was not removed:\n%s", zone)
	}
	if relay.created != 1 {
		t.Errorf("CreateIdentity should be 1x, got %d", relay.created)
	}

	// SES confirms → proceeds to active.
	relay.verified = true
	final = runToActive(t, m, store)
	if final.ProvisionState != StateActive {
		t.Fatalf("expected active after SES verification, got %s", final.ProvisionState)
	}
}

func containsStr(s, sub string) bool { return len(s) >= len(sub) && strings.Contains(s, sub) }
