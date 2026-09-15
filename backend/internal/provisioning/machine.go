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
	"log"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/ses"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
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
	// autoDKIM=false (modo relay) cria o domínio com DKIM Manual — o Stalwart não assina.
	CreateDomain(ctx context.Context, name, description string, autoDKIM bool) (string, error)
	GetDomainDNSZone(ctx context.Context, domainID string) (string, error)
	CreateAccount(ctx context.Context, in stalwart.AccountInput) (string, error)
}

// DNSVerifier confirma se os registros do domínio já foram publicados pelo cliente.
type DNSVerifier interface {
	Verify(ctx context.Context, domain, zone string) (bool, error)
}

// EmailRelay é o relay de saída (Amazon SES) em modo WHITE-LABEL (BYODKIM): regista o domínio como
// identidade com uma chave DKIM NOSSA e devolve o seletor + a chave pública para o cliente publicar
// — o DNS do cliente nunca revela o Amazon. nil = sem relay (dev/self-hosted puro).
type EmailRelay interface {
	CreateIdentity(ctx context.Context, domain string) (selector, publicKey string, err error)
	IdentityVerified(ctx context.Context, domain string) (bool, error)
	// SetMailFrom define o custom MAIL FROM (Return-Path bounce.<domínio>) para o "enviado por"
	// mostrar o domínio do cliente, não o amazonses.
	SetMailFrom(ctx context.Context, domain string) error
	// MailFromSubdomain/MailFromMX descrevem o registo que o cliente publica para o MAIL FROM.
	MailFromSubdomain(domain string) string
	MailFromMX() string
	// CreateTenant/AssociateIdentity isolam a reputação deste domínio da dos outros clientes. Sem
	// isto, devoluções de um cliente degradam a reputação da CONTA e a AWS pode suspender o correio
	// de toda a gente; com isto, pausa só o tenant afetado.
	CreateTenant(ctx context.Context, name string) error
	AssociateIdentity(ctx context.Context, tenant, domain string) error
	// O conjunto de configuração é o que funciona HOJE: colado à identidade, a AWS aplica-o a todas
	// as mensagens do domínio sem que nada no caminho do correio mude.
	CreateConfigurationSet(ctx context.Context, name string) error
	SetIdentityConfigurationSet(ctx context.Context, domain, name string) error
}

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
	brand string     // domínio da marca (ex.: beskos.com) para _spf.<brand> e rua do DMARC
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

// WithRelay liga o relay de saída (SES white-label) ao provisionar domínios. brand é o domínio da
// plataforma usado nos registos que escondem o SES (`_spf.<brand>`, rua do DMARC).
func (m *Machine) WithRelay(r EmailRelay, brand string) *Machine {
	m.relay = r
	m.brand = brand
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
		return false, fmt.Errorf("unknown state: %q", d.ProvisionState)
	}
}

