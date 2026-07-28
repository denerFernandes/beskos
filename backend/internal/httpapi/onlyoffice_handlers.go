package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
)

const docTokenTTL = 10 * time.Hour

// aiPluginGUID é o plugin de IA nativo do Document Server (autostart no editor autenticado).
const aiPluginGUID = "asc.{9DC93CDB-B576-4F0C-B55E-FCC9C48DD007}"

var errStorageDisabled = errors.New("storage disabled")

// handleEditSession abre uma sessão de edição de office: devolve o api.js do Document Server e o
// config JWT-assinado que o editor consome. Fase A: somente visualização (mode=view).
func (s *Server) handleEditSession(w http.ResponseWriter, r *http.Request) {
	if s.office == nil {
		writeErr(w, http.StatusServiceUnavailable, "files.editing_disabled")
		return
	}
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	f, canWrite, ok := s.resolveDocAccess(w, r, mu)
	if !ok {
		return
	}
	// Edição office por plano (manager M3): plano sem OnlyOffice edit rebaixa para visualização.
	if canWrite && !s.tenantPlanByID(r.Context(), mu.TenantID).OnlyOfficeEdit {
		canWrite = false
	}
	ext := extOf(f.Nome)
	if !officeExts[ext] {
		writeErr(w, http.StatusUnprocessableEntity, "files.not_editable")
		return
	}
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "pt-BR"
	}
	// Tema da UI do editor combinando com o Beskos (claro/escuro). Params grátis (standard branding).
	darkTheme := r.URL.Query().Get("theme") == "dark"
	token := s.office.mintToken(docClaims{
		FileID: f.ID, UserID: mu.ID, TenantID: mu.TenantID, CanWrite: canWrite,
		Exp: time.Now().Add(docTokenTTL),
	})
	base := s.office.filesHostURL
	// key: único por versão (alfanumérico) — muda a cada nova versão para o DS recarregar.
	key := strings.ReplaceAll(uuid.NewSHA1(f.ID, []byte(strconv.Itoa(int(f.Versao)))).String(), "-", "")

	// Enxuga o cromo para integrar ao Beskos. A marca do cabeçalho é nossa (applyBranding), o que
	// exige o patch de branding no Document Server; a atribuição à Ascensio System SIA fica no
	// "Sobre". Ver onlyoffice_branding.go e deploy/DIVERGENCES.md.
	customization := officeChrome(darkTheme, true)
	// Botão de fechar NO cabeçalho do editor (dispensa uma barra nossa por cima). Só aparece se
	// o cliente registar o evento onRequestClose — é ele que decide para onde voltar. Sem `text`:
	// o Document Server usa a sua própria string, já traduzida pelo `lang` (lei §4.6).
	customization["close"] = map[string]any{"visible": true}
	s.office.applyBranding(customization)

	config := map[string]any{
		"document": map[string]any{
			"fileType": ext,
			"key":      key,
			"title":    f.Nome,
			"url":      base + "/doc/files/" + f.ID.String() + "/download?token=" + token,
			"permissions": map[string]any{
				"edit": canWrite, "download": true, "print": true,
			},
		},
		"documentType": documentType(ext),
		"editorConfig": map[string]any{
			"mode":        map[bool]string{true: "edit", false: "view"}[canWrite],
			"lang":        lang,
			"user":        map[string]any{"id": mu.ID.String(), "name": mu.Name},
			"callbackUrl": base + "/doc/callback/" + f.ID.String() + "?token=" + token,
			// Plugins LIGADOS no editor autenticado (o de IA usa-se aqui; o público fica sem plugins).
			"plugins": map[string]any{
				"autostart": []string{aiPluginGUID},
			},
			"customization": customization,
		},
	}
	signed, err := s.office.jwtSign(config)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.editor_config_failed")
		return
	}
	config["token"] = signed

	writeJSON(w, http.StatusOK, map[string]any{
		"apiJs":    s.office.publicURL + "/web-apps/apps/api/documents/api.js",
		"config":   config,
		"canWrite": canWrite,
		"name":     f.Nome,
	})
}

// resolveDocAccess decide se o usuário pode abrir o arquivo e se pode escrever. Dono do arquivo
// escreve; compartilhado como "editor" escreve; "leitor" abre só para ver; sem vínculo → 403.
// Sempre isolado por tenant (lei §5.1) e checando posse/compartilhamento (lei §5.2).
func (s *Server) resolveDocAccess(w http.ResponseWriter, r *http.Request, mu db.MailUser) (db.StorageFile, bool, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return db.StorageFile{}, false, false
	}
	f, err := s.q.GetStorageFileInTenant(r.Context(), db.GetStorageFileInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return db.StorageFile{}, false, false
	}
	if f.OwnerUserID.Valid && f.OwnerUserID.Bytes == mu.ID {
		return f, true, true // dono escreve
	}
	shares, _ := s.q.ListSharesByFile(r.Context(), db.ListSharesByFileParams{FileID: id, TenantID: mu.TenantID})
	if canWrite, has := shareAccess(shares, mu.Email); has {
		return f, canWrite, true
	}
	writeErr(w, http.StatusForbidden, "files.no_access")
	return db.StorageFile{}, false, false
}

