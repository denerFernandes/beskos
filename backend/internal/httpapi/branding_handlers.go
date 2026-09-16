package httpapi

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Organization branding (primary color + logo). Read by any tenant user (for the app's top bar);
// write only by admin/owner (RBAC org.settings). At login, resolved by email.

type brandingResp struct {
	Name          string `json:"name"`
	Color         string `json:"color"`
	Logo          string `json:"logo"`            // data URI, or empty
	LogoWhiteDark bool   `json:"logo_white_dark"` // forces the logo to white in dark theme
}

// handleGetBranding returns the authenticated user's tenant branding.
func (s *Server) handleGetBranding(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	t, err := s.q.GetTenant(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "branding.load_failed")
		return
	}
	writeJSON(w, http.StatusOK, brandingResp{Name: t.Name, Color: t.BrandColor, Logo: t.BrandLogo, LogoWhiteDark: t.BrandLogoWhiteDark})
}

type updateBrandingReq struct {
	Color         string `json:"color"`
	Logo          string `json:"logo"`
	LogoWhiteDark bool   `json:"logo_white_dark"`
}

const maxLogoBytes = 512 * 1024 // ~512 KB data URI — enough for a logo, keeps the row from bloating

// handleUpdateBranding saves the branding (admin/owner). Validates the color and the logo size.
func (s *Server) handleUpdateBranding(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req updateBrandingReq
	if !decode(w, r, &req) {
		return
	}
	if req.Color != "" && !isHexColor(req.Color) {
		writeErr(w, http.StatusBadRequest, "branding.invalid_color")
		return
	}
	if len(req.Logo) > maxLogoBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "branding.logo_too_large")
		return
	}
	if req.Logo != "" && !isDataImage(req.Logo) {
		writeErr(w, http.StatusBadRequest, "branding.invalid_logo")
		return
	}
	if err := s.q.UpdateTenantBranding(r.Context(), db.UpdateTenantBrandingParams{
		ID: mu.TenantID, BrandColor: req.Color, BrandLogo: req.Logo, BrandLogoWhiteDark: req.LogoWhiteDark,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "branding.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleResolveBranding returns the tenant's branding for an email, for the login screen before any
// session exists. An unknown email returns the empty (default) branding, never revealing whether
// the account exists.
func (s *Server) handleResolveBranding(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		writeJSON(w, http.StatusOK, brandingResp{})
		return
	}
	b, err := s.q.GetTenantBrandingByEmail(r.Context(), email)
	if err != nil {
		writeJSON(w, http.StatusOK, brandingResp{}) // unknown -> default, without leaking existence
		return
	}
	writeJSON(w, http.StatusOK, brandingResp{Name: b.Name, Color: b.BrandColor, Logo: b.BrandLogo})
}

func isHexColor(s string) bool {
	if (len(s) != 7 && len(s) != 4) || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func isDataImage(s string) bool {
	return len(s) > 11 && s[:11] == "data:image/"
}
