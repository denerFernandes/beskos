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
	reputationInterval = time.Hour
	// Janela de análise. Vinte e quatro horas alisa o ruído de um envio pequeno: com dez mensagens,
	// uma devolução dá 10% e suspenderia um cliente por nada.
	reputationWindow = 24 * time.Hour
	// Mínimo de envios para a taxa significar alguma coisa.
	minSends = 100

	bounceLimit    = 0.05  // 5%
	complaintLimit = 0.001 // 0,1%
)

func startReputation(q *db.Queries, relay *ses.Client) {
	if relay == nil {
		log.Printf("reputation watchdog disabled (SES off)")
		return
	}
	go func() {
		time.Sleep(2 * time.Minute) // deixa o arranque respirar
		for {
			checkReputation(context.Background(), q, relay)
			time.Sleep(reputationInterval)
		}
	}()
	log.Printf("reputation watchdog started (every %s, bounces>%.0f%% or complaints>%.1f%% suspend the customer)",
		reputationInterval, bounceLimit*100, complaintLimit*100)
}

func checkReputation(ctx context.Context, q *db.Queries, relay *ses.Client) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	domains, err := q.ListAllDomains(ctx)
	if err != nil {
		log.Printf("reputation: list domains: %v", err)
		return
	}

	// ALINHADO À MEIA-NOITE UTC, e não a rolar a partir de agora. A SES só devolve os dados diários
	// agregados quando o intervalo vai de meia-noite a meia-noite; com marcas de dia parcial responde
	// HTTP 400 e não devolve número nenhum. Isto foi um erro que só apareceu em produção: o vigia
	// falhava em TODAS as passagens desde que foi instalado, e ninguém era medido.
	//
	// O preço é a janela terminar na última meia-noite, ou seja até 24 h de atraso sobre o que está a
	// acontecer agora. É aceitável porque este vigia nunca foi a defesa em tempo real — essa é a
	// pausa automática da SES por tenant, que atua sem nós. Este é a rede que apanha a tendência.
	until := time.Now().UTC().Truncate(24 * time.Hour)
	from := until.Add(-reputationWindow)

	for _, d := range domains {
		configSet := ses.TenantName(d.Domain)
		rep, err := relay.ConfigSetReputation(ctx, configSet, from, until)
		if err != nil {
			// Não suspende por não conseguir medir: um erro de leitura não é prova de nada, e
			// suspender um cliente por engano é o mesmo estrago pelo motivo errado.
			log.Printf("reputation: %s: %v", d.Domain, err)
			continue
		}
		if rep.Sent < minSends {
			continue
		}

		bounceRate, complaintRate := rep.BounceRate(), rep.ComplaintRate()
		if bounceRate <= bounceLimit && complaintRate <= complaintLimit {
			continue
		}

		log.Printf("reputation: SUSPENDING %s — %.0f sent, %.2f%% bounced (limit %.0f%%), %.3f%% complained (limit %.1f%%)",
			d.Domain, rep.Sent, bounceRate*100, bounceLimit*100, complaintRate*100, complaintLimit*100)

		if err := relay.SetSendingEnabled(ctx, configSet, false); err != nil {
			log.Printf("reputation: %s: SUSPENSION FAILED, the account is still exposed: %v", d.Domain, err)
			continue
		}
		log.Printf("reputation: %s suspended; reinstating is a manual decision (ses-tenants has no --reativar on purpose)",
			d.Domain)
	}
}
