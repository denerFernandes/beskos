package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/mailbox"
	"github.com/beskosorg/beskos/backend/internal/office"
)

// buildAssistant monta o motor agêntico. É chamado no arranque; as dependências são fechos sobre o
// Server, portanto leem os campos opcionais (armazenamento, cifra) na altura do uso e não aqui —
// o que permite construir o motor antes dos Enable* correrem.
func (s *Server) buildAssistant() *assistant.Engine {
	reg := assistant.BuildRegistry(assistant.Deps{
		Mail:          s.assistantMailAccess,
		Drive:         s.assistantDriveSearch,
		Docs:          s.assistantDocsCreate,
		DocsRead:      s.assistantDocsRead,
		DocsEdit:      s.assistantDocsEdit,
		DocsShare:     s.assistantDocsShare,
		Tasks:         s.assistantTaskCreate,
		Calendar:      s.assistantCalendarCreate,
		Contacts:      s.assistantContactsSearch,
		ContactCreate: s.assistantContactCreate,
		ContactShare:  s.assistantContactShare,
		// Wiki (§7.5): leitura + escrita-com-confirmação, sempre pela mesma superfície vis do serviço.
		WikiSearch:     s.assistantWikiSearch,
		WikiRead:       s.assistantWikiRead,
		WikiCreatePage: s.assistantWikiCreate,
		WikiEditPage:   s.assistantWikiEdit,
		// Notes: private by default, so each adapter re-checks NoteRole for the caller.
		NotesSearch: s.assistantNotesSearch,
		NotesRead:   s.assistantNotesRead,
		NotesCreate: s.assistantNotesCreate,
		NotesEdit:   s.assistantNotesEdit,
	})
	// O Confirmer é o que torna `mail.send` executável de todo: sem ele o motor recusa tudo o que
	// sai para fora, em vez de deixar passar (§6.3, fail-closed).
	return assistant.NewEngine(s.ai, reg, assistantAudit{q: s.q}, assistant.DefaultMaxSteps).
		WithConfirmer(s.confirms)
}

// assistantMailAccess abre a caixa do utilizador AUTENTICADO. O par (user, tenant) vem da Identity,
// que por sua vez vem da sessão — nunca de argumentos do modelo (§6.2, lei 5.1). A consulta filtra
// por tenant de propósito: mesmo com um user_id correto, um tenant errado não devolve nada.
func (s *Server) assistantMailAccess(ctx context.Context, id assistant.Identity) (assistant.MailBox, error) {
	mu, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return nil, fmt.Errorf("mail user lookup: %w", err)
	}
	cred, ok := s.decryptCred(mu.JmapCred)
	if !ok {
		return nil, fmt.Errorf("mailbox credential unavailable")
	}
	// Embrulha a caixa para, ao enviar, gravar os destinatários como contactos — o mesmo que o
	// webmail faz por /contacts/seen, mas pelo lado do servidor porque o assistente envia aqui.
	return recordingMailBox{MailBox: mailbox.NewUserClient(s.sw.BaseURL(), cred), s: s, id: id}, nil
}

// recordingMailBox é a caixa do assistente que, além de enviar, regista os destinatários como
// contactos. Só o SendEmail muda; o resto delega. Um erro a gravar contactos não afeta o envio.
type recordingMailBox struct {
	assistant.MailBox
	s  *Server
	id assistant.Identity
}

func (b recordingMailBox) SendEmail(ctx context.Context, d mailbox.Draft) (string, error) {
	// Limite diário de envio do plano (docs/manager.md §4) vale também para o assistente.
	rcpts := len(d.To) + len(d.Cc)
	if !b.s.mailSendAllowed(ctx, b.id.TenantID, rcpts) {
		return "", fmt.Errorf("daily send limit reached for this plan")
	}
	emailID, err := b.MailBox.SendEmail(ctx, d)
	if err == nil {
		b.s.recordMailSent(ctx, b.id.TenantID, rcpts)
		b.s.recordRecipientContacts(ctx, b.id.TenantID, b.id.UserID, append(append([]string{}, d.To...), d.Cc...))
	}
	return emailID, err
}

