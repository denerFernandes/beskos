package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
)

// Sync — upload DELTA (M4): num ficheiro grande que mudou pouco, enviar tudo outra vez é desperdício.
//
// Desenho deliberado: o armazenamento continua a ser **um blob inteiro por ficheiro**
// (content-addressed, com dedup e versões). Não passámos a guardar blocos — isso obrigaria a mexer
// em download, versões, OnlyOffice, zip, miniaturas… Em vez disso o servidor:
//   1. expõe os hashes por bloco do conteúdo ATUAL (`GET .../blocks`);
//   2. aceita só os blocos alterados (`PUT .../delta`) e **reconstrói** o ficheiro completo,
//      copiando o resto do blob antigo.
// Resultado: poupa-se rede sem tocar no resto do sistema.

const (
	defaultBlockSize = 4 << 20  // 4 MiB
	minBlockSize     = 64 << 10 // 64 KiB
	maxBlockSize     = 16 << 20 // 16 MiB
)

// blockSizeParam lê e valida ?block_size=, caindo no default.
func blockSizeParam(r *http.Request) (int64, bool) {
	v := r.URL.Query().Get("block_size")
	if v == "" {
		return defaultBlockSize, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < minBlockSize || n > maxBlockSize {
		return 0, false
	}
	return n, true
}

// handleSyncFileBlocks: GET /api/sync/files/{id}/blocks?block_size=N — hashes SHA-256 por bloco do
// conteúdo atual. O cliente compara com os seus e descobre que blocos precisa mesmo de enviar.
func (s *Server) handleSyncFileBlocks(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	bs, ok := blockSizeParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "sync.bad_block_size")
		return
	}
	f, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()

	hashes := make([]string, 0, 8)
	buf := make([]byte, bs)
	for {
		n, rerr := io.ReadFull(obj, buf)
		if n > 0 {
			sum := sha256.Sum256(buf[:n])
			hashes = append(hashes, hex.EncodeToString(sum[:]))
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			writeErr(w, http.StatusBadGateway, "storage.download_failed")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"block_size": bs,
		"total_size": f.Tamanho,
		"hash":       f.Hash, // hash do ficheiro inteiro: serve de If-Match na escrita
		"hashes":     hashes,
	})
}

