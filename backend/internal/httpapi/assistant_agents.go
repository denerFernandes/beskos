package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/assistant"
	"mailin/backend/internal/db"
)

// Limites de um perfil criado pelo cliente. Existem para que um agente não vire um sítio onde se
// cola um prompt de 50 KB que depois se paga a cada turno.
const (
	agentNameMax         = 60
	agentPurposeMax      = 200
	agentInstructionsMax = 4000
)

// assistantAgentsFor devolve os agentes DISPONÍVEIS a esta organização: os de origem que não foram
// desligados, mais os criados pelo cliente.
//
// Uma falha a ler a base não deixa a pessoa sem assistente: devolve pelo menos o agente geral. Sem
// isto, um erro de base transitório tiraria a funcionalidade toda em vez de a degradar.
func (s *Server) assistantAgentsFor(ctx context.Context, tenant uuid.UUID) []assistant.Agent {
	general, _ := assistant.Builtin(assistant.AgentGeneral)
	out := make([]assistant.Agent, 0, 6)

	disabled := map[string]bool{}
	if rows, err := s.q.ListDisabledBuiltinAgents(ctx, tenant); err == nil {
		for _, k := range rows {
			disabled[k] = true
		}
	} else {
		log.Printf("assistant: read builtin settings: %v", err)
	}
	for _, key := range assistant.BuiltinKeys() {
		// Desligar o agente geral desliga o assistente por inteiro (é ele o "assistente"): o chat
		// recusa à entrada com assistant.disabled. Aqui só o tiramos da lista de disponíveis como
		// qualquer outro — a decisão de bloquear tudo vive no handler do chat, não aqui.
		if disabled[key] {
			continue
		}
		if ag, ok := assistant.Builtin(key); ok {
			out = append(out, ag)
		}
	}

	rows, err := s.q.ListClientAgents(ctx, tenant)
	if err != nil {
		log.Printf("assistant: list client agents: %v", err)
		if len(out) == 0 {
			out = append(out, general)
		}
		return out
	}
	for _, r := range rows {
		out = append(out, clientAgent(r))
	}
	return out
}

// clientAgent traduz a linha para o perfil que o motor usa. As ferramentas são filtradas contra o
// TETO outra vez, na leitura: se uma versão nossa remover uma ferramenta do catálogo, um perfil
// gravado há meses não pode continuar a concedê-la.
func clientAgent(r db.AssistantAgent) assistant.Agent {
	return assistant.Agent{
		Key:          r.ID.String(),
		Name:         r.Name,
		Purpose:      r.Purpose,
		Instructions: r.Instructions,
		Tools:        capTools(r.Tools),
	}
}

// capTools deita fora tudo o que não esteja no teto. É a validação do servidor, aplicada na
// gravação E na leitura (§3.2): a interface não é a defesa.
func capTools(want []string) []string {
	max := assistant.MaxTools()
	out := make([]string, 0, len(want))
	for _, w := range want {
		for _, m := range max {
			if w == m {
				out = append(out, w)
				break
			}
		}
	}
	return out
}

// assistantAgentByKey resolve um agente pela chave que veio no pedido — de origem ou do cliente.
// Só devolve o que está disponível a esta organização: um agente desligado é como se não existisse.
func (s *Server) assistantAgentByKey(ctx context.Context, tenant uuid.UUID, key string) (assistant.Agent, bool) {
	for _, ag := range s.assistantAgentsFor(ctx, tenant) {
		if ag.Key == key {
			return ag, true
		}
	}
	return assistant.Agent{}, false
}

// --- API de gestão ---

type agentJSON struct {
	Key     string   `json:"key"`
	Name    string   `json:"name,omitempty"`
	Purpose string   `json:"purpose,omitempty"`
	Builtin bool     `json:"builtin"`
	Enabled bool     `json:"enabled"`
	Tools   []string `json:"tools"`
	// Instructions só vai nos do cliente: as dos agentes de origem são nossas e não são editáveis
	// nem interessantes ao administrador (§3.2).
	Instructions string `json:"instructions,omitempty"`
}

