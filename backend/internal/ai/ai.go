// Package ai é a camada de IA do Beskos. A interface Provider abstrai o modelo/fornecedor,
// para que a troca (DeepSeek hoje; Claude/OpenAI/etc. no futuro) seja só configuração — o
// resto do código depende de `Provider`, nunca de um SDK específico (lei §1.1/2.5).
//
// A chave de API vive SÓ no backend, via ambiente (lei §3.5/§10). Nada aqui vaza ao frontend.
package ai

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	// RoleTool carrega o RESULTADO de uma ferramenta de volta ao modelo. O conteúdo é sempre
	// dado não fiável — nunca instrução (§6.1 do desenho do assistente).
	RoleTool Role = "tool"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// ToolCalls é preenchido em mensagens do assistente que pedem ferramentas.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID liga uma mensagem RoleTool à chamada que a originou.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Tool descreve uma ferramenta ao modelo. Parameters é um JSON Schema do tipo "object".
// O catálogo real vive no pacote assistant; aqui só passa a descrição ao fornecedor.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolCall é um pedido do modelo para invocar uma ferramenta. Arguments é o JSON cru tal como o
// modelo o produziu — validar antes de usar é responsabilidade de quem despacha.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Request é o pedido genérico de completude. Campos zero usam os defaults do provider.
type Request struct {
	Messages    []Message
	Temperature float64
	MaxTokens   int
	// Tools, quando não vazio, permite ao modelo pedir ferramentas em vez de só responder texto.
	Tools []Tool
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Response struct {
	Text  string
	Model string
	Usage Usage
	// ToolCalls não é vazio quando o modelo quer invocar ferramentas em vez de responder.
	ToolCalls []ToolCall
	// FinishReason é o motivo de paragem do fornecedor (ex.: "stop", "tool_calls", "length").
	FinishReason string
}

// Provider é o contrato que todo fornecedor de LLM implementa. Manter mínimo e estável:
// adicionar um modelo novo = implementar esta interface, sem tocar nos consumidores.
type Provider interface {
	// Name identifica o provider (ex.: "deepseek") para logs/métricas.
	Name() string
	// Complete faz uma completude síncrona. ctx carrega timeout/cancelamento.
	Complete(ctx context.Context, req Request) (Response, error)
}

// StreamingProvider é opcional: providers que suportam streaming token-a-token o implementam.
// Consumidores fazem type assertion quando quiserem stream; a interface base não exige.
type StreamingProvider interface {
	Provider
	Stream(ctx context.Context, req Request, onDelta func(string)) (Response, error)
}
