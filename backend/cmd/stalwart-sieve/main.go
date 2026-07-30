// stalwart-sieve instala no Stalwart o script que identifica o cliente ao relay de saída.
//
// Acrescenta `X-SES-TENANT: <domínio-com-hífens>` a cada mensagem. Sem esse cabeçalho, os tenants
// do SES existem mas não recebem tráfego — e a pausa automática da AWS, que é a única defesa que
// funciona com o nosso código em baixo, nunca dispara.
//
//	stalwart-sieve            # mostra o script e a configuração atual, não altera nada
//	stalwart-sieve --aplicar  # instala e liga à fase DATA
//
// Imprime SEMPRE a configuração atual da fase DATA antes de mexer: se algo correr mal, a reversão
// está no registo e não na memória de alguém.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"mailin/backend/internal/stalwart"
)

const nomeDoScript = "ses-tenant"

func main() {
	log.SetFlags(0)
	aplicar := flag.Bool("aplicar", false, "instala mesmo; sem isto só mostra")
	flag.Parse()

	log.Printf("=== corrida %s ===", time.Now().UTC().Format(time.RFC3339))

	c := stalwart.New(env("STALWART_URL"), env("STALWART_ADMIN_ID"), env("STALWART_ADMIN_SECRET"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	atual, err := c.GetDataStageScript(ctx)
	if err != nil {
		log.Fatalf("ler a fase DATA: %v", err)
	}
	log.Printf("FASE DATA ATUAL (guardar — é a reversão):\n%s", identar(atual))
	log.Printf("SCRIPT A INSTALAR (%s):\n%s", nomeDoScript, stalwart.ScriptTenantSES())

	if !*aplicar {
		log.Printf("nada foi alterado (faltou --aplicar)")
		return
	}

	id, err := c.UpsertSieveScript(ctx, nomeDoScript, stalwart.ScriptTenantSES())
	if err != nil {
		log.Fatalf("instalar o script: %v", err)
	}
	log.Printf("script instalado, id %s", id)

	if err := c.SetDataStageScript(ctx, nomeDoScript); err != nil {
		log.Fatalf("ligar à fase DATA: %v", err)
	}
	log.Printf("fase DATA ligada ao script")

	depois, err := c.GetDataStageScript(ctx)
	if err != nil {
		log.Fatalf("reler: %v", err)
	}
	log.Printf("FASE DATA AGORA:\n%s", identar(depois))
	log.Printf("falta RECARREGAR o Stalwart para isto valer — `docker service update --force stalwart`")
}

func identar(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	bonito, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(bonito)
}

func env(nome string) string {
	v := os.Getenv(nome)
	if v == "" {
		log.Fatalf("%s em falta", nome)
	}
	return v
}
