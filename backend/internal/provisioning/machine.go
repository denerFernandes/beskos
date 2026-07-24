// Package provisioning implementa a máquina de estados de provisionamento de domínio
// (brief §7), agora sobre JMAP (ver deploy/DIVERGENCES.md §D6).
//
//	pending → domain_created → dkim_created → dns_pending → dns_verified → active
//	                                                                   ↘ error
//
// Cada transição é idempotente e recuperável: o worker sempre retoma do último estado
// salvo, nunca do zero (brief §7/§10). Interfaces (Mailserver, DNSVerifier, Store) permitem
// testar a máquina sem um Stalwart real.
package provisioning

import (
	"context"
	"fmt"
	"strings"

	"mailin/backend/internal/db"
	"mailin/backend/internal/stalwart"
)

// Estados persistidos em domains.provision_state.
const (
	StatePending       = "pending"
	StateDomainCreated = "domain_created"
	StateDKIMCreated   = "dkim_created"
	StateDNSPending    = "dns_pending"
	StateDNSVerified   = "dns_verified"
	StateActive        = "active"
	StateError         = "error"
)

// ActiveStates são os estados não-terminais que o worker precisa processar.
var ActiveStates = []string{StatePending, StateDomainCreated, StateDKIMCreated, StateDNSPending, StateDNSVerified}

// Mailserver é o subconjunto do client Stalwart que a máquina usa (mockável nos testes).
type Mailserver interface {
	CreateDomain(ctx context.Context, name, description string) (string, error)
	GetDomainDNSZone(ctx context.Context, domainID string) (string, error)
	CreateAccount(ctx context.Context, in stalwart.AccountInput) (string, error)
}

// DNSVerifier confirma se os registros do domínio já foram publicados pelo cliente.
type DNSVerifier interface {
	Verify(ctx context.Context, domain, zone string) (bool, error)
}

// EmailRelay é o relay de saída (Amazon SES): regista o domínio como identidade (Easy DKIM),
// configura o custom MAIL FROM (subdomínio do próprio domínio — regra do SES) e diz quando a
// verificação do lado do relay concluiu. nil = sem relay (dev/self-hosted puro).
type EmailRelay interface {
	CreateIdentity(ctx context.Context, domain string) (dkimTokens []string, err error)
	// SetupMailFrom devolve o alvo do MX (feedback-smtp da região) para a zona do cliente.
	SetupMailFrom(ctx context.Context, domain, mailFromDomain string) (mxTarget string, err error)
	IdentityVerified(ctx context.Context, domain string) (bool, error)
}

// mailFromPrefix é o subdomínio de MAIL FROM por tenant (pedido 22/07/2026): mailer.<domínio>.
const mailFromPrefix = "mailer."

// Store é o subconjunto do db.Queries usado pela máquina.
type Store interface {
	UpdateDomainProvision(ctx context.Context, arg db.UpdateDomainProvisionParams) (db.Domain, error)
	CreateMailUser(ctx context.Context, arg db.CreateMailUserParams) (db.MailUser, error)
	GetMailUserByEmail(ctx context.Context, email string) (db.MailUser, error)
}

type Machine struct {
	sw    Mailserver
	dns   DNSVerifier
	store Store
	relay EmailRelay // nil = sem SES
}

func NewMachine(sw Mailserver, dns DNSVerifier, store Store) *Machine {
	return &Machine{sw: sw, dns: dns, store: store}
}

// WithRelay liga o registo automático no relay de saída (SES) ao provisionar domínios.
func (m *Machine) WithRelay(r EmailRelay) *Machine {
	m.relay = r
	return m
}

// AdvanceOnce executa NO MÁXIMO uma transição para o domínio e persiste o resultado.
// Retorna changed=true se o estado mudou. Um estado que ainda "espera" (ex.: DKIM não
// gerado, DNS não publicado) retorna changed=false sem erro — o worker tenta de novo depois.
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
		return false, nil // terminais
	default:
		return false, fmt.Errorf("estado desconhecido: %q", d.ProvisionState)
	}
}

// pending → domain_created: cria o domínio no Stalwart. Idempotente: se já há stalwart_id
// (retomada após crash entre a chamada e o commit), pula a criação.
func (m *Machine) stepCreateDomain(ctx context.Context, d db.Domain) (bool, error) {
	stalwartID := strval(d.StalwartID)
	if stalwartID == "" {
		id, err := m.sw.CreateDomain(ctx, d.Dominio, "tenant_id="+d.TenantID.String())
		if err != nil {
			return false, fmt.Errorf("criar domínio: %w", err)
		}
		stalwartID = id
	}
	return m.persist(ctx, d, StateDomainCreated, &stalwartID, d.DkimStatus, d.DnsStatus, d.DnsZone)
}

