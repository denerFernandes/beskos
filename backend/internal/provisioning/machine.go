// Package provisioning implements the domain provisioning state machine, built on top of JMAP.
//
//	pending → domain_created → dkim_created → dns_pending → dns_verified → active
//	                                                                   ↘ error
//
// Every transition is idempotent and recoverable: the worker always resumes from the last saved
// state, never from scratch. Interfaces (Mailserver, DNSVerifier, Store) let the machine be
// tested without a real Stalwart.
package provisioning

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/ses"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// States persisted in domains.provision_state.
const (
	StatePending       = "pending"
	StateDomainCreated = "domain_created"
	StateDKIMCreated   = "dkim_created"
	StateDNSPending    = "dns_pending"
	StateDNSVerified   = "dns_verified"
	StateActive        = "active"
	StateError         = "error"
)

// ActiveStates are the non-terminal states the worker needs to process.
var ActiveStates = []string{StatePending, StateDomainCreated, StateDKIMCreated, StateDNSPending, StateDNSVerified}

// Mailserver is the subset of the Stalwart client the machine uses (mockable in tests).
type Mailserver interface {
	// autoDKIM=false (relay mode) creates the domain with Manual DKIM — Stalwart doesn't sign.
	CreateDomain(ctx context.Context, name, description string, autoDKIM bool) (string, error)
	GetDomainDNSZone(ctx context.Context, domainID string) (string, error)
	CreateAccount(ctx context.Context, in stalwart.AccountInput) (string, error)
}

// DNSVerifier confirms whether the domain's records have already been published by the customer.
type DNSVerifier interface {
	Verify(ctx context.Context, domain, zone string) (bool, error)
}

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

// Store is the subset of db.Queries used by the machine.
type Store interface {
	UpdateDomainProvision(ctx context.Context, arg db.UpdateDomainProvisionParams) (db.Domain, error)
	CreateMailUser(ctx context.Context, arg db.CreateMailUserParams) (db.MailUser, error)
	GetMailUserByEmail(ctx context.Context, email string) (db.MailUser, error)
}

type Machine struct {
	sw    Mailserver
	dns   DNSVerifier
	store Store
	relay EmailRelay // nil = no SES
	brand string     // brand domain (e.g. beskos.com) for _spf.<brand> and the DMARC rua
	// Public names the customer will publish. Stalwart writes its own container hostname into every
	// host-valued record, which resolves nowhere outside the swarm -- see PublicHosts.
	mailHost   string
	mtaStsHost string
}

// WithPublicHosts sets the names that replace Stalwart's internal hostname in the customer's zone.
// Without them the MX handed out points at a container id and no mail is ever delivered.
func (m *Machine) WithPublicHosts(mailHost, mtaStsHost string) *Machine {
	m.mailHost, m.mtaStsHost = mailHost, mtaStsHost
	return m
}

func NewMachine(sw Mailserver, dns DNSVerifier, store Store) *Machine {
	return &Machine{sw: sw, dns: dns, store: store}
}

// WithRelay attaches the outbound relay (SES white-label) when provisioning domains. brand is the
// platform's domain used in the records that hide SES (`_spf.<brand>`, the DMARC rua).
func (m *Machine) WithRelay(r EmailRelay, brand string) *Machine {
	m.relay = r
	m.brand = brand
	return m
}

// AdvanceOnce executes AT MOST one transition for the domain and persists the result.
// Returns changed=true if the state changed. A state that's still "waiting" (e.g. DKIM not
// generated, DNS not published) returns changed=false without error — the worker retries later.
func (m *Machine) AdvanceOnce(ctx context.Context, d db.Domain) (changed bool, err error) {
	switch d.ProvisionState {
	case StatePending:
		return m.stepCreateDomain(ctx, d)
	case StateDomainCreated:
		return m.stepConfirmDKIM(ctx, d)
	case StateDKIMCreated:
		return m.stepOfferDNS(ctx, d)
	case StateDNSPending:
		return m.stepVerifyDNS(ctx, d)
	case StateDNSVerified:
		return m.stepActivate(ctx, d)
	case StateActive, StateError:
		return false, nil // terminal states
	default:
		return false, fmt.Errorf("unknown state: %q", d.ProvisionState)
	}
}

// pending → domain_created: creates the domain in Stalwart. Idempotent: if there's already a
// stalwart_id (resumed after a crash between the call and the commit), skip creation.
func (m *Machine) stepCreateDomain(ctx context.Context, d db.Domain) (bool, error) {
	stalwartID := strval(d.StalwartID)
	if stalwartID == "" {
		// With relay (SES), Stalwart doesn't manage DKIM (autoDKIM=false): SES signs via BYODKIM.
		autoDKIM := m.relay == nil
		id, err := m.sw.CreateDomain(ctx, d.Domain, "tenant_id="+d.TenantID.String(), autoDKIM)
		if err != nil {
			return false, fmt.Errorf("create domain: %w", err)
		}
		stalwartID = id
	}
	return m.persist(ctx, d, StateDomainCreated, &stalwartID, d.DkimStatus, d.DnsStatus, d.DnsZone)
}

