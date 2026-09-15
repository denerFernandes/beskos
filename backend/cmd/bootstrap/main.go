// cmd/bootstrap: completa o bootstrap inicial do Stalwart (uma vez por instância).
// Necessário antes de qualquer provisionamento (DIVERGENCES §D3). Após rodar, REINICIE
// o processo do Stalwart (docker restart) para o bootstrap valer.
//
// Uso:
//
//	STALWART_URL=... STALWART_ADMIN_ID=admin STALWART_ADMIN_SECRET=... \
//	STALWART_HOSTNAME=mail.beskos.app STALWART_DEFAULT_DOMAIN=beskos.app \
//	go run ./cmd/bootstrap
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	sw := stalwart.New(
		env("STALWART_URL", "http://localhost:8080"),
		env("STALWART_ADMIN_ID", "admin"),
		os.Getenv("STALWART_ADMIN_SECRET"),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ok, err := sw.IsBootstrapped(ctx)
	if err != nil {
		log.Fatalf("check bootstrap: %v", err)
	}
	if ok {
		log.Printf("Stalwart is already bootstrapped - nothing to do.")
		return
	}

	res, err := sw.Bootstrap(ctx, stalwart.BootstrapInput{
		ServerHostname: env("STALWART_HOSTNAME", "mail.localhost"),
		DefaultDomain:  env("STALWART_DEFAULT_DOMAIN", "localhost"),
	})
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	log.Printf("Bootstrap complete. Permanent admin: %s (secret shown only now: %s)", res.Username, res.Secret)
	log.Printf("NOW RESTART STALWART: docker compose restart stalwart")
}
