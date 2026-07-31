// Package httpapi expõe a API do control plane (painel do cliente + admin interno).
// Toda operação administrativa passa por aqui e checa posse do tenant antes de falar
// com o Stalwart (brief §2/§10). O frontend NUNCA recebe credencial de admin do Stalwart.
package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"mailin/backend/internal/ai"
	"mailin/backend/internal/assistant"
	"mailin/backend/internal/billing"
	"mailin/backend/internal/chat"
	"mailin/backend/internal/db"
	"mailin/backend/internal/notify"
	"mailin/backend/internal/rbac"
	"mailin/backend/internal/secretbox"
	"mailin/backend/internal/ses"
	"mailin/backend/internal/sms"
	"mailin/backend/internal/stalwart"
	"mailin/backend/internal/storage"
	"mailin/backend/internal/wiki"
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
	email        *notify.Email // envio de e-mail (notificações); nil = desligado
	hub          *chat.Hub     // tempo real do chat (Redis pub/sub); nil = desligado
	wiki         *wiki.Service // base de conhecimento (Confluence-style); serviço sobre as queries
	wikiEmb      *ai.Embedder  // embeddings do RAG da wiki (nil = busca semântica desligada)
	push         *webPush      // Web Push (notificações desktop com o app fechado); nil = desligado
	maxUpload    int64         // teto por ficheiro no upload para o Files (bytes); 0 = default
	corsOrigins  []string      // origens permitidas para CORS (app nativo Tauri, web externa)
	cookieSecure bool          // cookie de sessão como Secure+SameSite=None (HTTPS/cross-site)
	publicURL    string        // URL pública da app p/ links absolutos que saem em e-mails (sem "/" final)
	mailHost     string        // host público do servidor de correio (IMAP/SMTP/JMAP) para os clientes externos
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

// SetReadiness liga a sonda de prontidão (/readyz): a função deve falhar se uma dependência crítica
// (ex.: Postgres) estiver indisponível. Sem ela, /readyz responde sempre "ready".
func (s *Server) SetReadiness(ping func(context.Context) error) { s.readiness = ping }

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.readiness != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.readiness(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// EnableSES liga a limpeza de identidades SES ao apagar domínios.
func (s *Server) EnableSES(c *ses.Client) { s.sesRelay = c }

// EnableTOTPEncryption liga a cifra do segredo TOTP em repouso (MFA_ENC_KEY). Vazio = desligado.
func (s *Server) EnableTOTPEncryption(key string) { s.totpBox = secretbox.New(key) }

// encTOTP/decTOTP cifram/decifram o segredo TOTP; com box nil, são no-op (texto claro).
func (s *Server) encTOTP(plain string) string {
	if v, err := s.totpBox.Encrypt(plain); err == nil {
		return v
	}
	return plain
}

func (s *Server) decTOTP(stored string) string {
	if v, err := s.totpBox.Decrypt(stored); err == nil {
		return v
	}
	return stored
}

// decryptCred decifra a credencial JMAP guardada (brokering H11). Fail-closed: string vazia ou erro
// de decifra → (,"", false), o que o proxy trata como "sem mailbox" (401 → re-login).
func (s *Server) decryptCred(stored string) (string, bool) {
	if stored == "" {
		return "", false
	}
	v, err := s.totpBox.Decrypt(stored)
	if err != nil || v == "" {
		return "", false
	}
	return v, true
}

// SetFileUploadLimit define o tamanho máximo por ficheiro no Files (configurável via env).
func (s *Server) SetFileUploadLimit(n int64) { s.maxUpload = n }

// SetAppOrigins liga o CORS para as origens dadas (ex.: app Tauri) e define se o cookie de sessão é
// Secure/SameSite=None (necessário cross-site sobre HTTPS).
func (s *Server) SetAppOrigins(origins []string, cookieSecure bool) {
	s.corsOrigins = origins
	s.cookieSecure = cookieSecure
}

// SetPublicURL define a URL pública da app, usada para montar links absolutos que saem para fora
// (ex.: o link de uma sala de reunião num e-mail). Sem ela, esses links ficariam relativos e
// inúteis fora da app.
func (s *Server) SetPublicURL(u string) { s.publicURL = strings.TrimRight(u, "/") }

// originHostPatterns extrai os hosts das origens configuradas para o OriginPatterns do WebSocket.
// Autoriza o app nativo (cross-origin) sem abrir o WS para qualquer site — o default (lista vazia)
// já aceita só same-origin, fechando o CSWSH.
func originHostPatterns(origins []string) []string {
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			out = append(out, u.Host)
		}
	}
	return out
}

func (s *Server) originAllowed(origin string) bool {
	for _, o := range s.corsOrigins {
		if o == origin {
			return true
		}
	}
	return false
}

// SetSignupsOpen abre/fecha o registo self-service (SIGNUPS_OPEN). Fechado, a página mostra
// "em breve" e a API recusa — o resto do produto continua igual para quem já é cliente.
func (s *Server) SetSignupsOpen(open bool) { s.signupsOpen = open }

// EnableEmail liga o envio de e-mail para notificações (opcional).
func (s *Server) EnableEmail(e *notify.Email) { s.email = e }

// EnableChat liga o tempo real do chat (WebSocket + Redis pub/sub).
func (s *Server) EnableChat(h *chat.Hub) { s.hub = h }

