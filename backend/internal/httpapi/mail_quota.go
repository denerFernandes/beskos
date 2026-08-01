package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"mailin/backend/internal/db"
)

// Limite diário de envio por plano (docs/manager.md §4) + medição (tenant_mail_sent). Conta
// DESTINATÁRIOS (como o SES cobra). Três caminhos de envio passam por aqui: outbox (criação +
// worker), assistente (SendEmail) e JMAP EmailSubmission/set direto (interceptado no proxy).

// mailSendAllowed diz se o tenant ainda pode enviar para n destinatários hoje. Limite 0 = sem
// teto. Erro de leitura NÃO bloqueia (fail-open: quota comercial, não barreira de segurança —
// mesmo racional da quota de IA).
func (s *Server) mailSendAllowed(ctx context.Context, tenantID uuid.UUID, n int) bool {
	limit := s.tenantPlanByID(ctx, tenantID).MaxDailySend
	if limit <= 0 {
		return true
	}
	sent, err := s.q.MailSentToday(ctx, tenantID)
	if err != nil {
		log.Printf("mail quota: count: %v", err)
		return true
	}
	return int(sent)+n <= limit
}

// recordMailSent soma n destinatários ao contador de hoje (best-effort).
func (s *Server) recordMailSent(ctx context.Context, tenantID uuid.UUID, n int) {
	if n <= 0 {
		return
	}
	if err := s.q.IncrementMailSent(ctx, db.IncrementMailSentParams{TenantID: tenantID, N: int32(n)}); err != nil {
		log.Printf("mail quota: increment: %v", err)
	}
}

// jmapSubmissionRcpts conta os destinatários dos EmailSubmission/set de um corpo JMAP. 0 = o
// pedido não submete e-mail. Submissão sem envelope explícito conta 1 (o Stalwart deriva do Email).
func jmapSubmissionRcpts(body []byte) int {
	var req struct {
		MethodCalls []json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 0
	}
	total := 0
	for _, call := range req.MethodCalls {
		var parts []json.RawMessage
		if json.Unmarshal(call, &parts) != nil || len(parts) < 2 {
			continue
		}
		var name string
		if json.Unmarshal(parts[0], &name) != nil || name != "EmailSubmission/set" {
			continue
		}
		var args struct {
			Create map[string]struct {
				Envelope *struct {
					RcptTo []json.RawMessage `json:"rcptTo"`
				} `json:"envelope"`
			} `json:"create"`
		}
		if json.Unmarshal(parts[1], &args) != nil {
			continue
		}
		for _, c := range args.Create {
			if c.Envelope != nil && len(c.Envelope.RcptTo) > 0 {
				total += len(c.Envelope.RcptTo)
			} else {
				total++
			}
		}
	}
	return total
}

// jmapMailQuota intercepta POSTs JMAP: um pedido com EmailSubmission/set acima do limite diário
// leva 429 antes de chegar ao Stalwart; abaixo, conta e segue. A contagem é otimista (regista na
// entrada; uma submissão recusada pelo Stalwart fica contada) — aproximação aceite e registada.
func (s *Server) jmapMailQuota(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.Header.Get("Content-Type"), "json") {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if n := jmapSubmissionRcpts(body); n > 0 {
			mu := mailUserFrom(r.Context())
			// The account gate goes BEFORE the daily quota: an organisation whose trial ran out is
			// not over its limit, and telling them so would send them looking for the wrong thing.
			if s.accountState(r.Context(), mu.TenantID).Blocked() {
				writeErr(w, http.StatusPaymentRequired, "account.sending_blocked")
				return
			}
			if !s.mailSendAllowed(r.Context(), mu.TenantID, n) {
				writeErr(w, http.StatusTooManyRequests, "mail.daily_limit_reached")
				return
			}
			s.recordMailSent(r.Context(), mu.TenantID, n)
		}
		next.ServeHTTP(w, r)
	})
}
