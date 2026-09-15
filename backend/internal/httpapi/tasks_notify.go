package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Notificação de tarefas: quem recebe o quê, e por que motivo.
//
// Regras (decididas com o cliente, 2026-07-15):
//   - quem @menciona alguém → o mencionado é avisado;
//   - quem responde a um comentário → o autor do comentário respondido é avisado;
//   - quem comentou antes na tarefa → é avisado dos comentários seguintes (é a conversa dele);
//   - quem CRIOU a tarefa → avisado quando ela muda ou recebe comentário;
//   - quem VAI FAZER a tarefa (responsável) → avisado das mudanças e dos comentários.
//
// Duas regras que valem mais que a lista: (1) NUNCA avisar quem fez a ação — receber e-mail do
// que você mesmo acabou de escrever é ruído que ensina a ignorar o resto; (2) uma pessoa recebe
// UM aviso por evento, mesmo que se encaixe em cinco motivos acima.

type notifyKind string

const (
	kindTaskComment  notifyKind = "task_comment"
	kindTaskMention  notifyKind = "task_mention"
	kindTaskReply    notifyKind = "task_reply"
	kindTaskAssigned notifyKind = "task_assigned"
	kindTaskChanged  notifyKind = "task_changed"
)

// notifyTarget é uma pessoa + o motivo pelo qual ela está a ser avisada. O motivo mais forte
// ganha: ser mencionado é mais relevante que "eu também comentei aqui".
type notifyTarget struct {
	userID uuid.UUID
	kind   notifyKind
}

// rank ordena os motivos: quanto maior, mais específico.
func (k notifyKind) rank() int {
	switch k {
	case kindTaskMention:
		return 4
	case kindTaskReply:
		return 3
	case kindTaskAssigned:
		return 2
	default:
		return 1
	}
}

// audience junta os motivos e devolve um aviso por pessoa, com o motivo mais forte, já sem o autor
// da ação.
func audience(actor uuid.UUID, targets []notifyTarget) []notifyTarget {
	best := map[uuid.UUID]notifyKind{}
	for _, t := range targets {
		if t.userID == uuid.Nil || t.userID == actor {
			continue // nunca avisar quem fez a ação
		}
		if cur, ok := best[t.userID]; !ok || t.kind.rank() > cur.rank() {
			best[t.userID] = t.kind
		}
	}
	out := make([]notifyTarget, 0, len(best))
	for id, k := range best {
		out = append(out, notifyTarget{userID: id, kind: k})
	}
	return out
}

// resolveUser só aceita um responsável do MESMO tenant. Id inválido ou de fora → sem responsável,
// nunca um e-mail para o utilizador errado.
func (s *Server) resolveUser(ctx context.Context, mu db.MailUser, raw string) pgtype.UUID {
	if raw == "" {
		return pgtype.UUID{}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return pgtype.UUID{}
	}
	if _, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID}); err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// taskWatchers são os interessados fixos de uma tarefa: quem a criou e quem vai fazê-la.
func taskWatchers(card db.Card) []notifyTarget {
	var out []notifyTarget
	if card.CreatedBy.Valid {
		out = append(out, notifyTarget{userID: uuid.UUID(card.CreatedBy.Bytes), kind: kindTaskChanged})
	}
	if card.AssigneeID.Valid {
		out = append(out, notifyTarget{userID: uuid.UUID(card.AssigneeID.Bytes), kind: kindTaskAssigned})
	}
	return out
}

// notifyTask grava o aviso no sino e manda e-mail a quem não desligou. Best-effort de propósito:
// falhar um e-mail não pode derrubar o comentário que a pessoa acabou de escrever.
func (s *Server) notifyTask(ctx context.Context, actor db.MailUser, card db.Card, targets []notifyTarget, title, body string) {
	// Mesmo endereço que a UI usa e que o botão "copiar link" produz. Com ?card= o quadro abria
	// mas a tarefa não — o link do e-mail tem de cair na tarefa.
	link := fmt.Sprintf("/mail/kanban/%s/%s", card.BoardID, card.ID)
	who := actor.Name
	if who == "" {
		who = actor.Email
	}
	// Dados estruturados p/ o frontend traduzir por server.notif.<kind> (CLAUDE.md §4.6). O `body`
	// (comentário/nome da tarefa) é conteúdo do usuário e segue exibido cru como fallback.
	args := notifArgs(map[string]string{"actor": who, "card": card.Title})
	for _, t := range audience(actor.ID, targets) {
		u, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: t.userID, TenantID: actor.TenantID})
		if err != nil {
			continue // de outro tenant ou apagado: não existe para nós
		}
		if _, err := s.q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: actor.TenantID, UserID: u.ID, Kind: string(t.kind),
			Title: title, Body: body, Link: link, MeetingID: pgtype.UUID{}, Args: args,
		}); err != nil {
			slog.Error("task notification failed", "kind", t.kind, "user", u.ID, "err", err)
		}
		if s.email == nil || !u.NotifyEmail || u.Email == "" {
			continue
		}
		subject, text := emailFor(t.kind, actor, card, title, body)
		if err := s.email.Send(u.Email, subject, text); err != nil {
			// Não falha o pedido: o aviso no sino já está lá, e o e-mail é o extra.
			slog.Error("notification email failed", "to", u.Email, "kind", t.kind, "err", err)
		}
	}
}

// emailFor monta o texto. O assunto diz o MOTIVO — "mencionou você" e "comentou" pedem atenções
// diferentes, e quem lê a caixa decide pelo assunto.
func emailFor(k notifyKind, actor db.MailUser, card db.Card, title, body string) (string, string) {
	who := actor.Name
	if who == "" {
		who = actor.Email
	}
	var reason string
	switch k {
	case kindTaskMention:
		reason = who + " mentioned you in a task"
	case kindTaskReply:
		reason = who + " replied to your comment"
	case kindTaskAssigned:
		reason = "Task assigned to you"
	case kindTaskChanged:
		reason = who + " changed one of your tasks"
	default:
		reason = who + " commented on a task"
	}
	text := strings.TrimSpace(fmt.Sprintf("%s\n\nTask: %s\n\n%s\n\n---\n%s",
		reason, card.Title, body, "Beskos"))
	return fmt.Sprintf("[%s] %s", card.Title, title), text
}

// parseMentions extrai @mencionados do texto e devolve os ids dos COLEGAS que casarem. Casa por
// e-mail (@ana@acme.com) ou pelo local-part (@ana) — nunca por nome, que se repete e muda.
func (s *Server) parseMentions(ctx context.Context, tenantID uuid.UUID, body string) []uuid.UUID {
	raw := mentionRe.FindAllStringSubmatch(body, -1)
	if len(raw) == 0 {
		return nil
	}
	people, err := s.q.ListColleagues(ctx, tenantID)
	if err != nil {
		return nil
	}
	var out []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, m := range raw {
		needle := strings.ToLower(m[1])
		for _, p := range people {
			local := strings.ToLower(strings.SplitN(p.Email, "@", 2)[0])
			if strings.ToLower(p.Email) == needle || local == needle {
				if !seen[p.ID] {
					seen[p.ID] = true
					out = append(out, p.ID)
				}
			}
		}
	}
	return out
}

// @ana ou @ana@acme.com — o local-part pode ter ponto, hífen e underscore.
var mentionRe = regexp.MustCompile(`@([A-Za-z0-9._%+-]+(?:@[A-Za-z0-9.-]+\.[A-Za-z]{2,})?)`)