// assistantDriveSearch reutiliza as MESMAS consultas da pesquisa do Drive na interface — incluindo
// o escape dos curingas do LIKE. Sem SQL novo (§5): o que o assistente vê é o que o utilizador vê.
func (s *Server) assistantDriveSearch(ctx context.Context, id assistant.Identity, query string, limit int) ([]assistant.DriveHit, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	owner := pgtype.UUID{Bytes: id.UserID, Valid: true}
	pattern := likePattern(query)
	files, err := s.q.SearchStorageFiles(ctx, db.SearchStorageFilesParams{OwnerUserID: owner, Name: pattern})
	if err != nil {
		return nil, fmt.Errorf("file search: %w", err)
	}
	folders, err := s.q.SearchStorageFolders(ctx, db.SearchStorageFoldersParams{OwnerUserID: id.UserID, Name: pattern})
	if err != nil {
		return nil, fmt.Errorf("folder search: %w", err)
	}
	// Pastas fora do alcance do assistente (§6.6): o que está numa pasta excluída não aparece na
	// pesquisa, e as próprias pastas excluídas também não. É a válvula que compensa o alcance amplo
	// por omissão (decisão §12.1).
	excluded := s.assistantExcludedFolders(ctx, id.TenantID)
	out := make([]assistant.DriveHit, 0, limit)
	for _, f := range files {
		if len(out) >= limit {
			break
		}
		if f.FolderID.Valid && excluded[uuidText(f.FolderID)] {
			continue
		}
		hit := assistant.DriveHit{ID: f.ID.String(), Name: f.Name, Kind: "file", Size: f.Size}
		if f.FolderID.Valid {
			hit.Folder = uuidText(f.FolderID)
		}
		out = append(out, hit)
	}
	for _, f := range folders {
		if len(out) >= limit {
			break
		}
		if excluded[f.ID.String()] {
			continue
		}
		out = append(out, assistant.DriveHit{ID: f.ID.String(), Name: f.Name, Kind: "folder"})
	}
	return out, nil
}

