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

	// Three blind passes is three hours. Long enough that a one-off AWS hiccup stays quiet, short
	// enough that a broken watchdog is caught the same working day instead of the same week.
	blindPassesBeforeAlarm = 3
)

func startReputation(q *db.Queries, relay *ses.Client) {
	if relay == nil {
		log.Printf("reputation watchdog disabled (SES off)")
		return
	}
	go func() {
		time.Sleep(2 * time.Minute) // deixa o arranque respirar
		// Consecutive passes where NOT ONE domain could be measured. Law 2.8: a single failing
		// domain is ordinary noise, but every read failing, pass after pass, means the watchdog is
		// blind -- and a blind watchdog looks exactly like a happy one.
		var blind int
		for {
			measured, failed := checkReputation(context.Background(), q, relay)
			was := blind
			blind = blindAfter(blind, measured, failed)
			switch {
			case blind == 0 && was >= blindPassesBeforeAlarm:
				log.Printf("reputation: RECOVERED, measuring again after %d blind passes", was)
			case blind == blindPassesBeforeAlarm:
				log.Printf("reputation: BLIND for %d passes — not one domain could be measured. "+
					"Nobody is being watched; a customer can burn the account's reputation unnoticed. "+
					"Check the SES credentials and the metrics window.", blind)
			case blind > blindPassesBeforeAlarm && blind%6 == 0:
				log.Printf("reputation: still blind after %d passes", blind)
			}
			time.Sleep(reputationInterval)
		}
	}()
	log.Printf("reputation watchdog started (every %s, bounces>%.0f%% or complaints>%.1f%% suspend the customer)",
		reputationInterval, bounceLimit*100, complaintLimit*100)
}

// blindAfter folds one pass into the blind-pass counter.
//
// Blind means NOT ONE domain could be read while at least one read was attempted. A single failing
// domain is ordinary noise; measuring nothing at all, pass after pass, means the watchdog is down
// and looks exactly like a watchdog with nothing to report. Zero attempts (no domains, SES off) is
// not blindness -- there was nothing to see.
func blindAfter(blind, measured, failed int) int {
	if measured > 0 || failed == 0 {
		return 0
	}
	return blind + 1
}

// checkReputation runs one pass and reports how many domains it could read and how many it could
// not. The caller needs both to tell "one flaky domain" from "the whole thing is broken" (law 2.8).
func checkReputation(ctx context.Context, q *db.Queries, relay *ses.Client) (measured, failed int) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	domains, err := q.ListAllDomains(ctx)
	if err != nil {
		log.Printf("reputation: list domains: %v", err)
		// Not blindness: without the list there was nothing to measure, so it does not count as a
		// failed read. Saying otherwise would raise the alarm for an empty database.
		return 0, 0
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
			failed++
			continue
		}
		measured++
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
	return measured, failed
}