// domain_created → dkim_created: DKIM is auto-generated on create, but the TXT record does not
// appear immediately. We fetch the zonefile and only advance once the DKIM record actually
// exists. With relay (SES): register the identity HERE and add the Easy DKIM CNAMEs, the
// include:amazonses.com SPF entry, and a DMARC record to the zone — the client publishes
// everything in one shot.
func (m *Machine) stepConfirmDKIM(ctx context.Context, d db.Domain) (bool, error) {
	zone, err := m.sw.GetDomainDNSZone(ctx, strval(d.StalwartID))
	if err != nil {
		return false, fmt.Errorf("read zone: %w", err)
	}
	if m.relay != nil {
		// White-label: SES signs via BYODKIM (our own key). We register the identity and swap
		// Stalwart's DKIM for ours — the customer publishes only our public key, never seeing Amazon.
		selector, publicKey, err := m.relay.CreateIdentity(ctx, d.Domain)
		if err != nil {
			return false, fmt.Errorf("register identity with the relay: %w", err) // retry next tick
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
			return false, fmt.Errorf("configure MAIL FROM on the relay: %w", err) // retry next tick
		}
		zone = StripStalwartDKIM(zone)
		zone = AugmentZoneForSES(zone, d.Domain, selector, publicKey, m.brand,
			m.relay.MailFromSubdomain(d.Domain), m.relay.MailFromMX())
		zone = PublicHosts(zone, m.mailHost, m.mtaStsHost)
		return m.persist(ctx, d, StateDKIMCreated, d.StalwartID, "ses", d.DnsStatus, &zone)
	}
	// Without relay: wait for Stalwart's DKIM (doesn't appear right away — DIVERGENCES §D4).
	if !hasDKIM(zone) {
		return false, nil // not generated yet; try again later
	}
	zone = PublicHosts(zone, m.mailHost, m.mtaStsHost)
	return m.persist(ctx, d, StateDKIMCreated, d.StalwartID, "generated", d.DnsStatus, &zone)
}

// dkim_created → dns_pending: records ready; handed off for the customer to publish (parks here).
func (m *Machine) stepOfferDNS(ctx context.Context, d db.Domain) (bool, error) {
	return m.persist(ctx, d, StateDNSPending, d.StalwartID, d.DkimStatus, "awaiting", d.DnsZone)
}

// dns_pending → dns_verified: the worker polls until the records match. With relay, it also
// requires SES to confirm the identity's DKIM (otherwise SES would later refuse the MAIL FROM).
func (m *Machine) stepVerifyDNS(ctx context.Context, d db.Domain) (bool, error) {
	ok, err := m.dns.Verify(ctx, d.Domain, strval(d.DnsZone))
	if err != nil {
		return false, fmt.Errorf("verify DNS: %w", err)
	}
	if !ok {
		waiting(d.Domain, "dns", "MX and "+dkimHost(strval(d.DnsZone), d.Domain))
		return false, nil // customer hasn't published yet
	}
	if m.relay != nil {
		verified, rerr := m.relay.IdentityVerified(ctx, d.Domain)
		if rerr != nil {
			return false, fmt.Errorf("verify identity with the relay: %w", rerr)
		}
		if !verified {
			waiting(d.Domain, "relay", "identity verification")
			return false, nil // SES still validating the DKIM CNAMEs; try again later
		}
	}
	waitingDone(d.Domain)
	// The customer has published mta-sts.<domain> by now, so the policy host can be reached under
	// that name and issue its certificate. Doing it here means the first person to look -- usually
	// the customer, checking their own work -- finds it already serving. See mtasts_warm.go.
	m.warmMTASTS(ctx, d.Domain)
	return m.persist(ctx, d, StateDNSVerified, d.StalwartID, d.DkimStatus, "verified", d.DnsZone)
}

// dns_verified → active: creates the initial mailbox (postmaster) and marks it active.
// Idempotent: CreateAccount failing because it already exists doesn't block activation.
func (m *Machine) stepActivate(ctx context.Context, d db.Domain) (bool, error) {
	email := "postmaster@" + d.Domain
	// Idempotency: if the mailbox has already been persisted (resumed), skip creation and just activate.
	if _, err := m.store.GetMailUserByEmail(ctx, email); err != nil {
		acctID, err := m.sw.CreateAccount(ctx, stalwart.AccountInput{
			Login:       "postmaster",
			DomainID:    strval(d.StalwartID),
			DisplayName: "Postmaster",
			Password:    genInitialSecret(d.ID.String()),
			QuotaBytes:  1 << 30,
		})
		if err != nil {
			return false, fmt.Errorf("create the initial mailbox: %w", err)
		}
		if _, err := m.store.CreateMailUser(ctx, db.CreateMailUserParams{
			TenantID: d.TenantID, Email: email, Name: "Postmaster", Role: "admin",
			StalwartAccountID: &acctID, QuotaBytes: 1 << 30,
		}); err != nil {
			return false, fmt.Errorf("persist mailbox: %w", err)
		}
	}
	return m.persist(ctx, d, StateActive, d.StalwartID, d.DkimStatus, d.DnsStatus, d.DnsZone)
}

func (m *Machine) persist(ctx context.Context, d db.Domain, state string, stalwartID *string, dkim, dns string, zone *string) (bool, error) {
	_, err := m.store.UpdateDomainProvision(ctx, db.UpdateDomainProvisionParams{
		ID: d.ID, ProvisionState: state, StalwartID: stalwartID,
		DkimStatus: dkim, DnsStatus: dns, DnsZone: zone,
	})
	if err != nil {
		return false, fmt.Errorf("persist state %s: %w", state, err)
	}
	return true, nil
}

func strval(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func hasDKIM(zone string) bool {
	return strings.Contains(zone, "DKIM1") || strings.Contains(zone, "_domainkey")
}