// domain_created → dkim_created: DKIM é auto-gerado no create, mas o TXT não aparece na hora
// (DIVERGENCES §D4). Buscamos o zonefile e só avançamos quando o registro DKIM já existe.
// Com relay (SES): regista a identidade AQUI e acrescenta à zona os CNAMEs de Easy DKIM, o
// include:amazonses.com no SPF e um DMARC — o cliente publica tudo de uma vez (pedido 22/07/2026).
func (m *Machine) stepConfirmDKIM(ctx context.Context, d db.Domain) (bool, error) {
	zone, err := m.sw.GetDomainDNSZone(ctx, strval(d.StalwartID))
	if err != nil {
		return false, fmt.Errorf("ler zona: %w", err)
	}
	if !hasDKIM(zone) {
		return false, nil // ainda não gerado; tenta de novo depois
	}
	if m.relay != nil {
		tokens, err := m.relay.CreateIdentity(ctx, d.Dominio)
		if err != nil {
			return false, fmt.Errorf("registar identidade no relay: %w", err) // retry no próximo tick
		}
		// MAIL FROM próprio (mailer.<domínio>): alinhamento SPF+DKIM no DMARC e envelope com a
		// marca do cliente em vez de amazonses.com.
		mailFrom := mailFromPrefix + d.Dominio
		mxTarget, err := m.relay.SetupMailFrom(ctx, d.Dominio, mailFrom)
		if err != nil {
			return false, fmt.Errorf("configurar mail-from no relay: %w", err)
		}
		zone = AugmentZoneForSES(zone, d.Dominio, tokens)
		zone = AugmentZoneMailFrom(zone, mailFrom, mxTarget)
	}
	return m.persist(ctx, d, StateDKIMCreated, d.StalwartID, "generated", d.DnsStatus, &zone)
}

// dkim_created → dns_pending: registros prontos; devolvidos ao cliente publicar (trava aqui).
func (m *Machine) stepOfferDNS(ctx context.Context, d db.Domain) (bool, error) {
	return m.persist(ctx, d, StateDNSPending, d.StalwartID, d.DkimStatus, "awaiting", d.DnsZone)
}

// dns_pending → dns_verified: worker faz polling até os registros baterem. Com relay, exige
// também que o SES confirme o DKIM da identidade (senão o SES recusaria o MAIL FROM depois).
func (m *Machine) stepVerifyDNS(ctx context.Context, d db.Domain) (bool, error) {
	ok, err := m.dns.Verify(ctx, d.Dominio, strval(d.DnsZone))
	if err != nil {
		return false, fmt.Errorf("verificar DNS: %w", err)
	}
	if !ok {
		return false, nil // cliente ainda não publicou
	}
	if m.relay != nil {
		verified, rerr := m.relay.IdentityVerified(ctx, d.Dominio)
		if rerr != nil {
			return false, fmt.Errorf("verificar identidade no relay: %w", rerr)
		}
		if !verified {
			return false, nil // SES ainda a validar os CNAMEs DKIM; tenta de novo depois
		}
	}
	return m.persist(ctx, d, StateDNSVerified, d.StalwartID, d.DkimStatus, "verified", d.DnsZone)
}

// dns_verified → active: cria a mailbox inicial (postmaster) e marca ativo.
// Idempotente: CreateAccount falhando por já existir não bloqueia a ativação.
func (m *Machine) stepActivate(ctx context.Context, d db.Domain) (bool, error) {
	email := "postmaster@" + d.Dominio
	// Idempotência: se a mailbox já foi persistida (retomada), pula a criação e só ativa.
	if _, err := m.store.GetMailUserByEmail(ctx, email); err != nil {
		acctID, err := m.sw.CreateAccount(ctx, stalwart.AccountInput{
			Login:       "postmaster",
			DomainID:    strval(d.StalwartID),
			DisplayName: "Postmaster",
			Password:    genInitialSecret(d.ID.String()),
			QuotaBytes:  1 << 30,
		})
		if err != nil {
			return false, fmt.Errorf("criar mailbox inicial: %w", err)
		}
		if _, err := m.store.CreateMailUser(ctx, db.CreateMailUserParams{
			TenantID: d.TenantID, Email: email, Name: "Postmaster", Role: "admin",
			StalwartAccountID: &acctID, QuotaBytes: 1 << 30,
		}); err != nil {
			return false, fmt.Errorf("persistir mailbox: %w", err)
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
		return false, fmt.Errorf("persistir estado %s: %w", state, err)
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
