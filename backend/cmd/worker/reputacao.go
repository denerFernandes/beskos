package main

import (
	"context"
	"log"
	"time"

	"mailin/backend/internal/db"
	"mailin/backend/internal/ses"
)

// Vigia de reputação: suspende o envio de um cliente antes que a AWS suspenda o de todos.
//
// O PROBLEMA. As identidades de todos os clientes vivem na mesma conta AWS. Um cliente com uma
// lista comprada faz subir as devoluções da CONTA, e a AWS estrangula ou suspende a conta inteira —
// o correio de toda a gente para por causa de um. Até aqui, perante isso, as opções eram não fazer
// nada ou suspender a conta nós mesmos.
//
// Com um conjunto de configuração por domínio (ver internal/ses), a SES mede cada cliente à parte e
// deixa desligar o envio de um sem tocar nos outros. Este vigia é o que liga as duas coisas.
//
// LIMIARES: os da própria AWS — 5% de devoluções e 0,1% de queixas são as marcas a partir das quais
// ela começa a olhar para a conta. Suspender antes disso é precisamente evitar que ela o faça.
//
// SUSPENDE MAS NÃO RETOMA. Reativar um cliente que estava a queimar reputação é uma decisão com
// consequências para todos os outros, e deve ser de uma pessoa que olhou para o caso. Um vigia que
// retoma sozinho acabaria a ligar e desligar o mesmo cliente em ciclo.
//
// ESTE VIGIA NÃO CHEGA, e é bom que se diga: ele é código nosso, portanto pode estar em baixo
// exatamente quando é preciso. A segunda linha é a pausa automática da AWS, que funciona sem nós —
// mas exige o cabeçalho X-SES-TENANT em cada mensagem, o que obriga a mexer no caminho do correio.

const (
	intervaloReputacao = time.Hour
	// Janela de análise. Vinte e quatro horas alisa o ruído de um envio pequeno: com dez mensagens,
	// uma devolução dá 10% e suspenderia um cliente por nada.
	janelaReputacao = 24 * time.Hour
	// Mínimo de envios para a taxa significar alguma coisa.
	minimoEnvios = 100

	limiteDevolucao = 0.05  // 5%
	limiteQueixa    = 0.001 // 0,1%
)

func startReputacao(q *db.Queries, relay *ses.Client) {
	if relay == nil {
		log.Printf("reputation watchdog disabled (SES off)")
		return
	}
	go func() {
		time.Sleep(2 * time.Minute) // deixa o arranque respirar
		for {
			verificarReputacao(context.Background(), q, relay)
			time.Sleep(intervaloReputacao)
		}
	}()
	log.Printf("reputation watchdog started (every %s, bounces>%.0f%% or complaints>%.1f%% suspend the customer)",
		intervaloReputacao, limiteDevolucao*100, limiteQueixa*100)
}

func verificarReputacao(ctx context.Context, q *db.Queries, relay *ses.Client) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	dominios, err := q.ListAllDomains(ctx)
	if err != nil {
		log.Printf("reputation: listar domínios: %v", err)
		return
	}

	ate := time.Now()
	de := ate.Add(-janelaReputacao)

	for _, d := range dominios {
		conjunto := ses.TenantName(d.Dominio)
		rep, err := relay.ReputacaoDoConjunto(ctx, conjunto, de, ate)
		if err != nil {
			// Não suspende por não conseguir medir: um erro de leitura não é prova de nada, e
			// suspender um cliente por engano é o mesmo estrago pelo motivo errado.
			log.Printf("reputation: %s: %v", d.Dominio, err)
			continue
		}
		if rep.Enviadas < minimoEnvios {
			continue
		}

		devolucao, queixa := rep.TaxaDevolucao(), rep.TaxaQueixa()
		if devolucao <= limiteDevolucao && queixa <= limiteQueixa {
			continue
		}

		log.Printf("reputation: SUSPENDING %s — %.0f sent, %.2f%% bounced (limit %.0f%%), %.3f%% complained (limit %.1f%%)",
			d.Dominio, rep.Enviadas, devolucao*100, limiteDevolucao*100, queixa*100, limiteQueixa*100)

		if err := relay.SetSendingEnabled(ctx, conjunto, false); err != nil {
			log.Printf("reputation: %s: SUSPENSION FAILED, the account is still exposed: %v", d.Dominio, err)
			continue
		}
		log.Printf("reputation: %s suspended; reinstating is a manual decision (ses-tenants has no --reativar on purpose)",
			d.Dominio)
	}
}
