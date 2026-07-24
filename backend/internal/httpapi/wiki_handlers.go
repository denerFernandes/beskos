package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/wiki"
)

// Wiki: thin HTTP transport over the wiki service (law 1.3). Every handler takes tenant+user from the
// authenticated session (mailUserFrom), never from the request body. The service resolves access in
// SQL and returns sentinel errors that writeWikiErr maps to a stable error_code the frontend
// translates via server.<code> (law 4.4/4.6).

// writeWikiErr maps a wiki service error to an HTTP status + error_code.
func writeWikiErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wiki.ErrNotFound):
		writeErr(w, http.StatusNotFound, "wiki.not_found")
	case errors.Is(err, wiki.ErrForbidden):
		writeErr(w, http.StatusForbidden, "wiki.forbidden")
	case errors.Is(err, wiki.ErrVersionConflict):
		writeErr(w, http.StatusConflict, "wiki.version_conflict")
	case errors.Is(err, wiki.ErrInvalid):
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
	default:
		writeErr(w, http.StatusInternalServerError, "wiki.failed")
	}
}

// wikiURLID parses a UUID path param, writing 400 wiki.invalid on failure.
func wikiURLID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return uuid.Nil, false
	}
	return id, true
}

// optUUID parses an optional UUID string ("" / null -> nil). A present-but-bad value is an error.
func optUUID(raw *string) (*uuid.UUID, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		return nil, wiki.ErrInvalid
	}
	return &id, nil
}

// ---------- spaces ----------

type wikiSpaceReq struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Visibility  string `json:"visibility"`
	AiIndexed   bool   `json:"ai_indexed"`
}

func (r wikiSpaceReq) input() wiki.SpaceInput {
	return wiki.SpaceInput{
		Key: r.Key, Name: r.Name, Description: r.Description, Icon: r.Icon,
		Visibility: r.Visibility, AiIndexed: r.AiIndexed,
	}
}

func (s *Server) handleListWikiSpaces(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	spaces, err := s.wiki.ListSpaces(r.Context(), mu.TenantID, mu.ID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(spaces))
}

func (s *Server) handleCreateWikiSpace(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req wikiSpaceReq
	if !decode(w, r, &req) {
		return
	}
	sp, err := s.wiki.CreateSpace(r.Context(), mu.TenantID, mu.ID, req.input())
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sp)
}

func (s *Server) handleGetWikiSpace(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	sp, role, err := s.wiki.GetSpace(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"space": sp, "role": role})
}