// handleAssistantAgents lista os agentes da organização — os de origem (com o seu estado de
// ligado/desligado) e os do cliente.
//
// Os de origem não trazem nome nem descrição: esse texto é de interface e vem do i18n pela chave
// (lei 4.1). Os do cliente trazem, porque o nome foi escrito por uma pessoa e não se traduz.
func (s *Server) handleAssistantAgents(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	disabled := map[string]bool{}
	if rows, err := s.q.ListDisabledBuiltinAgents(r.Context(), mu.TenantID); err == nil {
		for _, k := range rows {
			disabled[k] = true
		}
	}
	// Apps desligadas pela organização também tiram o agente que delas depende (ex.: sem Ficheiros,
	// sem agente de Documentos; sem Correio, sem o de Caixa) — não só das ferramentas, mas do picker.
	disabledApps := s.disabledAppsEffective(r.Context(), mu.TenantID)

	items := make([]agentJSON, 0, 6)
	for _, key := range assistant.BuiltinKeys() {
		ag, ok := assistant.Builtin(key)
		if !ok || !assistant.AgentAvailable(key, disabledApps) {
			continue
		}
		items = append(items, agentJSON{
			Key: ag.Key, Builtin: true, Tools: assistant.AllowedTools(ag.Tools, disabledApps),
			Enabled: !disabled[key],
		})
	}
	rows, err := s.q.ListClientAgents(r.Context(), mu.TenantID)
	if err != nil {
		log.Printf("assistant: list client agents: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	for _, c := range rows {
		items = append(items, agentJSON{
			Key: c.ID.String(), Name: c.Name, Purpose: c.Purpose,
			Instructions: c.Instructions, Tools: capTools(c.Tools), Enabled: true,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "max_tools": assistant.MaxTools()})
}

type agentWriteReq struct {
	Name         string   `json:"name"`
	Purpose      string   `json:"purpose"`
	Instructions string   `json:"instructions"`
	Tools        []string `json:"tools"`
	// DuplicateOf, quando preenchido, parte de um agente de ORIGEM. A cópia é independente: não
	// recebe as nossas atualizações, e isso é dito ao duplicar (§3.2).
	DuplicateOf string `json:"duplicate_of"`
}

// validate normaliza e recusa o que não pode ser gravado. A recusa de ferramentas é a importante:
// um perfil nunca pode ter mais do que o teto, e isso decide-se AQUI, não na interface.
func (a *agentWriteReq) validate() string {
	a.Name = strings.TrimSpace(a.Name)
	a.Purpose = strings.TrimSpace(a.Purpose)
	if a.Name == "" || len([]rune(a.Name)) > agentNameMax {
		return "assistant.agent_name_invalid"
	}
	if len([]rune(a.Purpose)) > agentPurposeMax || len([]rune(a.Instructions)) > agentInstructionsMax {
		return "assistant.agent_too_long"
	}
	// Uma ferramenta fora do teto é RECUSADA, não silenciosamente removida: quem pediu tem de saber
	// que não a levou. Filtrar em silêncio daria um agente diferente do que se julgou criar.
	max := assistant.MaxTools()
	for _, w := range a.Tools {
		found := false
		for _, m := range max {
			if w == m {
				found = true
				break
			}
		}
		if !found {
			return "assistant.agent_tool_not_allowed"
		}
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	return ""
}

// handleAssistantAgentCreate cria um perfil do cliente, do zero ou duplicando um de origem.
func (s *Server) handleAssistantAgentCreate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req agentWriteReq
	if !decode(w, r, &req) {
		return
	}
	if req.DuplicateOf != "" {
		src, ok := assistant.Builtin(req.DuplicateOf)
		if !ok {
			// Só se duplicam os de ORIGEM: duplicar um do cliente seria copiar uma cópia, e o
			// documento só promete partir de uma base que funciona.
			writeErr(w, http.StatusBadRequest, "assistant.unknown_agent")
			return
		}
		if req.Instructions == "" {
			req.Instructions = src.Instructions
		}
		if len(req.Tools) == 0 {
			req.Tools = src.Tools
		}
		if req.Purpose == "" {
			req.Purpose = src.Purpose
		}
	}
	if code := req.validate(); code != "" {
		writeErr(w, http.StatusBadRequest, code)
		return
	}
	row, err := s.q.CreateClientAgent(r.Context(), db.CreateClientAgentParams{
		TenantID: mu.TenantID, Name: req.Name, Purpose: req.Purpose,
		Instructions: req.Instructions, Tools: req.Tools,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, agentWriteError(err))
		return
	}
	writeJSON(w, http.StatusCreated, agentJSON{
		Key: row.ID.String(), Name: row.Name, Purpose: row.Purpose,
		Instructions: row.Instructions, Tools: capTools(row.Tools), Enabled: true,
	})
}

// handleAssistantAgentUpdate altera um perfil do cliente. Os de origem não passam por aqui: a rota
// exige um UUID, e as chaves dos de origem não são UUID — a imutabilidade não depende de um `if`.
func (s *Server) handleAssistantAgentUpdate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_agent")
		return
	}
	var req agentWriteReq
	if !decode(w, r, &req) {
		return
	}
	if code := req.validate(); code != "" {
		writeErr(w, http.StatusBadRequest, code)
		return
	}
	row, err := s.q.UpdateClientAgent(r.Context(), db.UpdateClientAgentParams{
		ID: id, TenantID: mu.TenantID, Name: req.Name, Purpose: req.Purpose,
		Instructions: req.Instructions, Tools: req.Tools,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, agentWriteError(err))
		return
	}
	writeJSON(w, http.StatusOK, agentJSON{
		Key: row.ID.String(), Name: row.Name, Purpose: row.Purpose,
		Instructions: row.Instructions, Tools: capTools(row.Tools), Enabled: true,
	})
}

