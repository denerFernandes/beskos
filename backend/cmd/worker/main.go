// cmd/worker: roda a máquina de estados de provisionamento (§7), sempre retomando do
// último estado salvo, de forma idempotente. Ver internal/provisioning.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"mailin/backend/internal/ai"
	"mailin/backend/internal/billing"
	"mailin/backend/internal/chat"
	"mailin/backend/internal/config"
	"mailin/backend/internal/db"
	"mailin/backend/internal/httpapi"
	"mailin/backend/internal/mailbox"
	"mailin/backend/internal/provisioning"
	"mailin/backend/internal/secretbox"
	"mailin/backend/internal/ses"
	"mailin/backend/internal/stalwart"
	"mailin/backend/internal/storage"
	"mailin/backend/internal/wiki"
)

type vapidKeys struct{ public, private, subject string }

// scanReminders dispara os lembretes de evento cujo momento chegou: notificação no sino + Web Push
// para o dono, e marca reminded para não repetir. O texto é em pt-BR como as demais notificações.
func scanReminders(ctx context.Context, q *db.Queries, v vapidKeys) {
	rows, err := q.EventsToRemind(ctx)
	if err != nil {
		log.Printf("reminder-scan: query failed: %v", err)
		return
	}
	for _, e := range rows {
		owner := uuid.UUID(e.OwnerUserID.Bytes)
		body := "Começa às " + e.StartsAt.Format("15:04")
		if _, nerr := q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: e.TenantID, UserID: owner, Kind: "event_reminder",
			Title: "Lembrete: " + e.Title, Body: body, Link: "/mail/calendar", MeetingID: pgtype.UUID{},
		}); nerr != nil {
			log.Printf("reminder-scan: notify failed for event %s: %v", e.ID, nerr)
			continue
		}
		sendPushToUser(ctx, q, v, owner, map[string]string{"kind": "event_reminder", "title": "Lembrete: " + e.Title, "body": body, "link": "/mail/calendar"})
		_ = q.MarkEventReminded(ctx, e.ID)
	}
	if len(rows) > 0 {
		log.Printf("reminder-scan: fired %d reminder(s)", len(rows))
	}
}

// scanScheduledMessages envia as mensagens de chat cujo horário chegou: cria a mensagem e publica no
// hub (mesmo caminho do envio normal), para quem está no canal ver em tempo real; depois remove.
func scanScheduledMessages(ctx context.Context, q *db.Queries, hub *chat.Hub) {
	rows, err := q.DueScheduledMessages(ctx)
	if err != nil {
		log.Printf("sched-msg: query failed: %v", err)
		return
	}
	for _, sm := range rows {
		msg, cerr := q.CreateMessage(ctx, db.CreateMessageParams{TenantID: sm.TenantID, ChannelID: sm.ChannelID, UserID: sm.UserID, Body: sm.Body})
		if cerr != nil {
			log.Printf("sched-msg: create failed: %v", cerr)
			continue
		}
		if hub != nil {
			members, _ := q.ListMemberUserIDs(ctx, sm.ChannelID)
			rcpt := make([]string, 0, len(members))
			for _, m := range members {
				rcpt = append(rcpt, m.String())
			}
			payload, _ := json.Marshal(map[string]any{
				"id": msg.ID.String(), "channel_id": sm.ChannelID.String(), "user_id": sm.UserID.String(),
				"parent_id": "", "body": sm.Body, "author": sm.AuthorName, "author_email": sm.AuthorEmail,
				"created_at": msg.CreatedAt.Format(time.RFC3339), "reactions": []any{}, "attachments": []any{}, "reply_count": 0,
			})
			hub.Publish(ctx, chat.Event{Type: "message", ChannelID: sm.ChannelID.String(), Recipients: rcpt, Payload: payload})
		}
		_ = q.PurgeScheduledMessage(ctx, sm.ID)
	}
	if len(rows) > 0 {
		log.Printf("sched-msg: sent %d scheduled message(s)", len(rows))
	}
}

