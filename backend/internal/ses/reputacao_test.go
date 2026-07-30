package ses

import "testing"

// As taxas decidem se um cliente é suspenso. Um erro aqui suspende quem não devia, ou deixa passar
// quem devia — e nos dois casos o estrago é da conta inteira.
func TestTaxas(t *testing.T) {
	casos := []struct {
		nome      string
		r         Reputacao
		devolucao float64
		queixa    float64
	}{
		{"normal", Reputacao{Enviadas: 1000, Devolvidas: 20, Queixas: 1}, 0.02, 0.001},
		{"devoluções altas", Reputacao{Enviadas: 200, Devolvidas: 30, Queixas: 0}, 0.15, 0},
		// Sem envios não há taxa. Dividir por zero daria NaN, e NaN comparado com o limite é sempre
		// falso — um cliente que nunca enviou passaria despercebido ou seria suspenso, conforme o
		// lado da comparação. Zero é a resposta certa e explícita.
		{"nunca enviou", Reputacao{}, 0, 0},
	}
	for _, c := range casos {
		if got := c.r.TaxaDevolucao(); got != c.devolucao {
			t.Errorf("%s: devolução %v, esperava %v", c.nome, got, c.devolucao)
		}
		if got := c.r.TaxaQueixa(); got != c.queixa {
			t.Errorf("%s: queixa %v, esperava %v", c.nome, got, c.queixa)
		}
	}
}

// As devoluções permanentes e temporárias contam ambas: um endereço que devolve por caixa cheia
// conta para a reputação tal como um que não existe.
func TestDevolucoesSomamOsDoisTipos(t *testing.T) {
	r := Reputacao{Enviadas: 100, Devolvidas: 3 + 2}
	if got := r.TaxaDevolucao(); got != 0.05 {
		t.Fatalf("taxa %v, esperava 0.05", got)
	}
}
