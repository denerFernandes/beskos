// stalwart-sieve instala no Stalwart o script que identifica o cliente ao relay de saída.
//
// Acrescenta `X-SES-TENANT: <domínio-com-hífens>` a cada mensagem. Sem esse cabeçalho, os tenants
// do SES existem mas não recebem tráfego — e a pausa automática da AWS, que é a única defesa que
// funciona com o nosso código em baixo, nunca dispara.
//
//	stalwart-sieve            # mostra o script e a configuração atual, não altera nada
//	stalwart-sieve --apply  # instala e liga à fase DATA
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

	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

const scriptName = "ses-tenant"

func main() {
	log.SetFlags(0)
	apply := flag.Bool("apply", false, "actually install; without this it only reports")
	flag.Parse()

	log.Printf("=== corrida %s ===", time.Now().UTC().Format(time.RFC3339))

	c := stalwart.New(env("STALWART_URL"), env("STALWART_ADMIN_ID"), env("STALWART_ADMIN_SECRET"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	current, err := c.GetDataStageScript(ctx)
	if err != nil {
		log.Fatalf("read the DATA stage: %v", err)
	}
	log.Printf("CURRENT DATA STAGE (keep — it is the rollback):\n%s", indent(current))
	log.Printf("SCRIPT A INSTALAR (%s):\n%s", scriptName, stalwart.ScriptTenantSES())

	if !*apply {
		log.Printf("nothing was changed (--apply was not given)")
		return
	}

	id, err := c.UpsertSieveScript(ctx, scriptName, stalwart.ScriptTenantSES())
	if err != nil {
		log.Fatalf("instalar o script: %v", err)
	}
	log.Printf("script installed, id %s", id)

	if err := c.SetDataStageScript(ctx, scriptName); err != nil {
		log.Fatalf("connect to the DATA stage: %v", err)
	}
	log.Printf("fase DATA ligada ao script")

	after, err := c.GetDataStageScript(ctx)
	if err != nil {
		log.Fatalf("re-read: %v", err)
	}
	log.Printf("FASE DATA AGORA:\n%s", indent(after))
	log.Printf("Stalwart still has to be RELOADED for this to take effect — `docker service update --force stalwart`")
}

func indent(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(pretty)
}

func env(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("%s missing", name)
	}
	return v
}
