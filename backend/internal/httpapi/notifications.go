package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
)

// Notificações in-app (sino) + disparo de e-mail. O envio real de e-mail é opcional (só se
// NOTIFY_* estiver configurado); as notificações in-app funcionam sempre.

// notifyUser cria uma notificação in-app para um usuário do tenant.
//
// `args` leva os dados estruturados do evento (ator, título, quando…) para o frontend traduzir por
// `server.notif.<kind>` no idioma do usuário (CLAUDE.md §4.6). title/body continuam preenchidos em
// inglês como fallback e para e-mail/web-push, que são renderizados no backend (sem frontend).
func (s *Server) notifyUser(ctx context.Context, tenantID, userID uuid.UUID, kind, title, body, link string, meetingID uuid.UUID, args map[string]string) {
	_, err := s.q.CreateNotification(ctx, db.CreateNotificationParams{
		TenantID: tenantID, UserID: userID, Kind: kind, Title: title, Body: body, Link: link,
		MeetingID: pgtype.UUID{Bytes: meetingID, Valid: meetingID != uuid.Nil},
		Args:      notifArgs(args),
	})
	if err != nil {
		log.Printf("notify: create in-app failed: %v", err)
	}
	// Espelha como notificação desktop (Web Push) — entregue mesmo com o Beskos fechado. Best-effort.
	s.sendWebPush(ctx, userID, pushPayload{Kind: kind, Title: title, Body: body, Link: link})
}

// notifyPushOnly envia SÓ o push desktop, sem gravar na área de notificações (o sino). Usado pelo
// chat (24/07/2026): o não-lido do chat já vive nos canais, então mensagens não devem poluir o
// sino — mas o push (app fechado) continua a avisar.
func (s *Server) notifyPushOnly(ctx context.Context, userID uuid.UUID, kind, title, body, link string) {
	s.sendWebPush(ctx, userID, pushPayload{Kind: kind, Title: title, Body: body, Link: link})
}

// notifArgs serializa os dados estruturados da notificação como JSON (objeto vazio se nada).
func notifArgs(m map[string]string) json.RawMessage {
	if len(m) == 0 {
		return json.RawMessage("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// sendEmail dispara um e-mail se o sender estiver configurado (best-effort).
func (s *Server) sendEmail(to, subject, body string) bool {
	if s.email == nil {
		return false
	}
	if err := s.email.Send(to, subject, body); err != nil {
		log.Printf("notify: email to %s failed: %v", to, err)
		return false
	}
	return true
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	items, err := s.q.ListNotifications(r.Context(), db.ListNotificationsParams{UserID: mu.ID, Limit: 30})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "notifications.list_failed")
		return
	}
	if items == nil {
		items = []db.Notification{}
	}
	unread, _ := s.q.CountUnreadNotifications(r.Context(), mu.ID)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "notifications.invalid_id")
		return
	}
	if err := s.q.MarkNotificationRead(r.Context(), db.MarkNotificationReadParams{ID: id, UserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "notifications.update_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if err := s.q.MarkAllNotificationsRead(r.Context(), mu.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "notifications.update_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMarkNotificationsReadByKind marca lidas as notificações de um tipo (limpa a badge da área
// ao abri-la, ex.: meeting_invite ao ver Meet, file_share ao ver Arquivos).
func (s *Server) handleMarkNotificationsReadByKind(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Kind string `json:"kind"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Kind == "" {
		writeErr(w, http.StatusBadRequest, "notifications.invalid_kind")
		return
	}
	if err := s.q.MarkNotificationsReadByKind(r.Context(), db.MarkNotificationsReadByKindParams{UserID: mu.ID, Kind: req.Kind}); err != nil {
		writeErr(w, http.StatusInternalServerError, "notifications.update_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
