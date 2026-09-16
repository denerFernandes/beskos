package main

import (
	"github.com/beskosorg/beskos/backend/internal/httpapi"
	"github.com/beskosorg/beskos/backend/internal/ses"
	"log"
)

func enableHostedServices(srv *httpapi.Server) {
	// SES (optional): used only to clean up the identity when deleting a domain.
	if c := ses.NewFromEnv(); c != nil {
		srv.EnableSES(c)
		log.Printf("SES identity cleanup enabled")
	}
}
