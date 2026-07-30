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
//	ses-tenants --criar    # cria o que faltar
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
	criar := flag.Bool("criar", false, "cria os tenants em falta; sem isto só mostra")
	flag.Parse()

	relay := ses.NewFromEnv()
	if relay == nil {
		log.Fatal("SES desligado (faltam SES_REGION e as credenciais AWS)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, env("DATABASE_URL"))
	if err != nil {
		log.Fatalf("base de dados: %v", err)
	}
	defer pool.Close()

	dominios, err := db.New(pool).ListAllDomains(ctx)
	if err != nil {
		log.Fatalf("listar domínios: %v", err)
	}
	log.Printf("%d domínios", len(dominios))

	var feitos, falhas int
	for _, d := range dominios {
		nome := ses.TenantName(d.Dominio)

		if !*criar {
			estado, err := relay.TenantSendingStatus(ctx, nome)
			if err == nil {
				log.Printf("  %-24s %-24s tenant existe (envio: %s)", d.Dominio, nome, estado)
			} else {
				log.Printf("  %-24s %-24s EM FALTA", d.Dominio, nome)
				feitos++
			}
			continue
		}

		// O conjunto de configuração primeiro: é o que produz efeito sem depender de mais nada.
		if err := relay.CreateConfigurationSet(ctx, nome); err != nil {
			log.Printf("  %-24s criar conjunto: %v", d.Dominio, err)
			falhas++
			continue
		}
		if err := relay.SetIdentityConfigurationSet(ctx, d.Dominio, nome); err != nil {
			log.Printf("  %-24s predefinir conjunto: %v", d.Dominio, err)
			falhas++
			continue
		}
		if err := relay.CreateTenant(ctx, nome); err != nil {
			log.Printf("  %-24s criar tenant: %v", d.Dominio, err)
			falhas++
			continue
		}
		if err := relay.AssociateIdentity(ctx, nome, d.Dominio); err != nil {
			log.Printf("  %-24s associar identidade: %v", d.Dominio, err)
			falhas++
			continue
		}
		log.Printf("  %-24s %-24s conjunto predefinido + tenant associado", d.Dominio, nome)
		feitos++
	}

	verbo := "por criar"
	if *criar {
		verbo = "criados"
	}
	log.Printf("%d %s, %d falhas", feitos, verbo, falhas)
	if falhas > 0 {
		os.Exit(1)
	}
}

func env(nome string) string {
	v := os.Getenv(nome)
	if v == "" {
		log.Fatalf("%s em falta", nome)
	}
	return v
}