// scanMessageReminders dispara os lembretes de mensagem: notificação no sino + Web Push, depois remove.
func scanMessageReminders(ctx context.Context, q *db.Queries, v vapidKeys) {
	rows, err := q.DueMessageReminders(ctx)
	if err != nil {
		log.Printf("msg-remind: query failed: %v", err)
		return
	}
	for _, rm := range rows {
		body := rm.Note
		if body == "" {
			body = "Reminder about a message in #" + rm.ChannelName
		}
		args, _ := json.Marshal(map[string]string{"channel": rm.ChannelName, "note": rm.Note})
		if _, nerr := q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: rm.TenantID, UserID: rm.UserID, Kind: "chat_reminder",
			Title: "Reminder", Body: body, Link: "/mail/chat", MeetingID: pgtype.UUID{}, Args: args,
		}); nerr != nil {
			log.Printf("msg-remind: notify failed: %v", nerr)
			continue
		}
		sendPushToUser(ctx, q, v, rm.UserID, map[string]string{"kind": "chat_reminder", "title": "Reminder", "body": body, "link": "/mail/chat"})
		_ = q.PurgeMessageReminder(ctx, rm.ID)
	}
	if len(rows) > 0 {
		log.Printf("msg-remind: fired %d reminder(s)", len(rows))
	}
}

// sendPushToUser entrega um Web Push a todas as subscrições do utilizador. Poda subscrições mortas.
func sendPushToUser(ctx context.Context, q *db.Queries, v vapidKeys, userID uuid.UUID, payload map[string]string) {
	if v.private == "" {
		return
	}
	subs, err := q.ListPushSubscriptionsByUser(ctx, userID)
	if err != nil {
		return
	}
	msg, _ := json.Marshal(payload)
	for _, sub := range subs {
		resp, err := webpush.SendNotification(msg, &webpush.Subscription{
			Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
		}, &webpush.Options{Subscriber: v.subject, VAPIDPublicKey: v.public, VAPIDPrivateKey: v.private, TTL: 60})
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			_ = q.DeletePushSubscriptionByEndpoint(ctx, sub.Endpoint)
		}
		resp.Body.Close()
	}
}

// pollNewMail verifica a caixa de cada utilizador com push e notifica os e-mails novos (chegados
// depois de mail_seen_at). Primeira verificação só estabelece a base — evita uma enxurrada inicial.
func pollNewMail(ctx context.Context, q *db.Queries, box *secretbox.Box, stalwartURL string, v vapidKeys) {
	rows, err := q.UsersForMailPush(ctx)
	if err != nil {
		log.Printf("mail-push: query failed: %v", err)
		return
	}
	notified := 0
	for _, u := range rows {
		basic, derr := box.Decrypt(u.JmapCred)
		if derr != nil || basic == "" {
			continue
		}
		msgs, merr := mailbox.NewUserClient(stalwartURL, basic).InboxRecent(ctx, 20)
		if merr != nil || len(msgs) == 0 {
			continue
		}
		newest := msgs[0].ReceivedAt // ordenado do mais novo para o mais antigo
		if !u.MailSeenAt.Valid {
			_ = q.UpdateMailSeen(ctx, db.UpdateMailSeenParams{ID: u.ID, MailSeenAt: pgtype.Timestamptz{Time: newest, Valid: true}})
			continue
		}
		seen := u.MailSeenAt.Time
		sent := 0
		for _, m := range msgs {
			if !m.ReceivedAt.After(seen) {
				break // lista desc: o primeiro <= seen encerra
			}
			if sent < 5 { // teto por ciclo: não estourar o serviço de push numa rajada
				title := m.From
				if title == "" {
					title = "Novo e-mail"
				}
				body := m.Subject
				if body == "" {
					body = "(sem assunto)"
				}
				sendPushToUser(ctx, q, v, u.ID, map[string]string{"kind": "new_mail", "title": title, "body": body, "link": "/mail/mail"})
				sent++
				notified++
			}
		}
		if newest.After(seen) {
			_ = q.UpdateMailSeen(ctx, db.UpdateMailSeenParams{ID: u.ID, MailSeenAt: pgtype.Timestamptz{Time: newest, Valid: true}})
		}
	}
	if notified > 0 {
		log.Printf("mail-push: sent %d new-mail notification(s)", notified)
	}
}

// scanSnoozes devolve à Caixa de entrada os e-mails cujo prazo de snooze venceu. Usa a credencial
// JMAP cifrada de cada dono (decifrada com a MFA_ENC_KEY) para agir na caixa dele via JMAP.
func scanSnoozes(ctx context.Context, q *db.Queries, box *secretbox.Box, stalwartURL string) {
	rows, err := q.DueSnoozes(ctx)
	if err != nil {
		log.Printf("snooze-scan: query failed: %v", err)
		return
	}
	for _, s := range rows {
		basic, derr := box.Decrypt(s.JmapCred)
		if derr != nil || basic == "" {
			log.Printf("snooze-scan: cannot decrypt cred for snooze %s: %v", s.ID, derr)
			continue // sem cred não dá para mover; deixa para uma próxima (ou re-login regrava a cred)
		}
		uc := mailbox.NewUserClient(stalwartURL, basic)
		if err := uc.MoveToInbox(ctx, s.EmailID); err != nil {
			log.Printf("snooze-scan: move failed for %s: %v", s.ID, err)
			continue
		}
		_ = q.MarkSnoozeDone(ctx, s.ID)
	}
	if len(rows) > 0 {
		log.Printf("snooze-scan: woke %d email(s)", len(rows))
	}
}

