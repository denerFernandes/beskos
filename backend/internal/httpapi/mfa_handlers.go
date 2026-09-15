package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/totp"
)

const (
	mfaChallengeTTL = 5 * time.Minute
	mfaMaxAttempts  = 5
	recoveryCount   = 10
)

// ---- Login em 2 passos ----

// startMFAChallenge emite um desafio após a senha correta. Se SMS ativo, envia o código.
func (s *Server) startMFAChallenge(w http.ResponseWriter, r *http.Request, u db.MailUser) {
	token, err := auth.NewSessionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	smsHash := ""
	methods := []string{}
	if u.MfaTotpEnabled {
		methods = append(methods, "totp")
	}
	if u.MfaSmsEnabled {
		methods = append(methods, "sms")
		code := numericCode(6)
		if h, err := auth.HashPassword(code); err == nil {
			smsHash = h
			_ = s.sms.Send(r.Context(), u.Phone, "Seu código de acesso Beskos: "+code)
		}
	}
	if err := s.q.CreateMFAChallenge(r.Context(), db.CreateMFAChallengeParams{
		Token: token, UserID: u.ID, SmsCodeHash: smsHash, ExpiresAt: time.Now().Add(mfaChallengeTTL),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mfa_required": true, "challenge": token, "methods": methods})
}

type mfaVerifyReq struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

// handleMFAVerify troca um desafio + 2º fator por uma sessão. Aceita TOTP, código SMS ou
// código de recuperação (uso único). Rate limit por desafio (mfaMaxAttempts) contra brute force.
func (s *Server) handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	// Limite por IP além do limite por desafio: sem isto, um atacante com a senha pede um desafio
	// novo a cada 5 palpites e faz brute-force do código de 6 dígitos (auditoria H5/M4).
	if !s.mfaIP.allow("mfa:"+clientIP(r), time.Now()) {
		writeErr(w, http.StatusTooManyRequests, "mfa.too_many")
		return
	}
	var req mfaVerifyReq
	if !decode(w, r, &req) {
		return
	}
	ch, err := s.q.GetMFAChallenge(r.Context(), req.Challenge)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "mfa.invalid_challenge")
		return
	}
	attempts, _ := s.q.IncrMFAChallengeAttempts(r.Context(), req.Challenge)
	if attempts > mfaMaxAttempts {
		_ = s.q.DeleteMFAChallenge(r.Context(), req.Challenge)
		writeErr(w, http.StatusTooManyRequests, "mfa.too_many")
		return
	}
	u, err := s.q.GetUserByID(r.Context(), ch.UserID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "mfa.invalid_challenge")
		return
	}
	if !s.checkSecondFactor(r.Context(), u, ch, req.Code) {
		writeErr(w, http.StatusUnauthorized, "mfa.invalid_code")
		return
	}
	_ = s.q.DeleteMFAChallenge(r.Context(), req.Challenge)
	token, ok := s.issueSession(w, r.Context(), u)
	if !ok {
		return
	}
	// Também aqui: com 2º fator a sessão só nasce depois do código, e é este o momento do login.
	s.alertNewLogin(r, u)
	writeJSON(w, http.StatusOK, map[string]any{"user_id": u.ID, "token": token})
}

// checkSecondFactor tenta TOTP, depois SMS, depois códigos de recuperação (consumindo o usado).
func (s *Server) checkSecondFactor(ctx context.Context, u db.MailUser, ch db.MfaChallenge, code string) bool {
	if u.MfaTotpEnabled && u.TotpSecret != "" {
		if step, ok := totp.NowStep(s.decTOTP(u.TotpSecret), code); ok {
			// Anti-replay: só aceita um passo ainda não usado. SetTOTPLastStep só avança (guarda
			// `$2 > last_totp_step`), então um passo repetido não regrava — mas também não bloqueia
			// aqui se o relógio empatar; a checagem estrita é abaixo.
			if step <= u.LastTotpStep {
				return false // código deste passo (ou anterior) já foi usado
			}
			_ = s.q.SetTOTPLastStep(ctx, db.SetTOTPLastStepParams{ID: u.ID, LastTotpStep: step})
			return true
		}
	}
	if u.MfaSmsEnabled && ch.SmsCodeHash != "" && auth.CheckPassword(ch.SmsCodeHash, code) {
		return true
	}
	// Códigos de recuperação (uso único).
	codes, err := s.q.ListActiveRecoveryCodes(ctx, u.ID)
	if err == nil {
		for _, rc := range codes {
			if auth.CheckPassword(rc.CodeHash, code) {
				_ = s.q.UseRecoveryCode(ctx, rc.ID)
				return true
			}
		}
	}
	return false
}

// ---- Enrollment TOTP ----

func (s *Server) handleTOTPStart(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	secret, err := totp.GenerateSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	// Guarda o segredo CIFRADO em repouso; devolve o claro só agora (para o QR/app autenticador).
	if err := s.q.SetTOTPSecret(r.Context(), db.SetTOTPSecretParams{ID: u.ID, TotpSecret: s.encTOTP(secret)}); err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauth_uri": totp.ProvisioningURI(secret, u.Email)})
}

type codeReq struct {
	Code string `json:"code"`
}

