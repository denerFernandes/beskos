package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Sentinela: verifica que o Stalwart está alcançável PELO NOME que os clientes usam.
//
// PORQUÊ, e porque não chega o healthcheck do contentor: a 30/07/2026 o correio e o webmail
// estiveram meia hora em baixo com o Docker a dizer `healthy` o tempo todo — e o Docker tinha razão.
// O healthcheck da imagem faz `curl http://127.0.0.1:8080/healthz/live` DENTRO do contentor, o
// processo estava vivo e respondia. O que estava partido era o encaminhamento do Swarm: quem se
// ligasse a `stalwart:8080` era aceite e depois cortado.
//
// Um healthcheck de loopback não pode ver isso, por definição — não passa pelo caminho que falha.
// Esta sentinela passa: liga-se pelo mesmo nome e pela mesma rede que a API usa. É a diferença
// entre "o processo está de pé" e "o serviço está a servir".
//
// Não repara nada, de propósito. Só grita. Uma sentinela que reinicia serviços sozinha transforma
// um problema de rede numa cascata de reinícios — e o registo passa a contar a história errada.

const (
	intervaloSentinela = time.Minute
	// Só se queixa depois de falhar seguidas: um pico de rede não é uma avaria, e um alarme que
	// dispara sozinho de vez em quando deixa de ser lido.
	falhasParaAlarme = 3
)

func startSentinela(stalwartURL string) {
	if stalwartURL == "" {
		log.Printf("stalwart sentinel disabled (STALWART_URL unset)")
		return
	}
	alvo := strings.TrimRight(stalwartURL, "/") + "/healthz/live"

	go func() {
		cli := &http.Client{Timeout: 10 * time.Second}
		var seguidas int
		for {
			time.Sleep(intervaloSentinela)

			err := espreitar(cli, alvo)
			if err == nil {
				if seguidas >= falhasParaAlarme {
					log.Printf("stalwart sentinel: RECOVERED, %s reachable again after %d failures",
						alvo, seguidas)
				}
				seguidas = 0
				continue
			}

			seguidas++
			// O primeiro aviso é o que interessa: diz o que falhou e, sobretudo, distingue-o do
			// healthcheck do contentor, que continuará a dizer `healthy`.
			if seguidas == falhasParaAlarme {
				log.Printf("stalwart sentinel: UNREACHABLE by service name for %d checks: %v "+
					"(the container healthcheck may still report healthy — it probes loopback, "+
					"this probes the path clients use; suspect swarm routing, see docs/DEPLOY.md)",
					seguidas, err)
			} else if seguidas > falhasParaAlarme && seguidas%10 == 0 {
				log.Printf("stalwart sentinel: still unreachable after %d checks: %v", seguidas, err)
			}
		}
	}()
	log.Printf("stalwart sentinel started (checks %s every %s)", alvo, intervaloSentinela)
}

func espreitar(cli *http.Client, alvo string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, alvo, nil)
	if err != nil {
		return err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Qualquer resposta HTTP serve: o que se está a verificar é que a ligação chega ao fim e é
	// atendida, não o que o Stalwart acha da saúde dele.
	if resp.StatusCode >= 500 {
		return fmt.Errorf("respondeu %s", resp.Status)
	}
	return nil
}
