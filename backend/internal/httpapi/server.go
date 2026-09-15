package httpapi

// Router builds the chi router: every route this server answers to, grouped by whether a
// session is required. The Server type and its constructor live in server_types.go.

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/beskosorg/beskos/backend/ee"
	"github.com/beskosorg/beskos/backend/internal/rbac"
)

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// requestLogger replaces middleware.Logger: the session token can arrive in ?access_token=
	// (WS/media), and the default logger would write the whole URL — a 7-day token in the logs is
	// a session hijack waiting to happen. The sensitive query is redacted here before logging.
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
		r.Get("/edition", s.handleGetEdition)         // capabilities unlocked on this instance (Community/Pro)

		// Everything behind the same session. RBAC decides what each role can see.
		r.Group(func(r chi.Router) {
			r.Use(s.requireWebmailSession)
			r.Use(s.csrfGuard)
			r.Post("/auth/logout", s.handleWebmailLogout)
			r.Post("/webmail/logout", s.handleWebmailLogout)
			s.mountMail(r) // the mailbox itself: snooze, outbox, templates, moving in
			r.Get("/me", s.handleMe)
			r.Get("/me/mail-connection", s.handleMailConnection) // IMAP/SMTP para clientes externos

			// MFA (second-factor enrollment). Requires an already active session.
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
			// Mailbox lifecycle (mailbox_lifecycle.go). Deleting has its own permission because it is
			// destructive: running mailboxes and destroying them are different jobs.
			r.With(s.requirePerm(rbac.PermMailboxManage)).Put("/mailboxes/{id}/status", s.handleSetMailboxStatus)
			r.With(s.requirePerm(rbac.PermMailboxDelete)).Delete("/mailboxes/{id}", s.handleDeleteMailbox)
			r.With(s.requirePerm(rbac.PermMailboxDelete)).Post("/mailboxes/{id}/restore", s.handleRestoreMailbox)
			// Delete OR return (GDPR 28(3)(g)): during the window the data can still be taken out.
			r.With(s.requirePerm(rbac.PermMailboxDelete)).Get("/mailboxes/{id}/export", s.handleExportMailbox)

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
			ee.Register(r, s.edition, ee.Deps{Queries: s.q}) // no-op until a Pro feature moves here (F2)
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

			// Office editing (OnlyOffice): create a blank file + open the editor session.
			r.Get("/docs/recent", s.handleListRecentDocuments) // the Documents app: a view over Drive
			r.Post("/files/office", s.handleCreateOfficeFile)
			r.Post("/files/{id}/edit-session", s.handleEditSession)
			r.Get("/ai/editor-key", s.handleAIEditorKey) // config do plugin de IA do editor (proxy)

			s.routesMeet(r)

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

		// Directory sync (headless daemon): a separate group, authenticated by a DEVICE TOKEN
		// (not a session). Minimal scope — only reading the changes feed and downloading files;
		// the token doesn't reach the rest of the API. No csrfGuard (not a cookie/browser flow).
		r.Group(func(r chi.Router) {
			r.Use(s.requireDeviceToken)
			r.Get("/sync/files/changes", s.handleListFileChanges)
			r.Get("/sync/files/{id}/download", s.handleDownloadFile)
			// Writes: PUT requires If-Match (optimistic concurrency); POST creates a new file.
			r.Put("/sync/files/{id}", s.handleSyncUpdateFile)
			r.Post("/sync/files", s.handleSyncCreateFile)
			// Folder tree (subfolder mirror): list everything + create the missing ones.
			r.Get("/sync/folders", s.handleListSyncFolders)
			r.Post("/sync/folders", s.handleCreateSyncFolder)
			// Mirrors what happens in the local folder: delete → trash (recoverable); move/rename.
			r.Post("/sync/files/{id}/trash", s.handleSyncTrashFile)
			r.Post("/sync/files/{id}/move", s.handleSyncMoveFile)
			// Delta upload: block hashes, only the changed blocks are sent.
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

	// Document Server (OnlyOffice) endpoints — called server-to-server, authenticated by an HMAC
	// token in the query string (not the cookie session). They live outside /api for that reason.
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
