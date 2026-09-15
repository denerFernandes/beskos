package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Meet presentations (BigBlueButton-style): the presenter picks a PDF/PPTX (upload or from
// Drive), we convert PPTX->PDF (OnlyOffice) and store the PDF in MinIO. Each participant (member
// OR guest) renders the PDF client-side (pdf.js), and the current annotation/slide syncs in real
// time over the LiveKit data channel. The PDF is served via a signed URL (HMAC token), reachable
// by everyone in the room without depending on a session — that's why guests can see it too.

// presReady exige storage + office (para converter) + meet ligados.
func (s *Server) presReady(w http.ResponseWriter) bool {
	if s.office == nil {
		writeErr(w, http.StatusServiceUnavailable, "office.disabled")
		return false
	}
	return s.storageReady(w)
}

// --- token HMAC das URLs públicas (PDF ao cliente; fonte ao Document Server) ---

func (o *onlyoffice) mintPresToken(kind, value string, ttl time.Duration) string {
	payload := kind + "|" + value + "|" + strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write([]byte(payload))
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(mac.Sum(nil))
}

func (o *onlyoffice) parsePresToken(tok, wantKind string) (string, bool) {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	sig, err := enc.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", false
	}
	f := strings.Split(string(payload), "|")
	if len(f) != 3 || f[0] != wantKind {
		return "", false
	}
	exp, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil || time.Now().After(time.Unix(exp, 0)) {
		return "", false
	}
	return f[1], true
}

// presentationJSON serializa a apresentação para o cliente, com um token de PDF fresco (6h).
func (s *Server) presentationJSON(p db.MeetPresentation) map[string]any {
	tok := s.office.mintPresToken("pdf", p.ID.String(), 6*time.Hour)
	return map[string]any{
		"id":            p.ID.String(),
		"name":          p.Name,
		"current_slide": p.CurrentSlide,
		"token":         tok,
		// by = identidade LiveKit do apresentador (mu.ID). Permite ao próprio, ao reentrar na sala,
		// recuperar o papel de apresentador (controlos + responder aos hellos dos que chegam).
		"by": p.CreatedBy.String(),
	}
}

// buildPresentationPDF grava o PDF final no MinIO e devolve a chave. PDF entra direto; office
// (pptx/ppt/odp/…) é convertido pelo Document Server. src é lido por completo.
func (s *Server) buildPresentationPDF(ctx context.Context, tenant uuid.UUID, name, ext string, size int64, src io.Reader) (string, error) {
	pdfKey := tenant.String() + "/meet-pres/" + uuid.New().String() + ".pdf"
	if ext == "pdf" {
		if err := s.storage.Put(ctx, pdfKey, src, size, "application/pdf"); err != nil {
			return "", err
		}
		return pdfKey, nil
	}
	// Office → PDF: sobe a fonte para o MinIO, dá ao DS uma URL assinada para buscá-la, converte,
	// baixa o PDF gerado e guarda. A fonte temporária é removida no fim.
	srcKey := tenant.String() + "/meet-src/" + uuid.New().String() + "." + ext
	if err := s.storage.Put(ctx, srcKey, src, size, "application/octet-stream"); err != nil {
		return "", err
	}
	defer func() { _ = s.storage.Remove(context.Background(), srcKey) }()

	srcTok := s.office.mintPresToken("src", srcKey, 10*time.Minute)
	srcURL := s.office.filesHostURL + "/meet/presentation-src/" + srcTok
	pdfURL, err := s.office.convertToPDF(ctx, srcURL, ext, uuid.New().String(), name)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.office.reachable(pdfURL), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", err
	}
	if err := s.storage.Put(ctx, pdfKey, resp.Body, resp.ContentLength, "application/pdf"); err != nil {
		return "", err
	}
	return pdfKey, nil
}

// finishStart desativa apresentações anteriores da sala e cria a nova (ativa), devolvendo o JSON.
func (s *Server) finishStart(w http.ResponseWriter, r *http.Request, room, name, pdfKey string, tenant, user uuid.UUID) {
	_ = s.q.DeactivateRoomPresentations(r.Context(), db.DeactivateRoomPresentationsParams{Room: room, TenantID: tenant})
	pres, err := s.q.CreatePresentation(r.Context(), db.CreatePresentationParams{
		Room: room, TenantID: tenant, Name: name, ObjectKey: pdfKey, CreatedBy: user,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.presentation_create_failed")
		return
	}
	writeJSON(w, http.StatusOK, s.presentationJSON(pres))
}

func presentableExt(ext string) bool { return ext == "pdf" || officeExts[ext] }

// POST /api/meet/presentation/upload — inicia apresentação a partir de um ficheiro enviado.
func (s *Server) handleStartPresentationUpload(w http.ResponseWriter, r *http.Request) {
	if !s.presReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "files.upload_failed")
		return
	}
	room := strings.TrimSpace(r.FormValue("room"))
	if room == "" {
		writeErr(w, http.StatusBadRequest, "meet.room_required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.upload_failed")
		return
	}
	defer file.Close()
	ext := extOf(header.Filename)
	if !presentableExt(ext) {
		writeErr(w, http.StatusBadRequest, "meet.presentation_bad_type")
		return
	}
	pdfKey, err := s.buildPresentationPDF(r.Context(), mu.TenantID, header.Filename, ext, header.Size, file)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "meet.presentation_convert_failed")
		return
	}
	s.finishStart(w, r, room, header.Filename, pdfKey, mu.TenantID, mu.ID)
}

