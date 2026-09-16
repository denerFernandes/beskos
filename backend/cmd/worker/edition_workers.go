package main

import (
	"context"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
	"github.com/beskosorg/beskos/backend/internal/ses"
	"log"
	"os"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// startEditionWorkers registers the private edition's background jobs.
func startEditionWorkers(ctx context.Context, q *db.Queries, st *storage.Service) {
	startMetering(ctx, q, st)
}

func configureEditionProvisioning(q *db.Queries, machine *provisioning.Machine) {
	if relay := ses.NewFromEnv(); relay != nil {
		brand := os.Getenv("MAIL_BRAND_DOMAIN")
		if brand == "" {
			brand = os.Getenv("STALWART_DEFAULT_DOMAIN")
		}
		machine.WithRelay(relay, brand)
		log.Printf("SES relay enabled (white-label BYODKIM; brand=%s)", brand)
		// Reputation sentinel: suspends one customer before AWS suspends everyone.
		startReputation(q, relay)
	}
}
