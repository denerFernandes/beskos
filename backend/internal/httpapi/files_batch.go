package httpapi

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Cópia/duplicação, download de pasta em zip e ações em lote sobre ficheiros.

// handleCopyFile duplica um ficheiro do dono. O armazenamento é endereçado por conteúdo (hash), então
// a cópia REUTILIZA o mesmo blob — não recopia bytes, não muda o uso de disco. Novo id, mesma pasta.
// O nome vem do frontend (que sabe o locale p/ o sufixo "(cópia)"); vazio = mesmo nome.
func (s *Server) handleCopyFile(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	mu := mailUserFrom(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // corpo opcional
	name := f.Name
	if n := strings.TrimSpace(req.Name); n != "" {
		name = n
	}
	prevMax, _ := s.q.MaxVersionByName(r.Context(), db.MaxVersionByNameParams{TenantID: mu.TenantID, Name: name})
	rec, err := s.q.CreateStorageFile(r.Context(), db.CreateStorageFileParams{
		TenantID: mu.TenantID, OwnerUserID: ownerPg(mu),
		Name: name, BlobKey: f.BlobKey, Hash: f.Hash, Size: f.Size, Version: prevMax + 1, FolderID: f.FolderID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: mu.TenantID, Valid: true}, Actor: mu.Email, Action: "file.copy", Target: name})
	writeJSON(w, http.StatusCreated, rec)
}

// handleZipFolder transmite todos os ficheiros de uma pasta (recursivo) como um .zip. Monta a árvore
// de pastas do dono em memória e faz BFS a partir da raiz, preservando os caminhos relativos.
func (s *Server) handleZipFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	root, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: id, OwnerUserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	all, _ := s.q.ListAllFoldersByOwner(r.Context(), mu.ID)
	childrenOf := map[string][]db.StorageFolder{}
	for _, fo := range all {
		p := ""
		if fo.ParentID.Valid {
			p = uuid.UUID(fo.ParentID.Bytes).String()
		}
		childrenOf[p] = append(childrenOf[p], fo)
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", zipSafe(root.Name)+".zip"))
	zw := zip.NewWriter(w)
	defer zw.Close()

	type node struct{ id, rel string }
	queue := []node{{root.ID.String(), ""}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		fid, _ := uuid.Parse(n.id)
		files, _ := s.q.ListFilesInFolder(r.Context(), db.ListFilesInFolderParams{
			OwnerUserID: ownerPg(mu), FolderID: pgtype.UUID{Bytes: fid, Valid: true},
		})
		for _, file := range files {
			obj, gerr := s.storage.Get(r.Context(), file.BlobKey)
			if gerr != nil {
				continue // blob ausente: pula o ficheiro em vez de quebrar o zip
			}
			if fw, cerr := zw.Create(path.Join(n.rel, file.Name)); cerr == nil {
				_, _ = io.Copy(fw, obj)
			}
			_ = obj.Close()
		}
		for _, c := range childrenOf[n.id] {
			queue = append(queue, node{c.ID.String(), path.Join(n.rel, c.Name)})
		}
	}
}

// zipSafe limpa o nome da pasta para virar nome de ficheiro (sem separadores de caminho).
func zipSafe(name string) string {
	name = strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(strings.TrimSpace(name))
	if name == "" {
		return "folder"
	}
	return name
}

// handleBatchFiles aplica uma ação a vários ficheiros do dono de uma vez: trash | restore | move.
// Cada operação é filtrada por owner_user_id (multi-tenant), então ids de fora simplesmente não casam.
func (s *Server) handleBatchFiles(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		IDs      []string `json:"ids"`
		Action   string   `json:"action"`
		FolderID string   `json:"folder_id"` // p/ "move" ("" = raiz)
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "storage.batch_empty")
		return
	}
	if req.Action != "trash" && req.Action != "restore" && req.Action != "move" {
		writeErr(w, http.StatusBadRequest, "storage.bad_action")
		return
	}
	var dest pgtype.UUID
	if req.Action == "move" && req.FolderID != "" {
		fid, err := uuid.Parse(req.FolderID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "storage.invalid_id")
			return
		}
		if _, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: fid, OwnerUserID: mu.ID}); err != nil {
			writeErr(w, http.StatusNotFound, "storage.not_found") // pasta destino não é do dono
			return
		}
		dest = pgtype.UUID{Bytes: fid, Valid: true}
	}
	done := 0
	for _, raw := range req.IDs {
		fid, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		switch req.Action {
		case "trash":
			if _, err := s.q.TrashFile(r.Context(), db.TrashFileParams{ID: fid, OwnerUserID: ownerPg(mu)}); err == nil {
				done++
			}
		case "restore":
			if _, err := s.q.RestoreFile(r.Context(), db.RestoreFileParams{ID: fid, OwnerUserID: ownerPg(mu)}); err == nil {
				done++
			}
		case "move":
			if _, err := s.q.MoveStorageFile(r.Context(), db.MoveStorageFileParams{ID: fid, OwnerUserID: ownerPg(mu), FolderID: dest}); err == nil {
				done++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"done": done})
}
