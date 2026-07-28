package billing

import "testing"

func TestQuoteFor(t *testing.T) {
	cases := []struct {
		name          string
		kind, country string
		taxID         string
		wantRate      int
		wantReverse   bool
		wantTotal     int // subtotal 10000 (100,00 × 1)
	}{
		{"empresa polaca paga IVA polaco", "company", "PL", "PL1234567890", PolishVATRate, false, 12300},
		{"particular polaco paga IVA polaco", "individual", "PL", "", PolishVATRate, false, 12300},
		{"empresa da UE com NIF autoliquida", "company", "PT", "PT123456789", 0, true, 10000},
		{"empresa da UE sem NIF paga IVA polaco", "company", "PT", "", PolishVATRate, false, 12300},
		{"empresa da UE com NIF inválido paga IVA", "company", "PT", "PT12", PolishVATRate, false, 12300},
		{"particular da UE paga IVA polaco", "individual", "DE", "", PolishVATRate, false, 12300},
		{"fora da UE não leva IVA", "company", "BR", "", 0, false, 10000},
		{"particular fora da UE não leva IVA", "individual", "US", "", 0, false, 10000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := QuoteFor("EUR", 10000, 1, c.kind, c.country, c.taxID)
			if q.VATRateBps != c.wantRate {
				t.Errorf("taxa = %d, quero %d", q.VATRateBps, c.wantRate)
			}
			if q.ReverseCharge != c.wantReverse {
				t.Errorf("autoliquidação = %v, quero %v", q.ReverseCharge, c.wantReverse)
			}
			if q.TotalCents != c.wantTotal {
				t.Errorf("total = %d, quero %d", q.TotalCents, c.wantTotal)
			}
		})
	}
}

func TestQuoteForSeatsAndRounding(t *testing.T) {
	// 3 × 7,77 = 23,31 → IVA 23% = 5,3613 → arredonda a 5,36; total 28,67.
	q := QuoteFor("EUR", 777, 3, "individual", "PL", "")
	if q.SubtotalCents != 2331 || q.VATCents != 536 || q.TotalCents != 2867 {
		t.Fatalf("subtotal/IVA/total = %d/%d/%d, quero 2331/536/2867", q.SubtotalCents, q.VATCents, q.TotalCents)
	}
	if got := QuoteFor("EUR", 1000, 0, "individual", "PL", "").Seats; got != 1 {
		t.Errorf("seats mínimo = %d, quero 1", got)
	}
}

func TestNormalizeVATID(t *testing.T) {
	cases := map[string]struct{ in, country, want string }{
		"tira espaços e pontos": {"PL 123-456.7890", "PL", "PL1234567890"},
		"acrescenta o país":     {"1234567890", "pl", "PL1234567890"},
		"grécia declara-se EL":  {"123456789", "GR", "EL123456789"},
		"já tem prefixo":        {"de123456789", "DE", "DE123456789"},
		"vazio fica vazio":      {"", "PT", ""},
	}
	for name, c := range cases {
		if got := NormalizeVATID(c.in, c.country); got != c.want {
			t.Errorf("%s: %q → %q, quero %q", name, c.in, got, c.want)
		}
	}
}

func TestValidVATID(t *testing.T) {
	ok := map[string]string{"PL": "PL1234567890", "PT": "PT123456789", "DE": "DE123456789", "NL": "NL123456789B01", "GR": "EL123456789"}
	for cc, id := range ok {
		if !ValidVATID(id, cc) {
			t.Errorf("%s: %q devia ser válido", cc, id)
		}
	}
	bad := map[string]string{"PL": "PL123", "PT": "PT12345678", "DE": "DEABCDEFGHI", "BR": "12345678901"}
	for cc, id := range bad {
		if ValidVATID(id, cc) {
			t.Errorf("%s: %q não devia ser válido", cc, id)
		}
	}
}

func TestIsEU(t *testing.T) {
	for _, cc := range []string{"PL", "pt", " DE "} {
		if !IsEU(cc) {
			t.Errorf("%q devia ser da UE", cc)
		}
	}
	for _, cc := range []string{"BR", "US", "GB", "CH", ""} {
		if IsEU(cc) {
			t.Errorf("%q não devia ser da UE", cc)
		}
	}
}
