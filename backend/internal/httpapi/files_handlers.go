package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
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
	prevMax, _ := s.q.MaxVersionByName(r.Context(), db.MaxVersionByNameParams{TenantID: mu.TenantID, Nome: header.Filename})
	rec, err := s.q.CreateStorageFile(r.Context(), db.CreateStorageFileParams{
		TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Nome: header.Filename, BlobKey: blobKey, Hash: hash, Tamanho: header.Size, Versao: prevMax + 1, FolderID: folderID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: mu.TenantID, Valid: true}, Actor: mu.Email, Acao: "file.upload", Alvo: header.Filename})
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
	files, err := s.q.SearchStorageFiles(r.Context(), db.SearchStorageFilesParams{OwnerUserID: ownerPg(mu), Nome: pattern})
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
	ct := mime.TypeByExtension("." + extOf(f.Nome))
	if r.URL.Query().Get("inline") == "1" && safeInlineType(ct) {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", f.Nome))
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Nome))
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
	writeJSON(w, http.StatusOK, map[string]any{"current_version": f.Versao, "versions": orEmpty(vs)})
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
		FileID: f.ID, TenantID: f.TenantID, BlobKey: f.BlobKey, Hash: f.Hash, Tamanho: f.Tamanho, Versao: f.Versao,
	})
	updated, err := s.q.UpdateStorageFileContent(r.Context(), db.UpdateStorageFileContentParams{
		ID: f.ID, TenantID: f.TenantID, BlobKey: v.BlobKey, Hash: v.Hash, Tamanho: v.Tamanho,
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
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("v%d-%s", v.Versao, f.Nome)))
	_, _ = io.Copy(w, obj)
}

type shareReq struct {
	Principal string `json:"principal"`
	Nivel     string `json:"nivel"` // owner | editor | leitor | link
}

func (s *Server) handleShareFile(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	var req shareReq
	if !decode(w, r, &req) {
		return
	}
	valid := map[string]bool{"owner": true, "editor": true, "leitor": true, "link": true}
	if req.Principal == "" || !valid[req.Nivel] {
		writeErr(w, http.StatusBadRequest, "storage.invalid_share")
		return
	}
	sh, err := s.q.CreateShare(r.Context(), db.CreateShareParams{
		FileID: f.ID, TenantID: f.TenantID, Principal: req.Principal, Nivel: req.Nivel,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.share_failed")
		return
	}
	// Notifica o destinatário (se for um usuário do mesmo tenant): alimenta a badge de Arquivos.
	if target, terr := s.q.GetMailUserByEmail(r.Context(), req.Principal); terr == nil && target.TenantID == f.TenantID {
		mu := mailUserFrom(r.Context())
		s.notifyUser(r.Context(), f.TenantID, target.ID, "file_share",
			"New shared file", mu.Email+" shared \""+f.Nome+"\" with you", "/mail/files", uuid.Nil,
			map[string]string{"actor": mu.Email, "file": f.Nome})
	}
	writeJSON(w, http.StatusCreated, sh)
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	shares, err := s.q.ListSharesByFile(r.Context(), db.ListSharesByFileParams{FileID: f.ID, TenantID: f.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(shares))
}

func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "shareId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	if err := s.q.DeleteShare(r.Context(), db.DeleteShareParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.share_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// --- Pastas ---

type folderReq struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req folderReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "storage.folder_name_required")
		return
	}
	var parent pgtype.UUID
	if req.ParentID != "" {
		if id, err := uuid.Parse(req.ParentID); err == nil {
			parent = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	f, err := s.q.CreateFolder(r.Context(), db.CreateFolderParams{TenantID: mu.TenantID, OwnerUserID: mu.ID, ParentID: parent, Name: req.Name})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.folder_failed")
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleRenameFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	var req folderReq
	if !decode(w, r, &req) || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "storage.folder_name_required")
		return
	}
	f, err := s.q.RenameFolder(r.Context(), db.RenameFolderParams{ID: id, OwnerUserID: mu.ID, Name: req.Name})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

type moveFolderReq struct {
	ParentID *string `json:"parent_id"` // null/"" = raiz
}

// handleMoveFolder muda o pai de uma pasta. Barra ciclo: não deixa mover para si mesma nem para
// dentro de uma descendente (senão a subárvore some da navegação e vira um laço no banco).
func (s *Server) handleMoveFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	var req moveFolderReq
	if !decode(w, r, &req) {
		return
	}
	var parent pgtype.UUID
	if req.ParentID != nil && *req.ParentID != "" {
		pid, err := uuid.Parse(*req.ParentID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "storage.invalid_id")
			return
		}
		if pid == id {
			writeErr(w, http.StatusBadRequest, "storage.folder_cycle")
			return
		}
		// Sobe do pai proposto até a raiz; se cruzar a própria pasta, é descendente → ciclo.
		cur := pgtype.UUID{Bytes: pid, Valid: true}
		for cur.Valid {
			f, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: cur.Bytes, OwnerUserID: mu.ID})
			if err != nil {
				writeErr(w, http.StatusNotFound, "storage.not_found")
				return
			}
			if f.ID == id {
				writeErr(w, http.StatusBadRequest, "storage.folder_cycle")
				return
			}
			cur = f.ParentID
		}
		parent = pgtype.UUID{Bytes: pid, Valid: true}
	}
	f, err := s.q.MoveFolder(r.Context(), db.MoveFolderParams{ID: id, OwnerUserID: mu.ID, ParentID: parent})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	if err := s.q.DeleteFolder(r.Context(), db.DeleteFolderParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.folder_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
		if _, err := s.q.RenameStorageFile(r.Context(), db.RenameStorageFileParams{ID: id, OwnerUserID: ownerPg(mu), Nome: *req.Name}); err != nil {
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
