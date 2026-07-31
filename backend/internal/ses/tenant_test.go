package ses

import "testing"

// O nome do tenant tem de ser derivável do domínio à vista, sem consultar a nossa base de dados:
// quem abre o painel da AWS às três da manhã, porque um tenant foi pausado, tem de saber logo de
// que cliente se trata.
func TestTenantName(t *testing.T) {
	cases := map[string]string{
		"cliente.com.br": "cliente-com-br",
		"ACME.COM":       "acme-com",
		"  beskos.com  ": "beskos-com",
		"sub.dominio.pt": "sub-dominio-pt",
	}
	for entrada, esperado := range cases {
		if got := TenantName(entrada); got != esperado {
			t.Errorf("TenantName(%q) = %q, want %q", entrada, got, esperado)
		}
	}
}

// O SES limita o nome a 64 caracteres. Um domínio longo não pode fazer o provisionamento falhar.
func TestTenantNameTruncatesAtSixtyFour(t *testing.T) {
	long := ""
	for len(long) < 90 {
		long += "abcdefgh."
	}
	n := TenantName(long)
	if len(n) != 64 {
		t.Fatalf("name has %d characters, SES accepts at most 64", len(n))
	}
}

// Nenhum caractere fora do que o SES aceita: letras, números, hífen e underscore.
func TestTenantNameOnlyHasAllowedChars(t *testing.T) {
	for _, r := range TenantName("cliente.com.br") {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			t.Fatalf("character %q is not accepted by SES", r)
		}
	}
}

// A SES responde "já existe" de duas maneiras, e o comando de retroação tem de aceitar as duas —
// senão falha em tudo o que já estava criado, que é o caso normal quando se corre a segunda vez.
func TestAlreadyExistsAcceptsBothForms(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"nome da exceção", 400, `{"__type":"AlreadyExistsException"}`, true},
		{"frase em inglês", 400, `{"message":"Tenant with name beskos-com already exists in account 1"}`, true},
		{"conflito", 409, ``, true},
		{"já associado", 400, `{"message":"Resources arn:... has already been associated with tenant x"}`, true},
		{"outro erro 400", 400, `{"message":"invalid tenant name"}`, false},
		{"erro do servidor", 500, `already exists`, false}, // 500 nunca é repetição: é avaria
	}
	for _, c := range cases {
		if got := alreadyExists(c.status, []byte(c.body)); got != c.want {
			t.Errorf("%s: alreadyExists = %v, want %v", c.name, got, c.want)
		}
	}
}
