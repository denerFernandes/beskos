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
		nome  string
		rec   DNSRecord
		found []string
		want  bool
	}{
		{
			nome:  "MX ignora a prioridade e o ponto final",
			rec:   DNSRecord{Type: "MX", Value: "10 mail.exemplo.com.", Purpose: "mx"},
			found: []string{"mail.exemplo.com"},
			want:  true,
		},
		{
			nome:  "MX de outro fornecedor não passa",
			rec:   DNSRecord{Type: "MX", Value: "10 mail.exemplo.com.", Purpose: "mx"},
			found: []string{"aspmx.l.google.com"},
			want:  false,
		},
		{
			nome:  "SPF basta conter o nosso include",
			rec:   DNSRecord{Type: "TXT", Value: "v=spf1 include:_spf.beskos.com ~all", Purpose: "spf"},
			found: []string{"v=spf1 ip4:1.2.3.4 include:_spf.beskos.com include:outro.com -all"},
			want:  true,
		},
		{
			nome:  "SPF sem o nosso include falha",
			rec:   DNSRecord{Type: "TXT", Value: "v=spf1 include:_spf.beskos.com ~all", Purpose: "spf"},
			found: []string{"v=spf1 include:_spf.google.com ~all"},
			want:  false,
		},
		{
			nome:  "DKIM compara a chave, não a ordem das tags",
			rec:   DNSRecord{Type: "TXT", Value: "v=DKIM1; k=rsa; p=MIIBIjANBg", Purpose: "dkim"},
			found: []string{"k=rsa; p=MIIBIjANBg; v=DKIM1"},
			want:  true,
		},
		{
			nome:  "DKIM com outra chave falha (é o caso perigoso)",
			rec:   DNSRecord{Type: "TXT", Value: "v=DKIM1; k=rsa; p=MIIBIjANBg", Purpose: "dkim"},
			found: []string{"v=DKIM1; k=rsa; p=OUTRACHAVE"},
			want:  false,
		},
		{
			nome:  "DMARC aceita qualquer política publicada",
			rec:   DNSRecord{Type: "TXT", Value: "v=DMARC1; p=quarantine; rua=mailto:d@b.com", Purpose: "dmarc"},
			found: []string{"v=DMARC1; p=none"},
			want:  true,
		},
		{
			nome:  "SRV compara o alvo",
			rec:   DNSRecord{Type: "SRV", Value: "0 1 993 mail.exemplo.com.", Purpose: "other"},
			found: []string{"mail.exemplo.com"},
			want:  true,
		},
	}
	for _, c := range cases {
		if got := matches(c.rec, c.found); got != c.want {
			t.Errorf("%s: matches=%v, quer %v", c.nome, got, c.want)
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
