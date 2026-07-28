package httpapi

import (
	"encoding/base64"
	"errors"
	"log"
	"mailin/backend/internal/stalwart"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/auth"
	"mailin/backend/internal/db"
)

// Perfil do utilizador (menu do avatar — pedido 23/07/2026): nome, cargo, telefone, foto e troca
// de senha self-service. A segurança PESSOAL (MFA) usa os endpoints /mfa/* existentes.

const maxAvatarBytes = 150 << 10 // data-URL ~128px cabe folgado; acima disto é upload errado

func (s *Server) handleGetMyProfile(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	fresh, err := s.q.GetUserByID(r.Context(), u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "profile.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email": fresh.Email, "name": fresh.Name, "job_title": fresh.JobTitle,
		"phone": fresh.Phone, "avatar": fresh.Avatar,
	})
}

func (s *Server) handleUpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req struct {
		Name     string `json:"name"`
		JobTitle string `json:"job_title"`
		Phone    string `json:"phone"`
		Avatar   string `json:"avatar"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Avatar) > maxAvatarBytes {
		writeErr(w, http.StatusBadRequest, "profile.avatar_too_large")
		return
	}
	// Foto: ou vazia (remover) ou um data-URL de imagem — nunca HTML/URL externa (XSS/tracking).
	if req.Avatar != "" && !strings.HasPrefix(req.Avatar, "data:image/") {
		writeErr(w, http.StatusBadRequest, "profile.avatar_invalid")
		return
	}
	if err := s.q.UpdateMyProfile(r.Context(), db.UpdateMyProfileParams{
		ID: u.ID, Name: strings.TrimSpace(req.Name), JobTitle: strings.TrimSpace(req.JobTitle),
		Phone: strings.TrimSpace(req.Phone), Avatar: req.Avatar,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "profile.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleChangeMyPassword troca a própria senha com STEP-UP (senha atual obrigatória). Com caixa,
// troca no Stalwart E regrava a jmap_cred (senão o proxy JMAP ficaria com a credencial velha);
// derruba as outras sessões do utilizador (a atual continua).
func (s *Server) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.New) < 10 {
		writeErr(w, http.StatusBadRequest, "profile.password_weak")
		return
	}
	fresh, err := s.q.GetUserByID(r.Context(), u.ID)
	if err != nil || !s.verifyUserPassword(r.Context(), fresh, req.Current) {
		writeErr(w, http.StatusForbidden, "auth.invalid_credentials")
		return
	}
	if fresh.StalwartAccountID != nil {
		if err := s.sw.SetAccountPassword(r.Context(), *fresh.StalwartAccountID, req.New); err != nil {
			log.Printf("perfil: Stalwart recusou: %v", err)
			if errors.Is(err, stalwart.ErrWeakPassword) {
				writeErr(w, http.StatusBadRequest, "mailbox.password_weak")
				return
			}
			writeErr(w, http.StatusBadGateway, "mailbox.stalwart_failed")
			return
		}
		// Credencial JMAP acompanha a senha nova (brokering H11).
		basic := base64.StdEncoding.EncodeToString([]byte(fresh.Email + ":" + req.New))
		_ = s.q.SetJmapCred(r.Context(), db.SetJmapCredParams{ID: u.ID, JmapCred: s.encTOTP(basic)})
	}
	if hash, herr := auth.HashPassword(req.New); herr == nil {
		_ = s.q.UpdateUserPassword(r.Context(), db.UpdateUserPasswordParams{ID: u.ID, PasswordHash: hash})
	}
	_ = s.q.DeleteUserWebmailSessionsExcept(r.Context(), db.DeleteUserWebmailSessionsExceptParams{
		MailUserID: u.ID, KeepToken: sessionToken(r),
	})
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email,
		Acao: "user.password_change_self", Alvo: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
