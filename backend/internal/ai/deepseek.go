package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// A API compatível com OpenAI (DeepSeek incluído) EXIGE que o nome de uma função case
// ^[a-zA-Z0-9_-]+$ — sem pontos. Os nomes de ferramenta do domínio usam ponto (mail.search),
// portanto trocam-se por underscore ao enviar e revertem-se ao receber. Nenhum nome de ferramenta
// usa underscore, por isso a troca é uma bijeção segura — e vive SÓ nesta camada de tradução, para
// o resto do código continuar a ver mail.search.
func wireToolName(s string) string   { return strings.ReplaceAll(s, ".", "_") }
func domainToolName(s string) string { return strings.ReplaceAll(s, "_", ".") }

// DeepSeek implementa Provider via a API de chat compatível com OpenAI
// (POST {baseURL}/chat/completions). Provider padrão do Beskos.
type DeepSeek struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

func NewDeepSeek(apiKey, baseURL, model string) *DeepSeek {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if model == "" {
		model = "deepseek-chat"
	}
	return &DeepSeek{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (d *DeepSeek) Name() string { return "deepseek" }

// Formato de fio compatível com OpenAI. Os tipos wire* existem para que o domínio (Message, Tool,
// ToolCall) não fique preso ao formato de um fornecedor: trocar de provider é escrever outra
// tradução, não mudar os consumidores.
type chatRequest struct {
	Model       string     `json:"model"`
	Messages    []wireMsg  `json:"messages"`
	Temperature float64    `json:"temperature,omitempty"`
	MaxTokens   int        `json:"max_tokens,omitempty"`
	Stream      bool       `json:"stream"`
	Tools       []wireTool `json:"tools,omitempty"`
}

type wireMsg struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []wireCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type wireTool struct {
	Type     string   `json:"type"` // sempre "function"
	Function wireFunc `json:"function"`
}

type wireFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type wireCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"` // sempre "function"
	Function wireCallFun `json:"function"`
}

type wireCallFun struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      wireMsg `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// toWire traduz o domínio para o formato OpenAI. O nome da ferramenta viaja em "name" nas
// mensagens de resultado porque alguns fornecedores compatíveis ainda o exigem.
func toWire(msgs []Message) []wireMsg {
	out := make([]wireMsg, 0, len(msgs))
	for _, m := range msgs {
		w := wireMsg{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
		for _, c := range m.ToolCalls {
			w.ToolCalls = append(w.ToolCalls, wireCall{
				ID: c.ID, Type: "function",
				Function: wireCallFun{Name: wireToolName(c.Name), Arguments: c.Arguments},
			})
		}
		out = append(out, w)
	}
	return out
}

func toolsToWire(tools []Tool) []wireTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]wireTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, wireTool{
			Type:     "function",
			Function: wireFunc{Name: wireToolName(t.Name), Description: t.Description, Parameters: t.Parameters},
		})
	}
	return out
}

func fromWireCalls(calls []wireCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, ToolCall{ID: c.ID, Name: domainToolName(c.Function.Name), Arguments: c.Function.Arguments})
	}
	return out
}

func (d *DeepSeek) Complete(ctx context.Context, req Request) (Response, error) {
	payload, err := json.Marshal(chatRequest{
		Model:       d.model,
		Messages:    toWire(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      false,
		Tools:       toolsToWire(req.Tools),
	})
	if err != nil {
		return Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("deepseek: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return Response{}, fmt.Errorf("deepseek: resposta inválida (HTTP %d): %s", resp.StatusCode, string(body))
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if cr.Error != nil {
			msg = cr.Error.Message
		}
		return Response{}, fmt.Errorf("deepseek HTTP %d: %s", resp.StatusCode, msg)
	}
	if len(cr.Choices) == 0 {
		return Response{}, fmt.Errorf("deepseek: resposta sem choices")
	}
	return Response{
		Text:         cr.Choices[0].Message.Content,
		Model:        cr.Model,
		Usage:        cr.Usage,
		ToolCalls:    fromWireCalls(cr.Choices[0].Message.ToolCalls),
		FinishReason: cr.Choices[0].FinishReason,
	}, nil
}
