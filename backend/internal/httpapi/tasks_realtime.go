package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/chat"
	"mailin/backend/internal/db"
)

// Tempo real do quadro + arquivar card/quadro + editar comentário.
//
// O tempo real reusa o mesmo hub (Redis pub/sub) e WebSocket do chat — o mesmo caminho que as notas
// da reunião já usam. Um evento "board" é publicado a todos os utilizadores do tenant após cada
// mutação; o frontend, ao recebê-lo, revalida o quadro. É invalidação grossa (não incremental): o
// quadro muda com muito menos frequência que um canal movimentado, então basta.

func (s *Server) publishBoard(ctx context.Context, tenantID, boardID uuid.UUID) {
	if s.hub == nil {
		return
	}
	people, err := s.q.ListColleagues(ctx, tenantID)
	if err != nil {
		return
	}
	rcpt := make([]string, 0, len(people))
	for _, p := range people {
		rcpt = append(rcpt, p.ID.String())
	}
	if len(rcpt) == 0 {
		return
	}
	payload, _ := json.Marshal(map[string]string{"board_id": boardID.String()})
	s.hub.Publish(ctx, chat.Event{Type: "board", ChannelID: boardID.String(), Recipients: rcpt, Payload: payload})
}

// handleArchiveCard arquiva/restaura um card. Arquivado sai do quadro (mas é recuperável).
func (s *Server) handleArchiveCard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid_id")
		return
	}
	if !s.requireCardEditor(w, r, id) {
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	if !decode(w, r, &req) {
		return
	}
	card, err := s.q.SetCardArchived(r.Context(), db.SetCardArchivedParams{ID: id, TenantID: mu.TenantID, Archived: req.Archived})
	if err != nil {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return
	}
	act := "archived"
	if !req.Archived {
		act = "unarchived"
	}
	s.logCard(r.Context(), mu.TenantID, card.ID, mu.ID, act, "")
	s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	writeJSON(w, http.StatusOK, card)
}

// handleListArchivedCards lista os cards arquivados de um quadro (para restaurar).
func (s *Server) handleListArchivedCards(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	boardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	cards, err := s.q.ListArchivedCards(r.Context(), db.ListArchivedCardsParams{BoardID: boardID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.failed")
		return
	}
	if cards == nil {
		cards = []db.Card{}
	}
	writeJSON(w, http.StatusOK, cards)
}

// handleSearchCards procura tarefas por chave (QUA-12), número ou título — só nas que a pessoa
// pode ver. Serve o `/task` do chat: referenciar uma tarefa sem sair da conversa.
func (s *Server) handleSearchCards(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"cards": []db.SearchCardsRow{}})
		return
	}
	cards, err := s.q.SearchCards(r.Context(), db.SearchCardsParams{
		TenantID: mu.TenantID, Q: q, UserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.failed")
		return
	}
	if cards == nil {
		cards = []db.SearchCardsRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

// handleArchiveBoard arquiva/restaura um quadro inteiro.
func (s *Server) handleArchiveBoard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleManager) {
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	if !decode(w, r, &req) {
		return
	}
	board, err := s.q.SetBoardArchived(r.Context(), db.SetBoardArchivedParams{ID: id, TenantID: mu.TenantID, Archived: req.Archived})
	if err != nil {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	writeJSON(w, http.StatusOK, board)
}

// handleListArchivedBoards lista os quadros arquivados do tenant.
func (s *Server) handleListArchivedBoards(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	boards, err := s.q.ListArchivedBoards(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	if boards == nil {
		boards = []db.Board{}
	}
	writeJSON(w, http.StatusOK, boards)
}

// handlePatchCardComment edita o corpo de um comentário (só o autor).
func (s *Server) handlePatchCardComment(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	cid, err := uuid.Parse(chi.URLParam(r, "cid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "comments.invalid_id")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Body == "" {
		writeErr(w, http.StatusBadRequest, "comments.empty")
		return
	}
	c, err := s.q.EditCardComment(r.Context(), db.EditCardCommentParams{ID: cid, AuthorID: mu.ID, Body: req.Body})
	if err != nil {
		writeErr(w, http.StatusNotFound, "comments.not_found") // não é o autor ou não existe
		return
	}
	writeJSON(w, http.StatusOK, c)
}