func (s *Server) handleUpdateWikiSpace(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiSpaceReq
	if !decode(w, r, &req) {
		return
	}
	sp, err := s.wiki.UpdateSpace(r.Context(), mu.TenantID, mu.ID, id, req.input())
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

func (s *Server) handleDeleteWikiSpace(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	// B4: apagar wiki = dono do espaço OU admin da organização (papel RBAC do utilizador da sessão).
	isOrgAdmin := mu.Role == "owner" || mu.Role == "admin" || mu.Role == "internal_admin"
	if err := s.wiki.DeleteSpace(r.Context(), mu.TenantID, mu.ID, id, isOrgAdmin); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- grants ----------

type wikiGrantReq struct {
	SubjectType string  `json:"subject_type"`
	SubjectID   *string `json:"subject_id"`
	Role        string  `json:"role"`
}

func (s *Server) handleListWikiSpaceGrants(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	grants, err := s.wiki.ListSpaceGrants(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(grants))
}

func (s *Server) handleSetWikiSpaceGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiGrantReq
	if !decode(w, r, &req) {
		return
	}
	sid, err := optUUID(req.SubjectID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	grant, err := s.wiki.SetSpaceGrant(r.Context(), mu.TenantID, mu.ID, id, req.SubjectType, sid, req.Role)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

func (s *Server) handleDeleteWikiSpaceGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	grantID, ok := wikiURLID(w, r, "grantId")
	if !ok {
		return
	}
	if err := s.wiki.RemoveSpaceGrant(r.Context(), mu.TenantID, mu.ID, id, grantID); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- pages ----------

type wikiPageReq struct {
	ParentID        *string         `json:"parent_id"`
	Title           string          `json:"title"`
	BodyJSON        json.RawMessage `json:"body_json"`
	Status          string          `json:"status"`
	ExpectedVersion int32           `json:"expected_version"` // required on update (optimistic lock)
}

func (s *Server) handleWikiTree(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	tree, err := s.wiki.Tree(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(tree))
}

func (s *Server) handleCreateWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	spaceID, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiPageReq
	if !decode(w, r, &req) {
		return
	}
	parent, err := optUUID(req.ParentID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	page, err := s.wiki.CreatePage(r.Context(), mu.TenantID, mu.ID, spaceID, wiki.PageInput{
		ParentID: parent, Title: req.Title, BodyJSON: req.BodyJSON, Status: req.Status,
	})
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, page)
}

func (s *Server) handleGetWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	page, role, err := s.wiki.GetPage(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": page, "role": role})
}

func (s *Server) handleUpdateWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiPageReq
	if !decode(w, r, &req) {
		return
	}
	page, err := s.wiki.UpdatePage(r.Context(), mu.TenantID, mu.ID, id, wiki.PageInput{
		Title: req.Title, BodyJSON: req.BodyJSON, Status: req.Status,
	}, req.ExpectedVersion)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleDeleteWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	if err := s.wiki.DeletePage(r.Context(), mu.TenantID, mu.ID, id); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type wikiMoveReq struct {
	ParentID *string `json:"parent_id"`
	Position int32   `json:"position"`
}

func (s *Server) handleMoveWikiPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiMoveReq
	if !decode(w, r, &req) {
		return
	}
	parent, err := optUUID(req.ParentID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	if err := s.wiki.MovePage(r.Context(), mu.TenantID, mu.ID, id, parent, req.Position); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "moved"})
}

func (s *Server) handleListWikiPageVersions(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	versions, err := s.wiki.ListVersions(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(versions))
}

// wikiVersionNum parses the {version} path param as a positive int.
func wikiVersionNum(w http.ResponseWriter, r *http.Request) (int32, bool) {
	n, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || n < 1 {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return 0, false
	}
	return int32(n), true
}

