package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// Limits for a customer-created profile. They exist so that an agent doesn't turn into a place
// where someone pastes a 50 KB prompt that then gets paid for on every turn.
const (
	agentNameMax         = 60
	agentPurposeMax      = 200
	agentInstructionsMax = 4000
)

// assistantAgentsFor returns the agents AVAILABLE to this organisation: the built-in ones that
// haven't been disabled, plus the ones created by the customer.
//
// A failure reading the database doesn't leave the person without an assistant: it returns at
// least the general agent. Without this, a transient database error would take away the whole
// feature instead of degrading it.
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
		// Disabling the general agent disables the assistant entirely (it IS the "assistant"): chat
		// rejects at the door with assistant.disabled. Here we just remove it from the list of
		// available agents like any other — the decision to block everything lives in the chat
		// handler, not here.
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

// clientAgent translates the row into the profile the engine uses. Tools are filtered against the
// CAP again, on read: if one of our releases removes a tool from the catalog, a profile saved
// months ago can't keep granting it.
func clientAgent(r db.AssistantAgent) assistant.Agent {
	return assistant.Agent{
		Key:          r.ID.String(),
		Name:         r.Name,
		Purpose:      r.Purpose,
		Instructions: r.Instructions,
		Tools:        capTools(r.Tools),
	}
}

// capTools throws away anything that isn't in the cap. It's the server-side validation, applied on
// write AND on read (§3.2): the UI is not the defense.
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

// assistantAgentByKey resolves an agent by the key that came in the request — built-in or
// customer-created. Only returns what's available to this organisation: a disabled agent is as
// good as nonexistent.
func (s *Server) assistantAgentByKey(ctx context.Context, tenant uuid.UUID, key string) (assistant.Agent, bool) {
	for _, ag := range s.assistantAgentsFor(ctx, tenant) {
		if ag.Key == key {
			return ag, true
		}
	}
	return assistant.Agent{}, false
}

// --- Management API ---

type agentJSON struct {
	Key     string   `json:"key"`
	Name    string   `json:"name,omitempty"`
	Purpose string   `json:"purpose,omitempty"`
	Builtin bool     `json:"builtin"`
	Enabled bool     `json:"enabled"`
	Tools   []string `json:"tools"`
	// Instructions only goes out for the customer's own agents: the built-in ones' instructions are
	// ours and aren't editable, nor of interest to the admin (§3.2).
	Instructions string `json:"instructions,omitempty"`
}

// handleAssistantAgents lists the organisation's agents — the built-in ones (with their
// enabled/disabled state) and the customer's own.
//
// The built-in ones carry neither a name nor a description: that text belongs to the UI and comes
// from i18n by key (law 4.1). The customer's own carry them, because the name was written by a
// person and isn't translated.
func (s *Server) handleAssistantAgents(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	disabled := map[string]bool{}
	if rows, err := s.q.ListDisabledBuiltinAgents(r.Context(), mu.TenantID); err == nil {
		for _, k := range rows {
			disabled[k] = true
		}
	}
	// Apps disabled by the organization also remove the agent that depends on them (e.g. no Files,
	// no Documents agent; no Mail, no Inbox agent) — not just from the tools, but from the picker
	// too.
	disabledApps := s.disabledAppsEffective(r.Context(), mu.TenantID)
	blockedTools := s.disabledAssistantTools(r.Context(), mu.TenantID)

	items := make([]agentJSON, 0, 6)
	for _, key := range assistant.BuiltinKeys() {
		ag, ok := assistant.Builtin(key)
		if !ok || !assistant.AgentAvailable(key, disabledApps) {
			continue
		}
		items = append(items, agentJSON{
			Key: ag.Key, Builtin: true, Tools: assistant.WithoutTools(assistant.AllowedTools(ag.Tools, disabledApps), blockedTools),
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
	// DuplicateOf, when set, starts from a BUILT-IN agent. The copy is independent: it doesn't
	// receive our updates, and that's disclosed at duplication time (§3.2).
	DuplicateOf string `json:"duplicate_of"`
}

// validate normalizes and rejects whatever can't be saved. Rejecting tools is the important part:
// a profile can never have more than the cap, and that's decided HERE, not in the UI.
func (a *agentWriteReq) validate() string {
	a.Name = strings.TrimSpace(a.Name)
	a.Purpose = strings.TrimSpace(a.Purpose)
	if a.Name == "" || len([]rune(a.Name)) > agentNameMax {
		return "assistant.agent_name_invalid"
	}
	if len([]rune(a.Purpose)) > agentPurposeMax || len([]rune(a.Instructions)) > agentInstructionsMax {
		return "assistant.agent_too_long"
	}
	// A tool outside the cap is REJECTED, not silently dropped: whoever asked for it has to know
	// they didn't get it. Filtering silently would produce an agent different from the one they
	// thought they were creating.
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

// handleAssistantAgentCreate creates a customer profile, from scratch or by duplicating a
// built-in one.
func (s *Server) handleAssistantAgentCreate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req agentWriteReq
	if !decode(w, r, &req) {
		return
	}
	if req.DuplicateOf != "" {
		src, ok := assistant.Builtin(req.DuplicateOf)
		if !ok {
			// Only BUILT-IN agents can be duplicated: duplicating a customer one would be copying a
			// copy, and the spec only promises starting from a base that works.
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

// handleAssistantAgentUpdate changes a customer profile. Built-in agents never go through here:
// the route requires a UUID, and built-in keys aren't UUIDs — the immutability doesn't depend on
// an `if`.
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

// handleAssistantAgentToggle enables or disables a BUILT-IN agent for the organisation.
//
// It controls AVAILABILITY, never behavior: there's no endpoint that changes the instructions or
// tools of a built-in agent, because those live in code (§3.2, decision 3).
func (s *Server) handleAssistantAgentToggle(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	key := chi.URLParam(r, "key")
	if _, ok := assistant.Builtin(key); !ok {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_agent")
		return
	}
	// Disabling the general agent is allowed: it's the assistant's master switch. While it's
	// disabled, chat rejects at the door (assistant.disabled) — the other agents end up with no way
	// in, which is the intended behavior for "assistant disabled for the organisation".
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

// agentWriteError translates the database error into an i18n key. A duplicate name is the common
// case and deserves its own message; everything else stays generic, so as not to leak database
// details.
func agentWriteError(err error) string {
	if strings.Contains(err.Error(), "idx_assistant_agents_name") {
		return "assistant.agent_name_taken"
	}
	log.Printf("assistant: agent write: %v", fmt.Errorf("%w", err))
	return "assistant.failed"
}
