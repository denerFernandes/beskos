package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/assistant"
	"mailin/backend/internal/db"
	"mailin/backend/internal/rbac"
)

// assistantQuotaLimit lê o teto de tokens da janela de 6h. Configurável por ambiente (§9.2) com um
// default generoso e igual para todos; um valor inválido cai no default em vez de desligar a quota.
func assistantQuotaLimit() int {
	if v := os.Getenv("ASSISTANT_QUOTA_TOKENS_6H"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return assistant.DefaultQuotaTokens
}

// assistantQuotaStatus calcula a quota do utilizador na janela deslizante. O teto vem do PLANO do
// tenant quando definido (manager M2, ai_tokens_per_window > 0); senão do env/default do produto.
func (s *Server) assistantQuotaStatus(ctx context.Context, id assistant.Identity, now time.Time) assistant.QuotaStatus {
	limit := assistantQuotaLimit()
	if p := s.tenantPlanByID(ctx, id.TenantID); p.AITokensPerWindow > 0 {
		limit = p.AITokensPerWindow
	}
	since := now.Add(-assistant.QuotaWindow)
	row, err := s.q.SumAssistantUsageWindow(ctx, db.SumAssistantUsageWindowParams{
		MailUserID: id.UserID, Since: since,
	})
	if err != nil {
		// Falhar a somar não pode BLOQUEAR o utilizador: a quota é comercial, não uma barreira de
		// segurança. Um erro de leitura deixa passar (o teto de passos de §9.1 continua a proteger
		// contra o descontrolo real). Fail-open aqui é o oposto da confirmação, e de propósito.
		log.Printf("assistant: quota sum: %v", err)
		return assistant.QuotaStatus{Limit: limit, Remaining: limit}
	}
	// MIN(created_at) chega como interface{} (sqlc não infere o tipo de um agregado). Um type
	// assertion resolve-o; NULL (janela vazia) fica com o zero, que QuotaOf trata como "nada expira".
	var oldest time.Time
	if t, ok := row.Oldest.(time.Time); ok {
		oldest = t
	}
	return assistant.QuotaOf(limit, int(row.Total), oldest, now)
}

// recordAssistantUsage grava os tokens de um turno. Sem cancelamento: o gasto já aconteceu e tem de
// contar, mesmo que o utilizador tenha fechado o separador.
func (s *Server) recordAssistantUsage(ctx context.Context, id assistant.Identity, tokens int) {
	if tokens <= 0 {
		return
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.q.RecordAssistantUsage(saveCtx, db.RecordAssistantUsageParams{
		TenantID: id.TenantID, MailUserID: id.UserID, Tokens: int32(tokens),
	}); err != nil {
		log.Printf("assistant: record usage: %v", err)
	}
}

// handleAssistantUsage devolve o estado da quota: quanto resta e quando renova (§9.2). O ecrã
// mostra-o; o aviso ao aproximar-se do fim é do frontend, a partir daqui.
func (s *Server) handleAssistantUsage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id := assistant.Identity{TenantID: mu.TenantID, UserID: mu.ID}
	writeJSON(w, http.StatusOK, s.assistantQuotaStatus(r.Context(), id, time.Now()))
}

// --- Auditoria visível (§6.4) ---

type toolCallJSON struct {
	ID         uuid.UUID       `json:"id"`
	Agent      string          `json:"agent"`
	Tool       string          `json:"tool"`
	Args       json.RawMessage `json:"args"`
	Result     string          `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMs int32           `json:"duration_ms"`
	CreatedAt  time.Time       `json:"created_at"`
}

// handleAssistantAudit lista as chamadas de ferramenta do próprio utilizador (§6.4). Legível por
// ele, não só pelo administrador: é o que responde a "porque é que este e-mail foi enviado?".
func (s *Server) handleAssistantAudit(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	limit := int32(100)
	rows, err := s.q.ListAssistantToolCallsByUser(r.Context(), db.ListAssistantToolCallsByUserParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Lim: limit,
	})
	if err != nil {
		log.Printf("assistant: audit list: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	items := make([]toolCallJSON, 0, len(rows))
	for _, c := range rows {
		args := c.Args
		if len(args) == 0 || !json.Valid(args) {
			args = json.RawMessage("{}")
		}
		items = append(items, toolCallJSON{
			ID: c.ID, Agent: c.Agent, Tool: c.Tool, Args: args, Result: c.Result,
			Error: c.Error, DurationMs: c.DurationMs, CreatedAt: c.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// --- Exclusão de pastas (§6.6) ---

// assistantExcludedFolders devolve o conjunto de pastas fora do alcance do assistente. Usado pela
// pesquisa do Drive para filtrar o que o assistente vê (§6.6).
func (s *Server) assistantExcludedFolders(ctx context.Context, tenant uuid.UUID) map[string]bool {
	rows, err := s.q.ListExcludedFolders(ctx, tenant)
	if err != nil {
		log.Printf("assistant: excluded folders: %v", err)
		return nil
	}
	out := make(map[string]bool, len(rows))
	for _, f := range rows {
		out[f.String()] = true
	}
	return out
}

// handleAssistantExcludedFolders lista as pastas excluídas.
func (s *Server) handleAssistantExcludedFolders(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListExcludedFolders(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	ids := make([]string, 0, len(rows))
	for _, f := range rows {
		ids = append(ids, f.String())
	}
	writeJSON(w, http.StatusOK, map[string]any{"folder_ids": ids})
}

type excludeFolderReq struct {
	FolderID string `json:"folder_id"`
}

func (s *Server) handleAssistantExcludeFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req excludeFolderReq
	if !decode(w, r, &req) {
		return
	}
	fid, err := uuid.Parse(req.FolderID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	// A pasta tem de existir NESTE tenant — não se aceita um id de fora (hardening 24/07/2026).
	if _, err := s.q.GetFolderInTenant(r.Context(), db.GetFolderInTenantParams{ID: fid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	if err := s.q.AddExcludedFolder(r.Context(), db.AddExcludedFolderParams{
		TenantID: mu.TenantID, FolderID: fid,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAssistantUnexcludeFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	fid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	if err := s.q.RemoveExcludedFolder(r.Context(), db.RemoveExcludedFolderParams{
		TenantID: mu.TenantID, FolderID: fid,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// registerAssistantGovernance regista as rotas de §9/§6.4/§6.6. Chamado a partir do Router.
func (s *Server) registerAssistantGovernance(r chi.Router) {
	r.Get("/assistant/usage", s.handleAssistantUsage)
	r.Get("/assistant/audit", s.handleAssistantAudit)
	// A exclusão de pastas é controlo de alcance da organização (§6.6): administração.
	r.Get("/assistant/excluded-folders", s.handleAssistantExcludedFolders)
	r.With(s.requirePerm(rbac.PermOrgSettings)).Post("/assistant/excluded-folders", s.handleAssistantExcludeFolder)
	r.With(s.requirePerm(rbac.PermOrgSettings)).Delete("/assistant/excluded-folders/{id}", s.handleAssistantUnexcludeFolder)
}
