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

// A SES responde "já existe" de duas maneiras, e o comando de retroação tem de aceitar as duas —
// senão falha em tudo o que já estava criado, que é o caso normal quando se corre a segunda vez.
func TestJaExisteAceitaAsDuasFormas(t *testing.T) {
	casos := []struct {
		nome   string
		status int
		corpo  string
		quer   bool
	}{
		{"nome da exceção", 400, `{"__type":"AlreadyExistsException"}`, true},
		{"frase em inglês", 400, `{"message":"Tenant with name beskos-com already exists in account 1"}`, true},
		{"conflito", 409, ``, true},
		{"já associado", 400, `{"message":"Resources arn:... has already been associated with tenant x"}`, true},
		{"outro erro 400", 400, `{"message":"invalid tenant name"}`, false},
		{"erro do servidor", 500, `already exists`, false}, // 500 nunca é repetição: é avaria
	}
	for _, c := range casos {
		if got := jaExiste(c.status, []byte(c.corpo)); got != c.quer {
			t.Errorf("%s: jaExiste = %v, esperava %v", c.nome, got, c.quer)
		}
	}
}
