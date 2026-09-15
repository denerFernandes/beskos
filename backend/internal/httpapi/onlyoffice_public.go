package httpapi

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/auth"
)

// handlePublicDocSession opens a public session (no login) for a file via a link token. For
// office files it returns the OnlyOffice config; the user is ANONYMOUS (a random name, or one the
// client already translated). The mode (view/edit) and downloadability come from the link, not
// the request.
//
// Security: the link token is the authorization (as with public downloads). Anonymous writes go
// through the SAME OnlyOffice callback, with a doc token whose UserID is null — the new version
// counts against the file owner's quota/history and isn't attributed to anyone. The route is
// rate-limited (publicRateLimit) to curb abuse and password brute-forcing.
func (s *Server) handlePublicDocSession(w http.ResponseWriter, r *http.Request) {
	if s.office == nil {
		writeErr(w, http.StatusServiceUnavailable, "files.editing_disabled")
		return
	}
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
	if f.PasswordHash != "" {
		pw := r.URL.Query().Get("pw")
		if pw == "" || !auth.CheckPassword(f.PasswordHash, pw) {
			writeErr(w, http.StatusUnauthorized, "storage.link_password")
			return
		}
	}

	// Marca da organização (nome/cor/logo) para a barra do visualizador público: quem abre é anónimo
	// e de fora, e a barra diz de quem é o ficheiro. Sem marca configurada, vai vazio (marca padrão).
	brand := map[string]any{}
	if tn, terr := s.q.GetTenant(r.Context(), f.TenantID); terr == nil {
		brand = map[string]any{"name": tn.Name, "color": tn.BrandColor, "logo": tn.BrandLogo}
	}

	ext := extOf(f.Name)
	if !officeExts[ext] {
		// Não é office: o cliente mostra descarga (se permitida) ou "sem pré-visualização".
		writeJSON(w, http.StatusOK, map[string]any{
			"kind": "file", "name": f.Name, "allow_download": f.AllowDownload, "brand": brand,
		})
		return
	}

	canWrite := f.Permission == "edit"
	// Token do doc com utilizador ANÓNIMO (UserID nulo). CanWrite decide se o callback grava.
	docToken := s.office.mintToken(docClaims{
		FileID: f.ID, UserID: uuid.Nil, TenantID: f.TenantID, CanWrite: canWrite,
		Exp: time.Now().Add(docTokenTTL),
	})
	base := s.office.filesHostURL
	key := strings.ReplaceAll(uuid.NewSHA1(f.ID, []byte(strconv.Itoa(int(f.Version)))).String(), "-", "")

	// Nome do convidado: o cliente passa-o já traduzido/aleatório (?guest=). Sem isso, um neutro.
	guestName := strings.TrimSpace(r.URL.Query().Get("guest"))
	if guestName == "" || len(guestName) > 40 {
		guestName = randomGuestName()
	}
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "pt-BR"
	}
	darkTheme := r.URL.Query().Get("theme") == "dark"

	// Mesma marca do editor autenticado — quem abre um link público vê o Beskos, não o ONLYOFFICE.
	// A atribuição à Ascensio System SIA acompanha, no "Sobre" (ver onlyoffice_branding.go).
	// Sem plugins: quem abre um link público é anónimo e de fora.
	customization := officeChrome(darkTheme, false)
	s.office.applyBranding(customization)

	config := map[string]any{
		"document": map[string]any{
			"fileType": ext,
			"key":      key,
			"title":    f.Name,
			"url":      base + "/doc/files/" + f.ID.String() + "/download?token=" + docToken,
			"permissions": map[string]any{
				// Descarga/impressão seguem o toggle do link; edição segue a permissão.
				"edit": canWrite, "download": f.AllowDownload, "print": f.AllowDownload,
			},
		},
		"documentType": documentType(ext),
		"editorConfig": map[string]any{
			"mode":          map[bool]string{true: "edit", false: "view"}[canWrite],
			"lang":          lang,
			"user":          map[string]any{"id": uuid.NewString(), "name": guestName},
			"callbackUrl":   base + "/doc/callback/" + f.ID.String() + "?token=" + docToken,
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
		"kind":           "office",
		"apiJs":          s.office.publicURL + "/web-apps/apps/api/documents/api.js",
		"config":         config,
		"canWrite":       canWrite,
		"name":           f.Name,
		"allow_download": f.AllowDownload,
		"brand":          brand,
	})
}

// randomGuestName gera um nome de convidado neutro e imprevisível ("Guest 4821"). O cliente costuma
// passar um já traduzido; isto é só o recurso quando não o faz.
func randomGuestName() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return fmt.Sprintf("Guest %04d", (int(b[0])<<8|int(b[1]))%10000)
}
