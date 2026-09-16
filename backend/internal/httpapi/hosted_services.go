package httpapi

import (
	"context"
	"github.com/beskosorg/beskos/backend/internal/ses"
	"log"
)

type hostedServices struct {
	// sesRelay removes SES identities when a domain is deleted (nil = SES off). Identity
	// REGISTRATION lives in the worker (provisioning.EmailRelay); this is cleanup only.
	sesRelay *ses.Client
	// snsVerifier validates the signature of the SES event webhook (bounces/complaints).
	snsVerifier *ses.SNSVerifier
}

func newHostedServices() hostedServices {
	return hostedServices{snsVerifier: ses.NewSNSVerifier()}
}

// EnableSES wires the cleanup of SES identities when a domain is deleted.
func (s *Server) EnableSES(c *ses.Client) { s.sesRelay = c }

func (s *Server) removeRelayIdentity(ctx context.Context, domain string) {
	// SES identity: best-effort (no SES configured, or a transient failure, doesn't block — the
	// orphaned identity can be removed by hand in AWS; it gets logged).
	if s.sesRelay != nil {
		if err := s.sesRelay.DeleteIdentity(ctx, domain); err != nil {
			log.Printf("domains: ses delete identity %s: %v", domain, err)
		}
	}
}
