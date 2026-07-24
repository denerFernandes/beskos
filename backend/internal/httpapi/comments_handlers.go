package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
)

// Comentários de uma tarefa + os avisos que eles disparam (ver tasks_notify.go para as regras).

// cardOr404 carrega o card do tenant do usuário autenticado.
func (s *Server) cardOr404(w http.ResponseWriter, r *http.Request) (db.Card, bool) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid_id")
		return db.Card{}, false
	}
	c, err := s.q.GetCard(r.Context(), db.GetCardParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return db.Card{}, false
	}
	return c, true
}

func (s *Server) handleListCardComments(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	c, ok := s.cardOr404(w, r)
	if !ok {
		return
	}
	rows, _ := s.q.ListCardComments(r.Context(), db.ListCardCommentsParams{CardID: c.ID, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

func (s *Server) handleCreateCardComment(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	card, ok := s.cardOr404(w, r)
	if !ok {
		return
	}
	var req struct {
		Body     string `json:"body"`
		ParentID string `json:"parent_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeErr(w, http.StatusBadRequest, "comments.empty")
		return
	}

	// parent só vale se for comentário DESTA tarefa — senão dava para responder (e notificar) numa
	// conversa de outro quadro só forjando o id.
	var parent pgtype.UUID
	var parentAuthor uuid.UUID
	if req.ParentID != "" {
		pid, err := uuid.Parse(req.ParentID)
		if err == nil {
			if p, err := s.q.GetCardComment(r.Context(), db.GetCardCommentParams{ID: pid, TenantID: mu.TenantID}); err == nil && p.CardID == card.ID {
				parent = pgtype.UUID{Bytes: pid, Valid: true}
				parentAuthor = p.AuthorID
			}
		}
	}

	mentions := s.parseMentions(r.Context(), mu.TenantID, body)
	cm, err := s.q.CreateCardComment(r.Context(), db.CreateCardCommentParams{
		TenantID: mu.TenantID, CardID: card.ID, AuthorID: mu.ID,
		ParentID: parent, Body: body, Mentions: orEmpty(mentions), // NOT NULL: nil viraria NULL e o insert falha
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "comments.failed")
		return
	}

	// Quem é avisado: menções > resposta > responsável/criador > quem já comentou antes.
	targets := taskWatchers(card)
	for _, m := range mentions {
		targets = append(targets, notifyTarget{userID: m, kind: kindTaskMention})
	}
	if parentAuthor != uuid.Nil {
		targets = append(targets, notifyTarget{userID: parentAuthor, kind: kindTaskReply})
	}
	if authors, err := s.q.ListCommentAuthors(r.Context(), db.ListCommentAuthorsParams{CardID: card.ID, TenantID: mu.TenantID}); err == nil {
		for _, a := range authors {
			targets = append(targets, notifyTarget{userID: a, kind: kindTaskComment})
		}
	}
	s.notifyTask(r.Context(), mu, card, targets, "New comment", body)

	writeJSON(w, http.StatusCreated, cm)
}

func (s *Server) handleDeleteCardComment(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "cid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "comments.invalid_id")
		return
	}
	// Só o autor apaga o próprio comentário.
	if err := s.q.DeleteCardComment(r.Context(), db.DeleteCardCommentParams{ID: id, AuthorID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "comments.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
