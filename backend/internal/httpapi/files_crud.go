package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultMaxUploadBytes = 100 << 30 // 100 GiB (default se FILES_MAX_UPLOAD_BYTES não estiver setado)

// maxUploadLimit é o teto por ficheiro (configurável via env; a cota total do plano vale por cima).
func (s *Server) maxUploadLimit() int64 {
	if s.maxUpload > 0 {
		return s.maxUpload
	}
	return defaultMaxUploadBytes
}

// handleFileLimits expõe o teto por ficheiro ao frontend (para o aviso antes de um upload inviável).
func (s *Server) handleFileLimits(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int64{"max_upload_bytes": s.maxUploadLimit()})
}

func (s *Server) storageReady(w http.ResponseWriter) bool {
	if s.storage == nil {
		writeErr(w, http.StatusServiceUnavailable, "storage.disabled")
		return false
	}
	return true
}

// handleUploadFile recebe um arquivo (multipart), dedup por hash (não reenvia conteúdo idêntico),
// versiona por nome e grava o registro lógico. Isolamento por tenant via prefixo da chave.
func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadLimit())
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "storage.too_large")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.no_file")
		return
	}
	defer file.Close()

	// Hash do conteúdo (dedup) — depois rebobina para o upload.
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.hash_failed")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.seek_failed")
		return
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	blobKey := mu.TenantID.String() + "/" + hash // prefixo por tenant (isolamento)

	// Dedup: só envia ao MinIO se o blob ainda não existe.
	exists, err := s.storage.Exists(r.Context(), blobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.backend")
		return
	}
	// Cota de disco: só conta se o blob for novo (dedup não consome espaço adicional).
	if !exists {
		plan := s.tenantPlan(r, mu.TenantID)
		if used, _ := s.q.SumStorageBytes(r.Context(), mu.TenantID); used+header.Size > plan.MaxStorageBytes {
			writeErr(w, http.StatusPaymentRequired, "quota.storage")
			return
		}
	}
	if !exists {
		ct := header.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		if err := s.storage.Put(r.Context(), blobKey, file, header.Size, ct); err != nil {
			writeErr(w, http.StatusBadGateway, "storage.upload_failed")
			return
		}
	}

	// Pasta destino (opcional): campo folder_id do form.
	var folderID pgtype.UUID
	if fid := r.FormValue("folder_id"); fid != "" {
		if id, err := uuid.Parse(fid); err == nil {
			folderID = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	// Versionamento por nome (mesmo nome → nova versão).
	prevMax, _ := s.q.MaxVersionByName(r.Context(), db.MaxVersionByNameParams{TenantID: mu.TenantID, Name: header.Filename})
	rec, err := s.q.CreateStorageFile(r.Context(), db.CreateStorageFileParams{
		TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Name: header.Filename, BlobKey: blobKey, Hash: hash, Size: header.Size, Version: prevMax + 1, FolderID: folderID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: mu.TenantID, Valid: true}, Actor: mu.Email, Action: "file.upload", Target: header.Filename})
	writeJSON(w, http.StatusCreated, rec)
}

func ownerPg(mu db.MailUser) pgtype.UUID { return pgtype.UUID{Bytes: mu.ID, Valid: true} }

// handleListFiles lista a pasta atual (?folder=<id>, ausente = raiz): subpastas + arquivos + trilha.
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var folderID pgtype.UUID
	if fid := r.URL.Query().Get("folder"); fid != "" {
		if id, err := uuid.Parse(fid); err == nil {
			folderID = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	folders, err := s.q.ListFolders(r.Context(), db.ListFoldersParams{OwnerUserID: mu.ID, ParentID: folderID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	files, err := s.q.ListFilesInFolder(r.Context(), db.ListFilesInFolderParams{OwnerUserID: ownerPg(mu), FolderID: folderID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	if folders == nil { // nunca null no JSON (o frontend faz .length)
		folders = []db.StorageFolder{}
	}
	if files == nil {
		files = []db.StorageFile{}
	}
	// Trilha (breadcrumb): sobe até a raiz.
	type crumb struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	trail := []crumb{}
	cur := folderID
	for cur.Valid {
		f, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: cur.Bytes, OwnerUserID: mu.ID})
		if err != nil {
			break
		}
		trail = append([]crumb{{ID: f.ID, Name: f.Name}}, trail...)
		cur = f.ParentID
	}

	// IDs de arquivos com compartilhamento (para o selo "compartilhado" na UI).
	shared := []uuid.UUID{}
	if ids, err := s.q.SharedFileIDs(r.Context(), mu.TenantID); err == nil {
		shared = ids
	}
	// Uso de armazenamento vs. cota do plano (indicador estilo Drive).
	plan := s.tenantPlan(r, mu.TenantID)
	used, _ := s.q.SumStorageBytes(r.Context(), mu.TenantID)

	writeJSON(w, http.StatusOK, map[string]any{
		"folders":    folders,
		"files":      files,
		"breadcrumb": trail,
		"shared":     shared,
		"owner":      map[string]string{"name": mu.Name, "email": mu.Email},
		"quota":      map[string]int64{"used": used, "max": plan.MaxStorageBytes},
	})
}

// likePattern transforma o texto do utilizador num padrão LIKE, escapando os curingas para que '%'
// e '_' sejam tratados como texto literal. Partilhado pela pesquisa da interface e pela ferramenta
// drive.search do assistente — duas cópias divergiriam e uma delas passaria a dar resultados
// errados em silêncio.
func likePattern(q string) string {
	esc := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q)
	return "%" + esc + "%"
}

// handleSearchFiles: busca global (recursiva) por nome, em todas as pastas do dono. Substitui o
// filtro client-side que só via a pasta aberta. Casa ficheiros e pastas; devolve a lista plana.
func (s *Server) handleSearchFiles(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"files": []db.StorageFile{}, "folders": []db.StorageFolder{}})
		return
	}
	pattern := likePattern(q)
	files, err := s.q.SearchStorageFiles(r.Context(), db.SearchStorageFilesParams{OwnerUserID: ownerPg(mu), Name: pattern})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.search_failed")
		return
	}
	folders, err := s.q.SearchStorageFolders(r.Context(), db.SearchStorageFoldersParams{OwnerUserID: mu.ID, Name: pattern})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.search_failed")
		return
	}
	if files == nil {
		files = []db.StorageFile{}
	}
	if folders == nil {
		folders = []db.StorageFolder{}
	}
	shared := []uuid.UUID{}
	if ids, err := s.q.SharedFileIDs(r.Context(), mu.TenantID); err == nil {
		shared = ids
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files":   files,
		"folders": folders,
		"shared":  shared,
		"owner":   map[string]string{"name": mu.Name, "email": mu.Email},
	})
}

