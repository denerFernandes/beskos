package provisioning

import (
	"strings"
	"testing"
)

// O valor que o cliente publica quase nunca é igual ao nosso carácter a carácter. Estes testes
// fixam o que conta como "está publicado" em cada tipo — é aqui que um falso negativo mandaria
// alguém procurar um problema que não existe, e um falso positivo diria "está tudo bem" com o
// correio a cair.
func TestMatchesPorTipo(t *testing.T) {
	cases := []struct {
		name  string
		rec   DNSRecord
		found []string
		want  bool
	}{
		{
			name:  "MX ignora a prioridade e o ponto final",
			rec:   DNSRecord{Type: "MX", Value: "10 mail.exemplo.com.", Purpose: "mx"},
			found: []string{"mail.exemplo.com"},
			want:  true,
		},
		{
			name:  "MX de outro fornecedor não passa",
			rec:   DNSRecord{Type: "MX", Value: "10 mail.exemplo.com.", Purpose: "mx"},
			found: []string{"aspmx.l.google.com"},
			want:  false,
		},
		{
			name:  "SPF basta conter o nosso include",
			rec:   DNSRecord{Type: "TXT", Value: "v=spf1 include:_spf.beskos.com ~all", Purpose: "spf"},
			found: []string{"v=spf1 ip4:1.2.3.4 include:_spf.beskos.com include:outro.com -all"},
			want:  true,
		},
		{
			name:  "SPF sem o nosso include falha",
			rec:   DNSRecord{Type: "TXT", Value: "v=spf1 include:_spf.beskos.com ~all", Purpose: "spf"},
			found: []string{"v=spf1 include:_spf.google.com ~all"},
			want:  false,
		},
		{
			name:  "DKIM compara a chave, não a ordem das tags",
			rec:   DNSRecord{Type: "TXT", Value: "v=DKIM1; k=rsa; p=MIIBIjANBg", Purpose: "dkim"},
			found: []string{"k=rsa; p=MIIBIjANBg; v=DKIM1"},
			want:  true,
		},
		{
			name:  "DKIM com outra chave falha (é o caso perigoso)",
			rec:   DNSRecord{Type: "TXT", Value: "v=DKIM1; k=rsa; p=MIIBIjANBg", Purpose: "dkim"},
			found: []string{"v=DKIM1; k=rsa; p=OUTRACHAVE"},
			want:  false,
		},
		{
			name:  "DMARC aceita qualquer política publicada",
			rec:   DNSRecord{Type: "TXT", Value: "v=DMARC1; p=quarantine; rua=mailto:d@b.com", Purpose: "dmarc"},
			found: []string{"v=DMARC1; p=none"},
			want:  true,
		},
		{
			name:  "SRV compara o alvo",
			rec:   DNSRecord{Type: "SRV", Value: "0 1 993 mail.exemplo.com.", Purpose: "other"},
			found: []string{"mail.exemplo.com"},
			want:  true,
		},
	}
	for _, c := range cases {
		if got := matches(c.rec, c.found); got != c.want {
			t.Errorf("%s: matches=%v, quer %v", c.name, got, c.want)
		}
	}
}

func TestRequiredPurpose(t *testing.T) {
	for _, p := range []string{"mx", "spf", "dkim"} {
		if !requiredPurpose(p) {
			t.Errorf("%s devia ser obrigatório", p)
		}
	}
	// DMARC e o resto são recomendação: avisam, não bloqueiam.
	for _, p := range []string{"dmarc", "other", ""} {
		if requiredPurpose(p) {
			t.Errorf("%s não devia ser obrigatório", p)
		}
	}
}

func TestZoneFileMantemOsRegistos(t *testing.T) {
	zone := "exemplo.com. IN MX 10 mail.exemplo.com.\n\n_dmarc.exemplo.com. IN TXT \"v=DMARC1; p=none\"\n"
	out := ZoneFile("exemplo.com", zone)
	for _, want := range []string{"IN MX 10 mail.exemplo.com.", "_dmarc.exemplo.com."} {
		if !strings.Contains(out, want) {
			t.Errorf("a zona exportada perdeu %q", want)
		}
	}
	if out[0] != ';' {
		t.Error("o cabeçalho tem de ser comentário, senão o import do fornecedor rejeita")
	}
}

// A classificação decide o que a página apresenta como obrigatório e o que fica em "avançado".
// Um engano aqui pede ao cliente registos que ele não precisa — ou esconde um de que precisa.
func TestClassifyEObrigatoriedade(t *testing.T) {
	cases := []struct {
		rec      DNSRecord
		purpose  string
		required bool
	}{
		{DNSRecord{Type: "MX", Host: "e.com", Value: "10 mail.e.com."}, "mx", true},
		{DNSRecord{Type: "TXT", Host: "e.com", Value: "v=spf1 include:_spf.b.com ~all"}, "spf", true},
		{DNSRecord{Type: "TXT", Host: "sel._domainkey.e.com", Value: "v=DKIM1; p=x"}, "dkim", true},
		{DNSRecord{Type: "TXT", Host: "_dmarc.e.com", Value: "v=DMARC1; p=none"}, "dmarc", false},
		{DNSRecord{Type: "SRV", Host: "_imaps._tcp.e.com", Value: "0 1 993 mail.e.com."}, "srv", false},
		{DNSRecord{Type: "CNAME", Host: "mta-sts.e.com", Value: "mail.e.com."}, "mta_sts", false},
		{DNSRecord{Type: "TXT", Host: "_smtp._tls.e.com", Value: "v=TLSRPTv1; rua=mailto:p@e.com"}, "tls_rpt", false},
		{DNSRecord{Type: "CNAME", Host: "ua-auto-config.e.com", Value: "mail.e.com."}, "autoconfig", false},
	}
	for _, c := range cases {
		if got := classify(c.rec); got != c.purpose {
			t.Errorf("classify(%s %s) = %q, quer %q", c.rec.Type, c.rec.Host, got, c.purpose)
		}
		if got := requiredPurpose(c.purpose); got != c.required {
			t.Errorf("requiredPurpose(%s) = %v, quer %v", c.purpose, got, c.required)
		}
	}
}