func (s *Server) handleGetWikiPageVersion(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	ver, ok := wikiVersionNum(w, r)
	if !ok {
		return
	}
	v, err := s.wiki.GetVersion(r.Context(), mu.TenantID, mu.ID, id, ver)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// ---------- templates (B7) ----------

func (s *Server) handleSetWikiPageTemplate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		IsTemplate bool `json:"is_template"`
	}
	if !decode(w, r, &req) {
		return
	}
	page, err := s.wiki.SetTemplate(r.Context(), mu.TenantID, mu.ID, id, req.IsTemplate)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleListWikiTemplates(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	rows, err := s.wiki.ListTemplates(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

// ---------- app home (B5) ----------

func (s *Server) handleGetWikiHome(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	h, err := s.wiki.GetHome(r.Context(), mu.TenantID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// handleSetWikiHome grava a home (a rota exige PermOrgSettings via requirePerm).
func (s *Server) handleSetWikiHome(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		BodyJSON json.RawMessage `json:"body_json"`
	}
	if !decode(w, r, &req) {
		return
	}
	h, err := s.wiki.SetHome(r.Context(), mu.TenantID, mu.ID, req.BodyJSON)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// ---------- per-page grants (B3) ----------

func (s *Server) handleListWikiPageGrants(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	grants, err := s.wiki.ListPageGrants(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(grants))
}

func (s *Server) handleSetWikiPageGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req wikiGrantReq
	if !decode(w, r, &req) {
		return
	}
	sid, err := optUUID(req.SubjectID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	grant, err := s.wiki.SetPageGrant(r.Context(), mu.TenantID, mu.ID, id, req.SubjectType, sid, req.Role)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

func (s *Server) handleDeleteWikiPageGrant(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	grantID, ok := wikiURLID(w, r, "grantId")
	if !ok {
		return
	}
	if err := s.wiki.RemovePageGrant(r.Context(), mu.TenantID, mu.ID, id, grantID); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- public read-only access ----------

// handleSetWikiPublic toggles anonymous read-only access for a space (manager only).
func (s *Server) handleSetWikiPublic(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Enabled    bool `json:"enabled"`
		Regenerate bool `json:"regenerate"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Links públicos por plano (manager M3): ligar exige o knob; desligar é sempre permitido.
	if req.Enabled && !s.tenantPlanByID(r.Context(), mu.TenantID).WikiPublicLinks {
		writeErr(w, http.StatusForbidden, "wiki.public_not_in_plan")
		return
	}
	sp, err := s.wiki.SetPublicAccess(r.Context(), mu.TenantID, mu.ID, id, req.Enabled, req.Regenerate)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

// handlePublicWikiSpace serves a public space's metadata + published page tree (anonymous, no auth).
func (s *Server) handlePublicWikiSpace(w http.ResponseWriter, r *http.Request) {
	sp, tree, err := s.wiki.PublicSpace(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	// Plano sem links públicos: links existentes deixam de servir (fail-closed no read também).
	if !s.tenantPlanByID(r.Context(), sp.TenantID).WikiPublicLinks {
		writeErr(w, http.StatusNotFound, "wiki.not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"space": map[string]any{"name": sp.Name, "icon": sp.Icon, "description": sp.Description},
		"pages": orEmpty(tree),
	})
}

// handlePublicWikiPage serves one published page of a public space (anonymous). Only body_html leaves.
func (s *Server) handlePublicWikiPage(w http.ResponseWriter, r *http.Request) {
	spc, page, err := s.wiki.PublicPage(r.Context(), chi.URLParam(r, "slug"), chi.URLParam(r, "pageSlug"))
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	if !s.tenantPlanByID(r.Context(), spc.TenantID).WikiPublicLinks {
		writeErr(w, http.StatusNotFound, "wiki.not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title": page.Title, "slug": page.Slug, "body_html": page.BodyHtml, "updated_at": page.UpdatedAt,
	})
}

// ---------- attachments (reuse of the Files storage) ----------

func (s *Server) handleListWikiAttachments(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	rows, err := s.wiki.ListAttachments(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleAttachWikiFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		FileID string `json:"file_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	fid, err := uuid.Parse(req.FileID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return
	}
	if err := s.wiki.AttachFile(r.Context(), mu.TenantID, mu.ID, id, fid); err != nil {
		writeWikiErr(w, err)
		return
	}
	rows, err := s.wiki.ListAttachments(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleDetachWikiFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	attID, err := uuid.Parse(chi.URLParam(r, "attId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return
	}
	if err := s.wiki.DetachAttachment(r.Context(), mu.TenantID, mu.ID, id, attID); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "detached"})
}

// handleDownloadWikiAttachment streams an attachment blob after the service verifies page access.
func (s *Server) handleDownloadWikiAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	attID, err := uuid.Parse(chi.URLParam(r, "attId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "wiki.invalid")
		return
	}
	f, err := s.wiki.AttachmentBlob(r.Context(), mu.TenantID, mu.ID, attID)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	w.Header().Set("X-Content-Type-Options", "nosniff")
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

// ---------- comments ----------

func (s *Server) handleListWikiComments(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	comments, err := s.wiki.ListComments(r.Context(), mu.TenantID, mu.ID, id)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(comments))
}

func (s *Server) handleCreateWikiComment(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		BodyJSON json.RawMessage `json:"body_json"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := s.wiki.AddComment(r.Context(), mu.TenantID, mu.ID, id, req.BodyJSON)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleDeleteWikiComment(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	if err := s.wiki.DeleteComment(r.Context(), mu.TenantID, mu.ID, id); err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleRestoreWikiPageVersion(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	ver, ok := wikiVersionNum(w, r)
	if !ok {
		return
	}
	var req struct {
		ExpectedVersion int32 `json:"expected_version"`
	}
	if !decode(w, r, &req) {
		return
	}
	page, err := s.wiki.RestoreVersion(r.Context(), mu.TenantID, mu.ID, id, ver, req.ExpectedVersion)
	if err != nil {
		writeWikiErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
