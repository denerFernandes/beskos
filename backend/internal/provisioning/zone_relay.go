package provisioning

import (
	"fmt"
	"strings"
)

// AugmentZoneForSES prepares the client's zone in WHITE-LABEL mode: SES signs via BYODKIM (our own
// key), so the client publishes only a generic DKIM TXT record (our public key under a
// brand-specific selector) — with NO reference to amazonses anywhere in the APEX domain's records.
// SPF is extended to include `_spf.<brand>` (which, in OUR zone, wraps include:amazonses.com), and
// DMARC carries our brand. It also emits the custom MAIL FROM at `bounce.<domain>` (MX + SPF) — the
// ONLY place that references amazonses, and only in the bounce subdomain; without it, the
// recipient's "sent by" would show amazonses. DMARC aligns on DKIM+SPF.
// Idempotent. mailFromHost = "bounce.<domain>", mailFromMX = "feedback-smtp.<region>.amazonses.com".
func AugmentZoneForSES(zone, domain, selector, publicKey, brand, mailFromHost, mailFromMX string) string {
	spfInclude := "include:_spf." + brand
	lines := strings.Split(strings.TrimRight(zone, "\n"), "\n")
	for i, line := range lines {
		if strings.Contains(line, "v=spf1") && !strings.Contains(line, spfInclude) {
			// The MAIL FROM subdomain's SPF (bounce) stays untouched — it needs include:amazonses
			// and must NOT carry _spf.<brand> (or it would duplicate on the 2nd pass / go misaligned).
			if mailFromHost != "" && strings.HasPrefix(strings.TrimSpace(line), mailFromHost+".") {
				continue
			}
			lines[i] = spfWithInclude(line, spfInclude)
		}
	}
	out := strings.Join(lines, "\n")
	dkim := fmt.Sprintf("%s._domainkey.%s. IN TXT \"v=DKIM1; k=rsa; p=%s\"", selector, domain, publicKey)
	if !strings.Contains(out, selector+"._domainkey."+domain) {
		out += "\n" + dkim
	}
	if !strings.Contains(out, "_dmarc."+domain) {
		out += fmt.Sprintf("\n_dmarc.%s. IN TXT \"v=DMARC1; p=quarantine; rua=mailto:dmarc@%s\"", domain, brand)
	}
	// Custom MAIL FROM: MX (bounces) + subdomain SPF. SES requires both to verify the domain.
	if mailFromHost != "" && mailFromMX != "" && !strings.Contains(out, mailFromHost+". IN MX") {
		out += fmt.Sprintf("\n%s. IN MX 10 %s.", mailFromHost, mailFromMX)
		out += fmt.Sprintf("\n%s. IN TXT \"v=spf1 include:amazonses.com ~all\"", mailFromHost)
	}
	return out + "\n"
}

// spfWithInclude inserts an `include:...` mechanism before the terminal mechanism (-all/~all/?all);
// with no terminal, it appends to the end of the value.
func spfWithInclude(line, include string) string {
	for _, term := range []string{"-all", "~all", "?all", "+all"} {
		if i := strings.Index(line, term); i >= 0 {
			return line[:i] + include + " " + line[i:]
		}
	}
	return line + " " + include
}
