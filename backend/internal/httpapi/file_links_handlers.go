package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/auth"
	"mailin/backend/internal/db"
)

// linkExpiry converte os dias pedidos em prazo: ausente (nil) = padrão 30d; 0 = sem expiração (NULL);
// N = N dias. Teto de 365 para não virar "eterno" por engano.
func linkExpiry(days *int) pgtype.Timestamptz {
	d := 30
	if days != nil {
		d = *days
	}
	if d <= 0 {
		return pgtype.Timestamptz{} // NULL: sem expiração
	}
	if d > 365 {
		d = 365
	}
	return pgtype.Timestamptz{Time: time.Now().Add(time.Duration(d) * 24 * time.Hour), Valid: true}
}

// fileLinkTTL é a validade de um link público de download (anexos grandes). Passado o prazo, o token
// deixa de resolver e um novo é gerado no próximo envio.
const fileLinkTTL = 30 * 24 * time.Hour

// Links públicos de download para anexos grandes (>10MB) enviados por e-mail. O compositor sobe o
// ficheiro para o Files, pede um link e insere-o no corpo — como o Gmail. O link carrega um token
// secreto: quem o tem baixa, sem login. Same-org também ganha partilha no Files (handleShareFile).

// newLinkToken gera um token imprevisível (32 bytes → base64url), a autorização do link.
func newLinkToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// handleCreateFileLink devolve o token de download público de um ficheiro (do dono). Idempotente:
// reutiliza o link existente em vez de criar um novo a cada envio.
func (s *Server) handleCreateFileLink(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	var req struct {
		ExpiresDays   *int   `json:"expires_days"`   // nil=30d · 0=sem expiração · N=N dias
		Password      string `json:"password"`       // ""=sem senha
		Permission    string `json:"permission"`     // "view" (default) | "edit"
		AllowDownload *bool  `json:"allow_download"` // nil=true (default) · o toggle de descarga
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // corpo opcional: sem corpo mantém os padrões
	expires := linkExpiry(req.ExpiresDays)
	pwHash := ""
	if req.Password != "" {
		h, herr := auth.HashPassword(req.Password)
		if herr != nil {
			writeErr(w, http.StatusInternalServerError, "storage.link_failed")
			return
		}
		pwHash = h
	}
	perm := "view"
	if req.Permission == "edit" {
		perm = "edit"
	}
	allowDl := true
	if req.AllowDownload != nil {
		allowDl = *req.AllowDownload
	}
	// Já existe? Reconfigura (validade/senha/permissão/descarga) mantendo o mesmo token.
	if existing, err := s.q.GetFileLinkByFile(r.Context(), f.ID); err == nil {
		if _, uerr := s.q.UpdateFileLink(r.Context(), db.UpdateFileLinkParams{
			FileID: f.ID, ExpiresAt: expires, PasswordHash: pwHash, TenantID: f.TenantID,
			Permission: perm, AllowDownload: allowDl,
		}); uerr != nil {
			writeErr(w, http.StatusInternalServerError, "storage.link_failed")
			return
		}
		writeJSON(w, http.StatusOK, linkDTO(existing.Token, expires, pwHash != "", perm, allowDl))
		return
	}
	mu := mailUserFrom(r.Context())
	token, err := newLinkToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.link_failed")
		return
	}
	link, err := s.q.CreateFileLink(r.Context(), db.CreateFileLinkParams{
		TenantID: f.TenantID, FileID: f.ID, Token: token, CreatedBy: mu.ID,
		ExpiresAt: expires, PasswordHash: pwHash, Permission: perm, AllowDownload: allowDl,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.link_failed")
		return
	}
	writeJSON(w, http.StatusCreated, linkDTO(link.Token, expires, pwHash != "", perm, allowDl))
}

// linkDTO monta a resposta do link (token + validade + senha + permissão + descarga).
func linkDTO(token string, expires pgtype.Timestamptz, hasPassword bool, permission string, allowDownload bool) map[string]any {
	out := map[string]any{"token": token, "has_password": hasPassword, "permission": permission, "allow_download": allowDownload}
	if expires.Valid {
		out["expires_at"] = expires.Time
	}
	return out
}

// handleGetFileLink devolve o token do link público ATIVO de um ficheiro (ou token vazio se não há),
// para a UI mostrar/copiar/revogar. Só o dono. Sempre 200 (token "" = sem link) para simplificar o cliente.
func (s *Server) handleGetFileLink(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	link, err := s.q.GetFileLinkByFile(r.Context(), f.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"token": ""})
		return
	}
	writeJSON(w, http.StatusOK, linkDTO(link.Token, link.ExpiresAt, link.PasswordHash != "", link.Permission, link.AllowDownload))
}