// uuidText imprime um pgtype.UUID. Existe porque as colunas de UUID anuláveis chegam assim.
func uuidText(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// assistantAudit persiste o registo de auditoria (§6.4). Falhar aqui não aborta a execução — o
// motor trata disso —, mas fica no log do servidor porque perder auditoria é grave.
type assistantAudit struct{ q *db.Queries }

func (a assistantAudit) Record(ctx context.Context, e assistant.AuditEntry) error {
	args := e.Args
	if len(args) == 0 || !json.Valid(args) {
		args = json.RawMessage("{}")
	}
	// Conversa vazia fica NULL: uma execução sem fio (como as da A0) não inventa um id de zeros.
	conv := pgtype.UUID{}
	if e.ConversationID != (uuid.UUID{}) {
		conv = pgtype.UUID{Bytes: e.ConversationID, Valid: true}
	}
	return a.q.RecordAssistantToolCall(ctx, db.RecordAssistantToolCallParams{
		TenantID:       e.Identity.TenantID,
		MailUserID:     e.Identity.UserID,
		RunID:          e.RunID,
		ConversationID: conv,
		Agent:          e.Agent,
		Tool:           e.Tool,
		Args:           args,
		Result:         e.Result,
		Error:          e.Error,
		DurationMs:     e.DurationMs,
	})
}

// assistantTaskCreate cria uma tarefa com as MESMAS consultas do handler de criação de cards
// (§5: sem SQL novo). O quadro e a coluna são resolvidos aqui, não pelo modelo: o modelo não
// conhece ids, e obrigá-lo a fornecer um seria pedir-lhe para adivinhar.
func (s *Server) assistantTaskCreate(ctx context.Context, id assistant.Identity, t assistant.NewTask) (assistant.TaskRef, error) {
	boards, err := s.q.ListBoards(ctx, db.ListBoardsParams{TenantID: id.TenantID, UserID: pgtype.UUID{Bytes: id.UserID, Valid: true}})
	if err != nil {
		return assistant.TaskRef{}, fmt.Errorf("board lookup: %w", err)
	}
	if len(boards) == 0 {
		return assistant.TaskRef{}, fmt.Errorf("this organisation has no board yet")
	}
	board := boards[0]
	if want := strings.TrimSpace(t.Board); want != "" {
		found := false
		for _, b := range boards {
			if strings.EqualFold(b.Name, want) {
				board, found = b, true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(boards))
			for _, b := range boards {
				names = append(names, b.Name)
			}
			// Lista os quadros no erro: o modelo corrige à primeira em vez de tentar às cegas.
			return assistant.TaskRef{}, fmt.Errorf("no board named %q; available boards: %s", want, strings.Join(names, ", "))
		}
	}

	cols, err := s.q.ListColumns(ctx, board.ID)
	if err != nil {
		return assistant.TaskRef{}, fmt.Errorf("column lookup: %w", err)
	}
	if len(cols) == 0 {
		return assistant.TaskRef{}, fmt.Errorf("board %q has no column", board.Name)
	}

	// Número por quadro: a sequência do quadro é a fonte, como no handler.
	seq, err := s.q.NextCardNumber(ctx, board.ID)
	if err != nil {
		return assistant.TaskRef{}, fmt.Errorf("card number: %w", err)
	}
	// Responsável (pedido 23/07/2026: "task para o Bob" tem de atribuir MESMO ao Bob): resolve o
	// nome/e-mail entre os colegas do tenant. Ambíguo ou inexistente → erro claro (o modelo
	// pergunta ou corrige) em vez de criar a task sem dono em silêncio.
	assigneeID := pgtype.UUID{}
	assigneeName := ""
	if want := strings.TrimSpace(t.Assignee); want != "" {
		cols2, cerr := s.q.ListColleagues(ctx, id.TenantID)
		if cerr != nil {
			return assistant.TaskRef{}, fmt.Errorf("colleague lookup: %w", cerr)
		}
		var matches []db.ListColleaguesRow
		for _, c := range cols2 {
			if strings.EqualFold(c.Email, want) || strings.EqualFold(c.Name, want) {
				matches = []db.ListColleaguesRow{c}
				break
			}
			if strings.Contains(strings.ToLower(c.Name), strings.ToLower(want)) ||
				strings.Contains(strings.ToLower(c.Email), strings.ToLower(want)) {
				matches = append(matches, c)
			}
		}
		if len(matches) == 0 {
			return assistant.TaskRef{}, fmt.Errorf("no colleague matching %q in this organisation", want)
		}
		if len(matches) > 1 {
			opts := make([]string, 0, len(matches))
			for _, m := range matches {
				opts = append(opts, m.Email)
			}
			return assistant.TaskRef{}, fmt.Errorf("ambiguous assignee %q: %s — use the email", want, strings.Join(opts, ", "))
		}
		assigneeID = pgtype.UUID{Bytes: matches[0].ID, Valid: true}
		assigneeName = matches[0].Name
		if assigneeName == "" {
			assigneeName = matches[0].Email
		}
	}
	card, err := s.q.CreateCard(ctx, db.CreateCardParams{
		TenantID: id.TenantID, BoardID: board.ID, ColumnID: cols[0].ID,
		Title: t.Title, Description: t.Description, Due: t.Due,
		Priority:   normalizePriority(""),
		Position:   0,
		AssigneeID: assigneeID,
		CreatedBy:  pgtype.UUID{Bytes: id.UserID, Valid: true},
		Number:     seq.CardSeq,
	})
	if err != nil {
		return assistant.TaskRef{}, fmt.Errorf("card creation: %w", err)
	}
	// Avisa o responsável (mesmo caminho do handler manual): sino + push.
	if actor, aerr := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID}); aerr == nil {
		s.notifyTask(ctx, actor, card, taskWatchers(card), "New task", card.Title)
	}
	s.logCard(ctx, id.TenantID, card.ID, id.UserID, "created", "")
	s.publishBoard(ctx, id.TenantID, card.BoardID)
	return assistant.TaskRef{
		ID:       card.ID.String(),
		Board:    board.Name,
		Number:   fmt.Sprintf("%s-%d", seq.Prefix, seq.CardSeq),
		Assignee: assigneeName,
	}, nil
}

