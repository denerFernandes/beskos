// cmd/api: a API REST/JSON do control plane (painel do cliente + admin interno).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	_ "time/tzdata" // embute a base IANA de fusos no binário (containers slim não têm tzdata)

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/chat"
	"github.com/beskosorg/beskos/backend/internal/config"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/httpapi"
	"github.com/beskosorg/beskos/backend/internal/mailaudit"
	"github.com/beskosorg/beskos/backend/internal/notify"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
	"github.com/beskosorg/beskos/backend/internal/ses"
	"github.com/beskosorg/beskos/backend/internal/sms"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// The mail audit resolves DNS through this server when set. Without it the blocklist checks are
	// answered by a refusal from every list -- see mailaudit.UseResolver.
	mailaudit.UseResolver(os.Getenv("AUDIT_RESOLVER"))

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()

	sw := stalwart.New(cfg.StalwartURL, cfg.StalwartAdminID, cfg.StalwartAdminPwd)

	// IA: opcional. Sem AI_API_KEY, os endpoints de IA respondem 503 (feature desligada).
	var aiProvider ai.Provider
	if p, err := ai.New(ai.FromEnv()); err != nil {
		log.Printf("AI disabled: %v", err)
	} else {
		aiProvider = p
		log.Printf("AI enabled (provider=%s)", p.Name())
	}
	aiCfg := ai.FromEnv() // endpoint/chave/modelo em cru para o proxy de IA do editor OnlyOffice

	// SMS para MFA. Default "log" (dev). Provedor real (Twilio) via SMS_PROVIDER.
	smsSender, err := sms.New(sms.FromEnv())
	if err != nil {
		log.Fatalf("sms: %v", err)
	}

	// O mesmo *db.Queries serve o servidor e as chaves da cifra dos ficheiros.
	q := db.New(pool)

	// Storage (MinIO) opcional. Sem MINIO_ENDPOINT, os endpoints de arquivos respondem 503.
	var st *storage.Service
	if ep := os.Getenv("MINIO_ENDPOINT"); ep != "" {
		bucket := os.Getenv("MINIO_BUCKET")
		if bucket == "" {
			bucket = "mailin"
		}
		st, err = storage.New(ep, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"),
			bucket, os.Getenv("MINIO_REGION"), os.Getenv("MINIO_SSL") == "true")
		if err != nil {
			log.Fatalf("storage: %v", err)
		}
		// Cifra de envelope: sem FILE_ENC_KEYS o serviço escreve em claro, como antes. Uma chave
		// malformada é fatal de propósito — arrancar com ela seria escrever objetos que ninguém
		// mais consegue abrir. Ver docs/cifra-ficheiros.md.
		if err := st.EnableEncryption(q, os.Getenv("FILE_ENC_KEYS")); err != nil {
			log.Fatalf("storage encryption: %v", err)
		}
		if st.Encrypting() {
			log.Printf("storage enabled (%s), envelope encryption ON", ep)
		} else {
			log.Printf("storage enabled (%s), NOT encrypted (set FILE_ENC_KEYS)", ep)
		}
	} else {
		log.Printf("storage disabled (set MINIO_ENDPOINT)")
	}

	// Billing. Default "manual" (dev/self-serve). Produção: BILLING_PROVIDER=stripe + chaves.
	billingCfg := billing.FromEnv()
	billingProvider, err := billing.New(billingCfg)
	if err != nil {
		log.Fatalf("billing: %v", err)
	}
	bset := billingCfg.Settings()
	log.Printf("billing enabled (provider=%s currency=%s min_paid_users=%d)", billingProvider.Name(), bset.Currency, bset.MinPaidUsers)

	ed, err := edition.Load()
	if err != nil {
		log.Fatalf("edition: %v", err)
	}
	log.Printf("edition: %s", ed.Edition)

	srv := httpapi.NewServer(q, sw, aiProvider, smsSender, st, billingProvider, bset, ed)

	// Prontidão (/readyz): pinga o Postgres. O orquestrador só encaminha tráfego quando a base responde.
	srv.SetReadiness(pool.Ping)

	// SES (opcional): usado só para limpar a identidade ao apagar um domínio.
	if c := ses.NewFromEnv(); c != nil {
		srv.EnableSES(c)
		log.Printf("SES identity cleanup enabled")
	}

	// Cifra do segredo TOTP em repouso (opcional; sem MFA_ENC_KEY guarda em claro — só dev).
	srv.EnableTOTPEncryption(cfg.MFAEncKey)
	if cfg.MFAEncKey != "" {
		log.Printf("TOTP secret encryption enabled")
	}

	// Edição de office via OnlyOffice Document Server (opcional). Sem URLs/segredos, fica desligado.
	srv.EnableOnlyOffice(cfg.OnlyOfficeURL, cfg.OnlyOfficeInternalURL, cfg.FilesHostURL, cfg.OnlyOfficeJWTSecret, cfg.DocTokenSecret)
	// Brand shown in the editor's "about" panel. sourceURL is the modified-source offer AGPL §13
	// requires -- config already refuses to start without it in production; in dev, "about" ships
	// without the address. Falls back to BRAND_URL when PUBLIC_URL is unset (self-hosted, no
	// public domain yet).
	brandLinkURL := cfg.PublicURL
	if brandLinkURL == "" {
		brandLinkURL = cfg.BrandURL
	}
	srv.SetOfficeBranding(cfg.OnlyOfficeSourceURL, brandLinkURL)
	if cfg.OnlyOfficeURL != "" {
		log.Printf("office editing enabled (OnlyOffice %s)", cfg.OnlyOfficeURL)
		if cfg.OnlyOfficeSourceURL == "" {
			log.Printf("warning: ONLYOFFICE_SOURCE_URL is empty — the editor's About box cannot offer the modified source")
		}
	}

	// URL pública da app: usada para links absolutos que saem em e-mails (ex.: sala de reunião).
	srv.SetPublicURL(cfg.PublicURL)
	// Host público do correio: é o que mostramos a quem liga um cliente externo (IMAP/SMTP).
	srv.SetMailHost(cfg.StalwartHostname)
	// The host serving MTA-STS policies. The API needs it because it corrects the zone ON READ: a
	// domain provisioned before that substitution existed would name the container forever.
	srv.SetMTASTSHost(provisioning.MTASTSHost(cfg.StalwartHostname, os.Getenv("MTA_STS_HOSTNAME")))

	// Proxy de IA para o plugin do OnlyOffice: reencaminha ao DeepSeek com a chave server-side.
	if aiProvider != nil {
		srv.EnableAIProxy(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model)
	}

	// Meet via LiveKit (opcional).
	srv.EnableMeet(cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)
	if cfg.LiveKitURL != "" {
		log.Printf("meet enabled (LiveKit %s)", cfg.LiveKitURL)
	}
	// Gravação via Egress (opcional) — usa a API Twirp do LiveKit + MinIO como destino.
	if cfg.LiveKitHTTPURL != "" && os.Getenv("MINIO_ENDPOINT") != "" {
		srv.EnableMeetRecording(cfg.LiveKitHTTPURL, os.Getenv("MINIO_ENDPOINT"),
			os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"), os.Getenv("MINIO_BUCKET"))
		log.Printf("meet recording enabled (Egress → MinIO)")
	}

	// E-mail de notificação (opcional). Desligado, tudo o que é aviso ao cliente — conta criada,
	// teste, cobrança, novo login, convites e partilhas a gente de fora — deixa de sair, e sem este
	// aviso no arranque ninguém percebe porquê.
	if em := notify.NewEmail(cfg.NotifySMTPHost, cfg.NotifySMTPPort, cfg.NotifyFrom, cfg.NotifySMTPUser, cfg.NotifySMTPPassword, cfg.NotifySMTPInsecure); em != nil {
		srv.EnableEmail(em)
		log.Printf("email notifications enabled (from %s via %s)", cfg.NotifyFrom, cfg.NotifySMTPHost)
	} else {
		log.Printf("WARNING: email notifications DISABLED (missing NOTIFY_SMTP_HOST/NOTIFY_FROM) — " +
			"account, trial, billing, login alerts and external shares will not be delivered")
	}

	// Where the website's contact form delivers. A real mailbox somebody reads, not a table.
	srv.SetSalesEmail(cfg.SalesEmail)
	srv.SetBrandURL(cfg.BrandURL)

	srv.SetSignupsOpen(cfg.SignupsOpen)
	if !cfg.SignupsOpen {
		log.Printf("self-service signups CLOSED (SIGNUPS_OPEN=false) — /signup shows the coming-soon page")
	}

	// Teto por ficheiro no Files (configurável; default 100 GiB).
	srv.SetFileUploadLimit(cfg.FilesMaxUploadBytes)

	// CORS + cookie cross-site para o app nativo (Tauri). Sem APP_ORIGINS, fica desligado.
	srv.SetAppOrigins(cfg.AppOrigins, cfg.CookieSecure)
	if len(cfg.AppOrigins) > 0 {
		log.Printf("CORS enabled for app origins: %v (cookie secure=%v)", cfg.AppOrigins, cfg.CookieSecure)
	}

	// Web Push (opcional). Sem VAPID_PRIVATE_KEY, o push desktop com o app fechado fica desligado
	// (as notificações com aba aberta continuam a funcionar no cliente).
	if cfg.VAPIDPrivateKey != "" && cfg.VAPIDPublicKey != "" {
		srv.EnablePush(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject)
		log.Printf("web push enabled")
	}

	// Chat em tempo real via Redis pub/sub (opcional). Sem REDIS_URL, o chat fica só REST.
	if cfg.RedisURL != "" {
		opt, rerr := redis.ParseURL(cfg.RedisURL)
		if rerr != nil {
			log.Fatalf("redis url: %v", rerr)
		}
		hub := chat.New(redis.NewClient(opt))
		go hub.Run(context.Background())
		srv.EnableChat(hub)
		log.Printf("chat realtime enabled (Redis %s)", cfg.RedisURL)
	}

	log.Printf("control plane listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, srv.Router()); err != nil {
		log.Fatalf("http: %v", err)
	}
}