// handleTOTPEnable confirma o TOTP (código do app) e ativa; devolve os códigos de recuperação UMA vez.
func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	fresh, err := s.q.GetUserByID(r.Context(), u.ID)
	if err != nil || fresh.TotpSecret == "" {
		writeErr(w, http.StatusBadRequest, "mfa.not_started")
		return
	}
	var req codeReq
	if !decode(w, r, &req) {
		return
	}
	if !totp.Now(s.decTOTP(fresh.TotpSecret), req.Code) {
		writeErr(w, http.StatusBadRequest, "mfa.invalid_code")
		return
	}
	if err := s.q.EnableTOTP(r.Context(), u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	codes := s.regenRecoveryCodes(w, r.Context(), u.ID)
	if codes == nil {
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "mfa.totp_enabled", Target: ""})
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "recovery_codes": codes})
}

// ---- Enrollment SMS ----

type smsStartReq struct {
	Phone string `json:"phone"`
}

func (s *Server) handleSMSStart(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req smsStartReq
	if !decode(w, r, &req) {
		return
	}
	if req.Phone == "" {
		writeErr(w, http.StatusBadRequest, "mfa.phone_required")
		return
	}
	_ = s.q.SetPhone(r.Context(), db.SetPhoneParams{ID: u.ID, Phone: req.Phone})
	token, _ := auth.NewSessionToken()
	code := numericCode(6)
	hash, _ := auth.HashPassword(code)
	if err := s.q.CreateMFAChallenge(r.Context(), db.CreateMFAChallengeParams{
		Token: token, UserID: u.ID, SmsCodeHash: hash, ExpiresAt: time.Now().Add(mfaChallengeTTL),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "mfa.failed")
		return
	}
	_ = s.sms.Send(r.Context(), req.Phone, "Seu código de verificação Beskos: "+code)
	writeJSON(w, http.StatusOK, map[string]string{"challenge": token})
}

type smsEnableReq struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

func (s *Server) handleSMSEnable(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req smsEnableReq
	if !decode(w, r, &req) {
		return
	}
	ch, err := s.q.GetMFAChallenge(r.Context(), req.Challenge)
	if err != nil || ch.UserID != u.ID {
		writeErr(w, http.StatusUnauthorized, "mfa.invalid_challenge")
		return
	}
	if ch.SmsCodeHash == "" || !auth.CheckPassword(ch.SmsCodeHash, req.Code) {
		writeErr(w, http.StatusBadRequest, "mfa.invalid_code")
		return
	}
	_ = s.q.EnableSMS(r.Context(), u.ID)
	_ = s.q.DeleteMFAChallenge(r.Context(), req.Challenge)
	codes := s.regenRecoveryCodes(w, r.Context(), u.ID)
	if codes == nil {
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "mfa.sms_enabled", Target: ""})
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "recovery_codes": codes})
}

type mfaDisableReq struct {
	Password string `json:"password"`
}

func (s *Server) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req mfaDisableReq
	if !decode(w, r, &req) {
		return
	}
	// Step-up: desativar o 2º fator (e apagar os códigos de recuperação) exige re-autenticar com a
	// senha. Sem isto, uma sessão sequestrada removeria o MFA em silêncio (auditoria M6).
	// Política da organização (admin/security): com MFA obrigatório, desativar é recusado no
	// SERVIDOR também — esconder o botão na UI não é uma barreira (defesa em profundidade).
	if t, terr := s.q.GetTenant(r.Context(), u.TenantID); terr == nil && t.MfaRequired {
		writeErr(w, http.StatusForbidden, "mfa.org_required")
		return
	}
	fresh, err := s.q.GetUserByID(r.Context(), u.ID)
	if err != nil || !s.verifyUserPassword(r.Context(), fresh, req.Password) {
		writeErr(w, http.StatusForbidden, "auth.invalid_credentials")
		return
	}
	_ = s.q.DisableMFA(r.Context(), u.ID)
	_ = s.q.DeleteRecoveryCodes(r.Context(), u.ID)
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "mfa.disabled", Target: ""})
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// ---- helpers ----

// regenRecoveryCodes substitui os códigos de recuperação e devolve os novos EM CLARO (só aqui).
func (s *Server) regenRecoveryCodes(w http.ResponseWriter, ctx context.Context, userID uuid.UUID) []string {
	_ = s.q.DeleteRecoveryCodes(ctx, userID)
	out := make([]string, 0, recoveryCount)
	for i := 0; i < recoveryCount; i++ {
		c := recoveryCode()
		hash, err := auth.HashPassword(c)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "mfa.failed")
			return nil
		}
		if err := s.q.InsertRecoveryCode(ctx, db.InsertRecoveryCodeParams{UserID: userID, CodeHash: hash}); err != nil {
			writeErr(w, http.StatusInternalServerError, "mfa.failed")
			return nil
		}
		out = append(out, c)
	}
	return out
}

// numericCode gera um código numérico de n dígitos (crypto/rand), sem viés de módulo: rejeita os
// bytes ≥ 250 (250 = 25×10), garantindo distribuição uniforme dos 10 dígitos.
func numericCode(n int) string {
	out := make([]byte, n)
	buf := make([]byte, 1)
	for i := 0; i < n; {
		if _, err := rand.Read(buf); err != nil {
			continue
		}
		if buf[0] >= 250 {
			continue // descarta para não enviesar (256 não é múltiplo de 10)
		}
		out[i] = '0' + buf[0]%10
		i++
	}
	return string(out)
}

var b32nopad = base32.StdEncoding.WithPadding(base32.NoPadding)

// recoveryCode gera um código de recuperação legível (ex.: "K3M7-Q9AZ").
func recoveryCode() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	s := b32nopad.EncodeToString(b)
	return fmt.Sprintf("%s-%s", s[:4], s[4:8])
}