func NewServer(q *db.Queries, sw *stalwart.Client, aiProvider ai.Provider, smsSender sms.Sender, st *storage.Service, bp billing.Provider, bset billing.Settings) *Server {
	srv := &Server{
		q: q, sw: sw, ai: aiProvider, sms: smsSender, storage: st, billing: bp, billingCfg: bset,
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

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// requestLogger no lugar de middleware.Logger: o token de sessão pode chegar em ?access_token=
	// (WS/media) e o logger padrão gravaria a URL inteira — token de 7 dias em log = sequestro de
	// sessão (auditoria H6). Aqui a query sensível é redigida antes de logar.
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, requestLogger)
	r.Use(s.cors) // CORS para o app nativo (Tauri) — no-op se não houver origens configuradas

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	// Prontidão: healthz diz que o processo vive; readyz diz que as dependências (Postgres) respondem.
	// Load balancers/orquestradores usam readyz para não mandar tráfego antes da base estar pronta.
	r.Get("/readyz", s.handleReadyz)

	r.Route("/api", func(r chi.Router) {
		// As mesmas sondas, agora alcançáveis de fora: o proxy público só encaminha /api para cá,
		// e sem isto a página de estado media o servidor de ficheiros estáticos em vez da API.
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
		r.Get("/readyz", s.handleReadyz)

		// Públicos. Login unificado: com caixa valida no Stalwart, sem caixa (organização
		// recém-criada) valida no nosso cadastro. O /app deixou de existir.
		r.Post("/auth/signup", s.handleSignup)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/webmail/login", s.handleLogin)       // alias que o webmail já usa
		r.Post("/auth/mfa/verify", s.handleMFAVerify) // 2º passo do login

		// TUDO atrás da mesma sessão. O RBAC decide o que cada papel enxerga.
		r.Group(func(r chi.Router) {
			r.Use(s.requireWebmailSession)
			r.Use(s.csrfGuard)
			r.Post("/auth/logout", s.handleWebmailLogout)
			r.Post("/webmail/logout", s.handleWebmailLogout)
			r.Post("/mail/snooze", s.handleSnoozeEmail)         // adiar e-mail (worker devolve à inbox)
			r.Post("/mail/outbox", s.handleCreateOutbox)        // agenda o envio (undo-send / agendado)
			r.Get("/mail/outbox", s.handleListOutbox)           // e-mails ainda por enviar
			r.Delete("/mail/outbox/{id}", s.handleCancelOutbox) // cancela um envio pendente
			r.Get("/mail/templates", s.handleListTemplates)     // templates de resposta (canned)
			r.Post("/mail/templates", s.handleCreateTemplate)
			r.Patch("/mail/templates/{id}", s.handleUpdateTemplate)
			r.Delete("/mail/templates/{id}", s.handleDeleteTemplate)
			r.Get("/me", s.handleMe)
			r.Get("/me/mail-connection", s.handleMailConnection) // IMAP/SMTP para clientes externos

			// MFA (enrollment do 2º fator). Requer sessão já ativa.
			// Política de segurança da organização (MFA obrigatório) — só quem gere a org.
			r.With(s.requirePerm(rbac.PermOrgSecurity)).Get("/org/security", s.handleGetSecurityPolicy)
			r.With(s.requirePerm(rbac.PermOrgSecurity)).Put("/org/security", s.handleSetSecurityPolicy)

			r.Post("/mfa/totp/start", s.handleTOTPStart)
			r.Post("/mfa/totp/enable", s.handleTOTPEnable)
			r.Post("/mfa/sms/start", s.handleSMSStart)
			r.Post("/mfa/sms/enable", s.handleSMSEnable)
			r.Post("/mfa/disable", s.handleMFADisable)
			// Perfil do utilizador (menu do avatar): dados + troca de senha (step-up).
			r.Get("/me/profile", s.handleGetMyProfile)
			r.Put("/me/profile", s.handleUpdateMyProfile)
			r.Post("/me/password", s.handleChangeMyPassword)
			r.Patch("/me/settings", s.handleUpdateSettings) // preferências (idioma), persistidas
			r.Put("/me/status", s.handleSetMyStatus)        // status do utilizador (emoji + texto)
			r.Put("/me/presence", s.handleSetPresence)      // presença rica (active/away/dnd)
			r.Get("/user-status", s.handleGetUserStatus)    // status de um colega (por e-mail)

			// Domínios/mailboxes — gated por permissão (RBAC).
			// Marca da organização: qualquer utilizador lê (para o topo do app); só admin/owner grava.
			r.Get("/branding", s.handleGetBranding)
			r.With(s.requirePerm(rbac.PermOrgBranding)).Patch("/branding", s.handleUpdateBranding)

			// Apps da organização: qualquer utilizador lê (o rail filtra); só admin/owner liga/desliga.
			r.Get("/apps", s.handleGetApps)
			r.With(s.requirePerm(rbac.PermOrgBranding)).Put("/apps", s.handleSetApps)

			// Avisos da organização (mural). Todos leem os publicados; admin/owner gerem.
			r.Get("/announcements", s.handleListAnnouncements)
			r.With(s.requirePerm(rbac.PermAnnouncements)).Post("/announcements", s.handleCreateAnnouncement)
			r.With(s.requirePerm(rbac.PermAnnouncements)).Patch("/announcements/{id}", s.handleSetAnnouncementPublished)
			r.With(s.requirePerm(rbac.PermAnnouncements)).Delete("/announcements/{id}", s.handleDeleteAnnouncement)

			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/domains", s.handleListDomains)
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/domains/{id}", s.handleGetDomain)
			// Estado do DNS registo a registo + exportar a zona (docs/DNS-PAGE.md). Informativo: quem
			// promove o domínio continua a ser o worker.
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/domains/{id}/dns/check", s.handleCheckDomainDNS)
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/domains/{id}/dns/export", s.handleExportDomainDNS)
			r.With(s.requirePerm(rbac.PermDomainsManage)).Post("/domains", s.handleCreateDomain)
			r.With(s.requirePerm(rbac.PermDomainsDelete)).Delete("/domains/{id}", s.handleDeleteDomain)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Post("/domains/{id}/mailboxes", s.handleCreateMailbox)
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/mailboxes", s.handleListMailboxes)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Post("/mailboxes/{id}/password", s.handleResetMailboxPassword)

			// Aliases de e-mail (contacto@empresa.com → caixa de alguém). Gerir é operação de caixas;
			// ver a lista basta a quem já vê os domínios. Os endereços PRÓPRIOS (o "De:" do compositor)
			// não são administração — qualquer utilizador autenticado lê os seus.
			r.With(s.requirePerm(rbac.PermDomainsView)).Get("/aliases", s.handleListAliases)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Post("/aliases", s.handleCreateAlias)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Delete("/aliases/{id}", s.handleDeleteAlias)
			r.Get("/me/addresses", s.handleMyAddresses)

			// Usuários = caixa + papel (unificado). Criar provisiona caixa + conta juntas.
			r.With(s.requirePerm(rbac.PermMembersInvite)).Post("/users", s.handleCreateUser)
			r.With(s.requirePerm(rbac.PermMailboxManage)).Post("/users/reset-password", s.handleResetUserPassword)

			// Membros e convites (RBAC).
			r.With(s.requirePerm(rbac.PermMembersView)).Get("/members", s.handleListMembers)
			r.With(s.requirePerm(rbac.PermMembersManage)).Patch("/members/{id}/role", s.handleChangeRole)
			// Exceções de permissão por membro (RBAC granular): ver e gravar.
			r.With(s.requirePerm(rbac.PermMembersManage)).Get("/members/{id}/permissions", s.handleGetMemberPermissions)
			r.With(s.requirePerm(rbac.PermMembersManage)).Put("/members/{id}/permissions", s.handleSetMemberPermissions)
			r.With(s.requirePerm(rbac.PermMembersManage)).Delete("/members/{id}", s.handleRemoveMember)
			// Grupos DO usuário (um usuário pode estar em vários).
			r.With(s.requirePerm(rbac.PermMembersView)).Get("/members/{id}/groups", s.handleUserGroups)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Put("/members/{id}/groups", s.handleSetUserGroups)
			r.With(s.requirePerm(rbac.PermMembersManage)).Get("/invites", s.handleListInvites)
			r.With(s.requirePerm(rbac.PermMembersInvite)).Post("/invites", s.handleCreateInvite)
			r.With(s.requirePerm(rbac.PermMembersManage)).Delete("/invites/{id}", s.handleRevokeInvite)

			// Grupos de utilizadores (criar grupo cria o canal privado no chat).
			r.With(s.requirePerm(rbac.PermMembersView)).Get("/groups", s.handleListGroups)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Post("/groups", s.handleCreateGroup)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Patch("/groups/{id}", s.handleUpdateGroup)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Delete("/groups/{id}", s.handleDeleteGroup)
			r.With(s.requirePerm(rbac.PermMembersView)).Get("/groups/{id}/members", s.handleListGroupMembers)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Post("/groups/{id}/members", s.handleAddGroupMember)
			r.With(s.requirePerm(rbac.PermGroupsManage)).Delete("/groups/{id}/members/{userId}", s.handleRemoveGroupMember)

			// Billing e uso/cotas.
			r.With(s.requirePerm(rbac.PermUsageView)).Get("/usage", s.handleUsage)
			r.With(s.requirePerm(rbac.PermBillingView)).Get("/billing", s.handleGetBilling)
			r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/checkout", s.handleCheckout)
			r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/cancel", s.handleBillingCancel)
			r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/resume", s.handleBillingResume)
			r.With(s.requirePerm(rbac.PermBillingManage)).Post("/billing/portal", s.handleBillingPortal)
			// Dados de faturação (quem assina, para onde vai a fatura) + simulação com IVA.
			r.With(s.requirePerm(rbac.PermBillingView)).Get("/billing/profile", s.handleGetBillingProfile)
			r.With(s.requirePerm(rbac.PermBillingManage)).Put("/billing/profile", s.handleSaveBillingProfile)
			r.With(s.requirePerm(rbac.PermBillingView)).Post("/billing/quote", s.handleBillingQuote)

			// IA sobre e-mail (conteúdo vem do frontend, que lê via JMAP direto).
			// Assistente agêntico (ciclo com ferramentas). Distinto de /ai/*, que é de um só passo.
			r.Post("/assistant/chat", s.handleAssistantChat)
			r.Get("/assistant/conversations", s.handleAssistantConversations)
			r.Get("/assistant/conversations/{id}", s.handleAssistantConversation)
			r.Delete("/assistant/conversations/{id}", s.handleAssistantConversationDelete)
			r.Post("/assistant/confirm/{id}", s.handleAssistantConfirm)
			r.Get("/assistant/tasks", s.handleAssistantTasks)
			r.Post("/assistant/tasks/{id}/cancel", s.handleAssistantTaskCancel)
			// Memória de factos (§8). Ecrã de gestão: ver, acrescentar à mão, apagar item a item ou
			// tudo. É o direito do utilizador de ver e apagar o que a app sabe sobre si (RGPD, §3.5).
			r.Get("/assistant/memory", s.handleAssistantMemory)
			r.Post("/assistant/memory", s.handleAssistantMemoryCreate)
			r.Delete("/assistant/memory/{id}", s.handleAssistantMemoryDelete)
			r.Post("/assistant/memory/clear", s.handleAssistantMemoryClear)
			// Orçamento (§9), auditoria visível (§6.4) e exclusão de pastas (§6.6).
			s.registerAssistantGovernance(r)
			// Perfis de agente. Ler é para qualquer utilizador (o seletor do painel precisa da
			// lista); criar, alterar e ligar/desligar é da administração da organização.
			r.Get("/assistant/agents", s.handleAssistantAgents)
			r.With(s.requirePerm(rbac.PermAssistantManage)).Post("/assistant/agents", s.handleAssistantAgentCreate)
			r.With(s.requirePerm(rbac.PermAssistantManage)).Put("/assistant/agents/{id}", s.handleAssistantAgentUpdate)
			r.With(s.requirePerm(rbac.PermAssistantManage)).Delete("/assistant/agents/{id}", s.handleAssistantAgentDelete)
			r.With(s.requirePerm(rbac.PermAssistantManage)).Post("/assistant/agents/{key}/enabled", s.handleAssistantAgentToggle)

			r.Post("/ai/summarize", s.handleAISummarize)
			r.Post("/ai/draft", s.handleAIDraft)
			r.Post("/ai/task", s.handleAIPlanTask)
			r.Post("/ai/extract-tasks", s.handleAIExtractTasks)
			r.Post("/ai/briefing", s.handleAIBriefing)
			r.Post("/ai/wiki-write", s.handleAIWikiWrite) // conteúdo p/ o editor BlockNote da wiki

			// Marcadores (catálogo por tenant).
			r.Get("/labels", s.handleListLabels)
			r.Post("/labels", s.handleCreateLabel)
			r.Delete("/labels/{id}", s.handleDeleteLabel)

			// Kanban.
			r.Get("/boards", s.handleListBoards)
			r.Get("/boards/archived", s.handleListArchivedBoards) // quadros arquivados (restaurar)
			r.Post("/boards", s.handleCreateBoard)
			r.Post("/boards/{id}/archive", s.handleArchiveBoard)            // arquivar/restaurar quadro
			r.Get("/boards/{id}/archived-cards", s.handleListArchivedCards) // cards arquivados do quadro
			r.Get("/boards/{id}/automations", s.handleListAutomations)      // regras de automação
			r.Post("/boards/{id}/automations", s.handleCreateAutomation)
			r.Delete("/automations/{id}", s.handleDeleteAutomation)
			r.Get("/boards/{id}/access", s.handleBoardAccess)
			r.Get("/boards/{id}/grants", s.handleListBoardGrants)
			r.Put("/boards/{id}/grants", s.handleSetBoardGrant)
			r.Delete("/boards/{id}/grants/{grantId}", s.handleDeleteBoardGrant)
			r.Get("/boards/{id}", s.handleGetBoard)
			r.Patch("/boards/{id}", s.handleUpdateBoard)
			r.Delete("/boards/{id}", s.handleDeleteBoard)
			r.Post("/boards/{id}/cards", s.handleCreateCard)
			r.Get("/cards/search", s.handleSearchCards) // tarefas por chave/número/título (o /task do chat)
			r.Patch("/cards/{id}", s.handleUpdateCard)
			r.Patch("/cards/{id}/move", s.handleMoveCard)
			r.Post("/cards/{id}/archive", s.handleArchiveCard)       // arquivar/restaurar card
			r.Post("/cards/{id}/assignees", s.handleAddCardAssignee) // responsável adicional
			r.Delete("/cards/{id}/assignees/{userId}", s.handleRemoveCardAssignee)
			r.Post("/cards/{id}/deps", s.handleAddCardDep) // dependência (bloqueado por)
			r.Delete("/cards/{id}/deps/{depId}", s.handleRemoveCardDep)
			r.Post("/columns/{id}/reorder", s.handleReorderColumn) // ordem vertical do drag-and-drop
			r.Delete("/cards/{id}", s.handleDeleteCard)
			r.Post("/boards/{id}/columns", s.handleCreateColumn)
			r.Post("/boards/{id}/columns/reorder", s.handleReorderColumns) // ordem horizontal das colunas
			r.Patch("/columns/{id}", s.handleUpdateColumn)
			r.Delete("/columns/{id}", s.handleDeleteColumn)

			// Calendário.
			r.Get("/calendars", s.handleListCalendars)
			r.Post("/calendars", s.handleCreateCalendar)
			r.Patch("/calendars/{id}", s.handleUpdateCalendar)
			r.Delete("/calendars/{id}", s.handleDeleteCalendar)
			r.Get("/events/free-busy", s.handleFreeBusy) // agenda de colega (só ocupado/livre)
			// Booking: página pública de agendamento do colaborador (estilo Calendly).
			r.Get("/booking/page", s.handleGetBookingPage)
			r.Put("/booking/page", s.handleSaveBookingPage)
			r.Post("/timeoff", s.handleCreateTimeOff) // day off / férias (+ resposta automática)
			r.Get("/events", s.handleListEvents)
			r.Post("/events", s.handleCreateEvent)
			r.Get("/events/export", s.handleExportEvents)           // iCalendar (.ics)
			r.Post("/events/import", s.handleImportEvents)          // iCalendar (.ics)
			r.Get("/events/{id}/invites", s.handleEventInvites)     // convidados + RSVP
			r.Get("/events/{id}/externals", s.handleEventExternals) // convidados externos (e-mail)
			r.Post("/events/{id}/rsvp", s.handleEventRSVP)          // responder ao convite
			r.Patch("/events/{id}", s.handleUpdateEvent)
			r.Delete("/events/{id}", s.handleDeleteEvent)
			r.Post("/events/{id}/duplicate", s.handleDuplicateEvent) // cópia independente
			r.Post("/events/{id}/restore", s.handleRestoreEvent)     // desfaz exclusão (undo)
			r.Post("/events/{id}/meet", s.handleAddMeetToEvent)      // vincula sala Meet a evento existente

			// Tags (catálogo compartilhado: eventos, contatos, …).
			r.Get("/tags", s.handleListTags)
			r.Post("/tags", s.handleUpsertTag)
			r.Delete("/tags/{id}", s.handleDeleteTag)

			// IA do calendário.
			r.Post("/ai/calendar/parse", s.handleAIParseEvent)
			r.Post("/ai/calendar/suggest", s.handleAISuggestSlots)
			r.Post("/ai/calendar/agenda", s.handleAIAgenda)

			// CONTATOS: empresa ou pessoa, numa lista só. Ver é pelo grupo; editar é do dono.
			r.Get("/contacts", s.handleListContacts)
			r.Post("/contacts", s.handleCreateContact)
			r.Post("/contacts/seen", s.handleSeenRecipients)   // grava destinatários enviados como contactos
			r.Get("/contacts/export", s.handleExportContacts)  // vCard/CSV
			r.Post("/contacts/import", s.handleImportContacts) // vCard/CSV
			r.Post("/contacts/enrich", s.handleEnrichContacts) // liga pessoas↔empresas por domínio
			r.Get("/contacts/{id}", s.handleGetContact)
			r.Get("/contacts/{id}/vcard", s.handleContactVCard) // um contato como .vcf (anexar a e-mail)
			r.Patch("/contacts/{id}", s.handleUpdateContact)
			r.Delete("/contacts/{id}", s.handleDeleteContact)
			r.Post("/contacts/{id}/merge", s.handleMergeContacts)         // funde um duplicado neste
			r.Post("/contacts/{id}/avatar", s.handleSetContactAvatar)     // liga foto (file_id) ao contato
			r.Post("/contacts/{id}/transfer", s.handleTransferContact)    // transfere posse a um colega
			r.Patch("/contacts/{id}/notes/{noteId}", s.handleEditCrmNote) // editar nota (autor)
			r.Get("/contacts/{id}/notes", s.handleListCrmNotes)
			r.Post("/contacts/{id}/notes", s.handleCreateCrmNote)
			r.Get("/contacts/{id}/tasks", s.handleListContactTasks)
			r.Delete("/crm-notes/{id}", s.handleDeleteCrmNote)
			// Wiki / base de conhecimento (N wikis por tenant; acesso resolvido no serviço via SQL `vis`).
			s.mountWiki(r)
			s.mountNotes(r)
			// Partilha de um grupo de contatos com equipas E/OU pessoas — só o dono do grupo decide.
			r.Get("/contact-groups/{id}/shares", s.handleListGroupShares)
			r.Put("/contact-groups/{id}/shares", s.handleSetGroupShares)
			r.Get("/contact-share-teams", s.handleShareTeams) // equipas p/ o picker (não exige admin)
			// Colegas do tenant (id/nome/e-mail): quem faz uma tarefa é alguém da equipa.
			r.Get("/colleagues", s.handleListColleagues)
			// Preferências do utilizador (assinatura, prefs por app).
			r.Get("/settings", s.handleGetSettings)
			r.Patch("/settings", s.handleUpdateSettings)
			// Dispositivos: tokens para clientes headless (daemon de sync). O utilizador cria/lista/
			// revoga os seus; o token cru só volta na criação (guardamos só o hash).
			r.Get("/devices", s.handleListDeviceTokens)
			r.Post("/devices", s.handleCreateDeviceToken)
			r.Delete("/devices/{id}", s.handleRevokeDeviceToken)
			// Comentários da tarefa (disparam os avisos — ver tasks_notify.go).
			r.Patch("/boards/{id}/prefix", s.handleUpdateBoardPrefix)
			// Biblioteca de GIFs pessoal (o GIF é um ficheiro do Drive; isto é só a gaveta).
			r.Get("/gifs", s.handleListGifs)
			r.Post("/gifs", s.handleAddGif)
			r.Delete("/gifs/{id}", s.handleDeleteGif)
			r.Get("/cards/{id}/comments", s.handleListCardComments)
			r.Post("/cards/{id}/comments", s.handleCreateCardComment)
			r.Patch("/cards/{id}/comments/{cid}", s.handlePatchCardComment) // editar comentário (autor)
			r.Delete("/cards/{id}/comments/{cid}", s.handleDeleteCardComment)
			r.Get("/cards/{id}/activity", s.handleListCardActivity)
			r.Get("/cards/{id}/attachments", s.handleListCardAttachments)
			r.Post("/cards/{id}/attachments", s.handleAttachFile)
			r.Delete("/cards/{id}/attachments/{attId}", s.handleDetachFile)
			r.Get("/cards/{id}/attachments/{attId}/download", s.handleDownloadCardAttachment)
			r.Get("/cards/{id}/checklist", s.handleListChecklist)
			r.Post("/cards/{id}/checklist", s.handleCreateChecklistItem)
			r.Patch("/cards/{id}/checklist/{itemId}", s.handleToggleChecklistItem)
			r.Delete("/cards/{id}/checklist/{itemId}", s.handleDeleteChecklistItem)
			r.Patch("/contacts/{id}", s.handleUpdateContact)
			r.Delete("/contacts/{id}", s.handleDeleteContact)
			r.Get("/contact-groups", s.handleListContactGroups)
			r.Get("/contact-groups/{id}/emails", s.handleContactGroupEmails) // lista de distribuição
			r.Post("/contact-groups", s.handleCreateContactGroup)
			r.Patch("/contact-groups/{id}", s.handleUpdateContactGroup)
			r.Delete("/contact-groups/{id}", s.handleDeleteContactGroup)

			// Armazenamento (arquivos sobre MinIO) — pastas, mover, compartilhar.
			r.Get("/files", s.handleListFiles)
			r.Get("/files/search", s.handleSearchFiles)      // busca global recursiva por nome
			r.Get("/files/changes", s.handleListFileChanges) // feed incremental p/ o daemon de sync (M0)
			r.Get("/files/trash", s.handleListTrash)         // lixeira (soft-deleted)
			r.Delete("/files/trash", s.handleEmptyTrash)     // esvaziar lixeira (purga tudo)
			r.Post("/files", s.handleUploadFile)
			r.Post("/files/batch", s.handleBatchFiles) // ações em lote: trash | restore | move
			r.Get("/files/{id}/download", s.handleDownloadFile)
			r.Get("/files/{id}/thumb", s.handleThumbnail)        // miniatura JPEG (só imagens)
			r.Post("/files/{id}/star", s.handleStarFile)         // favorito on/off
			r.Post("/files/{id}/copy", s.handleCopyFile)         // duplica (reusa o blob)
			r.Patch("/files/{id}", s.handlePatchFile)            // mover/renomear
			r.Delete("/files/{id}", s.handleDeleteFile)          // soft delete → lixeira
			r.Post("/files/{id}/restore", s.handleRestoreFile)   // restaurar da lixeira
			r.Delete("/files/{id}/permanent", s.handlePurgeFile) // apagar de vez (irreversível)
			r.Get("/files/{id}/versions", s.handleListVersions)  // histórico de versões
			r.Get("/files/{id}/versions/{versionId}/download", s.handleDownloadVersion)
			r.Post("/files/{id}/versions/{versionId}/restore", s.handleRestoreVersion)
			r.Post("/files/{id}/share", s.handleShareFile)
			r.Get("/files/{id}/shares", s.handleListShares)
			r.Delete("/files/{id}/shares/{shareId}", s.handleDeleteShare)
			r.Get("/files/limits", s.handleFileLimits)           // teto por ficheiro (env)
			r.Get("/files-shared", s.handleListSharedWithMe)     // "partilhado comigo"
			r.Get("/files/{id}/link", s.handleGetFileLink)       // token do link público ativo (ou vazio)
			r.Post("/files/{id}/link", s.handleCreateFileLink)   // cria/reaproveita o link público (anexos grandes)
			r.Delete("/files/{id}/link", s.handleRevokeFileLink) // revoga o link público
			r.Post("/files/{id}/share-recipients", s.handleShareWithRecipients)
			r.Post("/files/{id}/share-group", s.handleShareWithGroup) // partilha com uma equipa (expande em e-mails)
			r.Get("/my-groups", s.handleMyGroups)                     // equipas do utilizador (para partilhar)
			r.Post("/folders", s.handleCreateFolder)
			r.Get("/folders/{id}/zip", s.handleZipFolder) // baixa a pasta (recursivo) como .zip
			r.Patch("/folders/{id}", s.handleRenameFolder)
			r.Post("/folders/{id}/move", s.handleMoveFolder) // muda o pai (com guarda de ciclo)
			r.Delete("/folders/{id}", s.handleDeleteFolder)

			// Edição de office (OnlyOffice): criar em branco + abrir a sessão do editor.
			r.Post("/files/office", s.handleCreateOfficeFile)
			r.Post("/files/{id}/edit-session", s.handleEditSession)
			r.Get("/ai/editor-key", s.handleAIEditorKey) // config do plugin de IA do editor (proxy)

			// Meet (LiveKit): salas, reuniões agendadas, RSVP.
			r.Post("/meet/join", s.handleJoinMeet)
			r.Get("/meet/meetings", s.handleListMeetings)
			r.Post("/meet/meetings", s.handleCreateMeeting)
			r.Post("/meet/meetings/{id}/rsvp", s.handleRSVP)
			r.Post("/meet/meetings/{id}/cancel", s.handleCancelMeeting)
			r.Patch("/meet/meetings/{id}", s.handleUpdateMeeting) // editar/reagendar
			// Controles do organizador (a própria query filtra owner_user_id → só o dono altera).
			r.Patch("/meet/meetings/{id}/controls", s.handleUpdateMeetControls)
			// v1.5: quem está na sala, expulsar, e as notas por e-mail. Só o dono nos dois últimos.
			r.Get("/meet/meetings/{id}/participants", s.handleMeetParticipants)
			r.Post("/meet/meetings/{id}/kick", s.handleKickParticipant)
			r.Post("/meet/meetings/{id}/mute", s.handleMuteParticipant)
			r.Get("/meet/meetings/{id}/cohosts", s.handleListCohosts)                    // co-hosts da sala
			r.Post("/meet/meetings/{id}/cohosts", s.handleAddCohost)                     // promover (dono)
			r.Delete("/meet/meetings/{id}/cohosts/{uid}", s.handleRemoveCohost)          // despromover (dono)
			r.Get("/meet/meetings/{id}/breakouts", s.handleListBreakouts)                // salas de breakout
			r.Post("/meet/meetings/{id}/breakouts", s.handleCreateBreakouts)             // criar (host)
			r.Delete("/meet/meetings/{id}/breakouts", s.handleCloseBreakouts)            // fechar todas (host)
			r.Post("/meet/meetings/{id}/breakouts/{bid}/assign", s.handleAssignBreakout) // atribuir (host)
			r.Get("/meet/meetings/{id}/breakouts/{bid}/token", s.handleBreakoutToken)    // entrar num breakout
			r.Post("/meet/meetings/{id}/notes/email", s.handleSendMeetNotes)
			r.Patch("/meet/meetings/{id}/access", s.handleUpdateMeetAccess)
			// Notas da reunião (viram tarefas via IA no Kanban).
			r.Get("/meet/meetings/{id}/notes", s.handleGetMeetNotes)
			r.Put("/meet/meetings/{id}/notes", s.handleSaveMeetNotes)

			// Gravação (LiveKit Egress → MinIO). Opcional.
			r.Post("/meet/record/start", s.handleStartRecording)
			r.Post("/meet/record/stop", s.handleStopRecording)
			r.Get("/meet/recordings", s.handleListRecordings)
			r.Get("/meet/recordings/{id}/play", s.handlePlayRecording)
			r.Delete("/meet/recordings/{id}", s.handleDeleteRecording)

			// Meet: apresentação (estilo BBB). Iniciar por upload ou do Drive; slide corrente; parar.
			r.Post("/meet/presentation/upload", s.handleStartPresentationUpload)
			r.Post("/meet/presentation/drive", s.handleStartPresentationDrive)
			r.Get("/meet/presentation", s.handleGetPresentation)
			r.Post("/meet/presentation/{id}/slide", s.handleSetPresentationSlide)
			r.Post("/meet/presentation/{id}/stop", s.handleStopPresentation)

			// Meet: lobby (aprovação de convidados) — só o host.
			r.Get("/meet/meetings/{id}/lobby", s.handleListLobby)
			r.Post("/meet/lobby/{id}/admit", s.handleAdmit)
			r.Post("/meet/lobby/{id}/deny", s.handleDeny)

			// Notificações in-app (sino).
			r.Get("/notifications", s.handleListNotifications)
			r.Post("/notifications/{id}/read", s.handleMarkNotificationRead)
			r.Post("/notifications/read-all", s.handleMarkAllNotificationsRead)
			r.Post("/notifications/read-kind", s.handleMarkNotificationsReadByKind)

			// Web Push (notificações desktop com o app fechado). O browser subscreve com a chave
			// pública VAPID e envia a subscrição; o servidor envia o push no notifyUser.
			r.Get("/push/vapid", s.handlePushVapid)
			r.Post("/push/subscribe", s.handlePushSubscribe)
			r.Post("/push/unsubscribe", s.handlePushUnsubscribe)

			// Chat (estilo Slack).
			r.Get("/chat/channels", s.handleListChannels)
			r.Post("/chat/channels", s.handleCreateChannel)
			r.Post("/chat/channels/{id}/join", s.handleJoinChannel)
			r.Patch("/chat/channels/{id}", s.handleUpdateChannel)                      // definições (gestor)
			r.Delete("/chat/channels/{id}", s.handleDeleteChannel)                     // excluir canal (owner)
			r.Post("/chat/channels/{id}/members", s.handleChatAddMember)               // adicionar membro
			r.Delete("/chat/channels/{id}/members/{userId}", s.handleChatRemoveMember) // remover / sair
			r.Patch("/chat/channels/{id}/members/{userId}", s.handleChatSetMemberRole) // promover a gestor
			r.Post("/chat/channels/{id}/schedule", s.handleScheduleMessage)            // agenda envio de mensagem
			r.Get("/chat/scheduled", s.handleListScheduled)                            // minhas mensagens agendadas
			r.Delete("/chat/scheduled/{id}", s.handleCancelScheduled)                  // cancela uma agendada
			r.Post("/chat/messages/{id}/remind", s.handleRemindMessage)                // lembrar-me desta mensagem
			r.Get("/chat/emojis", s.handleListEmojis)                                  // emojis customizados do workspace
			r.Post("/chat/emojis", s.handleCreateEmoji)                                // liga :nome: a um ficheiro (imagem)
			r.Delete("/chat/emojis/{id}", s.handleDeleteEmoji)                         // remove um emoji custom
			r.Post("/chat/dm", s.handleOpenDM)
			r.Post("/chat/group-dm", s.handleOpenGroupDM)                  // DM de grupo (3+ pessoas)
			r.Get("/chat/channels/archived", s.handleListArchivedChannels) // canais arquivados (restaurar)
			r.Post("/chat/channels/{id}/archive", s.handleArchiveChannel)  // arquivar/restaurar (dono do canal ou admin)
			r.Get("/chat/channels/{id}/webhooks", s.handleListWebhooks)    // integrações/bots do canal
			r.Post("/chat/channels/{id}/webhooks", s.handleCreateWebhook)  // cria token de entrada
			r.Delete("/chat/webhooks/{id}", s.handleDeleteWebhook)         // remove integração
			r.Get("/chat/channels/{id}/members", s.handleListChannelMembers)
			r.Get("/chat/channels/{id}/members/search", s.handleSearchChannelMembers) // paginado/busca
			r.Get("/chat/channels/{id}/canvas", s.handleGetCanvas)
			r.Put("/chat/channels/{id}/canvas", s.handleSaveCanvas)
			r.Get("/chat/channels/{id}/pins", s.handleListPins)
			r.Post("/chat/messages/{id}/pin", s.handlePinMessage)
			// Bookmarks (ficheiros & links curados) — qualquer membro adiciona/remove.
			r.Get("/chat/channels/{id}/bookmarks", s.handleListBookmarks)
			r.Post("/chat/channels/{id}/bookmarks", s.handleAddBookmark)
			r.Delete("/chat/channels/{id}/bookmarks/{bid}", s.handleDeleteBookmark)
			r.Get("/chat/channels/{id}/bookmarks/{bid}/download", s.handleBookmarkDownload)
			r.Post("/chat/channels/{id}/notif", s.handleSetChannelNotifPref)   // silenciar canal
			r.Get("/chat/channels/{id}/search", s.handleSearchChannelMessages) // busca no canal
			r.Get("/chat/channels/{id}/messages", s.handleListMessages)
			r.Post("/chat/channels/{id}/messages", s.handleSendMessage)
			r.Post("/chat/channels/{id}/read", s.handleMarkChannelRead)
			r.Post("/chat/messages/{id}/unread", s.handleMarkChannelUnread) // marcar não lido a partir daqui
			r.Patch("/chat/messages/{id}", s.handleEditMessage)
			r.Delete("/chat/messages/{id}", s.handleDeleteMessage)
			r.Post("/chat/messages/{id}/forward", s.handleForwardMessage) // encaminhar para outro canal
			r.Post("/chat/messages/{id}/save", s.handleSaveMessage)       // salvar (saved items)
			r.Delete("/chat/messages/{id}/save", s.handleUnsaveMessage)
			r.Get("/chat/saved", s.handleListSaved)
			r.Get("/chat/unfurl", s.handleUnfurl) // preview de link (com guarda de SSRF)
			r.Get("/chat/messages/{id}/replies", s.handleThreadReplies)
			r.Get("/chat/files/{fileId}/download", s.handleChatAttachment)
			r.Get("/chat/presence", s.handleChatPresence)
			r.Get("/chat/search", s.handleChatSearch)
			r.Post("/chat/messages/{id}/react", s.handleReact)
			r.Get("/chat/ws", s.handleChatWS)
		})

		// Sync de diretórios (daemon headless): grupo separado, autenticado por TOKEN DE DISPOSITIVO
		// (não sessão). Escopo mínimo — só ler o feed de changes e baixar ficheiros; o token não
		// alcança o resto da API. Sem csrfGuard (não é fluxo de cookie/browser).
		r.Group(func(r chi.Router) {
			r.Use(s.requireDeviceToken)
			r.Get("/sync/files/changes", s.handleListFileChanges)
			r.Get("/sync/files/{id}/download", s.handleDownloadFile)
			// M2: escrita. PUT exige If-Match (concorrência otimista); POST cria ficheiro novo.
			r.Put("/sync/files/{id}", s.handleSyncUpdateFile)
			r.Post("/sync/files", s.handleSyncCreateFile)
			// Árvore de pastas (espelho de subpastas): listar tudo + criar as em falta.
			r.Get("/sync/folders", s.handleListSyncFolders)
			r.Post("/sync/folders", s.handleCreateSyncFolder)
			// Espelho do que acontece na pasta local: apagar → lixeira (recuperável); mover/renomear.
			r.Post("/sync/files/{id}/trash", s.handleSyncTrashFile)
			r.Post("/sync/files/{id}/move", s.handleSyncMoveFile)
			// Upload delta (M4): hashes por bloco + envio só dos blocos alterados.
			r.Get("/sync/files/{id}/blocks", s.handleSyncFileBlocks)
			r.Put("/sync/files/{id}/delta", s.handleSyncUpdateFileDelta)
		})

		// Aceitar convite é público (o usuário ainda não tem conta).
		r.Get("/invites/{token}", s.handleGetInvite)
		r.Post("/invites/accept", s.handleAcceptInvite)

		// Webhook de billing (público; autenticado pela assinatura do provedor). Idempotente.
		r.Post("/billing/webhook", s.handleBillingWebhook)

		s.mountPublic(r)

		// Proxy de IA para o plugin do OnlyOffice (OpenAI-compatível). SEM cookie: o iframe do DS é
		// cross-origin e autentica por Bearer (token de IA do utilizador). A chave real do DeepSeek
		// fica no servidor; a quota é a do utilizador. OPTIONS para o preflight CORS.
		r.Options("/ai/openai/v1/chat/completions", s.handleAIChatCompletions)
		r.With(s.publicRateLimit).Post("/ai/openai/v1/chat/completions", s.handleAIChatCompletions)
		r.Options("/ai/openai/v1/models", s.handleAIModels)
		r.With(s.publicRateLimit).Get("/ai/openai/v1/models", s.handleAIModels)
		// Modo servidor do plugin de IA (aiSettings.proxy): recebe o envelope e o JWT do editor.
		r.Options("/ai/proxy", s.handleAIEnvelopeProxy)
		r.With(s.publicRateLimit).Post("/ai/proxy", s.handleAIEnvelopeProxy)
	})

	// O ai-proxy do Document Server usa SÓ a origem de aiSettings.proxy e acrescenta o caminho do
	// provider (/v1/chat/completions). Por isso o alvo bate na RAIZ da API, não sob /api. Autentica
	// pelo JWT do editor que o DS reencaminha; a quota é a do utilizador.
	r.Options("/v1/chat/completions", s.handleAIEnvelopeProxy)
	r.With(s.publicRateLimit).Post("/v1/chat/completions", s.handleAIEnvelopeProxy)
	r.With(s.publicRateLimit).Get("/v1/models", s.handleAIModelsEditor)

	// Endpoints do Document Server (OnlyOffice) — chamados server-to-server, autenticados por
	// token HMAC na query (não pela sessão de cookie). Ficam fora de /api por isso.
	r.Get("/doc/files/{id}/download", s.handleDocDownload)
	r.Post("/doc/callback/{id}", s.handleDocCallback)

	// Webhook do LiveKit (server-to-server, autenticado pela assinatura do LiveKit).
	r.Post("/meet/webhook", s.handleMeetWebhook)

	// Fonte da apresentação para o Document Server (server-to-server via filesHostURL). Fica no root
	// porque o DS a alcança direto pela URL interna, não passa por Vite/Caddy. O PDF ao cliente está
	// sob /api/public/meet/presentation/{id}/pdf (encaminhado). Ambos por token HMAC.
	r.Get("/meet/presentation-src/{token}", s.handlePresentationSource)

	// Proxy JMAP → Stalwart para o app nativo (Tauri). Só liga com APP_ORIGINS; na web o
	// Caddy/Vite já roteia /jmap same-origin. Ver jmap_proxy.go.
	s.mountJMAPProxy(r)

	return r
}

