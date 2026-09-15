package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Sync de diretórios — espelho de subpastas. O daemon precisa da árvore de pastas do utilizador para
// colocar cada ficheiro no sítio certo e para criar as pastas em falta ao subir. Escopo: pastas do
// próprio dono (leis §5).

type syncFolder struct {
	ID       uuid.UUID  `json:"id"`
	ParentID *uuid.UUID `json:"parent_id"` // null = raiz
	Name     string     `json:"name"`
}

func toSyncFolder(f db.StorageFolder) syncFolder {
	sf := syncFolder{ID: f.ID, Name: f.Name}
	if f.ParentID.Valid {
		pid := uuid.UUID(f.ParentID.Bytes)
		sf.ParentID = &pid
	}
	return sf
}

// handleListSyncFolders: GET /api/sync/folders — toda a árvore de pastas do dono (o daemon monta os
// caminhos em memória).
func (s *Server) handleListSyncFolders(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListAllFoldersByOwner(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.folders_failed")
		return
	}
	out := make([]syncFolder, 0, len(rows))
	for _, f := range rows {
		out = append(out, toSyncFolder(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}

// handleSyncTrashFile: POST /api/sync/files/{id}/trash — o ficheiro foi apagado na pasta local.
// Vai para a LIXEIRA (soft delete, recuperável), nunca hard delete: um engano local não destrói o
// original. O utilizador esvazia a lixeira no webmail se quiser mesmo apagar.
func (s *Server) handleSyncTrashFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	rec, err := s.q.TrashFile(r.Context(), db.TrashFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleSyncMoveFile: POST /api/sync/files/{id}/move?folder_id=<uuid|vazio>&name=<nome> — o ficheiro
// foi movido/renomeado na pasta local. `folder_id` vazio = raiz.
func (s *Server) handleSyncMoveFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	name := sanitizeUploadName(r.URL.Query().Get("name"))
	if name == "" {
		writeErr(w, http.StatusBadRequest, "files.invalid_name")
		return
	}
	var folder pgtype.UUID
	if fid := r.URL.Query().Get("folder_id"); fid != "" {
		pid, perr := uuid.Parse(fid)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "files.invalid_id")
			return
		}
		// A pasta destino tem de ser do próprio utilizador.
		if _, gerr := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: pid, OwnerUserID: mu.ID}); gerr != nil {
			writeErr(w, http.StatusNotFound, "files.folder_not_found")
			return
		}
		folder = pgtype.UUID{Bytes: pid, Valid: true}
	}
	rec, err := s.q.MoveRenameStorageFile(r.Context(), db.MoveRenameStorageFileParams{
		ID: id, OwnerUserID: ownerPg(mu), FolderID: folder, Name: name,
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleCreateSyncFolder: POST /api/sync/folders?name=<nome>&parent_id=<uuid> — cria uma pasta. O
// daemon só chama isto para os segmentos que ainda não existem (reusa a árvore que já leu).
func (s *Server) handleCreateSyncFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	name := sanitizeUploadName(r.URL.Query().Get("name"))
	if name == "" {
		writeErr(w, http.StatusBadRequest, "files.invalid_name")
		return
	}
	var parent pgtype.UUID
	if pid := r.URL.Query().Get("parent_id"); pid != "" {
		id, err := uuid.Parse(pid)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "files.invalid_id")
			return
		}
		// A pasta-pai TEM de ser do próprio utilizador — senão dava para pendurar conteúdo na árvore
		// de outro tenant.
		if _, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: id, OwnerUserID: mu.ID}); err != nil {
			writeErr(w, http.StatusNotFound, "files.folder_not_found")
			return
		}
		parent = pgtype.UUID{Bytes: id, Valid: true}
	}
	f, err := s.q.CreateFolder(r.Context(), db.CreateFolderParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID, ParentID: parent, Name: name,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.folder_create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, toSyncFolder(f))
}
