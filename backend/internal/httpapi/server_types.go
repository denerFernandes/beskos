// Package httpapi expõe a API do control plane (painel do cliente + admin interno).
// Toda operação administrativa passa por aqui e checa posse do tenant antes de falar
// com o Stalwart (brief §2/§10). O frontend NUNCA recebe credencial de admin do Stalwart.
//
// This file holds the Server type and its constructor; the route table lives in server.go, kept
// apart so neither file grows into the other (law §1.4).
package httpapi

import (
	"context"
	"time"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/chat"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/notify"
	"github.com/beskosorg/beskos/backend/internal/secretbox"
	"github.com/beskosorg/beskos/backend/internal/ses"
	"github.com/beskosorg/beskos/backend/internal/sms"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
	"github.com/beskosorg/beskos/backend/internal/storage"
	"github.com/beskosorg/beskos/backend/internal/wiki"
)

type Server struct {
	q            *db.Queries
	sw           *stalwart.Client
	ai           ai.Provider // pode ser nil se a IA não estiver configurada
	sms          sms.Sender
	storage      *storage.Service // pode ser nil se o MinIO não estiver configurado
	billing      billing.Provider
	signupsOpen  bool
	billingCfg   billing.Settings
	office       *onlyoffice   // edição de office (OnlyOffice Document Server); nil = desligado
	meet         *meet         // videochamada (LiveKit); nil = desligado
	salesEmail   string        // where the website's contact form delivers; empty = form disabled
	email        *notify.Email // envio de e-mail (notificações); nil = desligado
	hub          *chat.Hub     // tempo real do chat (Redis pub/sub); nil = desligado
	wiki         *wiki.Service // base de conhecimento (Confluence-style); serviço sobre as queries
	wikiEmb      *ai.Embedder  // embeddings do RAG da wiki (nil = busca semântica desligada)
	push         *webPush      // Web Push (notificações desktop com o app fechado); nil = desligado
	maxUpload    int64         // teto por ficheiro no upload para o Files (bytes); 0 = default
	corsOrigins  []string      // origens permitidas para CORS (app nativo Tauri, web externa)
	cookieSecure bool          // cookie de sessão como Secure+SameSite=None (HTTPS/cross-site)
	publicURL    string        // URL pública da app p/ links absolutos que saem em e-mails (sem "/" final)
	brandURL     string        // fallback for appURL when publicURL is unset (BRAND_URL)
	mailHost     string        // host público do servidor de correio (IMAP/SMTP/JMAP) para os clientes externos
	mtaStsHost   string        // public name serving MTA-STS policies; every customer's mta-sts.<domain> points here
	aiProxyBase  string        // endpoint DeepSeek (OpenAI-compat) p/ o proxy de IA do editor
	aiProxyKey   string        // chave real do DeepSeek — SÓ no servidor, nunca vai ao browser
	aiProxyModel string        // modelo a forçar no proxy (ex.: deepseek-chat)

	loginIP    *rateLimiter // tentativas de login por IP
	loginUser  *rateLimiter // tentativas de login por conta (e-mail)
	mfaIP      *rateLimiter // verificações de 2º fator por IP (trava rotação de challenges)
	aiLimiter  *rateLimiter // chamadas de IA por usuário (custo/abuso)
	pubLimiter *rateLimiter // endpoints públicos (Meet sem login) por IP
	// presLimiter: balde próprio para o PDF da apresentação. Um PDF é lido por INTERVALOS (dezenas
	// de pedidos para um deck grande) e a sala inteira pode estar atrás do mesmo IP (escritório,
	// casa) — no balde público de 60/min bastavam dois participantes para levarem 429 e a
	// apresentação nunca abrir. Continua limitado (não é uma torneira aberta), mas com folga real.
	presLimiter *rateLimiter
	// agentLimiter tem balde próprio: uma execução agêntica gasta vários pedidos ao modelo, logo
	// não pode partilhar o limite das chamadas de IA de um só passo.
	agentLimiter *rateLimiter
	// confirms guarda as ações do assistente à espera de aprovação humana (§6.3).
	confirms *assistantConfirms

	// planCat é o cache do catálogo de planos lido da BD (manager M2; ver plans_catalog.go).
	planCat planCatalog

	// edition holds which Pro capabilities this instance has unlocked (edition.Load at startup).
	edition edition.Capabilities

	// agent é o motor agêntico do assistente (ciclo pensar→agir→observar). O provider por baixo
	// pode ser nil se a IA não estiver configurada — o handler é que verifica.
	agent *assistant.Engine
	// agentRouter escolhe o perfil de agente a partir da mensagem do utilizador (§3.2). Fica FORA
	// do motor de propósito: o encaminhamento decide-se antes de o ciclo arrancar e nunca a meio.
	agentRouter *assistant.Router

	totpBox *secretbox.Box // cifra o segredo TOTP em repouso (nil = guarda em claro)

	// sesRelay remove identidades SES ao apagar domínios (nil = SES desligado). O REGISTO de
	// identidades vive no worker (provisioning.EmailRelay); aqui só a limpeza.
	sesRelay *ses.Client
	// snsVerifier valida a assinatura do webhook de eventos do SES (bounces/complaints).
	snsVerifier *ses.SNSVerifier

	readiness func(context.Context) error // testa dependências críticas (ex.: ping ao Postgres) no /readyz; nil = só liveness
}

func NewServer(q *db.Queries, sw *stalwart.Client, aiProvider ai.Provider, smsSender sms.Sender, st *storage.Service, bp billing.Provider, bset billing.Settings, ed edition.Capabilities) *Server {
	srv := &Server{
		q: q, sw: sw, ai: aiProvider, sms: smsSender, storage: st, billing: bp, billingCfg: bset, edition: ed,
		signupsOpen: true, // por omissão aberto; o main aplica a configuração
		snsVerifier: ses.NewSNSVerifier(),
		loginIP:     newRateLimiter(30, 5*time.Minute),
		loginUser:   newRateLimiter(8, 5*time.Minute),
		mfaIP:       newRateLimiter(20, 5*time.Minute),
		aiLimiter:   newRateLimiter(30, time.Minute),
		pubLimiter:  newRateLimiter(60, time.Minute),
		presLimiter: newRateLimiter(600, time.Minute),
		// 10 execuções por 5 minutos: cada uma pode gastar até DefaultMaxSteps chamadas ao modelo.
		agentLimiter: newRateLimiter(10, 5*time.Minute),
		confirms:     newAssistantConfirms(),
		wiki:         wiki.New(q),
		wikiEmb:      ai.NewEmbedderFromEnv(),
	}
	srv.agent = srv.buildAssistant()
	srv.agentRouter = assistant.NewRouter(aiProvider)
	return srv
}
