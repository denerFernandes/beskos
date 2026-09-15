package managerapi

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/totp"
)

// dummyBcryptHash iguala o tempo do login com e-mail inexistente (anti-enumeração por timing).
var dummyBcryptHash, _ = auth.HashPassword("manager-login-timing-equalizer")

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Code     string `json:"code"` // código TOTP (vazio no 1º passo)
}

// handleLogin autentica um operador. TOTP é OBRIGATÓRIO (doc §1): sem enrollment, a senha correta
// devolve o material de setup (secret + otpauth URI) e NENHUMA sessão é emitida até um código do
// app ser confirmado. Fluxo num único endpoint:
//
//	senha ok + sem TOTP + sem code  -> 200 {totp_setup:true, secret, otpauth_uri}
//	senha ok + sem TOTP + code      -> confirma enrollment e emite sessão
//	senha ok + TOTP ativo + sem code-> 401 manager.totp_code_required
//	senha ok + TOTP ativo + code    -> valida (anti-replay) e emite sessão
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	if !s.loginIP.allow("login:"+clientIP(r), now) {
		writeErr(w, http.StatusTooManyRequests, "manager.rate_limited")
		return
	}
	var req loginReq
	if !decode(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email != "" && !s.loginUser.allow("login:"+req.Email, now) {
		writeErr(w, http.StatusTooManyRequests, "manager.rate_limited")
		return
	}
	u, err := s.q.GetManagerUserByEmail(r.Context(), req.Email)
	if err != nil {
		auth.CheckPassword(dummyBcryptHash, req.Password) // iguala o tempo de resposta
		writeErr(w, http.StatusUnauthorized, "manager.invalid_credentials")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeErr(w, http.StatusUnauthorized, "manager.invalid_credentials")
		return
	}

	if !u.TotpConfirmed {
		s.loginEnroll(w, r, u, req.Code)
		return
	}

	// TOTP ativo: exige e valida o código, com anti-replay por passo de tempo.
	if req.Code == "" {
		writeErr(w, http.StatusUnauthorized, "manager.totp_code_required")
		return
	}
	secret, derr := s.box.Decrypt(u.TotpSecret)
	if derr != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	step, ok := totp.NowStep(secret, req.Code)
	if !ok || step <= u.LastTotpStep {
		writeErr(w, http.StatusUnauthorized, "manager.invalid_code")
		return
	}
	_ = s.q.SetManagerTOTPLastStep(r.Context(), db.SetManagerTOTPLastStepParams{ID: u.ID, LastTotpStep: step})
	s.issueSession(w, r, u)
}

// loginEnroll trata o 1º login (senha já verificada): gera/reapresenta o segredo TOTP e, com um
// código válido, confirma o enrollment e emite a primeira sessão.
func (s *Server) loginEnroll(w http.ResponseWriter, r *http.Request, u db.ManagerUser, code string) {
	if u.TotpSecret == "" || code == "" {
		secret := ""
		if u.TotpSecret != "" {
			// Setup já iniciado (ex.: página recarregada): reapresenta o MESMO segredo.
			dec, err := s.box.Decrypt(u.TotpSecret)
			if err == nil {
				secret = dec
			}
		}
		if secret == "" {
			gen, err := totp.GenerateSecret()
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "manager.failed")
				return
			}
			enc, err := s.box.Encrypt(gen)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "manager.failed")
				return
			}
			if err := s.q.SetManagerTOTPSecret(r.Context(), db.SetManagerTOTPSecretParams{ID: u.ID, TotpSecret: enc}); err != nil {
				writeErr(w, http.StatusInternalServerError, "manager.failed")
				return
			}
			secret = gen
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"totp_setup": true, "secret": secret, "otpauth_uri": totp.ProvisioningURI(secret, u.Email),
		})
		return
	}
	secret, err := s.box.Decrypt(u.TotpSecret)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	step, ok := totp.NowStep(secret, code)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "manager.invalid_code")
		return
	}
	if err := s.q.ConfirmManagerTOTP(r.Context(), u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	_ = s.q.SetManagerTOTPLastStep(r.Context(), db.SetManagerTOTPLastStepParams{ID: u.ID, LastTotpStep: step})
	s.auditUser(r.Context(), u, "totp_enrolled", "")
	s.issueSession(w, r, u)
}

