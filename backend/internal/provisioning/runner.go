package provisioning

import (
	"context"
	"log"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Lister é o subconjunto do db.Queries que o Runner usa para achar trabalho pendente.
type Lister interface {
	ListDomainsInState(ctx context.Context, arg db.ListDomainsInStateParams) ([]db.Domain, error)
}

// Runner varre os domínios em estados não-terminais e avança cada um uma transição por tick.
// Erros em um domínio não param os demais (isolamento de falha). O worker chama Tick num loop.
type Runner struct {
	machine *Machine
	lister  Lister
	batch   int32
}

func NewRunner(m *Machine, l Lister) *Runner {
	return &Runner{machine: m, lister: l, batch: 50}
}

// Tick processa uma rodada. Retorna quantas transições ocorreram (útil para logs/testes).
func (r *Runner) Tick(ctx context.Context) int {
	advanced := 0
	for _, state := range ActiveStates {
		domains, err := r.lister.ListDomainsInState(ctx, db.ListDomainsInStateParams{
			ProvisionState: state, Limit: r.batch,
		})
		if err != nil {
			log.Printf("provisioning: list state %s: %v", state, err)
			continue
		}
		for _, d := range domains {
			changed, err := r.machine.AdvanceOnce(ctx, d)
			if err != nil {
				// Erro (provavelmente transiente): loga e tenta de novo no próximo tick.
				log.Printf("provisioning: domain %s (%s): %v", d.Domain, d.ProvisionState, err)
				continue
			}
			if changed {
				advanced++
				log.Printf("provisioning: domain %s advanced from %s", d.Domain, d.ProvisionState)
			}
		}
	}
	return advanced
}
