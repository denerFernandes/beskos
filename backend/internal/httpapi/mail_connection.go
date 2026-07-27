package httpapi

import "net/http"

// Dados de ligação para clientes de e-mail externos (Outlook, Thunderbird, telemóvel) e para quem
// quer enviar a partir do próprio site por SMTP. É informação, não segredo: as portas e o host são
// públicos; a senha é a da caixa e nunca passa por aqui.
//
// O host vem de STALWART_HOSTNAME. Sem ele configurado, o que aparece na zona DNS é o hostname
// interno do contentor — inútil para quem está de fora.

// SetMailHost define o host público do servidor de correio (IMAP/SMTP/JMAP).
func (s *Server) SetMailHost(h string) { s.mailHost = h }

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
