package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Web Push: notificações desktop entregues pelo servidor quando o Beskos está fechado. Cifra o
// payload para cada subscrição (RFC 8291) e envia ao serviço de push do browser. Desligado se as
// chaves VAPID não estiverem configuradas — nesse caso só valem as notificações com a aba aberta.
type webPush struct {
	publicKey  string
	privateKey string
	subject    string
}

// EnablePush liga o Web Push com o par de chaves VAPID (gerado uma vez, guardado em env).
func (s *Server) EnablePush(publicKey, privateKey, subject string) {
	s.push = &webPush{publicKey: publicKey, privateKey: privateKey, subject: subject}
}

// handlePushVapid devolve a chave pública VAPID, que o browser precisa para se subscrever.
func (s *Server) handlePushVapid(w http.ResponseWriter, _ *http.Request) {
	if s.push == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "publicKey": s.push.publicKey})
}

type pushSubBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// handlePushSubscribe guarda a subscrição do browser para este utilizador.
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var b pushSubBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Endpoint == "" || b.Keys.P256dh == "" || b.Keys.Auth == "" {
		writeErr(w, http.StatusBadRequest, "push.invalid")
		return
	}
	if err := s.q.UpsertPushSubscription(r.Context(), db.UpsertPushSubscriptionParams{
		TenantID: mu.TenantID, UserID: mu.ID, Endpoint: b.Endpoint, P256dh: b.Keys.P256dh, Auth: b.Keys.Auth,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "push.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePushUnsubscribe remove a subscrição (o browser desligou o push ou revogou a permissão).
func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Endpoint == "" {
		writeErr(w, http.StatusBadRequest, "push.invalid")
		return
	}
	if err := s.q.DeletePushSubscriptionByEndpoint(r.Context(), b.Endpoint); err != nil {
		writeErr(w, http.StatusInternalServerError, "push.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// pushPayload é o que o service worker recebe e mostra como notificação.
type pushPayload struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Link  string `json:"link"`
}

// sendWebPush entrega uma notificação a todas as subscrições de um utilizador. Best-effort: uma
// subscrição morta (404/410) é removida; os outros erros são só registados. Não bloqueia o chamador.
func (s *Server) sendWebPush(ctx context.Context, userID uuid.UUID, p pushPayload) {
	if s.push == nil {
		return
	}
	// Não Perturbe: não envia push desktop (a notificação in-app do sino ainda é gravada).
	if u, err := s.q.GetUserByID(ctx, userID); err == nil && u.Presence == "dnd" {
		return
	}
	subs, err := s.q.ListPushSubscriptionsByUser(ctx, userID)
	if err != nil {
		log.Printf("push: list subs failed: %v", err)
		return
	}
	if len(subs) == 0 {
		return
	}
	msg, err := json.Marshal(p)
	if err != nil {
		return
	}
	for _, sub := range subs {
		resp, err := webpush.SendNotification(msg, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
		}, &webpush.Options{
			Subscriber:      s.push.subject,
			VAPIDPublicKey:  s.push.publicKey,
			VAPIDPrivateKey: s.push.privateKey,
			TTL:             60,
		})
		if err != nil {
			log.Printf("push: send failed: %v", err)
			continue
		}
		// 404/410: a subscrição expirou no serviço de push — limpa para não tentar de novo.
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			if derr := s.q.DeletePushSubscriptionByEndpoint(ctx, sub.Endpoint); derr != nil {
				log.Printf("push: prune dead sub failed: %v", derr)
			}
		}
		resp.Body.Close()
	}
}
