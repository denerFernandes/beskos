package httpapi

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/assistant"
	"mailin/backend/internal/db"
)

// assistantMemoryInject carrega os factos do utilizador para injetar numa execução (§8.3).
//
// Marca-os como usados: alimenta a proposta de remover o que nunca serve (§8.4). Com poucos factos
// entram todos; a filtragem por uso recente para quando forem muitas dezenas é trabalho de afinar
// depois — hoje a memória é pequena por natureza e cabe inteira.
func (s *Server) assistantMemoryInject(ctx context.Context, id assistant.Identity) []assistant.Fact {
	rows, err := s.q.ListAssistantMemory(ctx, db.ListAssistantMemoryParams{
		TenantID: id.TenantID, MailUserID: id.UserID,
	})
	if err != nil {
		log.Printf("assistant: load memory: %v", err)
		return nil
	}
	facts := make([]assistant.Fact, 0, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		facts = append(facts, assistant.Fact{Text: r.Fact, Category: r.Category})
		ids = append(ids, r.ID)
	}
	if len(ids) > 0 {
		if err := s.q.TouchAssistantMemoryUsed(ctx, db.TouchAssistantMemoryUsedParams{
			MailUserID: id.UserID, Ids: ids,
		}); err != nil {
			log.Printf("assistant: touch memory: %v", err)
		}
	}
	return facts
}

// assistantExtractMemory corre a passagem de extração sobre a MENSAGEM DO UTILIZADOR e guarda os
// factos novos. Depois de cada turno, barata e separada (§8.2).
//
// A regra dura de §6.5 está na assinatura: recebe `userMessage`, uma string, e mais nada. Não há por
// onde lhe entrar um resultado de ferramenta ou a resposta do assistente. Só o utilizador escreve na
// memória.
//
// Devolve os factos guardados e as recusas, para o handler os anunciar ao utilizador (§8.2): quando
// guarda algo, a conversa mostra-o; quando recusa, di-lo.
func (s *Server) assistantExtractMemory(ctx context.Context, id assistant.Identity, userMessage string, sourceMsg pgtype.UUID) ([]assistant.Fact, []assistant.Refusal) {
	if s.ai == nil {
		return nil, nil
	}
	facts, refusals := assistant.ExtractMemory(ctx, s.ai, userMessage)
	for _, f := range facts {
		if _, err := s.q.CreateAssistantMemory(ctx, db.CreateAssistantMemoryParams{
			TenantID: id.TenantID, MailUserID: id.UserID,
			Fact: f.Text, Category: f.Category, SourceMessageID: sourceMsg,
		}); err != nil {
			log.Printf("assistant: save memory: %v", err)
		}
	}
	return facts, refusals
}

// --- Ecrã de gestão (§3.5, §8.4) ---

type memoryJSON struct {
	ID         uuid.UUID  `json:"id"`
	Fact       string     `json:"fact"`
	Category   string     `json:"category"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// handleAssistantMemory lista tudo o que a aplicação sabe sobre o utilizador. Sem isto é um arquivo
// de dados pessoais que cresce sem ninguém ver — e o RGPD dá o direito de o ver e apagar (§3.5).
func (s *Server) handleAssistantMemory(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListAssistantMemory(r.Context(), db.ListAssistantMemoryParams{
		TenantID: mu.TenantID, MailUserID: mu.ID,
	})
	if err != nil {
		log.Printf("assistant: list memory: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	items := make([]memoryJSON, 0, len(rows))
	for _, r := range rows {
		m := memoryJSON{ID: r.ID, Fact: r.Fact, Category: r.Category, CreatedAt: r.CreatedAt}
		if r.LastUsedAt.Valid {
			t := r.LastUsedAt.Time
			m.LastUsedAt = &t
		}
		items = append(items, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type memoryWriteReq struct {
	Fact     string `json:"fact"`
	Category string `json:"category"`
}

// handleAssistantMemoryCreate deixa o utilizador acrescentar um facto À MÃO. É a via mais pura da
// regra de §6.5: só o utilizador escreve na memória, e aqui escreve-a diretamente. As categorias
// proibidas são recusadas mesmo assim — nem a pedido explícito se guarda uma senha (§6.5).
func (s *Server) handleAssistantMemoryCreate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req memoryWriteReq
	if !decode(w, r, &req) {
		return
	}
	fact := strings.TrimSpace(req.Fact)
	if fact == "" {
		writeErr(w, http.StatusBadRequest, "assistant.memory.empty")
		return
	}
	if code, bad := assistant.CheckForbidden(fact); bad {
		writeErr(w, http.StatusUnprocessableEntity, code)
		return
	}
	row, err := s.q.CreateAssistantMemory(r.Context(), db.CreateAssistantMemoryParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Fact: fact,
		Category: assistant.NormalizeCategory(req.Category),
	})
	if err != nil {
		log.Printf("assistant: create memory: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusCreated, memoryJSON{ID: row.ID, Fact: row.Fact, Category: row.Category, CreatedAt: row.CreatedAt})
}

func (s *Server) handleAssistantMemoryDelete(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.memory.unknown")
		return
	}
	if err := s.q.DeleteAssistantMemory(r.Context(), db.DeleteAssistantMemoryParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID,
	}); err != nil {
		log.Printf("assistant: delete memory: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAssistantMemoryClear apaga tudo de uma vez (§8.4). Direito do utilizador.
func (s *Server) handleAssistantMemoryClear(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if err := s.q.DeleteAllAssistantMemory(r.Context(), db.DeleteAllAssistantMemoryParams{
		TenantID: mu.TenantID, MailUserID: mu.ID,
	}); err != nil {
		log.Printf("assistant: clear memory: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
