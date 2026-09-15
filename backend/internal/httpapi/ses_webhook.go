package httpapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/ses"
)

// Webhook do SNS com os eventos do SES (bounce/complaint/delivery) — deliverability por tenant
// (docs/manager.md, 22/07/2026). Público mas FECHADO: só entra mensagem com assinatura SNS válida
// de cert *.amazonaws.com; a subscrição confirma-se sozinha (SubscribeURL com a mesma allowlist).
// Setup na AWS: SNS topic → subscrição HTTPS para <PUBLIC_URL>/api/webhooks/ses; no SES, ligar as
// notificações da identidade (ou configuration set) ao topic.

func (s *Server) handleSESWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "webhook.bad_body")
		return
	}
	msg, err := s.snsVerifier.Verify(body)
	if err != nil {
		log.Printf("ses webhook: reject: %v", err)
		writeErr(w, http.StatusForbidden, "webhook.bad_signature")
		return
	}
	switch msg.Type {
	case "SubscriptionConfirmation":
		if err := ses.ConfirmSubscription(msg.SubscribeURL); err != nil {
			log.Printf("ses webhook: confirm subscription: %v", err)
			writeErr(w, http.StatusBadGateway, "webhook.confirm_failed")
			return
		}
		log.Printf("ses webhook: subscription confirmed (topic %s)", msg.TopicArn)
	case "Notification":
		var ev ses.SESEvent
		if err := json.Unmarshal([]byte(msg.Message), &ev); err != nil {
			writeErr(w, http.StatusBadRequest, "webhook.bad_body")
			return
		}
		s.recordSESEvent(r, &ev)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// recordSESEvent grava um mail_event por destinatário afetado, resolvendo o tenant pelo domínio
// do remetente. Domínio desconhecido → tenant NULL (fica visível como "sem tenant" no manager).
func (s *Server) recordSESEvent(r *http.Request, ev *ses.SESEvent) {
	kind := ev.Kind()
	if kind == "" {
		return // evento sem interesse (open/click/etc.)
	}
	tenant := pgtype.UUID{}
	if at := strings.LastIndex(ev.Mail.Source, "@"); at >= 0 {
		domain := strings.ToLower(strings.Trim(ev.Mail.Source[at+1:], ">"))
		if tid, err := s.q.TenantByEmailDomain(r.Context(), domain); err == nil {
			tenant = pgtype.UUID{Bytes: tid, Valid: true}
		}
	}
	var subtype string
	var rcpts []string
	switch kind {
	case "bounce":
		subtype = ev.Bounce.BounceType
		for _, b := range ev.Bounce.BouncedRecipients {
			rcpts = append(rcpts, b.EmailAddress)
		}
	case "complaint":
		subtype = ev.Complaint.ComplaintFeedbackType
		for _, c := range ev.Complaint.ComplainedRecipients {
			rcpts = append(rcpts, c.EmailAddress)
		}
	case "delivery":
		rcpts = ev.Mail.Destination
	}
	if len(rcpts) == 0 {
		rcpts = []string{""}
	}
	for _, rc := range rcpts {
		if err := s.q.InsertMailEvent(r.Context(), db.InsertMailEventParams{
			TenantID: tenant, Type: kind, Subtype: subtype, Recipient: rc, Source: ev.Mail.Source,
		}); err != nil {
			log.Printf("ses webhook: insert event: %v", err)
		}
	}
}
