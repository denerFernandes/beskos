package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"mailin/backend/internal/db"
)

var errBadCursor = errors.New("cursor inválido")

// Sync de diretórios — M0 (control plane). Feed incremental das mudanças nos ficheiros do próprio
// utilizador, consumido pelo daemon de sincronização (desktop, escrito em Rust). O daemon guarda o
// `cursor` opaco e volta com ?since=<cursor>. Contrato:
//   - `hash` é o ETag do conteúdo (blob content-addressed): igual ⇒ ficheiro inalterado.
//   - `deleted=true` é um tombstone (foi para a lixeira): o cliente remove-o localmente.
//   - A purga (hard delete ao esvaziar a lixeira) NÃO aparece no feed; o cliente reconcilia por
//     varredura completa periódica (o feed acelera o caso comum, não substitui a reconciliação).
// Escopo: ficheiros do dono autenticado, dentro do tenant (multi-tenancy, lei §5.1).

const fileChangesMaxLimit = 500

type fileChange struct {
	ID        uuid.UUID  `json:"id"`
	FolderID  *uuid.UUID `json:"folder_id"` // null = raiz
	Name      string     `json:"name"`
	Hash      string     `json:"hash"` // ETag do conteúdo
	Size      int64      `json:"size"`
	Version   int32      `json:"version"`
	Deleted   bool       `json:"deleted"` // tombstone (na lixeira)
	UpdatedAt time.Time  `json:"updated_at"`
	// false = partilhado comigo (não sou dono): o cliente trata-o como SÓ-LEITURA.
	Owned bool `json:"owned"`
}

func (s *Server) handleListFileChanges(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	sinceTS, sinceID, err := parseChangeCursor(r.URL.Query().Get("since"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.bad_cursor")
		return
	}
	limit := fileChangesMaxLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 && n < limit {
			limit = n
		}
	}

	rows, err := s.q.ListFileChanges(r.Context(), db.ListFileChangesParams{
		TenantID:    mu.TenantID,
		OwnerUserID: ownerPg(mu),
		Principal:   mu.Email, // partilhas são por e-mail: traz também o que partilharam comigo
		UpdatedAt:   sinceTS,
		ID:          sinceID,
		Limit:       int32(limit),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.changes_failed")
		return
	}

	changes := make([]fileChange, 0, len(rows))
	for _, row := range rows {
		c := fileChange{ID: row.ID, Name: row.Nome, Hash: row.Hash, Size: row.Tamanho, Version: row.Versao, UpdatedAt: row.UpdatedAt, Owned: row.Owned}
		if row.FolderID.Valid {
			fid := uuid.UUID(row.FolderID.Bytes)
			c.FolderID = &fid
		}
		c.Deleted = row.DeletedAt.Valid
		changes = append(changes, c)
	}

	// Cursor = o da última linha; sem novidades, ecoa o cursor recebido para o cliente não recuar.
	cursor := r.URL.Query().Get("since")
	if n := len(changes); n > 0 {
		cursor = encodeChangeCursor(changes[n-1].UpdatedAt, changes[n-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"changes":  changes,
		"cursor":   cursor,
		"has_more": len(rows) == limit,
	})
}

// O cursor é opaco para o cliente: "<updated_at RFC3339Nano>|<id>". Vazio = início (época + id nulo),
// o que faz a primeira página trazer tudo por (updated_at > época).
func parseChangeCursor(s string) (time.Time, uuid.UUID, error) {
	if s == "" {
		return time.Time{}, uuid.Nil, nil
	}
	ts, idStr, ok := strings.Cut(s, "|")
	if !ok {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	return t, id, nil
}

func encodeChangeCursor(t time.Time, id uuid.UUID) string {
	return t.UTC().Format(time.RFC3339Nano) + "|" + id.String()
}
