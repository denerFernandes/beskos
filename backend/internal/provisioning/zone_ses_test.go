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
	// White-label: Stalwart doesn't sign (its DKIM is removed beforehand), SES signs with our own key.
	stripped := StripStalwartDKIM(sampleZone)
	out := AugmentZoneForSES(stripped, "acme.com", "mail", "PUBKEYb64", "beskos.com",
		"bounce.acme.com", "feedback-smtp.eu-west-1.amazonses.com")

	// The apex SPF gained include:_spf.beskos.com BEFORE -all (hides amazonses).
	if !strings.Contains(out, `"v=spf1 a:mail.beskos.com include:_spf.beskos.com -all"`) {
		t.Errorf("SPF not augmented: %s", out)
	}
	// DKIM with OUR OWN public key, brand selector.
	if !strings.Contains(out, `mail._domainkey.acme.com. IN TXT "v=DKIM1; k=rsa; p=PUBKEYb64"`) {
		t.Errorf("missing white-label DKIM: %s", out)
	}
	// DMARC with the rua on our own brand.
	if !strings.Contains(out, `_dmarc.acme.com. IN TXT "v=DMARC1; p=quarantine; rua=mailto:dmarc@beskos.com"`) {
		t.Errorf("missing DMARC: %s", out)
	}
	// Custom MAIL FROM: MX (bounces) + subdomain SPF — the recipient sees bounce.acme.com in
	// "sent by", not amazonses.
	if !strings.Contains(out, `bounce.acme.com. IN MX 10 feedback-smtp.eu-west-1.amazonses.com.`) {
		t.Errorf("missing MAIL FROM MX: %s", out)
	}
	if !strings.Contains(out, `bounce.acme.com. IN TXT "v=spf1 include:amazonses.com ~all"`) {
		t.Errorf("missing MAIL FROM SPF: %s", out)
	}

	// White-label: amazonses/feedback-smtp ONLY in the MAIL FROM subdomain (bounce.acme.com), never
	// in the apex domain's records.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "amazonses") || strings.Contains(line, "feedback-smtp") {
			if !strings.HasPrefix(strings.TrimSpace(line), "bounce.acme.com.") {
				t.Errorf("zone leaks amazonses outside MAIL FROM subdomain:\n%s", line)
			}
		}
	}
	// Stalwart's DKIM was removed.
	if strings.Contains(out, "stw._domainkey") {
		t.Errorf("stalwart DKIM not stripped: %s", out)
	}

	// Idempotent: applying it again doesn't duplicate.
	again := AugmentZoneForSES(out, "acme.com", "mail", "PUBKEYb64", "beskos.com",
		"bounce.acme.com", "feedback-smtp.eu-west-1.amazonses.com")
	if strings.Count(again, "mail._domainkey.acme.com") != 1 ||
		strings.Count(again, "include:_spf.beskos.com") != 1 ||
		strings.Count(again, "_dmarc.acme.com") != 1 ||
		strings.Count(again, "bounce.acme.com. IN MX") != 1 {
		t.Errorf("not idempotent:\n%s", again)
	}

	// The zone stays parseable for the UI (dns_pending screen).
	recs := ParseZone(again)
	byType := map[string]int{}
	for _, r := range recs {
		byType[r.Type]++
	}
	if byType["TXT"] < 4 { // SPF apex + DKIM nosso + DMARC + SPF do bounce
		t.Errorf("parsed TXTs = %d, want >= 4", byType["TXT"])
	}
}

func TestSpfWithIncludeNoTerminal(t *testing.T) {
	got := spfWithInclude(`v=spf1 a:mail.x.com`, "include:_spf.beskos.com")
	if !strings.Contains(got, "include:_spf.beskos.com") {
		t.Errorf("include missing: %s", got)
	}
}
