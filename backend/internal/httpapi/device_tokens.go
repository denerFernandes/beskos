package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Tokens de dispositivo: autenticação de clientes headless (o daemon de sync). Segurança:
//   - o token cru (32 bytes aleatórios, base64url, prefixo "mlnd_") é mostrado UMA vez, na criação;
//   - guardamos só o SHA-256 (hex) — um vazamento do banco não expõe tokens usáveis;
//   - escopo restrito: o token só passa no grupo /api/sync/* (ler changes + baixar), nunca no
//     resto da API nem no fluxo de sessão;
//   - revogável por dispositivo e com expiração opcional.
// A gestão (criar/listar/revogar) fica atrás da sessão do webmail — é o próprio utilizador a
// provisionar o seu dispositivo.

const deviceTokenPrefix = "mlnd_"

// hashDeviceToken devolve o SHA-256 em hex do token cru — a chave de busca guardada no banco.
func hashDeviceToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func newDeviceToken() (raw, prefix string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = deviceTokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	// Prefixo curto e não sensível, só para o utilizador reconhecer o token na lista.
	prefix = raw[:len(deviceTokenPrefix)+6]
	return raw, prefix, nil
}

func (s *Server) handleCreateDeviceToken(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Name      string `json:"name"`
		ExpiresIn int    `json:"expires_in_days"` // 0 = não expira
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Name) == 0 || len(req.Name) > 80 {
		writeErr(w, http.StatusBadRequest, "devices.invalid_name")
		return
	}
	raw, prefix, err := newDeviceToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "devices.token_gen_failed")
		return
	}
	var expires pgtype.Timestamptz
	if req.ExpiresIn > 0 {
		expires = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, req.ExpiresIn), Valid: true}
	}
	row, err := s.q.CreateDeviceToken(r.Context(), db.CreateDeviceTokenParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Name: req.Name,
		TokenHash: hashDeviceToken(raw), TokenPrefix: prefix, ExpiresAt: expires,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "devices.create_failed")
		return
	}
	// O token cru só é devolvido AQUI (nunca mais). O cliente guarda-o; nós só temos o hash.
	writeJSON(w, http.StatusOK, map[string]any{
		"id": row.ID, "name": row.Name, "token": raw, "prefix": row.TokenPrefix, "created_at": row.CreatedAt,
	})
}

func (s *Server) handleListDeviceTokens(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListDeviceTokens(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "devices.list_failed")
		return
	}
	if rows == nil {
		rows = []db.ListDeviceTokensRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": rows})
}

func (s *Server) handleRevokeDeviceToken(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "devices.invalid_id")
		return
	}
	if err := s.q.RevokeDeviceToken(r.Context(), db.RevokeDeviceTokenParams{ID: id, MailUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "devices.revoke_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// requireDeviceToken autentica o daemon pelo token de dispositivo (Bearer ou ?access_token=) e
// injeta a mail_user no contexto — os handlers de sync reusam mailUserFrom sem saber a origem.
func (s *Server) requireDeviceToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := sessionToken(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "auth.no_token")
			return
		}
		row, err := s.q.GetDeviceTokenUser(r.Context(), hashDeviceToken(raw))
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "auth.invalid_token")
			return
		}
		// Regista o "visto por último" para auditoria; falha aqui não bloqueia o pedido.
		_ = s.q.TouchDeviceToken(r.Context(), row.TokenID)
		ctx := context.WithValue(r.Context(), ctxMailUser, row.MailUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
