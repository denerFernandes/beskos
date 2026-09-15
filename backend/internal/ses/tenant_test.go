package ses

import "testing"

// The tenant name must be derivable from the domain on sight, without consulting our database:
// whoever opens the AWS console at three in the morning, because a tenant got paused, has to know
// right away which customer it is.
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

// SES limits the name to 64 characters. A long domain must not be able to make provisioning fail.
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

// No character outside what SES accepts: letters, digits, hyphen, and underscore.
func TestTenantNameOnlyHasAllowedChars(t *testing.T) {
	for _, r := range TenantName("cliente.com.br") {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			t.Fatalf("character %q is not accepted by SES", r)
		}
	}
}

// SES responds "already exists" in two ways, and the retry command has to accept both — otherwise
// it fails on everything that was already created, which is the normal case on a second run.
func TestAlreadyExistsAcceptsBothForms(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"exception name", 400, `{"__type":"AlreadyExistsException"}`, true},
		{"plain English sentence", 400, `{"message":"Tenant with name beskos-com already exists in account 1"}`, true}, // lang:ok
		{"conflict", 409, ``, true},
		{"already associated", 400, `{"message":"Resources arn:... has already been associated with tenant x"}`, true},
		{"other 400 error", 400, `{"message":"invalid tenant name"}`, false},
		{"server error", 500, `already exists`, false}, // 500 is never a repeat: it's a failure
	}
	for _, c := range cases {
		if got := alreadyExists(c.status, []byte(c.body)); got != c.want {
			t.Errorf("%s: alreadyExists = %v, want %v", c.name, got, c.want)
		}
	}
}