// orgSharePrincipal é o "principal" sentinela de uma partilha ao NÍVEL DA ORGANIZAÇÃO: qualquer
// membro autenticado do tenant tem o acesso concedido (leitor/editor). Não é um e-mail — o "@" à
// frente garante que nunca colide com um endereço real.
const orgSharePrincipal = "@org"

// shareAccess calcula o acesso de um utilizador a um ficheiro a partir das partilhas: o MELHOR entre
// a partilha direta (pelo e-mail) e a partilha de organização. Editor vence leitor. (canWrite, has).
func shareAccess(shares []db.StorageShare, email string) (canWrite, has bool) {
	for _, sh := range shares {
		if sh.Principal == email || sh.Principal == orgSharePrincipal {
			has = true
			if sh.Nivel == "editor" {
				canWrite = true
			}
		}
	}
	return
}

// docAuth valida o token HMAC da query e carrega o arquivo (isolado por tenant, lei §5.1).
func (s *Server) docAuth(w http.ResponseWriter, r *http.Request) (docClaims, db.StorageFile, bool) {
	var cl docClaims
	if s.office == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return cl, db.StorageFile{}, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return cl, db.StorageFile{}, false
	}
	cl, ok := s.office.parseToken(r.URL.Query().Get("token"))
	if !ok || cl.FileID != id {
		w.WriteHeader(http.StatusForbidden)
		return cl, db.StorageFile{}, false
	}
	f, err := s.q.GetStorageFileInTenant(r.Context(), db.GetStorageFileInTenantParams{ID: id, TenantID: cl.TenantID})
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return cl, db.StorageFile{}, false
	}
	return cl, f, true
}

// handleDocDownload: o Document Server busca o conteúdo do arquivo aqui.
func (s *Server) handleDocDownload(w http.ResponseWriter, r *http.Request) {
	_, f, ok := s.docAuth(w, r)
	if !ok {
		return
	}
	if !s.storageReady(w) {
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, obj)
}

// ooCallback é o corpo que o Document Server envia ao salvar.
type ooCallback struct {
	Status int    `json:"status"` // 2/6 = pronto para salvar
	URL    string `json:"url"`    // de onde baixar o arquivo editado
	Token  string `json:"token"`  // JWT do corpo (quando JWT ligado)
}

// handleDocCallback: o Document Server avisa que o documento foi salvo; buscamos o arquivo
// editado e gravamos como NOVA versão no MinIO. Fase A (view) nunca dispara save.
func (s *Server) handleDocCallback(w http.ResponseWriter, r *http.Request) {
	cl, f, ok := s.docAuth(w, r)
	if !ok {
		return
	}
	var cb ooCallback
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&cb); err != nil {
		writeJSON(w, http.StatusOK, map[string]int{"error": 1})
		return
	}
	// JWT do corpo é OBRIGATÓRIO: é a prova server-to-server de que o callback veio do Document
	// Server (assinado com o segredo compartilhado). Não pode ser opcional — senão basta omitir o
	// token e o HMAC da query (que vaza em logs/Referer) seria a única barreira (auditoria H2).
	if _, valid := s.office.jwtVerify(cb.Token); !valid {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	// Só status 2 (MustSave) e 6 (ForceSave) exigem gravar; e só com permissão de escrita.
	if (cb.Status == 2 || cb.Status == 6) && cl.CanWrite && cb.URL != "" {
		if err := s.saveEditedFile(r, f, cb.URL); err != nil {
			log.Printf("onlyoffice save failed (file=%s url=%s): %v", f.ID, cb.URL, err)
			writeJSON(w, http.StatusOK, map[string]int{"error": 1})
			return
		}
		log.Printf("onlyoffice saved new version (file=%s)", f.ID)
	}
	writeJSON(w, http.StatusOK, map[string]int{"error": 0})
}

// saveEditedFile baixa o arquivo editado do Document Server e grava como nova versão.
func (s *Server) saveEditedFile(r *http.Request, f db.StorageFile, url string) error {
	if s.storage == nil {
		return errStorageDisabled
	}
	target, ok := s.office.fetchURLAllowed(url)
	if !ok {
		return fmt.Errorf("onlyoffice: refused non-DocumentServer callback url %q", url)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	// Sem seguir redirects (um 3xx poderia saltar para um host interno) e com timeout.
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("onlyoffice: redirect on callback url blocked")
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.maxUploadLimit()))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	blobKey := f.TenantID.String() + "/" + hash
	if exists, _ := s.storage.Exists(r.Context(), blobKey); !exists {
		if err := s.storage.Put(r.Context(), blobKey, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			return err
		}
	}
	// Conteúdo igual ao atual (mesmo hash) → nada mudou; não cria versão nem bumpa. Evita lixo de
	// "versões" idênticas quando o Document Server dispara callbacks sem edição real.
	if hash == f.Hash {
		return nil
	}
	// Histórico: fotografa a versão CORRENTE antes de sobrescrever, para poder listar/restaurar.
	_ = s.q.SnapshotFileVersion(r.Context(), db.SnapshotFileVersionParams{
		FileID: f.ID, TenantID: f.TenantID, BlobKey: f.BlobKey, Hash: f.Hash, Tamanho: f.Tamanho, Versao: f.Versao,
	})
	_, err = s.q.UpdateStorageFileContent(r.Context(), db.UpdateStorageFileContentParams{
		ID: f.ID, TenantID: f.TenantID, BlobKey: blobKey, Hash: hash, Tamanho: int64(len(data)),
	})
	return err
}