// assistantCalendarCreate cria um evento na agenda do utilizador AUTENTICADO com as MESMAS consultas
// do handler da interface (§5: sem SQL novo). O par (user, tenant) vem da Identity — o modelo nunca
// escolhe a agenda (§6.2). Não faz aviso de conflito: no chat não há modal para confirmar e o
// utilizador pediu explicitamente para criar.
func (s *Server) assistantCalendarCreate(ctx context.Context, id assistant.Identity, ev assistant.NewEvent) (assistant.EventRef, error) {
	mu, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return assistant.EventRef{}, fmt.Errorf("user lookup: %w", err)
	}
	recurN, recurU := recurValues("", "")
	event, err := s.q.CreateEvent(ctx, db.CreateEventParams{
		TenantID: id.TenantID, OwnerUserID: pgtype.UUID{Bytes: id.UserID, Valid: true},
		Title: ev.Title, Description: ev.Description, Location: ev.Location, Color: "#116466",
		StartsAt: ev.StartsAt, EndsAt: ev.EndsAt, SourceEmailID: "", Tags: []string{},
		RemindMinutes: remindOrDefault(nil), AllDay: false,
		Recur: recurN, RecurUntil: recurU, CalendarID: s.calendarID(ctx, mu, ""),
	})
	if err != nil {
		return assistant.EventRef{}, fmt.Errorf("event creation: %w", err)
	}
	// Convidados: resolve cada e-mail para um COLEGA do mesmo tenant. Externos (ex.: fora da
	// organização) não são convidáveis na agenda — ignorados aqui, avisa-se por e-mail. O syncInvites
	// revalida a posse do tenant por UUID, mas filtramos já por tenant para não confiar às cegas.
	if len(ev.Invitees) > 0 {
		var ids []string
		for _, email := range ev.Invitees {
			u, err := s.q.GetMailUserByEmailInTenant(ctx, db.GetMailUserByEmailInTenantParams{Email: strings.TrimSpace(email), TenantID: id.TenantID})
			if err != nil {
				continue
			}
			ids = append(ids, u.ID.String())
		}
		s.syncInvites(ctx, event, mu, ids)
	}
	ref := assistant.EventRef{
		ID: event.ID.String(), Title: event.Title, StartsAt: event.StartsAt.Format(time.RFC3339),
	}
	// Sala de vídeo só se pedida. Devolve uma URL ABSOLUTA de convidado (/j/<sala>) para poder ir
	// num e-mail e ser clicável fora da app; sem PUBLIC_URL configurada, cai no caminho relativo.
	if ev.CreateMeet {
		if room := s.attachMeetToEvent(ctx, mu, event.ID, ev.Title, ev.StartsAt, ev.EndsAt); room != "" {
			if s.publicURL != "" {
				ref.MeetURL = s.publicURL + "/j/" + room
			} else {
				ref.MeetURL = "/j/" + room
			}
		}
	}
	return ref, nil
}