// outboxLoop envia a outbox ORIENTADA A EVENTOS: dorme até ao próximo send_at (ou até um NOTIFY em
// 'outbox_new', que a API dispara ao enfileirar), varre os vencidos e volta a dormir. Sem polling
// constante — o e-mail sai quase logo após a janela de "desfazer". Cap de 60s como rede de segurança
// caso um NOTIFY se perca ou o LISTEN caia. (24/07/2026)
func outboxLoop(pool *pgxpool.Pool, q *db.Queries, box *secretbox.Box, stalwartURL string) {
	ctx := context.Background()
	const safetyCap = 60 * time.Second

	// Conexão dedicada para LISTEN. Se falhar, seguimos só com o timer (degrada para polling lento de
	// 60s em vez de parar de enviar).
	notify := make(chan struct{}, 1)
	if lc, err := pool.Acquire(ctx); err != nil {
		log.Printf("outbox: LISTEN indisponível (%v); a usar apenas timer", err)
	} else if _, err := lc.Exec(ctx, "LISTEN outbox_new"); err != nil {
		log.Printf("outbox: LISTEN falhou (%v); a usar apenas timer", err)
		lc.Release()
	} else {
		go func() {
			defer lc.Release()
			for {
				if _, err := lc.Conn().WaitForNotification(ctx); err != nil {
					log.Printf("outbox: WaitForNotification terminou (%v); só timer daqui em diante", err)
					return
				}
				select {
				case notify <- struct{}{}:
				default: // já há um sinal pendente
				}
			}
		}()
	}

	for {
		scanOutbox(ctx, q, box, stalwartURL)

		wait := safetyCap
		if next, err := q.NextOutboxDue(ctx); err == nil {
			// Há algo em fila: dorme até lá (piso de 100ms para não rodar em vazio), no máx. o cap.
			d := time.Until(next)
			if d < 100*time.Millisecond {
				d = 100 * time.Millisecond
			}
			if d < wait {
				wait = d
			}
		}
		// err != nil normalmente = MIN(send_at) NULL (fila vazia): dorme o cap à espera de NOTIFY.

		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-notify:
			t.Stop()
		}
	}
}

// scanOutbox submete os e-mails da outbox cujo send_at venceu (undo-send e agendados). Usa a
// credencial JMAP cifrada do dono para o EmailSubmission/set. Chamada pelo outboxLoop (orientado a
// eventos). Idempotente por status: só toca em 'pending'.
func scanOutbox(ctx context.Context, q *db.Queries, box *secretbox.Box, stalwartURL string) {
	rows, err := q.DueOutbox(ctx)
	if err != nil {
		log.Printf("outbox-scan: query failed: %v", err)
		return
	}
	sent := 0
	for _, o := range rows {
		// Limite diário de envio do plano (docs/manager.md §4): revalida no envio real — a criação
		// no outbox já checou, mas o limite pode ter esgotado entre agendar e vencer.
		if limit, lerr := q.TenantDailySendLimit(ctx, o.TenantID); lerr == nil && limit > 0 {
			if today, terr := q.MailSentToday(ctx, o.TenantID); terr == nil && int(today)+len(o.Rcpts) > int(limit) {
				log.Printf("outbox-scan: %s over daily send limit (%d), marking failed", o.ID, limit)
				_ = q.MarkOutboxFailed(ctx, o.ID)
				continue
			}
		}
		basic, derr := box.Decrypt(o.JmapCred)
		if derr != nil || basic == "" {
			log.Printf("outbox-scan: cannot decrypt cred for outbox %s: %v", o.ID, derr)
			continue // sem cred não dá para enviar; re-login regrava a cred e a próxima varredura tenta
		}
		uc := mailbox.NewUserClient(stalwartURL, basic)
		if err := uc.SubmitEmail(ctx, o.EmailID, o.IdentityID, o.FromEmail, o.Rcpts); err != nil {
			log.Printf("outbox-scan: submit failed for %s: %v", o.ID, err)
			_ = q.MarkOutboxFailed(ctx, o.ID)
			continue
		}
		_ = q.MarkOutboxSent(ctx, o.ID)
		// Medição (tenant_mail_sent): conta destinatários no envio REAL (como o SES cobra).
		_ = q.IncrementMailSent(ctx, db.IncrementMailSentParams{TenantID: o.TenantID, N: int32(len(o.Rcpts))})
		sent++
	}
	if sent > 0 {
		log.Printf("outbox-scan: sent %d email(s)", sent)
	}
}

