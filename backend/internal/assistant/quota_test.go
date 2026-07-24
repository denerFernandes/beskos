package assistant

import (
	"testing"
	"time"
)

func TestQuotaRemainingAndRenewal(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	oldest := now.Add(-2 * time.Hour) // o uso mais antigo foi há 2h

	q := QuotaOf(200_000, 150_000, oldest, now)
	if q.Remaining != 50_000 {
		t.Errorf("restante = %d, quero 50000", q.Remaining)
	}
	// A janela desliza: o uso mais antigo sai dela 6h depois de ter acontecido, ou seja, daqui a 4h.
	want := oldest.Add(QuotaWindow)
	if !q.RenewsAt.Equal(want) {
		t.Errorf("renova em %v, quero %v", q.RenewsAt, want)
	}
}

// Esgotada é >=, não >: chegar ao teto é bater no teto.
func TestQuotaExceededIsInclusive(t *testing.T) {
	now := time.Now()
	if !QuotaOf(100, 100, now, now).Exceeded() {
		t.Error("gastar exatamente o teto devia contar como esgotado")
	}
	if QuotaOf(100, 99, now, now).Exceeded() {
		t.Error("abaixo do teto não está esgotado")
	}
	// Nunca fica negativo — um restante negativo confundiria o ecrã.
	if r := QuotaOf(100, 150, now, now).Remaining; r != 0 {
		t.Errorf("restante = %d, quero 0 (não negativo)", r)
	}
}

// Sem uso na janela, não há nada a renovar — e o campo fica vazio, não com uma data inventada.
func TestQuotaWithNoUsageHasNoRenewal(t *testing.T) {
	now := time.Now()
	q := QuotaOf(200_000, 0, time.Time{}, now)
	if !q.RenewsAt.IsZero() {
		t.Errorf("sem uso não devia haver renovação, tem %v", q.RenewsAt)
	}
	if q.Remaining != 200_000 {
		t.Errorf("restante = %d, quero o teto inteiro", q.Remaining)
	}
}

// O teto de passos por execução (§9.1) e a quota da janela (§9.2) são coisas distintas, que não se
// substituem. Este teste fixa a fronteira: a quota NÃO altera o teto de passos.
func TestQuotaIsSeparateFromStepCeiling(t *testing.T) {
	// O teto de passos é uma constante do motor, independente de qualquer quota.
	if DefaultMaxSteps != 12 {
		t.Errorf("o teto de passos mudou (%d) — é segurança e não devia depender da quota", DefaultMaxSteps)
	}
	// A janela é comercial e mede-se em horas; o teto de passos mede-se em passos. Não há conversão
	// entre eles, e é isso que os torna dois limites e não um.
	if QuotaWindow != 6*time.Hour {
		t.Errorf("a janela comercial mudou: %v", QuotaWindow)
	}
}