func (s *Server) fileOwned(w http.ResponseWriter, r *http.Request) (db.StorageFile, bool) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return db.StorageFile{}, false
	}
	f, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return db.StorageFile{}, false
	}
	return f, true
}

// fileForDownload resolve o ficheiro para leitura: aceita o dono OU quem recebeu a partilha
// (principal = o meu e-mail). Escreve 404 e devolve false se nenhum dos dois.
func (s *Server) fileForDownload(w http.ResponseWriter, r *http.Request) (db.StorageFile, bool) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return db.StorageFile{}, false
	}
	if f, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)}); err == nil {
		return f, true
	}
	// Não é o dono: aceita se lhe foi partilhado diretamente OU à organização inteira.
	f, err := s.q.GetStorageFileInTenant(r.Context(), db.GetStorageFileInTenantParams{ID: id, TenantID: mu.TenantID})
	if err == nil {
		shares, _ := s.q.ListSharesByFile(r.Context(), db.ListSharesByFileParams{FileID: id, TenantID: mu.TenantID})
		if _, has := shareAccess(shares, mu.Email); has {
			return f, true
		}
	}
	writeErr(w, http.StatusNotFound, "storage.not_found")
	return db.StorageFile{}, false
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	f, ok := s.fileForDownload(w, r)
	if !ok {
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	// nosniff sempre: impede o browser de reinterpretar o octet-stream como HTML/JS.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// inline=1 só é honrado para tipos seguros de visualizar (imagem/pdf/áudio/vídeo). Um .html/.svg
	// servido inline executaria JS no nosso origin (stored XSS) — para esses, força download.
	ct := mime.TypeByExtension("." + extOf(f.Name))
	if r.URL.Query().Get("inline") == "1" && safeInlineType(ct) {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", f.Name))
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Name))
	}
	_, _ = io.Copy(w, obj)
}

// safeInlineType diz se um Content-Type pode ser exibido inline sem risco de execução de script.
func safeInlineType(ct string) bool {
	ct = strings.ToLower(ct)
	if strings.HasPrefix(ct, "image/") && !strings.Contains(ct, "svg") {
		return true // SVG é HTML-capaz: nunca inline
	}
	return strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") ||
		strings.HasPrefix(ct, "application/pdf") || strings.HasPrefix(ct, "text/plain")
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	// Soft delete: vai para a lixeira, não some. O blob fica até a purga (esvaziar lixeira). Sem isto,
	// um clique errado apagava o dado de vez — o maior risco de perda de arquivo (auditoria Files).
	if _, err := s.q.TrashFile(r.Context(), db.TrashFileParams{ID: f.ID, OwnerUserID: ownerPg(mu)}); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "trashed"})
}

// --- Mover / renomear arquivo ---

type patchFileReq struct {
	FolderID *string `json:"folder_id"` // move (null = raiz)
	Name     *string `json:"name"`      // renomear
}

func (s *Server) handlePatchFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	var req patchFileReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil {
		if _, err := s.q.RenameStorageFile(r.Context(), db.RenameStorageFileParams{ID: id, OwnerUserID: ownerPg(mu), Name: *req.Name}); err != nil {
			writeErr(w, http.StatusNotFound, "storage.not_found")
			return
		}
	}
	if req.FolderID != nil {
		var folder pgtype.UUID
		if *req.FolderID != "" {
			if fid, err := uuid.Parse(*req.FolderID); err == nil {
				folder = pgtype.UUID{Bytes: fid, Valid: true}
			}
		}
		if _, err := s.q.MoveStorageFile(r.Context(), db.MoveStorageFileParams{ID: id, OwnerUserID: ownerPg(mu), FolderID: folder}); err != nil {
			writeErr(w, http.StatusNotFound, "storage.not_found")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
