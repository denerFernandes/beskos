package httpapi

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// ORGANIZATION-level security policy (admin/security): MFA required for the whole tenant, with
// the METHOD chosen by the admin ('any' = the user chooses; 'totp'/'sms' = only that one counts).
// Turning the policy ON kills every session in the tenant (except the admin's who enabled it):
// everyone re-enters and hits the webmail enrollment gate.

func (s *Server) handleGetSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	t, err := s.q.GetTenant(r.Context(), u.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "security.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mfa_required": t.MfaRequired, "mfa_method": t.MfaMethod})
}

func (s *Server) handleSetSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req struct {
		MFARequired bool   `json:"mfa_required"`
		MFAMethod   string `json:"mfa_method"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.MFAMethod == "" {
		req.MFAMethod = "any"
	}
	if req.MFAMethod != "any" && req.MFAMethod != "totp" && req.MFAMethod != "sms" {
		writeErr(w, http.StatusBadRequest, "security.invalid_method")
		return
	}
	if err := s.q.SetTenantMFAPolicy(r.Context(), db.SetTenantMFAPolicyParams{
		ID: u.TenantID, MfaRequired: req.MFARequired, MfaMethod: req.MFAMethod,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "security.failed")
		return
	}
	action := "security.mfa_required_off"
	if req.MFARequired {
		action = "security.mfa_required_on"
		// Drops every session in the tenant (except the current one): next access = login + gate.
		_ = s.q.DeleteTenantWebmailSessions(r.Context(), db.DeleteTenantWebmailSessionsParams{
			TenantID: u.TenantID, KeepToken: sessionToken(r),
		})
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email,
		Action: action, Target: req.MFAMethod,
	})
	writeJSON(w, http.StatusOK, map[string]any{"mfa_required": req.MFARequired, "mfa_method": req.MFAMethod})
}

// mfaSatisfied says whether the user meets the policy (method required by the tenant).
func mfaSatisfied(method string, totpOn, smsOn bool) bool {
	switch method {
	case "totp":
		return totpOn
	case "sms":
		return smsOn
	default: // any
		return totpOn || smsOn
	}
}
