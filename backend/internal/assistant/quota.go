package assistant

import "time"

// QuotaWindow is the sliding window for the commercial token quota. Six hours, self-renewing:
// spend older than six hours no longer counts.
//
// Not to be confused with the per-run step cap: that one is a safety limit, always applied, and
// built into the engine itself. This one is commercial, and is what will differentiate plans.
const QuotaWindow = 6 * time.Hour

// DefaultQuotaTokens é o teto de tokens por utilizador na janela, quando nada o configura. Um valor
// padrão generoso e igual para todos (decisão 6): sem uso real, calibrá-lo por plano seria inventar
// um número (§12.2). Fica configurável por ambiente para se ajustar sem recompilar.
const DefaultQuotaTokens = 200_000

// QuotaStatus é o que a quota deixa saber: quanto se gastou, quanto resta e quando renova.
type QuotaStatus struct {
	Limit     int `json:"limit"`
	Used      int `json:"used"`
	Remaining int `json:"remaining"`
	// RenewsAt é quando o uso mais antigo da janela sai dela — o momento em que volta a haver folga.
	// Zero quando não há uso a expirar (nada gasto na janela).
	RenewsAt time.Time `json:"renews_at,omitempty"`
}

// QuotaOf calcula o estado da quota. used é a soma da janela; oldest é o instante do uso mais antigo
// ainda dentro dela (para saber quando renova); now é o presente, injetado para o cálculo ser
// testável sem depender do relógio.
func QuotaOf(limit, used int, oldest, now time.Time) QuotaStatus {
	st := QuotaStatus{Limit: limit, Used: used, Remaining: limit - used}
	if st.Remaining < 0 {
		st.Remaining = 0
	}
	if !oldest.IsZero() {
		// A janela desliza: o uso mais antigo expira uma janela depois de ter acontecido.
		st.RenewsAt = oldest.Add(QuotaWindow)
	}
	return st
}

// Exceeded diz se a quota foi ultrapassada. É >=, não >: chegar ao teto é bater no teto.
func (s QuotaStatus) Exceeded() bool { return s.Used >= s.Limit }