// scanDueCards avisa o responsável de cards vencendo (até amanhã) ou já vencidos, uma vez por valor
// de prazo (due_notified_for). Datas são 'YYYY-MM-DD' (comparação lexicográfica = cronológica).
func scanDueCards(ctx context.Context, q *db.Queries) {
	today := time.Now().Format("2006-01-02")
	limit := time.Now().AddDate(0, 0, 1).Format("2006-01-02") // inclui o que vence até amanhã
	cards, err := q.CardsDueForNotify(ctx, limit)
	if err != nil {
		log.Printf("due-scan: query failed: %v", err)
		return
	}
	for _, c := range cards {
		title := "Prazo próximo"
		if c.Due < today {
			title = "Prazo vencido"
		}
		link := fmt.Sprintf("/mail/kanban/%s/%s", c.BoardID, c.ID)
		if _, err := q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: c.TenantID, UserID: c.AssigneeID.Bytes, Kind: "task_due",
			Title: title, Body: c.Title + " · " + c.Due, Link: link, MeetingID: pgtype.UUID{},
		}); err != nil {
			log.Printf("due-scan: notify failed for card %s: %v", c.ID, err)
			continue
		}
		_ = q.MarkCardDueNotified(ctx, c.ID)
	}
	if len(cards) > 0 {
		log.Printf("due-scan: notified %d card(s)", len(cards))
	}
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()

	q := db.New(pool)
	sw := stalwart.New(cfg.StalwartURL, cfg.StalwartAdminID, cfg.StalwartAdminPwd)

	// Verificação de DNS real por padrão; em dev/local domínios de teste não resolvem, então
	// PROVISION_SKIP_DNS_VERIFY=true pula a checagem. Nunca ligar isso em produção.
	var verifier provisioning.DNSVerifier = provisioning.NewNetDNSVerifier()
	if os.Getenv("PROVISION_SKIP_DNS_VERIFY") == "true" {
		verifier = provisioning.AlwaysVerified{}
		log.Printf("WARNING: DNS verification disabled (PROVISION_SKIP_DNS_VERIFY=true)")
	}

	machine := provisioning.NewMachine(sw, verifier, q)
	// Relay SES (produção, white-label): com SES_REGION + credenciais AWS, cada domínio novo é
	// registado no SES com BYODKIM (chave nossa) e a zona reportada esconde o Amazon (`_spf.<brand>`,
	// DKIM com a nossa chave, sem MAIL FROM). brand = MAIL_BRAND_DOMAIN (default STALWART_DEFAULT_DOMAIN).
	if relay := ses.NewFromEnv(); relay != nil {
		brand := os.Getenv("MAIL_BRAND_DOMAIN")
		if brand == "" {
			brand = os.Getenv("STALWART_DEFAULT_DOMAIN")
		}
		machine.WithRelay(relay, brand)
		log.Printf("SES relay enabled (white-label BYODKIM; brand=%s)", brand)
	}
	runner := provisioning.NewRunner(machine, q)

	log.Printf("provisioning worker started")

	// Prazos do Kanban + snoozes de e-mail num ticker próprio (1 min): independem do provisionamento.
	// O box decifra a credencial JMAP dos donos para o worker devolver e-mails "snoozed" à inbox.
	box := secretbox.New(cfg.MFAEncKey)
	vapid := vapidKeys{public: cfg.VAPIDPublicKey, private: cfg.VAPIDPrivateKey, subject: cfg.VAPIDSubject}
	// Hub p/ publicar mensagens agendadas em tempo real (mesmo canal Redis do chat). nil = sem Redis.
	var hub *chat.Hub
	if cfg.RedisURL != "" {
		if opt, rerr := redis.ParseURL(cfg.RedisURL); rerr == nil {
			hub = chat.New(redis.NewClient(opt))
		} else {
			log.Printf("worker: redis url invalid, scheduled messages won't broadcast live: %v", rerr)
		}
	}
	// Outbox (undo-send/agendados) num ticker rápido (10s): o undo-send precisa sair logo após a
	// janela curta; 1 min seria lento demais para o utilizador.
	// Envio da outbox ORIENTADO A EVENTOS (24/07/2026): em vez de sondar a BD num intervalo fixo, o
	// worker dorme EXATAMENTE até ao próximo send_at e a API acorda-o com pg_notify('outbox_new') ao
	// enfileirar. Resultado: o e-mail sai quase logo após a janela de "desfazer", sem polling
	// constante. Um cap de 60s serve de rede de segurança (se um NOTIFY se perder).
	go outboxLoop(pool, q, box, cfg.StalwartURL)
	go func() {
		due := time.NewTicker(time.Minute)
		defer due.Stop()
		scanDueCards(context.Background(), q) // uma vez ao subir, não só depois de 1 min
		scanSnoozes(context.Background(), q, box, cfg.StalwartURL)
		scanReminders(context.Background(), q, vapid)
		scanScheduledMessages(context.Background(), q, hub)
		scanMessageReminders(context.Background(), q, vapid)
		pollNewMail(context.Background(), q, box, cfg.StalwartURL, vapid)
		for range due.C {
			scanDueCards(context.Background(), q)
			scanSnoozes(context.Background(), q, box, cfg.StalwartURL)
			scanReminders(context.Background(), q, vapid)
			scanScheduledMessages(context.Background(), q, hub)
			scanMessageReminders(context.Background(), q, vapid)
			pollNewMail(context.Background(), q, box, cfg.StalwartURL, vapid)
		}
	}()

	// Metering do manager (M3): rollup diário de uso + retenção de gravações por plano.
	startMetering(q)

	// Montagem offline das gravações (track egress → MP4 via ffmpeg; fila por prioridade).
	// A montagem das gravações e a selagem partilham o mesmo serviço de storage.
	stWorker := meteringStorage(q)
	startAssembly(q, stWorker)
	startSealing(stWorker)

	// Tarefas em fundo do assistente (§7.5 do desenho do assistente). O executor é montado a
	// partir de um httpapi.Server porque precisa exatamente das mesmas dependências do motor
	// síncrono — ter duas montagens do mesmo motor seria ter duas superfícies de risco a divergir.
	startAssistantTasks(cfg, q, sw)

	// (Re)embedding da wiki para o RAG (docs/wiki-features.md §7.2): idempotente, roda a cada 30s e
	// processa páginas published desatualizadas. Sem EMBEDDINGS_API_KEY o RAG fica desligado.
	if emb := ai.NewEmbedderFromEnv(); emb != nil {
		wikiSvc := wiki.New(q)
		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for range t.C {
				if n, err := wikiSvc.EmbedPending(context.Background(), emb, 8); err != nil {
					log.Printf("wiki embed: %v", err)
				} else if n > 0 {
					log.Printf("wiki embed: %d page(s) indexed", n)
				}
			}
		}()
		log.Printf("wiki embedding worker started")
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		runner.Tick(context.Background())
	}
}

