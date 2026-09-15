package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Sync de diretórios — M2 (upload local → nuvem), autenticado pelo token de dispositivo. Escreve
// SEMPRE de forma segura:
//   - PUT exige If-Match (hash base que o daemon conhecia). Se o servidor mudou entretanto (hash
//     diferente) → 412 Precondition Failed: é um conflito, o daemon resolve sem sobrescrever.
//   - antes de sobrescrever, o estado corrente é fotografado no histórico de versões (nunca perde).
//   - cota, dedup por hash e isolamento por tenant/dono preservados (leis §5).

// hashToTemp escoa o corpo para um ficheiro temporário calculando o SHA-256 ao mesmo tempo (sem
// carregar o ficheiro inteiro em memória). Devolve o temp (rebobinado), o hash hex e o tamanho.
func hashToTemp(r io.Reader) (*os.File, string, int64, error) {
	tmp, err := os.CreateTemp("", "mailin-sync-*")
	if err != nil {
		return nil, "", 0, err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, "", 0, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, "", 0, err
	}
	return tmp, hex.EncodeToString(h.Sum(nil)), size, nil
}

// putBlobWithQuota grava o blob (dedup por tenant/hash) respeitando a cota. Devolve a blobKey.
func (s *Server) putBlobWithQuota(r *http.Request, mu db.MailUser, hash string, size int64, body io.Reader) (string, int, string) {
	blobKey := mu.TenantID.String() + "/" + hash
	exists, err := s.storage.Exists(r.Context(), blobKey)
	if err != nil {
		return "", http.StatusBadGateway, "storage.backend"
	}
	if !exists {
		plan := s.tenantPlan(r, mu.TenantID)
		if used, _ := s.q.SumStorageBytes(r.Context(), mu.TenantID); used+size > plan.MaxStorageBytes {
			return "", http.StatusPaymentRequired, "quota.storage"
		}
		if err := s.storage.Put(r.Context(), blobKey, body, size, "application/octet-stream"); err != nil {
			return "", http.StatusBadGateway, "storage.upload_failed"
		}
	}
	return blobKey, 0, ""
}

// handleSyncUpdateFile: PUT /api/sync/files/{id} — atualiza o conteúdo de um ficheiro existente com
// controlo de concorrência otimista (If-Match). Corpo = bytes crus.
func (s *Server) handleSyncUpdateFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	// If-Match é obrigatório: sem uma base conhecida, um upload cego poderia perder a versão do
	// servidor. Ausente → 428 Precondition Required.
	base := r.Header.Get("If-Match")
	if base == "" {
		writeErr(w, http.StatusPreconditionRequired, "sync.if_match_required")
		return
	}
	cur, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	// O servidor mudou desde a base do daemon → conflito. O daemon trata (guarda cópia de conflito).
	if cur.Hash != base {
		writeErr(w, http.StatusPreconditionFailed, "sync.conflict")
		return
	}

	tmp, hash, size, err := hashToTemp(http.MaxBytesReader(w, r.Body, s.maxUploadLimit()))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "storage.too_large")
		return
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()

	// Sem alteração real (mesmo conteúdo) → no-op idempotente.
	if hash == cur.Hash {
		writeJSON(w, http.StatusOK, cur)
		return
	}

	blobKey, code, ecode := s.putBlobWithQuota(r, mu, hash, size, tmp)
	if code != 0 {
		writeErr(w, code, ecode)
		return
	}
	// Fotografa o estado corrente antes de sobrescrever (histórico navegável, nunca perde).
	_ = s.q.SnapshotFileVersion(r.Context(), db.SnapshotFileVersionParams{
		FileID: cur.ID, TenantID: mu.TenantID, BlobKey: cur.BlobKey, Hash: cur.Hash,
		Size: cur.Size, Version: cur.Version, CreatedBy: pgtype.UUID{Bytes: mu.ID, Valid: true},
	})
	rec, err := s.q.UpdateStorageFileContent(r.Context(), db.UpdateStorageFileContentParams{
		ID: cur.ID, TenantID: mu.TenantID, BlobKey: blobKey, Hash: hash, Size: size,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleSyncCreateFile: POST /api/sync/files?name=<nome>&folder_id=<uuid> — cria um novo ficheiro a
// partir do corpo (bytes crus). Serve os ficheiros novos que aparecem na pasta local.
func (s *Server) handleSyncCreateFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	name := r.URL.Query().Get("name")
	if clean := sanitizeUploadName(name); clean != "" {
		name = clean
	} else {
		writeErr(w, http.StatusBadRequest, "files.invalid_name")
		return
	}
	var folderID pgtype.UUID
	if fid := r.URL.Query().Get("folder_id"); fid != "" {
		if pid, err := uuid.Parse(fid); err == nil {
			folderID = pgtype.UUID{Bytes: pid, Valid: true}
		}
	}

	tmp, hash, size, err := hashToTemp(http.MaxBytesReader(w, r.Body, s.maxUploadLimit()))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "storage.too_large")
		return
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()

	blobKey, code, ecode := s.putBlobWithQuota(r, mu, hash, size, tmp)
	if code != 0 {
		writeErr(w, code, ecode)
		return
	}
	prevMax, _ := s.q.MaxVersionByName(r.Context(), db.MaxVersionByNameParams{TenantID: mu.TenantID, Name: name})
	rec, err := s.q.CreateStorageFile(r.Context(), db.CreateStorageFileParams{
		TenantID: mu.TenantID, OwnerUserID: ownerPg(mu),
		Name: name, BlobKey: blobKey, Hash: hash, Size: size, Version: prevMax + 1, FolderID: folderID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

// sanitizeUploadName garante um nome de ficheiro seguro (um só componente, sem separadores nem ".."),
// espelhando a defesa do daemon do lado do servidor.
func sanitizeUploadName(name string) string {
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, 0) {
		return ""
	}
	return name
}
