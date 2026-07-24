package db

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // driver database/sql para o goose
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate aplica as migrations pendentes. Idempotente (goose controla a versão aplicada).
// Roda no boot do cmd/api para simplicidade na Fase 0.
func Migrate(databaseURL string) error {
	sqldb, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("abrir db p/ migrate: %w", err)
	}
	defer sqldb.Close()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(sqldb, "migrations")
}
