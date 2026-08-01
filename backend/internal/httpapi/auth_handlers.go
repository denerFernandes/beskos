package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/auth"
	"mailin/backend/internal/db"
	"mailin/backend/internal/rbac"
)

type ctxKey string

// dummyBcryptHash é um hash fixo para gastar tempo de bcrypt no login de e-mail inexistente
// (anti-enumeração por timing). Calculado uma vez no arranque.
var dummyBcryptHash, _ = auth.HashPassword("mailin-login-timing-equalizer")

// verifyUserPassword confere a senha na ponta certa: Stalwart quando há caixa, senão o hash local.
// Usado no login e como step-up (re-autenticação) em ações sensíveis (ex.: desativar MFA).
func (s *Server) verifyUserPassword(ctx context.Context, u db.MailUser, password string) bool {
	if password == "" {
		return false
	}
	if u.StalwartAccountID != nil {
		ok, err := s.sw.VerifyUser(ctx, u.Email, password)
		return err == nil && ok
	}
	return auth.CheckPassword(u.PasswordHash, password)
}

type signupReq struct {
	OrgName  string `json:"org_name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleSignup cria tenant + Stripe customer (Stripe fica pra Fase 4) + primeiro admin do tenant,
// e responde na hora. O provisionamento de domínio/mailbox é assíncrono (máquina de estados §7, Fase 1).
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupReq
	if !decode(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" || req.OrgName == "" {
		writeErr(w, http.StatusBadRequest, "auth.missing_fields")
		return
	}

	tenant, err := s.q.CreateTenant(r.Context(), db.CreateTenantParams{Name: req.OrgName, Plan: "trial"})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "tenant.create_failed")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
		return
	}
	u, err := s.q.CreateAccount(r.Context(), db.CreateAccountParams{
		TenantID:     tenant.ID,
		Email:        req.Email,
		PasswordHash: hash,
		Role:         string(rbac.RoleOwner), // primeiro usuário = dono da organização
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "auth.email_taken")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: tenant.ID, Valid: true}, Actor: req.Email, Action: "signup", Target: tenant.ID.String(),
	})
	token, ok := s.issueSession(w, r.Context(), u)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"tenant_id": tenant.ID, "user_id": u.ID, "token": token})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	// Anti brute-force / credential stuffing (auditoria H5): limita por IP e por conta antes de
	// gastar bcrypt ou uma ida ao Stalwart. A senha é checada aqui, então sem isto o login fica
	// aberto a adivinhação online mesmo com MFA (o MFA vem depois da senha).
	if !s.loginIP.allow("login:"+clientIP(r), now) {
		writeErr(w, http.StatusTooManyRequests, "auth.rate_limited")
		return
	}
	var req loginReq
	if !decode(w, r, &req) {
		return
	}
	if req.Email != "" && !s.loginUser.allow("login:"+strings.ToLower(req.Email), now) {
		writeErr(w, http.StatusTooManyRequests, "auth.rate_limited")
		return
	}
	u, err := s.q.GetMailUserByEmail(r.Context(), req.Email)
	if err != nil {
		// Anti-enumeração: gasta um bcrypt "à toa" para igualar o tempo do caminho em que a conta
		// existe (senão um e-mail inexistente responderia mais rápido e vazaria quais existem).
		auth.CheckPassword(dummyBcryptHash, req.Password)
		writeErr(w, http.StatusUnauthorized, "auth.invalid_credentials")
		return
	}
	// A suspended or deleted mailbox has no credentials in Stalwart, so the password check below
	// would fail anyway -- but it would fail as "wrong password", which sends somebody to reset a
	// password that will not help. Say the true thing instead, and say it before touching Stalwart.
	// It also covers accounts with no mailbox, where nothing else would stop a suspended person.
	if u.Status != "active" {
		writeErr(w, http.StatusForbidden, "auth.account_suspended")
		return
	}
	if u.StalwartAccountID != nil {
		// Tem caixa: a credencial é a do Stalwart (o e-mail é a fonte de verdade da senha).
		ok, verr := s.sw.VerifyUser(r.Context(), req.Email, req.Password)
		if verr != nil {
			writeErr(w, http.StatusBadGateway, "webmail.stalwart_unreachable")
			return
		}
		if !ok {
			writeErr(w, http.StatusUnauthorized, "auth.invalid_credentials")
			return
		}
		// Brokering do JMAP (H11): guarda a credencial Basic CIFRADA server-side para o proxy injetar.
		// O browser deixa de guardar a senha da caixa; autentica-se só com a sessão. Feito aqui (senha
		// conhecida), antes do 2º fator — a sessão só é emitida depois, então a cred fica pronta.
		basic := base64.StdEncoding.EncodeToString([]byte(req.Email + ":" + req.Password))
		_ = s.q.SetJmapCred(r.Context(), db.SetJmapCredParams{ID: u.ID, JmapCred: s.encTOTP(basic)})
	} else if !auth.CheckPassword(u.PasswordHash, req.Password) {
		// Sem caixa (organização recém-criada, ainda sem domínio): senha do nosso cadastro.
		// Custo constante: bcrypt já foi calculado; nada a fazer além de responder genérico.
		writeErr(w, http.StatusUnauthorized, "auth.invalid_credentials")
		return
	}
	// Se há MFA, NÃO emite sessão ainda: devolve um desafio para o 2º fator (login em 2 passos).
	if u.MfaTotpEnabled || u.MfaSmsEnabled {
		s.startMFAChallenge(w, r, u)
		return
	}
	token, ok := s.issueSession(w, r.Context(), u)
	if !ok {
		return
	}
	// Aviso de novo início de sessão (quando, de onde, com que aparelho): é o que permite a alguém
	// perceber que lhe roubaram a senha. Best-effort, fora do caminho do login.
	s.alertNewLogin(r, u)
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role,
		"has_mailbox": u.StalwartAccountID != nil, "token": token,
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	disabled := s.disabledAppsEffective(r.Context(), u.TenantID) // admin da org + exclusões do plano (M2)
	// Assistente ligado: a IA tem de estar configurada E o admin não pode tê-lo desligado. Falso em
	// qualquer dos casos → o frontend esconde a bolha do assistente (não adianta oferecer o que recusa).
	assistantEnabled := s.ai != nil && s.agent != nil && !s.assistantDisabled(r.Context(), u.TenantID)
	// Política da organização: MFA obrigatório e o utilizador ainda sem 2º fator → o webmail
	// bloqueia num gate de enrollment até configurar (admin/security do tenant).
	mfaSetupRequired := false
	orgMFARequired := false
	mfaMethod := "any"
	if t, terr := s.q.GetTenant(r.Context(), u.TenantID); terr == nil {
		mfaMethod = t.MfaMethod
		orgMFARequired = t.MfaRequired
		mfaSetupRequired = t.MfaRequired && !mfaSatisfied(t.MfaMethod, u.MfaTotpEnabled, u.MfaSmsEnabled)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": u.ID, "email": u.Email, "role": u.Role, "tenant_id": u.TenantID, "name": u.Name, "avatar": u.Avatar,
		"mfa_setup_required":  mfaSetupRequired,
		"mfa_required_method": mfaMethod,
		"org_mfa_required":    orgMFARequired,
		"language":            u.Language,                                              // "" = automático (browser); senão a preferência salva
		"permissions":         s.effectivePerms(r.Context(), u.TenantID, u.ID, u.Role), // papel ± exceções; a UI gateia botões com isto
		"mfa":                 map[string]bool{"totp": u.MfaTotpEnabled, "sms": u.MfaSmsEnabled},
		"presence":            u.Presence,
		"disabled_apps":       orEmpty(disabled), // o rail some com estas; a IA perde as ferramentas delas
		"assistant_enabled":   assistantEnabled,
		// The organisation's billing state. Here rather than in a call of its own: the warning banner
		// has to be on screen at the first render, not a moment later.
		"account": accountView(s.accountState(r.Context(), u.TenantID)),
		// Whether their own domain is live in DNS. Until it is, no mail arrives and nothing sent from
		// here would pass SPF at the far end -- so the app says so instead of showing an empty inbox.
		"domain": domainView(s.domainPublished(r.Context(), u.Email)),
	})
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	domains, err := s.q.ListDomainsByTenant(r.Context(), u.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "domains.list_failed")
		return
	}
	writeJSON(w, http.StatusOK, domains)
}

// --- helpers de sessão / decode ---

// issueSession cria a sessão, grava o cookie (web same-origin) e DEVOLVE o token para o corpo da
// resposta — o app nativo (Tauri) guarda-o e envia-o em `Authorization: Bearer` (sem depender de
// cookies cross-site). Devolve ("", false) se falhou (já escreveu o erro).
func (s *Server) issueSession(w http.ResponseWriter, ctx context.Context, u db.MailUser) (string, bool) {
	token, err := auth.NewSessionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "auth.session_create_failed")
		return "", false
	}
	if _, err = s.q.CreateWebmailSession(ctx, db.CreateWebmailSessionParams{
		Token: token, MailUserID: u.ID, ExpiresAt: time.Now().Add(sessionTTL),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "auth.session_persist_failed")
		return "", false
	}
	// Cookie: SameSite=None+Secure quando servido por HTTPS (permite webview cross-site); senão Lax
	// (dev em http). O app nativo não depende disto — usa o token do corpo.
	same, secure := http.SameSiteLaxMode, false
	if s.cookieSecure {
		same, secure = http.SameSiteNoneMode, true
	}
	http.SetCookie(w, &http.Cookie{
		Name: "wm_session", Value: token, Path: "/", HttpOnly: true,
		SameSite: same, Secure: secure, MaxAge: int(sessionTTL.Seconds()),
	})
	return token, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "request.invalid_json")
		return false
	}
	return true
}
