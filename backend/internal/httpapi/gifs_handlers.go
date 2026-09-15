package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Biblioteca de GIFs pessoal. O GIF é um ficheiro do Drive (storage_files) — isto só marca quais
// estão na gaveta de quem os guardou. Nada sai para serviços de terceiros.

func (s *Server) handleListGifs(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListGifs(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gifs.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleAddGif(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		FileID string `json:"file_id"`
		Name   string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	fid, err := uuid.Parse(req.FileID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "gifs.invalid_file")
		return
	}
	// Só um ficheiro DELE entra na gaveta dele — nunca confiar no id do request. GetStorageFile
	// filtra por owner: guardar o GIF de outra pessoa na minha gaveta daria acesso ao ficheiro
	// dela quando eu o enviasse.
	f, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: fid, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}})
	if err != nil {
		writeErr(w, http.StatusNotFound, "gifs.file_not_found")
		return
	}
	g, err := s.q.AddGif(r.Context(), db.AddGifParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID, FileID: f.ID,
		Name: strings.TrimSpace(req.Name),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gifs.failed")
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) handleDeleteGif(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "gifs.invalid_id")
		return
	}
	// Tirar da gaveta NÃO apaga o ficheiro do Drive: são coisas diferentes.
	if err := s.q.DeleteGif(r.Context(), db.DeleteGifParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "gifs.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
