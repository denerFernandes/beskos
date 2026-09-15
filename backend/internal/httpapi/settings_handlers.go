package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Preferências do utilizador (assinatura, prefs por app). Organizadas por app no JSONB `settings`;
// cada secção grava-se isolada (merge de topo na query), então guardar E-mail não toca no Chat.

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	raw, err := s.q.GetUserSettings(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "settings.failed")
		return
	}
	// raw é json.RawMessage (o JSONB da coluna); devolve-o tal como está.
	w.Header().Set("Content-Type", "application/json")
	if len(raw) == 0 {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_, _ = w.Write(raw)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	// Aceita um objeto {app: {...}} e faz merge. Valida que é JSON de objeto, não um valor solto.
	var patch map[string]json.RawMessage
	if !decode(w, r, &patch) {
		return
	}
	blob, err := json.Marshal(patch)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "settings.invalid")
		return
	}
	if err := s.q.UpdateUserSettings(r.Context(), db.UpdateUserSettingsParams{ID: mu.ID, Settings: blob}); err != nil {
		writeErr(w, http.StatusInternalServerError, "settings.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
