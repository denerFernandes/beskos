package httpapi

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
)

// Emojis customizados do workspace (:nome:). A imagem é enviada primeiro por /api/files; aqui só se
// liga o nome ao ficheiro (mesmo padrão do avatar de contato). O frontend renderiza via /thumb.

var emojiName = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)

func (s *Server) handleListEmojis(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	list, err := s.q.ListWorkspaceEmojis(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.emoji_failed")
		return
	}
	if list == nil {
		list = []db.ListWorkspaceEmojisRow{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateEmoji(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Name   string `json:"name"`
		FileID string `json:"file_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.ToLower(strings.TrimSpace(strings.Trim(req.Name, ":")))
	if !emojiName.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "chat.emoji_bad_name") // só [a-z0-9_], 2..32
		return
	}
	fid, err := uuid.Parse(req.FileID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.emoji_bad_file")
		return
	}
	// O ficheiro tem de ser do mesmo tenant (a imagem foi enviada por /api/files).
	if _, err := s.q.GetStorageFileInTenant(r.Context(), db.GetStorageFileInTenantParams{ID: fid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	e, err := s.q.CreateWorkspaceEmoji(r.Context(), db.CreateWorkspaceEmojiParams{
		TenantID: mu.TenantID, Name: name, FileID: fid, CreatedBy: mu.ID,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "chat.emoji_exists") // UNIQUE(tenant, name)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handleDeleteEmoji(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.emoji_bad_file")
		return
	}
	if err := s.q.DeleteWorkspaceEmoji(r.Context(), db.DeleteWorkspaceEmojiParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.emoji_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
