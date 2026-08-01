package httpapi

// Server configuration and optional wiring: what main switches on before serving, plus the
// health probes. Kept apart from the route table so that table reads as a table.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mailin/backend/internal/chat"
	"mailin/backend/internal/notify"
	"mailin/backend/internal/secretbox"
	"mailin/backend/internal/ses"
)

// SetReadiness liga a sonda de prontidão (/readyz): a função deve falhar se uma dependência crítica
// (ex.: Postgres) estiver indisponível. Sem ela, /readyz responde sempre "ready".
func (s *Server) SetReadiness(ping func(context.Context) error) { s.readiness = ping }

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.readiness != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.readiness(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// EnableSES liga a limpeza de identidades SES ao apagar domínios.
func (s *Server) EnableSES(c *ses.Client) { s.sesRelay = c }

// EnableTOTPEncryption liga a cifra do segredo TOTP em repouso (MFA_ENC_KEY). Vazio = desligado.
func (s *Server) EnableTOTPEncryption(key string) { s.totpBox = secretbox.New(key) }

// encTOTP/decTOTP cifram/decifram o segredo TOTP; com box nil, são no-op (texto claro).
func (s *Server) encTOTP(plain string) string {
	if v, err := s.totpBox.Encrypt(plain); err == nil {
		return v
	}
	return plain
}

func (s *Server) decTOTP(stored string) string {
	if v, err := s.totpBox.Decrypt(stored); err == nil {
		return v
	}
	return stored
}

// decryptCred decifra a credencial JMAP guardada (brokering H11). Fail-closed: string vazia ou erro
// de decifra → (,"", false), o que o proxy trata como "sem mailbox" (401 → re-login).
func (s *Server) decryptCred(stored string) (string, bool) {
	if stored == "" {
		return "", false
	}
	v, err := s.totpBox.Decrypt(stored)
	if err != nil || v == "" {
		return "", false
	}
	return v, true
}

// SetFileUploadLimit define o tamanho máximo por ficheiro no Files (configurável via env).
func (s *Server) SetFileUploadLimit(n int64) { s.maxUpload = n }

// SetAppOrigins liga o CORS para as origens dadas (ex.: app Tauri) e define se o cookie de sessão é
// Secure/SameSite=None (necessário cross-site sobre HTTPS).
func (s *Server) SetAppOrigins(origins []string, cookieSecure bool) {
	s.corsOrigins = origins
	s.cookieSecure = cookieSecure
}

// SetPublicURL define a URL pública da app, usada para montar links absolutos que saem para fora
// (ex.: o link de uma sala de reunião num e-mail). Sem ela, esses links ficariam relativos e
// inúteis fora da app.
func (s *Server) SetPublicURL(u string) { s.publicURL = strings.TrimRight(u, "/") }

// originHostPatterns extrai os hosts das origens configuradas para o OriginPatterns do WebSocket.
// Autoriza o app nativo (cross-origin) sem abrir o WS para qualquer site — o default (lista vazia)
// já aceita só same-origin, fechando o CSWSH.
func originHostPatterns(origins []string) []string {
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			out = append(out, u.Host)
		}
	}
	return out
}

func (s *Server) originAllowed(origin string) bool {
	for _, o := range s.corsOrigins {
		if o == origin {
			return true
		}
	}
	return false
}

// SetSignupsOpen abre/fecha o registo self-service (SIGNUPS_OPEN). Fechado, a página mostra
// "em breve" e a API recusa — o resto do produto continua igual para quem já é cliente.
func (s *Server) SetSignupsOpen(open bool) { s.signupsOpen = open }

// EnableEmail liga o envio de e-mail para notificações (opcional).
func (s *Server) EnableEmail(e *notify.Email) { s.email = e }

// EnableChat liga o tempo real do chat (WebSocket + Redis pub/sub).
func (s *Server) EnableChat(h *chat.Hub) { s.hub = h }