// issueSession cria a sessão (token opaco), põe o cookie e responde com o operador. Aproveita
// para podar sessões expiradas (housekeeping barato no caminho do login).
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, u db.ManagerUser) {
	token, err := auth.NewSessionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.session_failed")
		return
	}
	_ = s.q.DeleteExpiredManagerSessions(r.Context())
	if err := s.q.InsertManagerSession(r.Context(), db.InsertManagerSessionParams{
		Token: token, ManagerUserID: u.ID, Ip: clientIP(r),
		UserAgent: r.UserAgent(), ExpiresAt: time.Now().Add(sessionTTL),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.session_failed")
		return
	}
	_ = s.q.TouchManagerLastLogin(r.Context(), u.ID)
	s.auditUser(r.Context(), u, "login", "")
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.cookieSecure, MaxAge: int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": publicUser(u)})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := managerUserFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]any{
		"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role,
	}})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	u := managerUserFrom(r.Context())
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.q.DeleteManagerSession(r.Context(), c.Value)
	}
	s.audit(r.Context(), u, "logout", "", nil)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.cookieSecure, MaxAge: -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// auditUser regista uma ação de um ManagerUser (fora do middleware de sessão, ex.: login).
func (s *Server) auditUser(ctx context.Context, u db.ManagerUser, action, target string) {
	s.audit(ctx, db.GetManagerSessionRow{ID: u.ID, Email: u.Email}, action, target, nil)
}

func publicUser(u db.ManagerUser) map[string]any {
	return map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role}
}

// EnsureBootstrap cria o PRIMEIRO operador a partir do ambiente quando a tabela está vazia
// (MANAGER_BOOTSTRAP_EMAIL/PASSWORD). Idempotente: com qualquer operador existente, não faz nada.
//
// Recuperação de acesso: com MANAGER_BOOTSTRAP_RESET=1, repõe a senha do e-mail indicado (e limpa o
// 2.º fator, para se voltar a inscrever) mesmo que já existam operadores — e cria-o se não existir.
// Sem isto, mudar a senha no ambiente depois do primeiro arranque não tinha efeito nenhum e ficava-se
// de fora da consola sem forma de entrar. A variável tem de ser RETIRADA a seguir: enquanto lá
// estiver, cada arranque repõe a senha.
func EnsureBootstrap(ctx context.Context, q *db.Queries, email, password string) error {
	if os.Getenv("MANAGER_BOOTSTRAP_RESET") == "1" && email != "" && password != "" {
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		if err := q.ResetManagerUserCredentials(ctx, db.ResetManagerUserCredentialsParams{
			PasswordHash: hash, Email: strings.ToLower(email),
		}); err != nil {
			return err
		}
		log.Printf("manager bootstrap: RESET applied to %s (2nd factor cleared; remove MANAGER_BOOTSTRAP_RESET now)", email)
		// Segue para o caminho normal: se o e-mail não existia, o create abaixo trata dele.
	}
	n, err := q.CountManagerUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		// Com reset pedido e o e-mail ainda inexistente, cria-o na mesma — senão o reset não
		// resolvia o caso "o operador antigo tem outro endereço".
		if os.Getenv("MANAGER_BOOTSTRAP_RESET") != "1" || email == "" || password == "" {
			return nil
		}
		if _, err := q.GetManagerUserByEmail(ctx, email); err == nil {
			return nil // já existe e acabou de ser reposto
		}
	}
	if email == "" || password == "" {
		log.Printf("WARNING: no manager users exist; set MANAGER_BOOTSTRAP_EMAIL and MANAGER_BOOTSTRAP_PASSWORD to create the first operator")
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	u, err := q.CreateManagerUser(ctx, db.CreateManagerUserParams{
		Email: strings.ToLower(email), Name: "", PasswordHash: hash, Role: "admin",
	})
	if err != nil {
		return err
	}
	log.Printf("manager bootstrap: created first operator %s (TOTP enrollment required at first login)", u.Email)
	return nil
}
