// stalwart-sieve installs on Stalwart the script that identifies the customer to the outbound
// relay.
//
// Adds `X-SES-TENANT: <domain-with-hyphens>` to every message. Without that header, SES tenants
// exist but receive no traffic — and AWS's automatic pause, which is the only defense that works
// once our code is down, never triggers.
//
//	stalwart-sieve            # shows the script and current configuration, changes nothing
//	stalwart-sieve --apply  # installs and connects it to the DATA stage
//
// ALWAYS prints the DATA stage's current configuration before touching it: if something goes
// wrong, the rollback is in the log and not in someone's memory.
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

	log.Printf("=== run %s ===", time.Now().UTC().Format(time.RFC3339))

	c := stalwart.New(env("STALWART_URL"), env("STALWART_ADMIN_ID"), env("STALWART_ADMIN_SECRET"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	current, err := c.GetDataStageScript(ctx)
	if err != nil {
		log.Fatalf("read the DATA stage: %v", err)
	}
	log.Printf("CURRENT DATA STAGE (keep — it is the rollback):\n%s", indent(current))
	log.Printf("SCRIPT TO INSTALL (%s):\n%s", scriptName, stalwart.ScriptTenantSES())

	if !*apply {
		log.Printf("nothing was changed (--apply was not given)")
		return
	}

	id, err := c.UpsertSieveScript(ctx, scriptName, stalwart.ScriptTenantSES())
	if err != nil {
		log.Fatalf("install the script: %v", err)
	}
	log.Printf("script installed, id %s", id)

	if err := c.SetDataStageScript(ctx, scriptName); err != nil {
		log.Fatalf("connect to the DATA stage: %v", err)
	}
	log.Printf("DATA stage connected to the script")

	after, err := c.GetDataStageScript(ctx)
	if err != nil {
		log.Fatalf("re-read: %v", err)
	}
	log.Printf("DATA STAGE NOW:\n%s", indent(after))
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
