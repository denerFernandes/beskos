package httpapi

import (
	"fmt"
	"io"
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// handleListTrash lista os ficheiros na lixeira do dono (restauráveis).
func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	files, err := s.q.ListTrashedFiles(r.Context(), ownerPg(mu))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	if files == nil {
		files = []db.StorageFile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "owner": map[string]string{"name": mu.Name, "email": mu.Email}})
}

// handleRestoreFile tira o ficheiro da lixeira (volta a ser ativo).
func (s *Server) handleRestoreFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	f, err := s.q.RestoreFile(r.Context(), db.RestoreFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handlePurgeFile apaga um ficheiro de vez (só faz sentido a partir da lixeira) e liberta o blob se
// nenhum outro registo (dedup/versão) ainda o referencia. Irreversível.
func (s *Server) handlePurgeFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	s.purgeFileAndBlobs(r, mu, f)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// purgeFileAndBlobs apaga o ficheiro de vez e liberta os blobs (o do ficheiro e os das versões) que
// NENHUM outro registo referencia. Recolhe os blobs das versões ANTES do delete (o cascade some com
// as linhas), e usa CountBlobRefsAll para não apagar conteúdo ainda partilhado por dedup.
func (s *Server) purgeFileAndBlobs(r *http.Request, mu db.MailUser, f db.StorageFile) {
	cands := map[string]bool{f.BlobKey: true}
	if vblobs, err := s.q.ListVersionBlobs(r.Context(), db.ListVersionBlobsParams{FileID: f.ID, TenantID: f.TenantID}); err == nil {
		for _, b := range vblobs {
			cands[b] = true
		}
	}
	if err := s.q.DeleteStorageFile(r.Context(), db.DeleteStorageFileParams{ID: f.ID, OwnerUserID: ownerPg(mu)}); err != nil {
		return
	}
	for b := range cands {
		if n, _ := s.q.CountBlobRefsAll(r.Context(), db.CountBlobRefsAllParams{TenantID: f.TenantID, BlobKey: b}); n == 0 {
			_ = s.storage.Remove(r.Context(), b)
		}
	}
}

// handleEmptyTrash purga TODOS os ficheiros na lixeira do dono, libertando os blobs órfãos.
func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	files, err := s.q.ListTrashedFiles(r.Context(), ownerPg(mu))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	for _, f := range files {
		s.purgeFileAndBlobs(r, mu, f)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "emptied", "count": len(files)})
}

// handleListVersions lista o histórico de versões de um ficheiro (mais recente primeiro).
func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	vs, err := s.q.ListFileVersions(r.Context(), db.ListFileVersionsParams{FileID: f.ID, TenantID: f.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"current_version": f.Version, "versions": orEmpty(vs)})
}

// handleRestoreVersion volta o ficheiro ao conteúdo de uma versão antiga. Fotografa a versão corrente
// antes (o restaurar é, ele próprio, uma nova versão — reversível).
func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	vid, err := uuid.Parse(chi.URLParam(r, "versionId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	v, err := s.q.GetFileVersion(r.Context(), db.GetFileVersionParams{ID: vid, FileID: f.ID, TenantID: f.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	_ = s.q.SnapshotFileVersion(r.Context(), db.SnapshotFileVersionParams{
		FileID: f.ID, TenantID: f.TenantID, BlobKey: f.BlobKey, Hash: f.Hash, Size: f.Size, Version: f.Version,
	})
	updated, err := s.q.UpdateStorageFileContent(r.Context(), db.UpdateStorageFileContentParams{
		ID: f.ID, TenantID: f.TenantID, BlobKey: v.BlobKey, Hash: v.Hash, Size: v.Size,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDownloadVersion serve o conteúdo de uma versão antiga (para inspecionar antes de restaurar).
func (s *Server) handleDownloadVersion(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	vid, err := uuid.Parse(chi.URLParam(r, "versionId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	v, err := s.q.GetFileVersion(r.Context(), db.GetFileVersionParams{ID: vid, FileID: f.ID, TenantID: f.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	obj, err := s.storage.Get(r.Context(), v.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	// Versão antiga: sempre download (nunca inline), com nome sufixado pela versão.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("v%d-%s", v.Version, f.Name)))
	_, _ = io.Copy(w, obj)
}