func (s *Server) handleAssistantAgentDelete(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_agent")
		return
	}
	if err := s.q.DeleteClientAgent(r.Context(), db.DeleteClientAgentParams{ID: id, TenantID: mu.TenantID}); err != nil {
		log.Printf("assistant: delete agent: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type agentToggleReq struct {
	Enabled bool `json:"enabled"`
}

// handleAssistantAgentToggle liga ou desliga um agente de ORIGEM para a organização.
//
// Controla a DISPONIBILIDADE, nunca o comportamento: não há endpoint nenhum que mude as instruções
// ou as ferramentas de um agente de origem, porque elas vivem em código (§3.2, decisão 3).
func (s *Server) handleAssistantAgentToggle(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	key := chi.URLParam(r, "key")
	if _, ok := assistant.Builtin(key); !ok {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_agent")
		return
	}
	// Desligar o geral é permitido: é o interruptor-mestre do assistente. Enquanto estiver desligado,
	// o chat recusa à entrada (assistant.disabled) — os outros agentes ficam sem por onde entrar, o
	// que é o comportamento pretendido de "assistente desligado para a organização".
	var req agentToggleReq
	if !decode(w, r, &req) {
		return
	}
	if err := s.q.SetBuiltinAgentEnabled(r.Context(), db.SetBuiltinAgentEnabledParams{
		TenantID: mu.TenantID, AgentKey: key, Enabled: req.Enabled,
	}); err != nil {
		log.Printf("assistant: toggle agent: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "enabled": req.Enabled})
}

// agentWriteError traduz o erro da base numa chave de i18n. O nome duplicado é o caso comum e
// merece uma mensagem própria; o resto fica genérico, para não vazar detalhes da base.
func agentWriteError(err error) string {
	if strings.Contains(err.Error(), "idx_assistant_agents_name") {
		return "assistant.agent_name_taken"
	}
	log.Printf("assistant: agent write: %v", fmt.Errorf("%w", err))
	return "assistant.failed"
}
