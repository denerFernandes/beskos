package httpapi

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// O proxy JMAP encaminha /jmap/*, /auth/*, /.well-known/* para o Stalwart. Faz o BROKERING da
// credencial (H11): o browser autentica-se com a SESSÃO (cookie/bearer/?access_token) e o proxy
// INJETA a credencial Basic da caixa (guardada cifrada em mail_users.jmap_cred) na requisição a
// caminho do Stalwart. Assim a senha da caixa nunca vive no browser. Precisa estar SEMPRE no caminho
// do JMAP (web e app nativo) — o deploy roteia /jmap para a NOSSA API, não direto ao Stalwart.
//
// SSE streaming (/jmap/eventsource) needs immediate flushing, so FlushInterval is -1.
func (s *Server) newJMAPProxy() http.Handler {
	target, err := url.Parse(s.sw.BaseURL())
	if err != nil {
		log.Fatalf("jmap proxy: invalid Stalwart URL %q: %v", s.sw.BaseURL(), err)
	}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1 // flush em tempo real (necessário para o eventsource SSE)
	base := rp.Director
	rp.Director = func(r *http.Request) {
		base(r)
		r.Host = target.Host // Stalwart roteia por Host; usa o do upstream, não o público
	}
	return rp
}

// mountJMAPProxy liga o proxy JMAP (com brokering de credencial) nas rotas do webmail. Montado
// SEMPRE — o brokering exige que o proxy esteja no caminho tanto na web quanto no app nativo.
func (s *Server) mountJMAPProxy(r interface {
	Handle(pattern string, h http.Handler)
}) {
	// broker (auth) → quota de envio do plano (EmailSubmission/set) → proxy para o Stalwart.
	p := s.jmapBrokerAuth(s.jmapMailQuota(s.newJMAPProxy()))
	r.Handle("/jmap/*", p)
	r.Handle("/auth/*", p)
	r.Handle("/.well-known/*", p)
}

// jmapBrokerAuth autentica a sessão do usuário e injeta a credencial Basic da caixa no request antes
// de encaminhá-lo ao Stalwart. O browser envia só a sessão (cookie/bearer/?access_token); nunca a
// credencial da caixa. Sem sessão válida ou sem credencial guardada → 401 (força login/re-login).
func (s *Server) jmapBrokerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := sessionToken(r)
		if tok == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		row, err := s.q.GetWebmailSession(r.Context(), tok)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		cred, ok := s.decryptCred(row.MailUser.JmapCred)
		if !ok {
			// Conta só-painel (sem caixa) ou sessão anterior ao brokering: sem credencial para injetar.
			http.Error(w, "no mailbox credential", http.StatusUnauthorized)
			return
		}
		// Substitui qualquer Authorization do cliente pela credencial da caixa (server-to-server).
		r.Header.Set("Authorization", "Basic "+cred)
		// Injeta o utilizador no contexto: o middleware de quota de envio (mail_quota.go) precisa
		// do tenant para aplicar o limite diário do plano.
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxMailUser, row.MailUser)))
	})
}
