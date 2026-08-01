package httpapi

import (
	"net/http"

	"mailin/backend/internal/db"
	"mailin/backend/internal/provisioning"
)

// Dados de ligação para clientes de e-mail externos (Outlook, Thunderbird, telemóvel) e para quem
// quer enviar a partir do próprio site por SMTP. É informação, não segredo: as portas e o host são
// públicos; a senha é a da caixa e nunca passa por aqui.
//
// O host vem de STALWART_HOSTNAME. Sem ele configurado, o que aparece na zona DNS é o hostname
// interno do contentor — inútil para quem está de fora.

// SetMailHost define o host público do servidor de correio (IMAP/SMTP/JMAP).
func (s *Server) SetMailHost(h string) { s.mailHost = h }

// SetMTASTSHost sets the public name that serves MTA-STS policies, which every customer's
// mta-sts.<domain> is pointed at. Empty leaves those records as Stalwart wrote them.
func (s *Server) SetMTASTSHost(h string) { s.mtaStsHost = h }

// zoneOf returns the customer-facing DNS zone: what Stalwart produced, with its own container
// hostname replaced by names a customer can actually publish.
//
// WHY AT READ TIME AND NOT ONLY WHEN PROVISIONING: the zone is generated once, on the day the
// domain is created, and stored as text. Stalwart writes its container hostname into it, so a
// domain provisioned before that substitution existed -- or before these public names changed --
// would show a target that resolves nowhere, for as long as the row lives. Correcting on the way
// out costs one pass over a handful of lines and makes the stored value stop mattering.
func (s *Server) zoneOf(d db.Domain) string {
	if d.DnsZone == nil {
		return ""
	}
	return provisioning.PublicHosts(*d.DnsZone, s.mailHost, s.mtaStsHost)
}

func (s *Server) handleMailConnection(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	host := s.mailHost
	writeJSON(w, http.StatusOK, map[string]any{
		"host":     host,
		"username": mu.Email, // sempre o endereço completo — é o que o Stalwart espera
		"imap":     map[string]any{"host": host, "port": 993, "security": "ssl"},
		"smtp": []map[string]any{
			{"host": host, "port": 465, "security": "ssl"},      // implícito, o mais simples
			{"host": host, "port": 587, "security": "starttls"}, // alternativa para quem só fala 587
		},
		"jmap":   map[string]any{"url": "https://" + host + "/.well-known/jmap"},
		"caldav": map[string]any{"url": "https://" + host + "/dav/"},
	})
}
