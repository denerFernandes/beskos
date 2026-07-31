package ses

import "testing"

// As taxas decidem se um cliente é suspenso. Um erro aqui suspende quem não devia, ou deixa passar
// quem devia — e nos dois casos o estrago é da conta inteira.
func TestRates(t *testing.T) {
	cases := []struct {
		name          string
		r             Reputation
		bounceRate    float64
		complaintRate float64
	}{
		{"normal", Reputation{Sent: 1000, Bounced: 20, Complaints: 1}, 0.02, 0.001},
		{"devoluções altas", Reputation{Sent: 200, Bounced: 30, Complaints: 0}, 0.15, 0},
		// Sem envios não há taxa. Dividir por zero daria NaN, e NaN comparado com o limite é sempre
		// falso — um cliente que nunca enviou passaria despercebido ou seria suspenso, conforme o
		// lado da comparação. Zero é a resposta certa e explícita.
		{"nunca enviou", Reputation{}, 0, 0},
	}
	for _, c := range cases {
		if got := c.r.BounceRate(); got != c.bounceRate {
			t.Errorf("%s: bounce %v, want %v", c.name, got, c.bounceRate)
		}
		if got := c.r.ComplaintRate(); got != c.complaintRate {
			t.Errorf("%s: complaint %v, want %v", c.name, got, c.complaintRate)
		}
	}
}

// As devoluções permanentes e temporárias contam ambas: um endereço que devolve por caixa cheia
// conta para a reputação tal como um que não existe.
func TestBouncesSumBothTypes(t *testing.T) {
	r := Reputation{Sent: 100, Bounced: 3 + 2}
	if got := r.BounceRate(); got != 0.05 {
		t.Fatalf("rate %v, want 0.05", got)
	}
}
