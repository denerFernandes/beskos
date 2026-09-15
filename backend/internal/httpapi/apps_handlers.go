package httpapi

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// toggleableApps são as apps do rail que o admin pode desligar para a organização. As Definições e a
// Administração não são apps do rail (não se desligam). Manter esta lista alinhada com o rail do
// frontend e com assistant.AppTools.
var toggleableApps = []string{"today", "mail", "chat", "kanban", "cal", "files", "docs", "contacts", "meet", "notes", "wiki"}

// handleGetApps devolve as apps alternáveis e as que estão desligadas na organização. Qualquer
// utilizador lê (o rail filtra por isto).
func (s *Server) handleGetApps(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	disabled := s.disabledAppsEffective(r.Context(), u.TenantID) // admin da org + exclusões do plano
	writeJSON(w, http.StatusOK, map[string]any{"apps": toggleableApps, "disabled": orEmpty(disabled)})
}

// handleSetApps grava as apps DESLIGADAS da organização (só admin/owner, gated na rota). Ignora ids
// que não sejam alternáveis — o Correio, por exemplo, nunca se desliga.
func (s *Server) handleSetApps(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disabled []string `json:"disabled"`
	}
	if !decode(w, r, &req) {
		return
	}
	valid := map[string]bool{}
	for _, a := range toggleableApps {
		valid[a] = true
	}
	seen := map[string]bool{}
	clean := []string{}
	for _, a := range req.Disabled {
		if valid[a] && !seen[a] {
			clean = append(clean, a)
			seen[a] = true
		}
	}
	u := mailUserFrom(r.Context())
	if err := s.q.SetTenantDisabledApps(r.Context(), db.SetTenantDisabledAppsParams{ID: u.TenantID, DisabledApps: clean}); err != nil {
		writeErr(w, http.StatusInternalServerError, "apps.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disabled": clean})
}