// POST /api/meet/presentation/drive — inicia a partir de um ficheiro do Drive (mesmo tenant/domínio).
func (s *Server) handleStartPresentationDrive(w http.ResponseWriter, r *http.Request) {
	if !s.presReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	var in struct {
		Room   string `json:"room"`
		FileID string `json:"file_id"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Room) == "" {
		writeErr(w, http.StatusBadRequest, "meet.room_required")
		return
	}
	fid, err := uuid.Parse(in.FileID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.not_found")
		return
	}
	// Escopo de domínio: o ficheiro tem de ser do mesmo tenant (lei §5.1).
	f, err := s.q.GetStorageFileInTenant(r.Context(), db.GetStorageFileInTenantParams{ID: fid, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	ext := extOf(f.Name)
	if !presentableExt(ext) {
		writeErr(w, http.StatusBadRequest, "meet.presentation_bad_type")
		return
	}
	rc, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.backend")
		return
	}
	defer rc.Close()
	pdfKey, err := s.buildPresentationPDF(r.Context(), mu.TenantID, f.Name, ext, f.Size, rc)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "meet.presentation_convert_failed")
		return
	}
	s.finishStart(w, r, in.Room, f.Name, pdfKey, mu.TenantID, mu.ID)
}

// GET /api/meet/presentation?room=... — apresentação ativa da sala (para quem entra a meio).
func (s *Server) handleGetPresentation(w http.ResponseWriter, r *http.Request) {
	if s.office == nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	mu := mailUserFrom(r.Context())
	room := r.URL.Query().Get("room")
	pres, err := s.q.GetActivePresentation(r.Context(), db.GetActivePresentationParams{Room: room, TenantID: mu.TenantID})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	out := s.presentationJSON(pres)
	out["active"] = true
	writeJSON(w, http.StatusOK, out)
}

// POST /api/meet/presentation/{id}/slide — o apresentador fixa o slide atual (persistido para quem
// entra depois). O sync em tempo real vai pelo data channel; isto é só o baseline dos atrasados.
func (s *Server) handleSetPresentationSlide(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.presentation_not_found")
		return
	}
	pres, err := s.q.GetPresentation(r.Context(), id)
	if err != nil || pres.TenantID != mu.TenantID {
		writeErr(w, http.StatusNotFound, "meet.presentation_not_found")
		return
	}
	// Só o apresentador controla os slides (não qualquer membro do tenant) — auditoria.
	if pres.CreatedBy != mu.ID {
		writeErr(w, http.StatusForbidden, "meet.not_presenter")
		return
	}
	var in struct {
		Slide int32 `json:"slide"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Slide < 0 {
		in.Slide = 0
	}
	_ = s.q.SetPresentationSlide(r.Context(), db.SetPresentationSlideParams{ID: id, CurrentSlide: in.Slide})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// POST /api/meet/presentation/{id}/stop — encerra a apresentação (some para todos).
func (s *Server) handleStopPresentation(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.presentation_not_found")
		return
	}
	// Só o apresentador encerra a própria apresentação (não qualquer membro do tenant).
	pres, err := s.q.GetPresentation(r.Context(), id)
	if err != nil || pres.TenantID != mu.TenantID {
		writeErr(w, http.StatusNotFound, "meet.presentation_not_found")
		return
	}
	if pres.CreatedBy != mu.ID {
		writeErr(w, http.StatusForbidden, "meet.not_presenter")
		return
	}
	_ = s.q.DeactivatePresentation(r.Context(), db.DeactivatePresentationParams{ID: id, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// GET /meet/presentation/{id}/pdf?token=... — PUBLIC: serves the PDF to any participant (member
// or guest) authorized by the HMAC token. No session (the token is the authorization).
func (s *Server) handlePresentationPDF(w http.ResponseWriter, r *http.Request) {
	if s.office == nil || s.storage == nil {
		writeErr(w, http.StatusServiceUnavailable, "office.disabled")
		return
	}
	val, ok := s.office.parsePresToken(r.URL.Query().Get("token"), "pdf")
	if !ok || val != chi.URLParam(r, "id") {
		writeErr(w, http.StatusForbidden, "meet.presentation_forbidden")
		return
	}
	id, err := uuid.Parse(val)
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.presentation_not_found")
		return
	}
	pres, err := s.q.GetPresentation(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "meet.presentation_not_found")
		return
	}
	// ServeContent (e não io.Copy): manda Content-Length e responde 206 a um pedido de Range. Com o
	// io.Copy a resposta ia em chunked, sem tamanho e ignorando Range — mas o proxy à frente anuncia
	// `Accept-Ranges: bytes`, e então o pdf.js pedia intervalos e recebia 200 com o ficheiro inteiro:
	// o PDF grande nunca abria (o PPTX convertido, pequeno, baixava de uma vez e abria).
	obj, err := s.storage.GetSeek(r.Context(), pres.ObjectKey)
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "presentation.pdf", pres.CreatedAt, obj)
}

// GET /meet/presentation-src/{token} — PÚBLICO: o Document Server busca aqui a FONTE (pptx/…) durante
// a conversão. Autorizado pelo token HMAC de curta duração emitido no buildPresentationPDF.
func (s *Server) handlePresentationSource(w http.ResponseWriter, r *http.Request) {
	if s.office == nil || s.storage == nil {
		writeErr(w, http.StatusServiceUnavailable, "office.disabled")
		return
	}
	key, ok := s.office.parsePresToken(chi.URLParam(r, "token"), "src")
	if !ok {
		writeErr(w, http.StatusForbidden, "meet.presentation_forbidden")
		return
	}
	s.streamObject(w, r, key, "application/octet-stream")
}

// streamObject copia um objeto do MinIO para a resposta.
func (s *Server) streamObject(w http.ResponseWriter, r *http.Request, key, contentType string) {
	rc, err := s.storage.Get(r.Context(), key)
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", contentType)
	_, _ = io.Copy(w, rc)
}
