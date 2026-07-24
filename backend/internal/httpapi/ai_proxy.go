package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"mailin/backend/internal/assistant"
)

// Proxy OpenAI-compatível para o plugin de IA do OnlyOffice usar o DeepSeek SEM que a chave real saia
// do servidor. O plugin corre no iframe do Document Server (outra origem) e autentica com um token
// estável do utilizador (Bearer) — que aqui vira identidade, quota e reencaminhamento com a chave
// verdadeira. Só no editor autenticado; a quota é a do próprio utilizador (a mesma janela de 6h do
// assistente). Ver docs/ASSISTENTE-IA.md (§9.2).

// aiProxyHTTP tem timeout largo: uma completude de IA demora mais do que um pedido normal.
var aiProxyHTTP = &http.Client{Timeout: 120 * time.Second}

// EnableAIProxy guarda o endpoint/chave/modelo do DeepSeek que o proxy reencaminha. A chave fica SÓ
// aqui, no servidor. Sem chave, o proxy responde desligado.
func (s *Server) EnableAIProxy(baseURL, apiKey, model string) {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if model == "" {
		model = "deepseek-chat"
	}
	s.aiProxyBase = strings.TrimRight(baseURL, "/")
	s.aiProxyKey = apiKey
	s.aiProxyModel = model
}

func (s *Server) aiProxyReady(w http.ResponseWriter) bool {
	if s.office == nil || s.aiProxyKey == "" {
		writeErr(w, http.StatusServiceUnavailable, "assistant.disabled")
		return false
	}
	return true
}

// handleAIEditorKey devolve ao editor AUTENTICADO a config para o plugin de IA apontar ao nosso proxy:
// URL base OpenAI-compatível + "API key" (token estável do utilizador) + modelo. A chave do DeepSeek
// nunca aparece aqui.
func (s *Server) handleAIEditorKey(w http.ResponseWriter, r *http.Request) {
	if !s.aiProxyReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	tok := s.office.mintAIToken(mu.ID, mu.TenantID, time.Now().Add(365*24*time.Hour))
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url": s.publicURL + "/api/ai/openai/v1",
		"api_key":  tok,
		"model":    s.aiProxyModel,
	})
}

// aiCORS trata o cross-origin do plugin (iframe do Document Server). Responde ao preflight e põe os
// cabeçalhos. Devolve true se já respondeu (OPTIONS) e o handler deve parar.
func (s *Server) aiCORS(w http.ResponseWriter, r *http.Request) bool {
	if s.office != nil && s.office.publicURL != "" {
		w.Header().Set("Access-Control-Allow-Origin", s.office.publicURL)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// aiAuth valida o Bearer (token de IA do utilizador) e devolve a Identity. Sem cookie: o iframe do DS
// não o tem.
func (s *Server) aiAuth(r *http.Request) (assistant.Identity, bool) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	userID, tenantID, ok := s.office.parseAIToken(tok)
	if !ok {
		return assistant.Identity{}, false
	}
	return assistant.Identity{UserID: userID, TenantID: tenantID}, true
}

// handleAIModels responde a lista de modelos (o plugin pede /v1/models). Só o nosso modelo, para o
// utilizador não escolher outro que o proxy não serve.
func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	if s.aiCORS(w, r) || !s.aiProxyReady(w) {
		return
	}
	if _, ok := s.aiAuth(r); !ok {
		aiError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   []map[string]any{{"id": s.aiProxyModel, "object": "model", "owned_by": "beskos"}},
	})
}

// handleAIModelsEditor responde /v1/models ao ai-proxy do Document Server (só o nosso modelo). A
// lista de modelos não é sensível; basta o proxy estar ligado.
func (s *Server) handleAIModelsEditor(w http.ResponseWriter, r *http.Request) {
	if s.aiCORS(w, r) || !s.aiProxyReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   []map[string]any{{"id": s.aiProxyModel, "object": "model", "owned_by": "beskos"}},
	})
}