// startAssistantTasks arranca o consumidor da fila de tarefas do assistente. Sem IA configurada,
// não faz nada — e dizê-lo no log é melhor do que ficar em silêncio a não consumir nada.
func startAssistantTasks(cfg *config.Config, q *db.Queries, sw *stalwart.Client) {
	aiProvider, err := ai.New(ai.FromEnv())
	if err != nil || aiProvider == nil {
		log.Printf("assistant tasks disabled (no AI provider configured)")
		return
	}
	// Storage opcional: sem MinIO, docs.create em tarefas de fundo devolve um erro traduzível em
	// vez de gravar num vazio. É o mesmo comportamento do servidor síncrono.
	var st *storage.Service
	if ep := os.Getenv("MINIO_ENDPOINT"); ep != "" {
		if s, serr := storage.New(ep, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"),
			os.Getenv("MINIO_BUCKET"), os.Getenv("MINIO_REGION"), os.Getenv("MINIO_SSL") == "true"); serr == nil {
			if err := s.EnableEncryption(q, os.Getenv("FILE_ENC_KEYS")); err != nil {
				log.Fatalf("storage encryption: %v", err)
			}
			st = s
		} else {
			log.Printf("assistant tasks: storage unavailable, docs.create will fail: %v", serr)
		}
	}
	srv := httpapi.NewServer(q, sw, aiProvider, nil, st, nil, billing.Settings{})
	srv.EnableTOTPEncryption(cfg.MFAEncKey) // decifra a credencial JMAP das caixas
	worker := httpapi.NewAssistantTaskWorker(srv)

	go func() {
		// Sondagem rápida quando há trabalho, lenta quando não há: uma tarefa que acaba de ser
		// pedida não deve esperar segundos para arrancar, mas uma fila vazia não precisa de ser
		// consultada dez vezes por segundo.
		for {
			if !worker.Tick(context.Background()) {
				time.Sleep(3 * time.Second)
			}
		}
	}()
	log.Printf("assistant task worker started")
}