// pending → domain_created: cria o domínio no Stalwart. Idempotente: se já há stalwart_id
// (retomada após crash entre a chamada e o commit), pula a criação.
func (m *Machine) stepCreateDomain(ctx context.Context, d db.Domain) (bool, error) {
	stalwartID := strval(d.StalwartID)
	if stalwartID == "" {
		// Com relay (SES), o Stalwart não gere DKIM (autoDKIM=false): quem assina é o SES via BYODKIM.
		autoDKIM := m.relay == nil
		id, err := m.sw.CreateDomain(ctx, d.Domain, "tenant_id="+d.TenantID.String(), autoDKIM)
		if err != nil {
			return false, fmt.Errorf("create domain: %w", err)
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
		return false, fmt.Errorf("read zone: %w", err)
	}
	if m.relay != nil {
		// White-label: o SES assina via BYODKIM (chave nossa). Registamos a identidade e trocamos o
		// DKIM do Stalwart pelo nosso — o cliente publica só a nossa chave pública, sem ver o Amazon.
		selector, publicKey, err := m.relay.CreateIdentity(ctx, d.Domain)
		if err != nil {
			return false, fmt.Errorf("register identity with the relay: %w", err) // retry no próximo tick
		}
		// Isolamento de reputação: um tenant SES por domínio, com a identidade associada. Falhar
		// aqui NÃO bloqueia o provisionamento — o correio sai à mesma, apenas sem o isolamento —,
		// mas fica registado, porque um cliente sem tenant é um cliente que pode derrubar os
		// outros.
		name := ses.TenantName(d.Domain)

		// O conjunto de configuração é o que produz efeito já: colado à identidade, dá métricas de
		// reputação por cliente e permite suspender só este domínio.
		if err := m.relay.CreateConfigurationSet(ctx, name); err != nil {
			log.Printf("provisioning: configuration set for %s not created (metrics stay aggregated): %v", d.Domain, err)
		} else if err := m.relay.SetIdentityConfigurationSet(ctx, d.Domain, name); err != nil {
			log.Printf("provisioning: configuration set %s did not become the default on %s: %v", name, d.Domain, err)
		}

		// O tenant fica criado à espera do dia em que as mensagens levem o cabeçalho X-SES-TENANT —
		// é o que traz a pausa automática da AWS por reputação.
		if err := m.relay.CreateTenant(ctx, name); err != nil {
			log.Printf("provisioning: SES tenant for %s not created: %v", d.Domain, err)
		} else if err := m.relay.AssociateIdentity(ctx, name, d.Domain); err != nil {
			log.Printf("provisioning: identity for %s not associated with tenant %s: %v", d.Domain, name, err)
		}

		// Custom MAIL FROM (bounce.<domínio>): sem ele o "enviado por" do destinatário mostra o
		// amazonses. Fallback USE_DEFAULT_VALUE — não bloqueia o envio enquanto o MX não propaga.
		if err := m.relay.SetMailFrom(ctx, d.Domain); err != nil {
			return false, fmt.Errorf("configurar MAIL FROM no relay: %w", err) // retry no próximo tick
		}
		zone = StripStalwartDKIM(zone)
		zone = AugmentZoneForSES(zone, d.Domain, selector, publicKey, m.brand,
			m.relay.MailFromSubdomain(d.Domain), m.relay.MailFromMX())
		zone = PublicHosts(zone, m.mailHost, m.mtaStsHost)
		return m.persist(ctx, d, StateDKIMCreated, d.StalwartID, "ses", d.DnsStatus, &zone)
	}
	// Sem relay: espera o DKIM do Stalwart (não aparece na hora — DIVERGENCES §D4).
	if !hasDKIM(zone) {
		return false, nil // ainda não gerado; tenta de novo depois
	}
	zone = PublicHosts(zone, m.mailHost, m.mtaStsHost)
	return m.persist(ctx, d, StateDKIMCreated, d.StalwartID, "generated", d.DnsStatus, &zone)
}

// dkim_created → dns_pending: registros prontos; devolvidos ao cliente publicar (trava aqui).
func (m *Machine) stepOfferDNS(ctx context.Context, d db.Domain) (bool, error) {
	return m.persist(ctx, d, StateDNSPending, d.StalwartID, d.DkimStatus, "awaiting", d.DnsZone)
}

// dns_pending → dns_verified: worker faz polling até os registros baterem. Com relay, exige
// também que o SES confirme o DKIM da identidade (senão o SES recusaria o MAIL FROM depois).
func (m *Machine) stepVerifyDNS(ctx context.Context, d db.Domain) (bool, error) {
	ok, err := m.dns.Verify(ctx, d.Domain, strval(d.DnsZone))
	if err != nil {
		return false, fmt.Errorf("verificar DNS: %w", err)
	}
	if !ok {
		waiting(d.Domain, "dns", "MX and "+dkimHost(strval(d.DnsZone), d.Domain))
		return false, nil // cliente ainda não publicou
	}
	if m.relay != nil {
		verified, rerr := m.relay.IdentityVerified(ctx, d.Domain)
		if rerr != nil {
			return false, fmt.Errorf("verify identity with the relay: %w", rerr)
		}
		if !verified {
			waiting(d.Domain, "relay", "identity verification")
			return false, nil // SES ainda a validar os CNAMEs DKIM; tenta de novo depois
		}
	}
	waitingDone(d.Domain)
	// The customer has published mta-sts.<domain> by now, so the policy host can be reached under
	// that name and issue its certificate. Doing it here means the first person to look -- usually
	// the customer, checking their own work -- finds it already serving. See mtasts_warm.go.
	m.warmMTASTS(ctx, d.Domain)
	return m.persist(ctx, d, StateDNSVerified, d.StalwartID, d.DkimStatus, "verified", d.DnsZone)
}

// dns_verified → active: cria a mailbox inicial (postmaster) e marca ativo.
// Idempotente: CreateAccount falhando por já existir não bloqueia a ativação.
func (m *Machine) stepActivate(ctx context.Context, d db.Domain) (bool, error) {
	email := "postmaster@" + d.Domain
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