// handleAIChatCompletions é o coração do proxy: autentica, verifica a quota, força o nosso modelo,
// reencaminha ao DeepSeek com a chave real e conta os tokens gastos na quota do utilizador.
func (s *Server) handleAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	if s.aiCORS(w, r) || !s.aiProxyReady(w) {
		return
	}
	id, ok := s.aiAuth(r)
	if !ok {
		aiError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	// Quota do próprio utilizador (a mesma janela do assistente). Esgotada → recusa, como no chat.
	if s.assistantQuotaStatus(r.Context(), id, time.Now()).Exceeded() {
		aiError(w, http.StatusTooManyRequests, "AI quota exceeded for this period")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		aiError(w, http.StatusBadRequest, "invalid request")
		return
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		aiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Força o nosso modelo (o utilizador não escolhe outro) e, em streaming, pede o uso no fim para
	// poder contar os tokens.
	req["model"] = s.aiProxyModel
	stream, _ := req["stream"].(bool)
	if stream {
		req["stream_options"] = map[string]any{"include_usage": true}
	}
	out, _ := json.Marshal(req)

	fReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.aiProxyBase+"/chat/completions", bytes.NewReader(out))
	if err != nil {
		aiError(w, http.StatusInternalServerError, "proxy error")
		return
	}
	fReq.Header.Set("Content-Type", "application/json")
	fReq.Header.Set("Authorization", "Bearer "+s.aiProxyKey)
	if stream {
		fReq.Header.Set("Accept", "text/event-stream")
	}
	resp, err := aiProxyHTTP.Do(fReq)
	if err != nil {
		aiError(w, http.StatusBadGateway, "AI provider unreachable")
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)

	// Grava o gasto na quota depois de a resposta passar. Um erro a contar não afeta o utilizador —
	// mas o gasto real deve contar (por isso context.WithoutCancel: sobrevive a fechar a ligação).
	tokens := pipeAndCountTokens(w, resp.Body, stream)
	if tokens > 0 {
		s.recordAssistantUsage(context.WithoutCancel(r.Context()), id, tokens)
	}
}

