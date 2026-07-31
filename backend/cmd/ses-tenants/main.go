// ses-tenants garante que cada domínio já provisionado tem o seu isolamento no SES:
// um conjunto de configuração predefinido na identidade (que é o que produz efeito hoje) e um
// tenant associado (que produz efeito quando as mensagens levarem o cabeçalho X-SES-TENANT).
//
// PORQUÊ UM COMANDO À PARTE: o isolamento de reputação passou a ser criado no provisionamento, mas
// os domínios que já lá estavam nunca passam por esse passo outra vez. Sem isto, os clientes
// antigos continuam todos na reputação da conta — que é precisamente quem tem mais correio enviado
// e, portanto, mais probabilidade de a estragar.
//
//	ses-tenants            # mostra o estado de cada domínio
//	ses-tenants --create    # cria o que faltar
//
// Idempotente: um tenant que já existe é sucesso. Correr duas vezes não faz mal.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mailin/backend/internal/db"
	"mailin/backend/internal/ses"
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
		name := ses.TenantName(d.Dominio)

		if !*create {
			status, err := relay.TenantSendingStatus(ctx, name)
			if err == nil {
				log.Printf("  %-24s %-24s tenant existe (envio: %s)", d.Dominio, name, status)
			} else {
				log.Printf("  %-24s %-24s MISSING", d.Dominio, name)
				done++
			}
			continue
		}

		// O conjunto de configuração primeiro: é o que produz efeito sem depender de mais nada.
		if err := relay.CreateConfigurationSet(ctx, name); err != nil {
			log.Printf("  %-24s create configuration set: %v", d.Dominio, err)
			failures++
			continue
		}
		if err := relay.SetIdentityConfigurationSet(ctx, d.Dominio, name); err != nil {
			log.Printf("  %-24s set default configuration set: %v", d.Dominio, err)
			failures++
			continue
		}
		if err := relay.CreateTenant(ctx, name); err != nil {
			log.Printf("  %-24s criar tenant: %v", d.Dominio, err)
			failures++
			continue
		}
		if err := relay.AssociateIdentity(ctx, name, d.Dominio); err != nil {
			log.Printf("  %-24s associar identidade: %v", d.Dominio, err)
			failures++
			continue
		}
		log.Printf("  %-24s %-24s default configuration set + tenant associated", d.Dominio, name)
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
