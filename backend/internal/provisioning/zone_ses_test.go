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
	tokens := []string{"tok1", "tok2", "tok3"}
	out := AugmentZoneForSES(sampleZone, "acme.com", tokens)

	// SPF ganhou o include ANTES do -all, mantendo o resto.
	if !strings.Contains(out, `"v=spf1 a:mail.beskos.com include:amazonses.com -all"`) {
		t.Errorf("SPF not augmented: %s", out)
	}
	// 3 CNAMEs de Easy DKIM.
	for _, tok := range tokens {
		want := tok + "._domainkey.acme.com. IN CNAME " + tok + ".dkim.amazonses.com."
		if !strings.Contains(out, want) {
			t.Errorf("missing DKIM CNAME %q", want)
		}
	}
	// DMARC adicionado.
	if !strings.Contains(out, `_dmarc.acme.com. IN TXT "v=DMARC1; p=quarantine; rua=mailto:postmaster@acme.com"`) {
		t.Errorf("missing DMARC: %s", out)
	}
	// DKIM do Stalwart intacto.
	if !strings.Contains(out, "stw._domainkey.acme.com. IN TXT") {
		t.Errorf("stalwart DKIM lost")
	}

	// Idempotente: aplicar de novo não duplica nada.
	again := AugmentZoneForSES(out, "acme.com", tokens)
	if strings.Count(again, "tok1._domainkey") != strings.Count(out, "tok1._domainkey") ||
		strings.Count(again, "include:amazonses.com") != 1 ||
		strings.Count(again, "_dmarc.acme.com") != 1 {
		t.Errorf("not idempotent:\n%s", again)
	}

	// A zona aumentada continua parseável para a UI (tela dns_pending).
	recs := ParseZone(again)
	byType := map[string]int{}
	for _, r := range recs {
		byType[r.Type]++
	}
	if byType["CNAME"] != 3 {
		t.Errorf("parsed CNAMEs = %d, want 3", byType["CNAME"])
	}
	if byType["TXT"] < 3 { // SPF + DKIM stalwart + DMARC
		t.Errorf("parsed TXTs = %d, want >= 3", byType["TXT"])
	}
}

func TestSpfWithSESNoTerminal(t *testing.T) {
	got := spfWithSES(`x.com. IN TXT "v=spf1 a:mail.x.com"`)
	if !strings.Contains(got, "include:amazonses.com") {
		t.Errorf("include missing: %s", got)
	}
}
