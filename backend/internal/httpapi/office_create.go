package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

//go:embed templates/empty.docx templates/empty.xlsx templates/empty.pptx
var officeTemplates embed.FS

// tipo de documento novo → (extensão, arquivo de template embutido).
var newDocKinds = map[string]struct{ ext, tmpl string }{
	"document":     {"docx", "templates/empty.docx"},
	"spreadsheet":  {"xlsx", "templates/empty.xlsx"},
	"presentation": {"pptx", "templates/empty.pptx"},
}

type createOfficeReq struct {
	Kind     string `json:"kind"` // document | spreadsheet | presentation
	Name     string `json:"name"`
	FolderID string `json:"folder_id"`
}

// handleCreateOfficeFile cria um documento de office EM BRANCO a partir de um template embutido,
// grava no MinIO e registra o arquivo. O frontend então abre o editor (Collabora/OnlyOffice).
func (s *Server) handleCreateOfficeFile(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	var req createOfficeReq
	if !decode(w, r, &req) {
		return
	}
	kind, ok := newDocKinds[req.Kind]
	if !ok {
		writeErr(w, http.StatusBadRequest, "files.invalid_kind")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "files.name_required")
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), "."+kind.ext) {
		name += "." + kind.ext
	}

	data, err := officeTemplates.ReadFile(kind.tmpl)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.template_missing")
		return
	}
	mu := mailUserFrom(r.Context())

	var folderID pgtype.UUID
	if req.FolderID != "" {
		if id, err := uuid.Parse(req.FolderID); err == nil {
			folderID = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	rec, code := s.storeUserFile(r.Context(), mu, name, data, folderID)
	if code != "" {
		writeErr(w, storageStatusFor(code), code)
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

// storeUserFile grava um blob no armazenamento do utilizador e regista o ficheiro. Partilhado entre
// a criação de ficheiros em branco (interface) e a geração pelo assistente (docs.create): a lógica
// de cota, dedup por hash e registo é a mesma, e duas cópias divergiriam em silêncio (§5).
//
// Devolve um error_code em vez de escrever na resposta, para servir também quem NÃO tem um
// http.ResponseWriter — como uma ferramenta do assistente.
func (s *Server) storeUserFile(ctx context.Context, mu db.MailUser, name string, data []byte, folderID pgtype.UUID) (db.StorageFile, string) {
	if s.storage == nil {
		return db.StorageFile{}, "storage.disabled"
	}
	// Cota de disco (só conta se o blob for novo — dedup por hash).
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	blobKey := mu.TenantID.String() + "/" + hash
	exists, err := s.storage.Exists(ctx, blobKey)
	if err != nil {
		return db.StorageFile{}, "storage.backend"
	}
	if !exists {
		plan := s.tenantPlanByID(ctx, mu.TenantID)
		if used, _ := s.q.SumStorageBytes(ctx, mu.TenantID); used+int64(len(data)) > plan.MaxStorageBytes {
			return db.StorageFile{}, "quota.storage"
		}
		if err := s.storage.Put(ctx, blobKey, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			return db.StorageFile{}, "storage.upload_failed"
		}
	}
	rec, err := s.q.CreateStorageFile(ctx, db.CreateStorageFileParams{
		TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
		Name: name, BlobKey: blobKey, Hash: hash, Size: int64(len(data)), Version: 1, FolderID: folderID,
	})
	if err != nil {
		return db.StorageFile{}, "storage.record_failed"
	}
	_ = s.q.InsertAudit(ctx, db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: mu.TenantID, Valid: true}, Actor: mu.Email, Action: "file.create", Target: name})
	return rec, ""
}

// storageStatusFor mapeia o error_code para o código HTTP. A cota é 402, o armazenamento em baixo
// é 502, o resto é 500 — a interface distingue "sem espaço" de "avaria" por aqui.
func storageStatusFor(code string) int {
	switch code {
	case "quota.storage":
		return http.StatusPaymentRequired
	case "storage.backend", "storage.upload_failed":
		return http.StatusBadGateway
	case "storage.disabled":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
