package provisioning

import (
	"strings"
	"testing"
)

// The zone handed to a customer is the whole of what they will publish. If a host in it is wrong,
// their mail does not arrive and nothing in the instructions looks broken -- which is the worst
// shape a bug can take, because the customer blames their own DNS.

const containerZone = `example.com. IN MX 10 3517d6da4cde.
mta-sts.example.com. IN CNAME 3517d6da4cde.
autoconfig.example.com. IN CNAME 3517d6da4cde.
example.com. IN TXT "v=spf1 mx -all"
_dmarc.example.com. IN TXT "v=DMARC1; p=reject; rua=mailto:postmaster@example.com"`

func TestPublicHostsReplacesTheContainerName(t *testing.T) {
	out := PublicHosts(containerZone, "mail.beskos.com", "mta-sts.beskos.com")
	if strings.Contains(out, "3517d6da4cde") {
		t.Fatalf("the container id survived:\n%s", out)
	}
	if !strings.Contains(out, "IN MX 10 mail.beskos.com.") {
		t.Errorf("MX not rewritten:\n%s", out)
	}
	// The policy is served by the policy host over HTTPS. Pointing it at the mail server would
	// answer with a certificate for the wrong name, and the policy would never be readable.
	if !strings.Contains(out, "mta-sts.example.com. IN CNAME mta-sts.beskos.com.") {
		t.Errorf("MTA-STS must point at the policy host:\n%s", out)
	}
	if !strings.Contains(out, "autoconfig.example.com. IN CNAME mail.beskos.com.") {
		t.Errorf("other CNAMEs go to the mail host:\n%s", out)
	}
}

// TXT values often end in a word without a dot ("-all", a DMARC address). Rewriting those would
// corrupt the SPF and the DMARC -- the two records the audit checks hardest.
func TestPublicHostsLeavesTextRecordsAlone(t *testing.T) {
	out := PublicHosts(containerZone, "mail.beskos.com", "mta-sts.beskos.com")
	if !strings.Contains(out, `"v=spf1 mx -all"`) {
		t.Errorf("SPF was altered:\n%s", out)
	}
	if !strings.Contains(out, "rua=mailto:postmaster@example.com") {
		t.Errorf("DMARC was altered:\n%s", out)
	}
}

// A target that is already public belongs to the customer, or to a provider they chose. Rewriting it
// would silently redirect their mail to us.
func TestPublicHostsNeverTouchesAPublicTarget(t *testing.T) {
	zone := "example.com. IN MX 10 aspmx.l.google.com."
	if out := PublicHosts(zone, "mail.beskos.com", "mta-sts.beskos.com"); out != zone {
		t.Errorf("a public MX was rewritten:\n%s", out)
	}
}

// Without a configured public host there is nothing to substitute, and inventing one would be worse
// than leaving the zone as it is: the customer at least sees something obviously wrong.
func TestPublicHostsIsANoOpWithoutAHost(t *testing.T) {
	if out := PublicHosts(containerZone, "", ""); out != containerZone {
		t.Error("with no mail host configured the zone must be left untouched")
	}
}
