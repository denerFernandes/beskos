// ses-tenants ensures every already-provisioned domain has its own isolation in SES:
// a configuration set predefined on the identity (which is what takes effect today) and an
// associated tenant (which takes effect once messages carry the X-SES-TENANT header).
//
// WHY A SEPARATE COMMAND: reputation isolation started being created during provisioning, but
// domains that were already there never go through that step again. Without this, the old
// customers all stay on the account's reputation — which is precisely the customer with the most
// mail sent, and therefore the most likely to spoil it.
//
//	ses-tenants            # shows the status of each domain
//	ses-tenants --create    # creates whatever is missing
//
// Idempotent: a tenant that already exists is a success. Running it twice does no harm.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/ses"
)

func main() {
	log.SetFlags(0)
	create := flag.Bool("create", false, "create the missing tenants; without this it only reports")
	flag.Parse()

	relay := ses.NewFromEnv()
	if relay == nil {
		log.Fatal("SES off (SES_REGION and the AWS credentials are missing)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, env("DATABASE_URL"))
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	domains, err := db.New(pool).ListAllDomains(ctx)
	if err != nil {
		log.Fatalf("list domains: %v", err)
	}
	log.Printf("%d domains", len(domains))

	var done, failures int
	for _, d := range domains {
		name := ses.TenantName(d.Domain)

		if !*create {
			status, err := relay.TenantSendingStatus(ctx, name)
			if err == nil {
				log.Printf("  %-24s %-24s tenant existe (envio: %s)", d.Domain, name, status)
			} else {
				log.Printf("  %-24s %-24s MISSING", d.Domain, name)
				done++
			}
			continue
		}

		// The configuration set first: it's what takes effect without depending on anything else.
		if err := relay.CreateConfigurationSet(ctx, name); err != nil {
			log.Printf("  %-24s create configuration set: %v", d.Domain, err)
			failures++
			continue
		}
		if err := relay.SetIdentityConfigurationSet(ctx, d.Domain, name); err != nil {
			log.Printf("  %-24s set default configuration set: %v", d.Domain, err)
			failures++
			continue
		}
		if err := relay.CreateTenant(ctx, name); err != nil {
			log.Printf("  %-24s create tenant: %v", d.Domain, err)
			failures++
			continue
		}
		if err := relay.AssociateIdentity(ctx, name, d.Domain); err != nil {
			log.Printf("  %-24s associar identidade: %v", d.Domain, err)
			failures++
			continue
		}
		log.Printf("  %-24s %-24s default configuration set + tenant associated", d.Domain, name)
		done++
	}

	verb := "por criar"
	if *create {
		verb = "criados"
	}
	log.Printf("%d %s, %d falhas", done, verb, failures)
	if failures > 0 {
		os.Exit(1)
	}
}

func env(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("%s missing", name)
	}
	return v
}