// --- logging ---

// redactSensitiveQuery devolve a URL com os parâmetros de credencial mascarados (para logs).
func redactSensitiveQuery(u *url.URL) string {
	if u.RawQuery == "" {
		return u.Path
	}
	q := u.Query()
	redacted := false
	for _, k := range []string{"access_token", "token"} {
		if q.Has(k) {
			q.Set(k, "REDACTED")
			redacted = true
		}
	}
	if !redacted {
		return u.Path + "?" + u.RawQuery
	}
	return u.Path + "?" + q.Encode()
}

// csrfGuard protege as rotas de sessão contra CSRF. Requisições que mudam estado precisam ou de um
// Authorization: Bearer (app nativo — token não-ambiente, sem risco de CSRF) ou do header custom
// X-Mailin-Client que só o nosso cliente envia. Um POST cross-site "simples" (form) não consegue
// setar header custom sem preflight, e o preflight bate na allow-list do CORS (auditoria M5).
func (s *Server) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if r.Header.Get("Authorization") == "" && r.Header.Get("X-Mailin-Client") == "" {
				writeErr(w, http.StatusForbidden, "csrf.blocked")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// publicRateLimit limita por IP os endpoints sem login (Meet público, download por token). Trava
// enumeração de salas/tokens e abuso desses endpoints anônimos (auditoria M).
func (s *Server) publicRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.pubLimiter.allow("pub:"+clientIP(r), time.Now()) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// presentationRateLimit limita o PDF da apresentação num balde próprio e generoso (ver presLimiter).
func (s *Server) presentationRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.presLimiter.allow("pres:"+clientIP(r), time.Now()) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLogger loga método, URL (com credenciais redigidas), status e duração.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		defer func() {
			log.Printf("%s %s -> %d (%s)", r.Method, redactSensitiveQuery(r.URL), ww.Status(), time.Since(start))
		}()
		next.ServeHTTP(ww, r)
	})
}

