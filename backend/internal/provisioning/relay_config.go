package provisioning

import (
	"context"
	"fmt"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/ses"
	"log"
)

// EmailRelay is the outbound relay (Amazon SES) in WHITE-LABEL mode (BYODKIM): it registers the
// domain as an identity with OUR OWN DKIM key and returns the selector + public key for the
// customer to publish — the customer's DNS never reveals Amazon. nil = no relay (pure dev/self-hosted).
type EmailRelay interface {
	CreateIdentity(ctx context.Context, domain string) (selector, publicKey string, err error)
	IdentityVerified(ctx context.Context, domain string) (bool, error)
	// SetMailFrom sets the custom MAIL FROM (Return-Path bounce.<domain>) so "sent by" shows the
	// customer's domain, not amazonses.
	SetMailFrom(ctx context.Context, domain string) error
	// MailFromSubdomain/MailFromMX describe the record the customer publishes for MAIL FROM.
	MailFromSubdomain(domain string) string
	MailFromMX() string
	// CreateTenant/AssociateIdentity isolate this domain's reputation from other customers'. Without
	// this, one customer's bounces degrade the ACCOUNT'S reputation and AWS can suspend mail for
	// everyone; with it, only the affected tenant gets paused.
	CreateTenant(ctx context.Context, name string) error
	AssociateIdentity(ctx context.Context, tenant, domain string) error
	// The configuration set is what actually works TODAY: attached to the identity, AWS applies it
	// to every message from the domain without anything in the mail path changing.
	CreateConfigurationSet(ctx context.Context, name string) error
	SetIdentityConfigurationSet(ctx context.Context, domain, name string) error
}

type relayConfig struct {
	relay EmailRelay // nil = no SES
	brand string     // brand domain (e.g. beskos.com) for _spf.<brand> and the DMARC rua
}

// WithRelay attaches the outbound relay (SES white-label) when provisioning domains. brand is the
// platform's domain used in the records that hide SES (`_spf.<brand>`, the DMARC rua).
func (m *Machine) WithRelay(r EmailRelay, brand string) *Machine {
	m.relay = r
	m.brand = brand
	return m
}

func (m *Machine) usesRelay() bool { return m.relay != nil }
func (m *Machine) verifyRelayIdentity(ctx context.Context, domain string) (bool, error) {
	if m.relay == nil {
		return true, nil
	}
	return m.relay.IdentityVerified(ctx, domain)
}
func (m *Machine) prepareRelayZone(ctx context.Context, d db.Domain, zone string) (string, error) {
	// White-label: SES signs via BYODKIM (our own key). We register the identity and swap
	// Stalwart's DKIM for ours — the customer publishes only our public key, never seeing Amazon.
	selector, publicKey, err := m.relay.CreateIdentity(ctx, d.Domain)
	if err != nil {
		return "", fmt.Errorf("register identity with the relay: %w", err) // retry next tick
	}
	// Reputation isolation: one SES tenant per domain, with the identity associated. Failing
	// here does NOT block provisioning — mail still goes out, just without the isolation — but
	// it gets logged, because a customer without a tenant is a customer who can take down
	// everyone else.
	name := ses.TenantName(d.Domain)

	// The configuration set is what has an effect right away: attached to the identity, it gives
	// per-customer reputation metrics and lets us suspend just this domain.
	if err := m.relay.CreateConfigurationSet(ctx, name); err != nil {
		log.Printf("provisioning: configuration set for %s not created (metrics stay aggregated): %v", d.Domain, err)
	} else if err := m.relay.SetIdentityConfigurationSet(ctx, d.Domain, name); err != nil {
		log.Printf("provisioning: configuration set %s did not become the default on %s: %v", name, d.Domain, err)
	}

	// The tenant is created ahead of the day messages carry the X-SES-TENANT header — that's
	// what brings AWS's automatic pause by reputation.
	if err := m.relay.CreateTenant(ctx, name); err != nil {
		log.Printf("provisioning: SES tenant for %s not created: %v", d.Domain, err)
	} else if err := m.relay.AssociateIdentity(ctx, name, d.Domain); err != nil {
		log.Printf("provisioning: identity for %s not associated with tenant %s: %v", d.Domain, name, err)
	}

	// Custom MAIL FROM (bounce.<domain>): without it the recipient's "sent by" shows amazonses.
	// Fallback USE_DEFAULT_VALUE — doesn't block sending while the MX hasn't propagated yet.
	if err := m.relay.SetMailFrom(ctx, d.Domain); err != nil {
		return "", fmt.Errorf("configure MAIL FROM on the relay: %w", err) // retry next tick
	}
	zone = StripStalwartDKIM(zone)
	zone = AugmentZoneForSES(zone, d.Domain, selector, publicKey, m.brand,
		m.relay.MailFromSubdomain(d.Domain), m.relay.MailFromMX())

	return zone, nil
}