// pipeAndCountTokens copia a resposta do DeepSeek para o cliente e devolve os tokens totais. Em
// streaming, lê linha a linha e apanha o "usage" do último pedaço (pedido com include_usage); em JSON
// simples, desserializa e lê usage.total_tokens.
func pipeAndCountTokens(w http.ResponseWriter, body io.Reader, stream bool) int {
	flusher, _ := w.(http.Flusher)
	if !stream {
		data, _ := io.ReadAll(io.LimitReader(body, 8<<20))
		_, _ = w.Write(data)
		var parsed struct {
			Usage struct {
				TotalTokens int `json:"total_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(data, &parsed)
		return parsed.Usage.TotalTokens
	}
	tokens := 0
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
		if flusher != nil {
			flusher.Flush()
		}
		trimmed := strings.TrimSpace(strings.TrimPrefix(string(line), "data:"))
		if trimmed == "" || trimmed == "[DONE]" {
			continue
		}
		var chunk struct {
			Usage *struct {
				TotalTokens int `json:"total_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(trimmed), &chunk) == nil && chunk.Usage != nil {
			tokens = chunk.Usage.TotalTokens
		}
	}
	return tokens
}

// handleAIEnvelopeProxy é o proxy do MODO SERVIDOR do plugin de IA do OnlyOffice (aiSettings.proxy).
// O plugin envia um envelope {target, method, headers, data} com o JWT do EDITOR no Bearer. Daqui:
// valida o JWT (prova a sessão), tira o utilizador (editorConfig.user.id) → tenant, verifica a quota,
// e reencaminha o pedido de dentro (data) ao DeepSeek com a NOSSA chave — ignorando o target/headers
// do envelope (a chave do DeepSeek nunca vem do browser). Assim: config no servidor, sem colar chave,
// e a cota é a do próprio utilizador.
func (s *Server) handleAIEnvelopeProxy(w http.ResponseWriter, r *http.Request) {
	if s.aiCORS(w, r) || !s.aiProxyReady(w) {
		return
	}
	// Identidade a partir do JWT do editor (assinado por nós; o segredo é partilhado com o DS).
	jwt := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	payload, ok := s.office.jwtVerify(jwt)
	if !ok {
		aiError(w, http.StatusUnauthorized, "invalid editor token")
		return
	}
	userID := userIDFromEditorJWT(payload)
	if userID == uuid.Nil {
		aiError(w, http.StatusUnauthorized, "no user in token")
		return
	}
	mu, err := s.q.GetUserByID(r.Context(), userID)
	if err != nil {
		aiError(w, http.StatusUnauthorized, "unknown user")
		return
	}
	id := assistant.Identity{UserID: mu.ID, TenantID: mu.TenantID}
	if s.assistantQuotaStatus(r.Context(), id, time.Now()).Exceeded() {
		aiError(w, http.StatusTooManyRequests, "AI quota exceeded for this period")
		return
	}

	// O corpo pode chegar como ENVELOPE {target, headers, data} (o pedido OpenAI vai em `data`) OU já
	// como o pedido OpenAI cru — depende de como o Document Server o reencaminha. Aceita os dois. O
	// `target`/`headers` do envelope são ignorados de propósito: não seguimos para onde mandarem nem
	// usamos chave de fora.
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	var req map[string]any
	if json.Unmarshal(raw, &env) == nil && len(env.Data) > 0 {
		req = decodeInnerRequest(env.Data)
	}
	if req == nil { // sem `data`: o corpo já É o pedido OpenAI
		req = decodeInnerRequest(raw)
	}
	if req == nil || req["messages"] == nil {
		aiError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req["model"] = s.aiProxyModel
	stream, _ := req["stream"].(bool)
	if stream {
		req["stream_options"] = map[string]any{"include_usage": true}
	}
	out, _ := json.Marshal(req)

	fReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.aiProxyBase+"/chat/completions", bytes.NewReader(out))
	if err != nil {
		aiError(w, http.StatusInternalServerError, "proxy error")
		return
	}
	fReq.Header.Set("Content-Type", "application/json")
	fReq.Header.Set("Authorization", "Bearer "+s.aiProxyKey)
	if stream {
		fReq.Header.Set("Accept", "text/event-stream")
	}
	resp, err := aiProxyHTTP.Do(fReq)
	if err != nil {
		aiError(w, http.StatusBadGateway, "AI provider unreachable")
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	tokens := pipeAndCountTokens(w, resp.Body, stream)
	if tokens > 0 {
		s.recordAssistantUsage(context.WithoutCancel(r.Context()), id, tokens)
	}
}

// userIDFromEditorJWT tira o id do utilizador do JWT que o Document Server dá ao plugin. O formato
// não está documentado (docs têm lacuna), por isso tenta os sítios plausíveis: editorConfig.user.id,
// user.id, userId no topo. O primeiro que for um UUID válido ganha.
func userIDFromEditorJWT(payload map[string]any) uuid.UUID {
	candidates := []string{}
	// O JWT que o Document Server dá ao ai-proxy tem o utilizador em payload.user.id (confirmado ao
	// vivo). Mantemos também os outros sítios plausíveis por robustez.
	if p, ok := payload["payload"].(map[string]any); ok {
		if usr, ok := p["user"].(map[string]any); ok {
			if s, _ := usr["id"].(string); s != "" {
				candidates = append(candidates, s)
			}
		}
	}
	if ec, ok := payload["editorConfig"].(map[string]any); ok {
		if usr, ok := ec["user"].(map[string]any); ok {
			if s, _ := usr["id"].(string); s != "" {
				candidates = append(candidates, s)
			}
		}
	}
	if usr, ok := payload["user"].(map[string]any); ok {
		if s, _ := usr["id"].(string); s != "" {
			candidates = append(candidates, s)
		}
	}
	for _, k := range []string{"userId", "userid", "uid", "sub"} {
		if s, _ := payload[k].(string); s != "" {
			candidates = append(candidates, s)
		}
	}
	for _, s := range candidates {
		if uid, err := uuid.Parse(s); err == nil {
			return uid
		}
	}
	return uuid.Nil
}

// decodeInnerRequest lê o pedido OpenAI de dentro do envelope. O `data` pode chegar como objeto JSON
// ou como string com JSON (o body do fetch do plugin) — trata os dois.
func decodeInnerRequest(data json.RawMessage) map[string]any {
	var asStr string
	if json.Unmarshal(data, &asStr) == nil {
		data = json.RawMessage(asStr)
	}
	var req map[string]any
	if json.Unmarshal(data, &req) != nil {
		return nil
	}
	return req
}

// aiError responde no formato de erro do OpenAI, que é o que o plugin sabe ler.
func aiError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": msg, "type": "invalid_request_error"}})
}