// --- helpers ---

// orEmpty troca slice nil por vazio: em JSON, nil vira `null` e o frontend faz .map/.length em
// cima disso. Lista vazia é `[]`.
func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr returns an error with a STABLE code (the frontend translates it via i18n — law §4.4)
// plus an English fallback message derived from the code. The code is the source of truth for
// the translation; the message is only a backup and is always English (law §4.6).
func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error_code": code, "error": humanizeCode(code)})
}

// humanizeCode turns "auth.invalid_credentials" into "Invalid credentials" (English fallback).
func humanizeCode(code string) string {
	s := code
	if i := strings.LastIndex(code, "."); i >= 0 {
		s = code[i+1:]
	}
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return "Error"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

const sessionTTL = 7 * 24 * time.Hour

// requirePerm exige que o usuário autenticado (sessão de painel) tenha a permissão. Fonte de
// verdade é o papel salvo (nunca vindo do request). Fail-closed (lei §5.2).
func (s *Server) requirePerm(perm rbac.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := mailUserFrom(r.Context())
			// Efetivas = papel ± exceções da pessoa. É uma leitura a mais por pedido, mas só nas
			// rotas de administração (baixo tráfego) e é o que torna as exceções reais: sem isto
			// seriam decoração na UI, com o backend a continuar a decidir só pelo papel.
			allow, deny := s.memberOverrides(r.Context(), u.TenantID, u.ID)
			if !rbac.HasEffective(rbac.Role(u.Role), allow, deny, perm) {
				writeErr(w, http.StatusForbidden, "rbac.forbidden")
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}
