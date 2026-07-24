package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
)

// Outbox de envio server-side: o cliente cria o rascunho no JMAP e regista aqui o envio (email_id +
// envelope + send_at). O worker submete quando vence. Serve undo-send (janela curta, cancelável) e
// agendamento gerenciável. Ver mailbox.UserClient.SubmitEmail e cmd/worker (scanOutbox).

func (s *Server) handleCreateOutbox(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		EmailID    string   `json:"email_id"`
		IdentityID string   `json:"identity_id"`
		From       string   `json:"from"`
		Rcpts      []string `json:"rcpts"`
		Subject    string   `json:"subject"`
		SendAt     string   `json:"send_at"` // RFC3339
	}
	if !decode(w, r, &req) {
		return
	}
	sendAt, err := time.Parse(time.RFC3339, req.SendAt)
	if err != nil || req.EmailID == "" || req.IdentityID == "" || req.From == "" || len(req.Rcpts) == 0 {
		writeErr(w, http.StatusBadRequest, "outbox.invalid")
		return
	}
	// Limite diário de envio do plano (docs/manager.md §4): recusa já na criação — melhor UX do
	// que falhar depois no worker (que revalida na mesma, pois o limite pode esgotar entretanto).
	if !s.mailSendAllowed(r.Context(), mu.TenantID, len(req.Rcpts)) {
		writeErr(w, http.StatusTooManyRequests, "mail.daily_limit_reached")
		return
	}
	row, err := s.q.CreateOutbox(r.Context(), db.CreateOutboxParams{
		TenantID: mu.TenantID, UserID: mu.ID, EmailID: req.EmailID, IdentityID: req.IdentityID,
		FromEmail: req.From, Rcpts: req.Rcpts, Subject: req.Subject, SendAt: sendAt,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "outbox.failed")
		return
	}
	// Acorda o worker de envio (LISTEN/NOTIFY): ele dorme até ao próximo send_at, sem polling — o
	// e-mail sai quase logo após a janela de "desfazer", em vez de esperar por um tick (24/07/2026).
	_ = s.q.NotifyOutbox(r.Context())
	writeJSON(w, http.StatusCreated, map[string]string{"id": row.ID.String()})
}

func (s *Server) handleListOutbox(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListPendingOutbox(r.Context(), db.ListPendingOutboxParams{UserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "outbox.failed")
		return
	}
	if rows == nil {
		rows = []db.ListPendingOutboxRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleCancelOutbox(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "outbox.invalid")
		return
	}
	n, err := s.q.CancelOutbox(r.Context(), db.CancelOutboxParams{ID: id, UserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "outbox.failed")
		return
	}
	if n == 0 {
		// Já enviado ou inexistente: nada a cancelar (a janela de undo pode ter passado).
		writeErr(w, http.StatusConflict, "outbox.too_late")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}
