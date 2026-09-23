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

// dummyBcryptHash equalizes login time for a nonexistent email (anti-enumeration by timing).
var dummyBcryptHash, _ = auth.HashPassword("manager-login-timing-equalizer")

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Code     string `json:"code"` // TOTP code (empty on the first step)
}

// handleLogin authenticates an operator. TOTP is MANDATORY: without enrollment, a correct password
// returns the setup material (secret + otpauth URI) and NO session is issued until a code from the
// app is confirmed. A single endpoint covers the whole flow:
//
//	password ok + no TOTP + no code     -> 200 {totp_setup:true, secret, otpauth_uri}
//	password ok + no TOTP + code        -> confirms enrollment and issues a session
//	password ok + TOTP active + no code -> 401 manager.totp_code_required
//	password ok + TOTP active + code    -> validates (anti-replay) and issues a session
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
		auth.CheckPassword(dummyBcryptHash, req.Password) // equalizes the response time
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

	// TOTP active: requires and validates the code, with anti-replay per time step.
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

// loginEnroll handles the first login (password already verified): generates or re-presents the
// TOTP secret, and with a valid code confirms enrollment and issues the first session.
func (s *Server) loginEnroll(w http.ResponseWriter, r *http.Request, u db.ManagerUser, code string) {
	if u.TotpSecret == "" || code == "" {
		secret := ""
		if u.TotpSecret != "" {
			// Setup already started (e.g. page reloaded): re-presents the SAME secret.
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

// issueSession creates the session (opaque token), sets the cookie, and responds with the
// operator. Also prunes expired sessions — cheap housekeeping on the login path.
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

// auditUser logs an action for a ManagerUser, outside the session middleware (e.g. login).
func (s *Server) auditUser(ctx context.Context, u db.ManagerUser, action, target string) {
	s.audit(ctx, db.GetManagerSessionRow{ID: u.ID, Email: u.Email}, action, target, nil)
}

func publicUser(u db.ManagerUser) map[string]any {
	return map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role}
}

// EnsureBootstrap creates the FIRST operator from the environment when the table is empty
// (MANAGER_BOOTSTRAP_EMAIL/PASSWORD). Idempotent: with any existing operator, it does nothing.
//
// Access recovery: with MANAGER_BOOTSTRAP_RESET=1, it resets the password of the given email (and
// clears the second factor, so it gets enrolled again) even if operators already exist — and
// creates the operator if it doesn't exist. Without this, changing the password in the environment
// after the first startup had no effect at all, leaving one locked out of the console with no way
// in. The variable must be REMOVED afterwards: while it is set, every startup resets the password.
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
		// Continue on the normal path: if the email didn't exist, the create below handles it.
	}
	n, err := q.CountManagerUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		// With a reset requested and the email still nonexistent, create it anyway — otherwise the
		// reset wouldn't solve the case "the old operator has a different address".
		if os.Getenv("MANAGER_BOOTSTRAP_RESET") != "1" || email == "" || password == "" {
			return nil
		}
		if _, err := q.GetManagerUserByEmail(ctx, email); err == nil {
			return nil // already exists and was just reset
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
