package provisioning

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"mailin/backend/internal/db"
	"mailin/backend/internal/stalwart"
)

// --- Fakes -------------------------------------------------------------------

type fakeMailserver struct {
	createDomainCalls int
	createAcctCalls   int
	zone              string
	failCreateAccount bool
}

func (f *fakeMailserver) CreateDomain(context.Context, string, string) (string, error) {
	f.createDomainCalls++
	return "sw-dom-1", nil
}
func (f *fakeMailserver) GetDomainDNSZone(context.Context, string) (string, error) {
	return f.zone, nil
}
func (f *fakeMailserver) CreateAccount(context.Context, stalwart.AccountInput) (string, error) {
	f.createAcctCalls++
	if f.failCreateAccount {
		return "", errors.New("boom")
	}
	return "sw-acct-1", nil
}

type fakeStore struct {
	domain    db.Domain
	mailUsers map[string]db.MailUser
	updates   int
}

func (s *fakeStore) UpdateDomainProvision(_ context.Context, arg db.UpdateDomainProvisionParams) (db.Domain, error) {
	s.updates++
	s.domain.ProvisionState = arg.ProvisionState
	s.domain.StalwartID = arg.StalwartID
	s.domain.DkimStatus = arg.DkimStatus
	s.domain.DnsStatus = arg.DnsStatus
	s.domain.DnsZone = arg.DnsZone
	return s.domain, nil
}
func (s *fakeStore) CreateMailUser(_ context.Context, arg db.CreateMailUserParams) (db.MailUser, error) {
	u := db.MailUser{Email: arg.Email, StalwartAccountID: arg.StalwartAccountID}
	if s.mailUsers == nil {
		s.mailUsers = map[string]db.MailUser{}
	}
	s.mailUsers[arg.Email] = u
	return u, nil
}
func (s *fakeStore) GetMailUserByEmail(_ context.Context, email string) (db.MailUser, error) {
	if u, ok := s.mailUsers[email]; ok {
		return u, nil
	}
	return db.MailUser{}, errors.New("not found")
}

func newDomain(state string) db.Domain {
	return db.Domain{ID: uuid.New(), TenantID: uuid.New(), Dominio: "cliente.com.br", ProvisionState: state}
}

// runToActive roda a máquina até um estado terminal ou até parar de progredir.
func runToActive(t *testing.T, m *Machine, store *fakeStore) db.Domain {
	t.Helper()
	for i := 0; i < 20; i++ {
		changed, err := m.AdvanceOnce(context.Background(), store.domain)
		if err != nil {
			t.Fatalf("AdvanceOnce erro no estado %s: %v", store.domain.ProvisionState, err)
		}
		if !changed {
			break
		}
	}
	return store.domain
}

// --- Testes ------------------------------------------------------------------

func TestHappyPath(t *testing.T) {
	sw := &fakeMailserver{zone: "sel._domainkey.cliente.com.br. IN TXT \"v=DKIM1; k=ed25519; p=abc\""}
	store := &fakeStore{domain: newDomain(StatePending)}
	m := NewMachine(sw, AlwaysVerified{}, store)

	final := runToActive(t, m, store)
	if final.ProvisionState != StateActive {
		t.Fatalf("esperado active, veio %s", final.ProvisionState)
	}
	if sw.createDomainCalls != 1 {
		t.Errorf("CreateDomain deveria ser chamado 1x, veio %d", sw.createDomainCalls)
	}
	if _, ok := store.mailUsers["postmaster@cliente.com.br"]; !ok {
		t.Errorf("mailbox postmaster não foi criada")
	}
}

func TestWaitsForDKIM(t *testing.T) {
	// Zona sem DKIM: não deve passar de domain_created.
	sw := &fakeMailserver{zone: "cliente.com.br. IN MX 10 mail.example.org."}
	store := &fakeStore{domain: newDomain(StatePending)}
	m := NewMachine(sw, AlwaysVerified{}, store)

	runToActive(t, m, store)
	if store.domain.ProvisionState != StateDomainCreated {
		t.Fatalf("sem DKIM deveria travar em domain_created, veio %s", store.domain.ProvisionState)
	}
}

type notYetDNS struct{ calls int }

func (n *notYetDNS) Verify(context.Context, string, string) (bool, error) {
	n.calls++
	return n.calls >= 3, nil // publica só na 3ª checagem
}