// handleSyncUpdateFileDelta: PUT /api/sync/files/{id}/delta?block_size=&total_size=&blocks=1,4,9
// com If-Match. O corpo é a concatenação, POR ORDEM, dos blocos listados em `blocks`. O servidor
// reconstrói o ficheiro: bloco alterado vem do corpo, bloco inalterado é copiado do blob antigo.
func (s *Server) handleSyncUpdateFileDelta(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	base := r.Header.Get("If-Match")
	if base == "" {
		writeErr(w, http.StatusPreconditionRequired, "sync.if_match_required")
		return
	}
	bs, ok := blockSizeParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "sync.bad_block_size")
		return
	}
	totalSize, err := strconv.ParseInt(r.URL.Query().Get("total_size"), 10, 64)
	if err != nil || totalSize < 0 || totalSize > s.maxUploadLimit() {
		writeErr(w, http.StatusBadRequest, "sync.bad_total_size")
		return
	}
	changed, err := parseBlockList(r.URL.Query().Get("blocks"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "sync.bad_block_list")
		return
	}

	cur, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	// O conteúdo tem de ser exatamente aquele sobre o qual o cliente calculou o delta — senão os
	// blocos "inalterados" que vamos copiar já não são os que ele viu.
	if cur.Hash != base {
		writeErr(w, http.StatusPreconditionFailed, "sync.conflict")
		return
	}

	old, err := s.storage.Get(r.Context(), cur.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer old.Close()

	tmp, err := os.CreateTemp("", "mailin-delta-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.temp_failed")
		return
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()

	body := http.MaxBytesReader(w, r.Body, s.maxUploadLimit())
	hasher := sha256.New()
	out := io.MultiWriter(tmp, hasher) // grava e vai calculando o hash final ao mesmo tempo

	if err := rebuildFromDelta(out, old, body, cur.Tamanho, totalSize, bs, changed); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	hash := hex.EncodeToString(hasher.Sum(nil))
	if hash == cur.Hash { // delta não mudou nada: no-op idempotente
		writeJSON(w, http.StatusOK, cur)
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.temp_failed")
		return
	}

	blobKey, code, ecode := s.putBlobWithQuota(r, mu, hash, totalSize, tmp)
	if code != 0 {
		writeErr(w, code, ecode)
		return
	}
	// Histórico: fotografa o estado anterior antes de trocar o conteúdo (igual ao upload normal).
	_ = s.q.SnapshotFileVersion(r.Context(), db.SnapshotFileVersionParams{
		FileID: cur.ID, TenantID: mu.TenantID, BlobKey: cur.BlobKey, Hash: cur.Hash,
		Tamanho: cur.Tamanho, Versao: cur.Versao, CreatedBy: pgtype.UUID{Bytes: mu.ID, Valid: true},
	})
	rec, err := s.q.UpdateStorageFileContent(r.Context(), db.UpdateStorageFileContentParams{
		ID: cur.ID, TenantID: mu.TenantID, BlobKey: blobKey, Hash: hash, Tamanho: totalSize,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// parseBlockList lê "0,3,7" para um conjunto de índices. Vazio = nenhum bloco alterado.
func parseBlockList(v string) (map[int64]bool, error) {
	out := map[int64]bool{}
	if strings.TrimSpace(v) == "" {
		return out, nil
	}
	for _, part := range strings.Split(v, ",") {
		n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || n < 0 {
			return nil, err
		}
		out[n] = true
	}
	return out, nil
}

// rebuildFromDelta escreve o conteúdo NOVO em `out`, bloco a bloco: os alterados vêm de `body`, os
// restantes são copiados de `old`. Isolado da HTTP de propósito — é a parte com risco real de
// correção (ficheiro a crescer/encolher, bloco final parcial), por isso tem testes.
//
// Os dois lados são streams sequenciais, por isso `old` é avançado mesmo nos blocos alterados,
// para se manter alinhado com o índice do bloco.
func rebuildFromDelta(out io.Writer, old io.Reader, body io.Reader, oldSize, totalSize, bs int64, changed map[int64]bool) error {
	var written int64
	for i := int64(0); written < totalSize; i++ {
		want := bs
		if rem := totalSize - written; rem < want {
			want = rem // último bloco é parcial
		}
		// Quanto é que este bloco ocupava no conteúdo antigo (0 se o ficheiro cresceu).
		oldRem := oldSize - i*bs
		if oldRem < 0 {
			oldRem = 0
		}
		if oldRem > bs {
			oldRem = bs
		}

		if changed[i] {
			if _, err := io.CopyN(out, body, want); err != nil {
				return errDeltaShortBody
			}
			if oldRem > 0 {
				if _, err := io.CopyN(io.Discard, old, oldRem); err != nil {
					return errDeltaOldRead
				}
			}
		} else {
			// Bloco inalterado: tem de existir no conteúdo antigo. Se não existe, o cliente
			// enganou-se a marcar os blocos — recusamos em vez de gravar lixo.
			if oldRem < want {
				return errDeltaMissingBlock
			}
			if _, err := io.CopyN(out, old, want); err != nil {
				return errDeltaOldRead
			}
		}
		written += want
	}
	return nil
}

// Erros do delta com o mesmo texto dos error_codes devolvidos ao cliente.
var (
	errDeltaShortBody    = deltaErr("sync.delta_short_body")
	errDeltaMissingBlock = deltaErr("sync.delta_missing_block")
	errDeltaOldRead      = deltaErr("storage.download_failed")
)

type deltaErr string

func (e deltaErr) Error() string { return string(e) }
