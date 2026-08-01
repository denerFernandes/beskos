package provisioning

// Getting the MTA-STS certificate issued BEFORE anyone needs it.
//
// The policy host serves every customer domain with a certificate issued on demand, during the TLS
// handshake, the first time somebody asks for that name. Issuance means a round trip to Let's
// Encrypt and a challenge: it takes seconds, not milliseconds.
//
// WHY THAT MATTERS ENOUGH TO WARRANT CODE: the first thing a customer does after publishing their
// records is check them -- often with our own free audit, whose TLS handshake gives up after four
// seconds. Without this, the very first look at a correctly configured domain reports MTA-STS as
// broken, and the customer concludes the product does not work. The mail itself would be fine; the
// impression would not be.
//
// Best-effort by design. A failure here means the certificate is issued later, on the first real
// request, exactly as it would have been anyway -- so nothing about provisioning depends on it and
// nothing about it can fail a domain.

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"
)

// warmMTASTS asks the policy host for this domain's policy once, so the certificate exists before
// the customer, or a sending server, looks for it.
//
// Does nothing unless a policy host is configured, which also keeps tests off the network: a fake
// domain would otherwise be resolved and dialled for real on every run.
func (m *Machine) warmMTASTS(ctx context.Context, domain string) {
	if domain == "" || m.mtaStsHost == "" {
		return
	}
	// Long enough for a full ACME issuance, which is the whole point of the call. It runs in the
	// worker's provisioning loop, once per domain, so the wait costs nothing anyone is waiting on.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	url := "https://mta-sts." + domain + "/.well-known/mta-sts.txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		// Expected while the customer's CNAME has not propagated yet. Logged rather than retried:
		// the next fetch of the policy issues the certificate regardless.
		log.Printf("mta-sts warm-up for %s: %v (the certificate will be issued on first use)", domain, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	log.Printf("mta-sts warm-up for %s: HTTP %d", domain, resp.StatusCode)
}
