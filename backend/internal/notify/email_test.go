package notify

import "testing"

// O remetente tem duas formas: o cabeçalho From: leva o nome ("Beskos <noreply@…>"), o envelope
// SMTP só aceita o endereço. Confundi-los deu `501 5.1.8 Bad sender's system address` em produção.
func TestFromAddressIsBare(t *testing.T) {
	cases := map[string]string{
		"Beskos <noreply@beskos.com>": "noreply@beskos.com",
		"noreply@beskos.com":          "noreply@beskos.com",
		"  <a@b.pt>":                  "a@b.pt",
	}
	for from, want := range cases {
		e := NewEmail("mail.example.com", "465", from, "u", "p", false)
		if e == nil {
			t.Fatalf("NewEmail devolveu nil para %q", from)
		}
		if e.fromAddr != want {
			t.Errorf("from %q → envelope %q, quero %q", from, e.fromAddr, want)
		}
		if e.from != from {
			t.Errorf("o cabeçalho From: tem de ficar intacto: %q", e.from)
		}
	}
}