// handleRevokeFileLink revoga (apaga) o link público de um ficheiro — só o dono.
func (s *Server) handleRevokeFileLink(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	if err := s.q.DeleteFileLinkByFile(r.Context(), db.DeleteFileLinkByFileParams{FileID: f.ID, TenantID: f.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.link_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// handleListSharedWithMe lista os ficheiros que outras pessoas do tenant partilharam comigo.
func (s *Server) handleListSharedWithMe(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	files, err := s.q.ListFilesSharedWithMe(r.Context(), db.ListFilesSharedWithMeParams{
		TenantID: mu.TenantID, Principal: mu.Email,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": orEmpty(files)})
}

// handleShareWithRecipients partilha um ficheiro com os destinatários de um e-mail que sejam da
// MESMA organização — assim o anexo grande aparece em "Partilhado comigo" no Files deles e dispara
// a notificação. Externos são ignorados (não têm conta): para eles vale só o link público.
func (s *Server) handleShareWithRecipients(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	var req struct {
		Emails []string `json:"emails"`
	}
	if !decode(w, r, &req) {
		return
	}
	mu := mailUserFrom(r.Context())
	shared, external, notEmailed := 0, 0, 0
	for _, email := range req.Emails {
		if email == "" || email == mu.Email {
			continue
		}
		target, terr := s.q.GetMailUserByEmail(r.Context(), email)
		if terr != nil || target.TenantID != f.TenantID {
			// De fora da organização: não tem app onde receber notificação — vai o link por e-mail.
			if link, sent := s.shareWithOutsider(r, f, mu, email); link != "" {
				shared++
				external++
				if !sent {
					notEmailed++
				}
			}
			continue
		}
		if _, err := s.q.CreateShare(r.Context(), db.CreateShareParams{
			FileID: f.ID, TenantID: f.TenantID, Principal: email, Nivel: "leitor",
		}); err != nil {
			continue
		}
		s.notifyUser(r.Context(), f.TenantID, target.ID, "file_share",
			"New shared file", mu.Email+" shared \""+f.Nome+"\" with you", fileLink(f.ID), uuid.Nil,
			map[string]string{"actor": mu.Email, "file": f.Nome})
		shared++
	}
	// `external`/`not_emailed`: a UI tem de poder dizer "o link foi criado mas o e-mail não saiu"
	// em vez de dar a partilha por concluída.
	writeJSON(w, http.StatusOK, map[string]int{"shared": shared, "external": external, "not_emailed": notEmailed})
}

// handleShareWithGroup partilha o ficheiro com TODOS os membros de um grupo/equipa da organização —
// expande em partilhas por e-mail (o modelo de shares é por principal). Dedup: membro já partilhado
// não vira linha duplicada. O grupo tem de ser do mesmo tenant (nunca confiar no id do request).
func (s *Server) handleShareWithGroup(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	var req struct {
		GroupID string `json:"group_id"`
		Nivel   string `json:"nivel"`
	}
	if !decode(w, r, &req) {
		return
	}
	gid, err := uuid.Parse(req.GroupID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	nivel := "leitor"
	if req.Nivel == "editor" {
		nivel = "editor"
	}
	mu := mailUserFrom(r.Context())
	if _, err := s.q.GetUserGroup(r.Context(), db.GetUserGroupParams{ID: gid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	// Dedup: e-mails já partilhados não repetem (não há unique em storage_shares).
	existing := map[string]bool{}
	if shares, err := s.q.ListSharesByFile(r.Context(), db.ListSharesByFileParams{FileID: f.ID, TenantID: f.TenantID}); err == nil {
		for _, sh := range shares {
			existing[sh.Principal] = true
		}
	}
	members, _ := s.q.ListGroupMembers(r.Context(), gid)
	shared := 0
	for _, m := range members {
		if m.Email == "" || m.Email == mu.Email || existing[m.Email] {
			continue
		}
		if _, err := s.q.CreateShare(r.Context(), db.CreateShareParams{
			FileID: f.ID, TenantID: f.TenantID, Principal: m.Email, Nivel: nivel,
		}); err != nil {
			continue
		}
		existing[m.Email] = true
		s.notifyUser(r.Context(), f.TenantID, m.ID, "file_share",
			"New shared file", mu.Email+" shared \""+f.Nome+"\" with you", fileLink(f.ID), uuid.Nil,
			map[string]string{"actor": mu.Email, "file": f.Nome})
		shared++
	}
	writeJSON(w, http.StatusOK, map[string]int{"shared": shared})
}

// handleMyGroups lista os grupos/equipas a que o utilizador atual pertence (para partilhar ficheiros
// com a equipa). Sem RBAC especial: é a própria filiação do utilizador.
func (s *Server) handleMyGroups(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	groups, _ := s.q.ListGroupsForUser(r.Context(), mu.ID)
	writeJSON(w, http.StatusOK, orEmpty(groups))
}

// handlePublicFileDownload serve o ficheiro por token, SEM sessão. O token é a autorização — não há
// verificação de dono/tenant de propósito. Força download (nunca inline: conteúdo de terceiros).
func (s *Server) handlePublicFileDownload(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	token := chi.URLParam(r, "token")
	if token == "" {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	f, err := s.q.GetFileByLinkToken(r.Context(), token)
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	// Link protegido por senha: exige ?pw= correto (a página de download a coleta e reenvia).
	if f.PasswordHash != "" {
		pw := r.URL.Query().Get("pw")
		if pw == "" || !auth.CheckPassword(f.PasswordHash, pw) {
			writeErr(w, http.StatusUnauthorized, "storage.link_password")
			return
		}
	}
	// Descarga desligada no link (só ver): a rota crua também recusa, não só o botão do visualizador.
	if !f.AllowDownload {
		writeErr(w, http.StatusForbidden, "storage.download_disabled")
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Nome))
	_, _ = io.Copy(w, obj)
}