func TestWaitsForDNSThenVerifies(t *testing.T) {
	sw := &fakeMailserver{zone: "sel._domainkey.cliente.com.br. IN TXT \"v=DKIM1; p=x\""}
	store := &fakeStore{domain: newDomain(StateDNSPending)}
	dns := &notYetDNS{}
	m := NewMachine(sw, dns, store)

	// Primeiras chamadas não avançam (DNS não publicado).
	for i := 0; i < 2; i++ {
		changed, _ := m.AdvanceOnce(context.Background(), store.domain)
		if changed {
			t.Fatalf("não deveria avançar antes do DNS publicado")
		}
	}
	// Agora publica e conclui.
	final := runToActive(t, m, store)
	if final.ProvisionState != StateActive {
		t.Fatalf("esperado active após DNS, veio %s", final.ProvisionState)
	}
}

func TestIdempotentDomainCreate(t *testing.T) {
	// Retomada a partir de pending com stalwart_id já setado: não recria o domínio.
	sw := &fakeMailserver{zone: "sel._domainkey.cliente.com.br. TXT DKIM1"}
	existing := "sw-dom-existing"
	d := newDomain(StatePending)
	d.StalwartID = &existing
	store := &fakeStore{domain: d}
	m := NewMachine(sw, AlwaysVerified{}, store)

	if _, err := m.AdvanceOnce(context.Background(), store.domain); err != nil {
		t.Fatal(err)
	}
	if sw.createDomainCalls != 0 {
		t.Errorf("CreateDomain não deveria ser chamado com stalwart_id existente")
	}
}

func TestIdempotentActivate(t *testing.T) {
	// Retomada em dns_verified com a mailbox já existente: não recria conta no Stalwart.
	sw := &fakeMailserver{}
	store := &fakeStore{
		domain:    newDomain(StateDNSVerified),
		mailUsers: map[string]db.MailUser{"postmaster@cliente.com.br": {Email: "postmaster@cliente.com.br"}},
	}
	m := NewMachine(sw, AlwaysVerified{}, store)

	changed, err := m.AdvanceOnce(context.Background(), store.domain)
	if err != nil || !changed {
		t.Fatalf("deveria ativar, changed=%v err=%v", changed, err)
	}
	if sw.createAcctCalls != 0 {
		t.Errorf("CreateAccount não deveria ser chamado quando a mailbox já existe")
	}
	if store.domain.ProvisionState != StateActive {
		t.Errorf("esperado active, veio %s", store.domain.ProvisionState)
	}
}

// --- Relay SES ---------------------------------------------------------------

type fakeRelay struct {
	created  int
	verified bool
	mailFrom string
}

func (f *fakeRelay) CreateIdentity(context.Context, string) ([]string, error) {
	f.created++
	return []string{"tok1", "tok2", "tok3"}, nil
}
func (f *fakeRelay) SetupMailFrom(_ context.Context, _, mailFrom string) (string, error) {
	f.mailFrom = mailFrom
	return "feedback-smtp.eu-west-1.amazonses.com", nil
}
func (f *fakeRelay) IdentityVerified(context.Context, string) (bool, error) {
	return f.verified, nil
}

// Com relay: a zona ganha os registros do SES e o dns_verified só sai quando o SES confirma.
func TestRelaySES(t *testing.T) {
	sw := &fakeMailserver{zone: "cliente.com.br. IN TXT \"v=spf1 a:mail.x -all\"\nsel._domainkey.cliente.com.br. IN TXT \"v=DKIM1; p=abc\""}
	store := &fakeStore{domain: newDomain(StatePending)}
	relay := &fakeRelay{}
	m := NewMachine(sw, AlwaysVerified{}, store).WithRelay(relay)

	final := runToActive(t, m, store)
	// SES ainda não verificou → trava em dns_pending.
	if final.ProvisionState != StateDNSPending {
		t.Fatalf("esperado dns_pending enquanto o SES não verifica, veio %s", final.ProvisionState)
	}
	zone := strval(final.DnsZone)
	for _, want := range []string{"tok1._domainkey.cliente.com.br. IN CNAME tok1.dkim.amazonses.com.",
		"include:amazonses.com", "_dmarc.cliente.com.br.",
		"mailer.cliente.com.br. IN MX 10 feedback-smtp.eu-west-1.amazonses.com.",
		"mailer.cliente.com.br. IN TXT \"v=spf1 include:amazonses.com ~all\""} {
		if !containsStr(zone, want) {
			t.Errorf("zona sem %q:\n%s", want, zone)
		}
	}
	if relay.created != 1 {
		t.Errorf("CreateIdentity deveria ser 1x, veio %d", relay.created)
	}
	if relay.mailFrom != "mailer.cliente.com.br" {
		t.Errorf("mail-from = %q, esperado mailer.cliente.com.br", relay.mailFrom)
	}

	// SES confirma → segue até active.
	relay.verified = true
	final = runToActive(t, m, store)
	if final.ProvisionState != StateActive {
		t.Fatalf("esperado active após verificação do SES, veio %s", final.ProvisionState)
	}
}

func containsStr(s, sub string) bool { return len(s) >= len(sub) && strings.Contains(s, sub) }
