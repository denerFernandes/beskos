// cmd/manager-api: a API do Manager — painel do OPERADOR do SaaS (docs/manager.md).
// Serviço separado do control plane de propósito: porta, sessões e utilizadores próprios.
// Config mínima via ambiente (lei 2.6): só o que este serviço realmente usa.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"

	"mailin/backend/internal/billing"
	"mailin/backend/internal/db"
	"mailin/backend/internal/managerapi"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatalf("config: DATABASE_URL is required")
	}
	addr := os.Getenv("MANAGER_HTTP_ADDR")
	if addr == "" {
		addr = ":8091"
	}

	// Mesmo esquema do cmd/api: as migrations são idempotentes (goose), então tanto faz quem
	// arranca primeiro — o manager pode correr sozinho num ambiente sem o api.
	if err := db.Migrate(dbURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()
	q := db.New(pool)

	// Primeiro operador via ambiente (tabela vazia). TOTP é confirmado no primeiro login.
	if err := managerapi.EnsureBootstrap(context.Background(), q,
		os.Getenv("MANAGER_BOOTSTRAP_EMAIL"), os.Getenv("MANAGER_BOOTSTRAP_PASSWORD")); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	// MFA_ENC_KEY: a MESMA chave do produto serve (cifra AES-GCM de segredos TOTP em repouso).
	bp, err := billing.New(billing.FromEnv())
	if err != nil {
		log.Fatalf("billing provider: %v", err)
	}
	srv := managerapi.NewServer(q, os.Getenv("MFA_ENC_KEY"), os.Getenv("COOKIE_SECURE") == "true", bp)
	if os.Getenv("MFA_ENC_KEY") != "" {
		log.Printf("TOTP secret encryption enabled")
	}

	log.Printf("manager API listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Router()); err != nil {
		log.Fatalf("http: %v", err)
	}
}
