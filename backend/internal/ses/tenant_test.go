package ses

import "testing"

// O nome do tenant tem de ser derivável do domínio à vista, sem consultar a nossa base de dados:
// quem abre o painel da AWS às três da manhã, porque um tenant foi pausado, tem de saber logo de
// que cliente se trata.
func TestTenantName(t *testing.T) {
	casos := map[string]string{
		"cliente.com.br": "cliente-com-br",
		"ACME.COM":       "acme-com",
		"  beskos.com  ": "beskos-com",
		"sub.dominio.pt": "sub-dominio-pt",
	}
	for entrada, esperado := range casos {
		if got := TenantName(entrada); got != esperado {
			t.Errorf("TenantName(%q) = %q, esperava %q", entrada, got, esperado)
		}
	}
}

// O SES limita o nome a 64 caracteres. Um domínio longo não pode fazer o provisionamento falhar.
func TestTenantNameTruncaEmSessentaEQuatro(t *testing.T) {
	longo := ""
	for len(longo) < 90 {
		longo += "abcdefgh."
	}
	n := TenantName(longo)
	if len(n) != 64 {
		t.Fatalf("nome com %d caracteres, o SES aceita no máximo 64", len(n))
	}
}

// Nenhum caractere fora do que o SES aceita: letras, números, hífen e underscore.
func TestTenantNameSoTemCaracteresAceites(t *testing.T) {
	for _, r := range TenantName("cliente.com.br") {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			t.Fatalf("caractere %q não é aceite pelo SES", r)
		}
	}
}
