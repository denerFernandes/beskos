package httpapi

import (
	"context"
	"net/http"
	"strings"

	"mailin/backend/internal/db"

	"mailin/backend/internal/billing"
)

// Sessão única do produto (cookie wm_session). O login vive em auth_handlers.go: com caixa a
// credencial é validada no Stalwart, sem caixa no nosso cadastro. O e-mail em si segue JMAP
// direto no Stalwart (DIVERGENCES §D7).

const ctxMailUser ctxKey = "mailUser"

func mailUserFrom(ctx context.Context) db.MailUser { return ctx.Value(ctxMailUser).(db.MailUser) }

// sessionToken extrai o token da sessão do webmail: header Bearer (app nativo) > cookie wm_session
// (web same-origin) > ?access_token= na query (WebSocket / mídia, que não mandam Authorization).
func sessionToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("wm_session"); err == nil {
		return c.Value
	}
	return r.URL.Query().Get("access_token")
}

// requireWebmailSession valida o cookie de sessão do webmail e injeta a mail_user no contexto.
func (s *Server) requireWebmailSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := sessionToken(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "auth.no_session")
			return
		}
		row, err := s.q.GetWebmailSession(r.Context(), token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "auth.invalid_session")
			return
		}
		// Suspending somebody has to take effect NOW, not when their cookie happens to expire.
		// Blocking the login was never enough: an admin suspends a person who is already signed in,
		// the panel says "suspended", and that person carries on reading their mail for a week --
		// which is the whole reason suspension exists, defeated silently.
		if row.MailUser.Status != "active" {
			s.endSession(w, token)
			writeErr(w, http.StatusForbidden, "auth.account_suspended")
			return
		}
		// A locked organisation loses access, not just the ability to send -- but only after the
		// grace week, during which everything above still works and the data can be taken out.
		if s.accountState(r.Context(), row.MailUser.TenantID).State == billing.StateLocked {
			s.endSession(w, token)
			writeErr(w, http.StatusPaymentRequired, "account.locked")
			return
		}
		ctx := context.WithValue(r.Context(), ctxMailUser, row.MailUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleWebmailLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("wm_session"); err == nil {
		_ = s.q.DeleteWebmailSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "wm_session", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// endSession destroys the session and clears the cookie. Used when access is withdrawn mid-session,
// so the next request is not merely refused but arrives with nothing to refuse.
func (s *Server) endSession(w http.ResponseWriter, token string) {
	if token != "" {
		_ = s.q.DeleteWebmailSession(context.Background(), token)
	}
	http.SetCookie(w, &http.Cookie{Name: "wm_session", Value: "", Path: "/", MaxAge: -1})
}