// assistantContactsSearch pesquisa nos contactos do utilizador (mesmas consultas da interface, §5).
// Filtra por nome/e-mail/empresa em Go — assim o assistente resolve "o Bob" e, havendo mais do que um,
// pode listá-los em vez de adivinhar. Scoped por tenant+dono (§6.2).
func (s *Server) assistantContactsSearch(ctx context.Context, id assistant.Identity, query string, limit int) ([]assistant.ContactHit, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows, err := s.q.ListContacts(ctx, db.ListContactsParams{OwnerUserID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return nil, fmt.Errorf("contact list: %w", err)
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]assistant.ContactHit, 0, limit)
	// Colegas primeiro: "Bob" quase sempre é alguém da organização, e pode não estar na agenda
	// pessoal (23/07/2026: o assistente não resolvia colegas). Mesmo padrão do InviteePicker.
	seen := map[string]bool{}
	if cols, cerr := s.q.ListColleagues(ctx, id.TenantID); cerr == nil {
		for _, c := range cols {
			if strings.EqualFold(c.Email, id.Email) {
				continue // o próprio utilizador não é um destinatário a resolver
			}
			hay := strings.ToLower(c.Name + " " + c.Email)
			if q != "" && !strings.Contains(hay, q) {
				continue
			}
			out = append(out, assistant.ContactHit{Name: c.Name, Email: c.Email, Company: "colleague"})
			seen[strings.ToLower(c.Email)] = true
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	for _, c := range rows {
		if c.OwnerUserID != id.UserID {
			continue // segurança: só os do próprio (ListContacts pode trazer partilhados)
		}
		if seen[strings.ToLower(c.Email)] {
			continue // já veio como colega
		}
		hay := strings.ToLower(c.Name + " " + c.Email + " " + c.Company)
		if q != "" && !strings.Contains(hay, q) {
			continue
		}
		out = append(out, assistant.ContactHit{Name: c.Name, Email: c.Email, Company: c.Company})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// assistantContactCreate cria um contacto (pessoa) na agenda do utilizador, com as MESMAS consultas
// da interface (§5). Scoped por tenant+dono (§6.2). Interno — não sai da organização.
func (s *Server) assistantContactCreate(ctx context.Context, id assistant.Identity, c assistant.NewContact) (assistant.ContactHit, error) {
	created, err := s.q.CreateContact(ctx, db.CreateContactParams{
		TenantID: id.TenantID, OwnerUserID: id.UserID, Kind: "person",
		Name: strings.TrimSpace(c.Name), Email: strings.TrimSpace(c.Email),
		Phone: strings.TrimSpace(c.Phone), Company: strings.TrimSpace(c.Company),
		Tags: []string{}, ExtraEmails: []string{}, ExtraPhones: []string{},
		AllowPhone: true, AllowEmail: true, CustomFields: json.RawMessage("{}"), // jsonb NOT NULL
	})
	if err != nil {
		return assistant.ContactHit{}, fmt.Errorf("contact create: %w", err)
	}
	return assistant.ContactHit{Name: created.Name, Email: created.Email, Company: created.Company}, nil
}

// assistantContactShare partilha um contacto com um COLEGA: copia o cartão do contacto (do próprio
// utilizador) para a agenda do colega, no mesmo tenant (§6.2). O contacto resolve-se por e-mail na
// agenda do próprio; o destino tem de ser um colega do tenant — nada sai da organização.
func (s *Server) assistantContactShare(ctx context.Context, id assistant.Identity, contactEmail, toEmail string) (assistant.ContactShareResult, error) {
	contactEmail = strings.ToLower(strings.TrimSpace(contactEmail))
	toEmail = strings.ToLower(strings.TrimSpace(toEmail))
	// Colega de destino: tem de ser do MESMO tenant (nunca partilha para fora).
	colleague, err := s.q.GetMailUserByEmailInTenant(ctx, db.GetMailUserByEmailInTenantParams{Email: toEmail, TenantID: id.TenantID})
	if err != nil {
		return assistant.ContactShareResult{}, fmt.Errorf("the recipient is not a colleague in this organisation")
	}
	// Contacto a partilhar: procura na agenda do PRÓPRIO utilizador (posse, §6.2).
	rows, err := s.q.ListContacts(ctx, db.ListContactsParams{OwnerUserID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return assistant.ContactShareResult{}, fmt.Errorf("contact lookup: %w", err)
	}
	var src *db.ListContactsRow
	for i := range rows {
		if rows[i].OwnerUserID == id.UserID && strings.EqualFold(rows[i].Email, contactEmail) {
			src = &rows[i]
			break
		}
	}
	if src == nil {
		return assistant.ContactShareResult{}, fmt.Errorf("no contact of yours with that e-mail")
	}
	// Copia o cartão para a agenda do colega (idempotência ligeira: se já tem esse e-mail, não duplica).
	dst, _ := s.q.ListContacts(ctx, db.ListContactsParams{OwnerUserID: colleague.ID, TenantID: id.TenantID})
	for _, d := range dst {
		if d.OwnerUserID == colleague.ID && strings.EqualFold(d.Email, contactEmail) {
			return assistant.ContactShareResult{Contact: src.Name, To: colleague.Name}, nil
		}
	}
	if _, err := s.q.CreateContact(ctx, db.CreateContactParams{
		TenantID: id.TenantID, OwnerUserID: colleague.ID, Kind: src.Kind,
		Name: src.Name, Email: src.Email, Phone: src.Phone, Company: src.Company,
		Title: src.Title, Mobile: src.Mobile, Notes: src.Notes,
		Tags: []string{}, ExtraEmails: []string{}, ExtraPhones: []string{},
		AllowPhone: true, AllowEmail: true, CustomFields: json.RawMessage("{}"), // jsonb NOT NULL
	}); err != nil {
		return assistant.ContactShareResult{}, fmt.Errorf("contact copy: %w", err)
	}
	who := colleague.Name
	if who == "" {
		who = colleague.Email
	}
	return assistant.ContactShareResult{Contact: src.Name, To: who}, nil
}

// assistantDocsShare partilha um ficheiro do Drive do utilizador com COLEGAS do mesmo tenant, com as
// MESMAS consultas da partilha da interface (§5: sem SQL novo). Verifica a posse do ficheiro (§6.2)
// e ignora endereços que não sejam de colegas — partilha interna dá acesso à app; um externo
// precisaria de um link público, que é outra coisa.
func (s *Server) assistantDocsShare(ctx context.Context, id assistant.Identity, fileID string, emails []string, role string) (assistant.ShareResult, error) {
	fid, err := uuid.Parse(fileID)
	if err != nil {
		return assistant.ShareResult{}, fmt.Errorf("invalid file id")
	}
	mu, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return assistant.ShareResult{}, fmt.Errorf("user lookup: %w", err)
	}
	f, err := s.q.GetStorageFile(ctx, db.GetStorageFileParams{ID: fid, OwnerUserID: ownerPg(mu)})
	if err != nil {
		return assistant.ShareResult{}, fmt.Errorf("file not found or not yours")
	}
	level := "leitor"
	if role == "editor" {
		level = "editor"
	}
	var shared []string
	for _, email := range emails {
		target, terr := s.q.GetMailUserByEmailInTenant(ctx, db.GetMailUserByEmailInTenantParams{Email: email, TenantID: id.TenantID})
		if terr != nil {
			continue // não é colega deste tenant: não se partilha às cegas
		}
		if _, err := s.q.CreateShare(ctx, db.CreateShareParams{
			FileID: f.ID, TenantID: f.TenantID, Principal: email, Level: level,
		}); err != nil {
			continue
		}
		s.notifyUser(ctx, f.TenantID, target.ID, "file_share",
			"New shared file", mu.Email+" shared \""+f.Name+"\" with you", fileLink(f.ID), uuid.Nil,
			map[string]string{"actor": mu.Email, "file": f.Name})
		shared = append(shared, email)
	}
	if len(shared) == 0 {
		return assistant.ShareResult{}, fmt.Errorf("none of those addresses is a colleague in this organisation")
	}
	return assistant.ShareResult{File: f.Name, SharedWith: shared}, nil
}

// assistantDocsCreate constrói o ficheiro Office EM GO (decisão §12.1, ponto 2) e grava-o no Drive
// do utilizador, com a MESMA lógica de cota e registo da criação pela interface (§5). Devolve nil
// (fora do catálogo) quando o armazenamento não está configurado.
func (s *Server) assistantDocsCreate(ctx context.Context, id assistant.Identity, name string, plan office.Plan) (assistant.DriveHit, error) {
	if s.storage == nil {
		return assistant.DriveHit{}, fmt.Errorf("file storage is not configured")
	}
	data, err := office.Build(plan)
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("build file: %w", err)
	}
	ext, _ := office.Ext(plan.Kind)
	if !strings.HasSuffix(strings.ToLower(name), "."+ext) {
		name += "." + ext
	}
	mu, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("user lookup: %w", err)
	}
	rec, code := s.storeUserFile(ctx, mu, name, data, pgtype.UUID{})
	if code != "" {
		// O código já é uma chave de i18n; o motor embrulha-o como erro de ferramenta e o frontend
		// traduz (lei 4.6).
		return assistant.DriveHit{}, fmt.Errorf("%s", code)
	}
	return assistant.DriveHit{ID: rec.ID.String(), Name: rec.Name, Kind: "file", Size: rec.Size}, nil
}
