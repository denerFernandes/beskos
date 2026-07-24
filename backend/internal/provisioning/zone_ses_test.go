package provisioning

import (
	"strings"
	"testing"
)

const sampleZone = `acme.com. IN MX 10 mail.beskos.com.
acme.com. IN TXT "v=spf1 a:mail.beskos.com -all"
stw._domainkey.acme.com. IN TXT "v=DKIM1; k=ed25519; p=abc"
`

func TestAugmentZoneForSES(t *testing.T) {
	// White-label: Stalwart não assina (o DKIM dele é removido antes), o SES assina com a nossa chave.
	stripped := StripStalwartDKIM(sampleZone)
	out := AugmentZoneForSES(stripped, "acme.com", "mail", "PUBKEYb64", "beskos.com")

	// SPF ganhou include:_spf.beskos.com ANTES do -all (esconde o amazonses).
	if !strings.Contains(out, `"v=spf1 a:mail.beskos.com include:_spf.beskos.com -all"`) {
		t.Errorf("SPF not augmented: %s", out)
	}
	// DKIM com a NOSSA chave pública, seletor da marca.
	if !strings.Contains(out, `mail._domainkey.acme.com. IN TXT "v=DKIM1; k=rsa; p=PUBKEYb64"`) {
		t.Errorf("missing white-label DKIM: %s", out)
	}
	// DMARC com rua na nossa marca.
	if !strings.Contains(out, `_dmarc.acme.com. IN TXT "v=DMARC1; p=quarantine; rua=mailto:dmarc@beskos.com"`) {
		t.Errorf("missing DMARC: %s", out)
	}
	// White-label: NADA do Amazon no DNS do cliente.
	for _, forbidden := range []string{"amazonses", "dkim.amazonses", "feedback-smtp"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("zone leaks %q:\n%s", forbidden, out)
		}
	}
	// DKIM do Stalwart foi removido.
	if strings.Contains(out, "stw._domainkey") {
		t.Errorf("stalwart DKIM not stripped: %s", out)
	}

	// Idempotente: aplicar de novo não duplica.
	again := AugmentZoneForSES(out, "acme.com", "mail", "PUBKEYb64", "beskos.com")
	if strings.Count(again, "mail._domainkey.acme.com") != 1 ||
		strings.Count(again, "include:_spf.beskos.com") != 1 ||
		strings.Count(again, "_dmarc.acme.com") != 1 {
		t.Errorf("not idempotent:\n%s", again)
	}

	// A zona continua parseável para a UI (tela dns_pending).
	recs := ParseZone(again)
	byType := map[string]int{}
	for _, r := range recs {
		byType[r.Type]++
	}
	if byType["TXT"] < 3 { // SPF + DKIM nosso + DMARC
		t.Errorf("parsed TXTs = %d, want >= 3", byType["TXT"])
	}
}

func TestSpfWithIncludeNoTerminal(t *testing.T) {
	got := spfWithInclude(`v=spf1 a:mail.x.com`, "include:_spf.beskos.com")
	if !strings.Contains(got, "include:_spf.beskos.com") {
		t.Errorf("include missing: %s", got)
	}
}
