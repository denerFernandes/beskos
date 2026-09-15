package httpapi

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"mailin/backend/internal/db"
	"mailin/backend/internal/notify"
)

// Avisos por e-mail nos momentos que importam ao cliente: conta criada, teste iniciado, subscrição
// ativa, cobrança e novo início de sessão. Todos best-effort e fora do caminho crítico (goroutine):
// um servidor de e-mail lento não pode atrasar um login nem falhar um registo.

// sendMsg entrega um aviso já montado. Sem SMTP configurado é um no-op silencioso.
func (s *Server) sendMsg(to string, m notify.Msg) {
	if s.email == nil || to == "" {
		return
	}
	go func() {
		if err := s.email.SendHTML(to, m.Subject, m.HTML, m.Text); err != nil {
			log.Printf("notify: %q para %s: %v", m.Subject, to, err)
		}
	}()
}

// appURL is the address used for the buttons in alert e-mails. A relative link would be useless
// there, so an unset PUBLIC_URL falls back to BRAND_URL rather than leaving the button broken.
func (s *Server) appURL(path string) string {
	base := strings.TrimSuffix(s.publicURL, "/")
	if base == "" {
		base = s.brandURL
	}
	return base + path
}

// clientLocation devolve o país de quem faz o pedido, quando a rede o diz. Atrás da Cloudflare vem
// no CF-IPCountry; sem ela ficamos sem localização — melhor dizer "desconhecida" do que adivinhar.
func clientLocation(r *http.Request) string {
	for _, h := range []string{"CF-IPCountry", "X-Vercel-IP-Country", "X-Country-Code"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" && v != "XX" {
			if city := strings.TrimSpace(r.Header.Get("CF-IPCity")); city != "" {
				return city + ", " + v
			}
			return v
		}
	}
	return ""
}

// clientDevice resume o User-Agent no que uma pessoa reconhece: navegador + sistema.
func clientDevice(r *http.Request) string {
	ua := r.Header.Get("User-Agent")
	if ua == "" {
		return "—"
	}
	browser := "—"
	switch {
	case strings.Contains(ua, "Beskos"): // app de desktop (Tauri)
		browser = "Beskos"
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	os := ""
	switch {
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		os = "iOS"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		os = "macOS"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}
	if os == "" {
		return browser
	}
	return browser + " · " + os
}

// alertNewLogin avisa do início de sessão com o que permite reconhecê-lo — ou perceber que não foi
// a pessoa. É o aviso que dá a alguém a hipótese de reagir a uma senha roubada.
func (s *Server) alertNewLogin(r *http.Request, u db.MailUser) {
	s.sendMsg(u.Email, notify.NewLogin(u.Language, u.Email, clientIP(r), clientLocation(r),
		clientDevice(r), time.Now().UTC(), s.appURL("/mail/profile/security")))
}

// alertAccountCreated dá as boas-vindas com o endereço criado e o que falta (DNS).
func (s *Server) alertAccountCreated(lang, email, domain string) {
	s.sendMsg(email, notify.AccountCreated(lang, email, domain, s.appURL("/mail")))
}

// alertTrialStarted confirma o teste e diz quando (e quanto) começa a pagar — a informação que
// evita a surpresa desagradável no fim do mês.
func (s *Server) alertTrialStarted(lang, email, plan string, days int, amount string) {
	s.sendMsg(email, notify.TrialStarted(lang, plan, days, time.Now().AddDate(0, 0, days), amount, s.appURL("/mail")))
}

// alertSubscription avisa quem gere a faturação (e o dono) de que a subscrição ficou ativa.
func (s *Server) alertSubscription(ctx context.Context, tid [16]byte, plan, cycle, amount string) {
	to, lang := s.billingContact(ctx, tid)
	s.sendMsg(to, notify.SubscriptionActive(lang, plan, cycle, amount, s.appURL("/mail/admin/billing")))
}

// alertCharge avisa da cobrança (fim do teste ou renovação), com o link da fatura.
func (s *Server) alertCharge(ctx context.Context, tid [16]byte, plan, amount, invoiceURL string) {
	to, lang := s.billingContact(ctx, tid)
	s.sendMsg(to, notify.ChargeStarted(lang, plan, amount, invoiceURL, s.appURL("/mail/admin/billing")))
}

// billingContact devolve para onde vão os avisos de dinheiro: o e-mail de faturação indicado no
// registo e, na falta dele, o dono da organização (que é sempre alguém).
func (s *Server) billingContact(ctx context.Context, tid [16]byte) (email, lang string) {
	if p, err := s.q.GetBillingProfile(ctx, tid); err == nil && p.InvoiceEmail != "" {
		email = p.InvoiceEmail
	}
	if owner, err := s.q.GetTenantOwner(ctx, tid); err == nil {
		lang = owner.Language
		if email == "" {
			email = owner.Email
		}
	}
	return email, lang
}

// shareWithOutsider trata a partilha com alguém de FORA da organização: garante um link público do
// ficheiro e manda-o por e-mail. Sem isto, partilhar com um cliente ou fornecedor não fazia nada —
// a pessoa não tem conta para receber a notificação nem "Partilhado comigo" onde procurar.
//
// Devolve o link público e se o e-mail chegou a ser enviado. O LINK é criado mesmo sem SMTP: é o
// que a pessoa copia e manda à mão — sem ele, partilhar com alguém de fora não produzia nada.
func (s *Server) shareWithOutsider(r *http.Request, f db.StorageFile, actor db.MailUser, email string) (link string, emailed bool) {
	token := s.ensureFileLink(r, f, actor)
	if token == "" {
		return "", false
	}
	link = s.appURL("/p/" + token)
	if s.email == nil {
		log.Printf("share of %q with %s: link created, but e-mail sending is off", f.Name, email)
		return link, false
	}
	who := actor.Name
	if who == "" {
		who = actor.Email
	}
	s.sendMsg(email, notify.FileShared(actor.Language, who, f.Name, link, ""))
	return link, true
}

// ensureFileLink devolve o token do link público do ficheiro, criando um se ainda não existe. O
// link herda a validade por omissão (30 dias) — quem quiser outra coisa muda-a na app.
func (s *Server) ensureFileLink(r *http.Request, f db.StorageFile, actor db.MailUser) string {
	if existing, err := s.q.GetFileLinkByFile(r.Context(), f.ID); err == nil {
		return existing.Token
	}
	token, err := newLinkToken()
	if err != nil {
		return ""
	}
	link, err := s.q.CreateFileLink(r.Context(), db.CreateFileLinkParams{
		TenantID: f.TenantID, FileID: f.ID, Token: token, CreatedBy: actor.ID,
		ExpiresAt: linkExpiry(nil), PasswordHash: "", Permission: "view", AllowDownload: true,
	})
	if err != nil {
		log.Printf("share: create public link for %s: %v", f.ID, err)
		return ""
	}
	return link.Token
}
