package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// The OpenAI-compatible API (DeepSeek included) REQUIRES a function name to match
// ^[a-zA-Z0-9_-]+$ — no dots. Domain tool names use a dot (mail.search), so they get swapped for
// an underscore when sending and reverted on receipt. No tool name uses an underscore, so the swap
// is a safe bijection — and it lives ONLY in this translation layer, so the rest of the code keeps
// seeing mail.search.
func wireToolName(s string) string   { return strings.ReplaceAll(s, ".", "_") }
func domainToolName(s string) string { return strings.ReplaceAll(s, "_", ".") }

// DeepSeek implements Provider via the OpenAI-compatible chat API
// (POST {baseURL}/chat/completions). Beskos's default provider.
type DeepSeek struct {
	apiKey    string
	baseURL   string
	model     string
	reasoning bool // v4 "thinks" before answering; off by default (latency)
	http      *http.Client
	// name identifies WHO serves the model (not the model itself). The same client serves DeepSeek
	// directly and TensorX, which speaks the same protocol — and the difference between the two is
	// the jurisdiction where the customer's content is processed. The startup log has to tell the
	// truth about that.
	name string
}

func NewDeepSeek(apiKey, baseURL, model string) *DeepSeek {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if model == "" {
		// DeepSeek discontinued "deepseek-chat"; the API now only accepts the v4 models
		// (deepseek-v4-flash / deepseek-v4-pro), which are reasoning models. Flash is the default
		// (cheaper/faster); swap it via AI_MODEL. The text comes in `content` (`reasoning_content`
		// is ignored).
		model = "deepseek-v4-flash"
	}
	return &DeepSeek{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		// Reasoning OFF by default: v4 generates reasoning_content BEFORE the text, and in
		// streaming the user stares at nothing for seconds (feels slow). Off, it answers
		// immediately, like the old deepseek-chat. AI_REASONING=on re-enables it (stronger
		// answers, slower).
		reasoning: os.Getenv("AI_REASONING") == "on",
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

// thinkingParam returns the reasoning control for the request body: {"type":"disabled"} when
// off (nil when on, so the model uses its own default). Only affects DeepSeek's v4 models.
func (d *DeepSeek) thinkingParam() *thinkingOpt {
	if d.reasoning {
		return nil
	}
	return &thinkingOpt{Type: "disabled"}
}

func (d *DeepSeek) Name() string {
	if d.name == "" {
		return "deepseek"
	}
	return d.name
}

// OpenAI-compatible wire format. The wire* types exist so the domain (Message, Tool, ToolCall)
// stays independent of any one provider's format: switching providers means writing another
// translation, not changing the consumers.
type chatRequest struct {
	Model       string       `json:"model"`
	Messages    []wireMsg    `json:"messages"`
	Temperature float64      `json:"temperature,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Stream      bool         `json:"stream"`
	Tools       []wireTool   `json:"tools,omitempty"`
	Thinking    *thinkingOpt `json:"thinking,omitempty"`
}

// thinkingOpt controls reasoning for DeepSeek v4 models ({"type":"disabled"} = no thinking).
type thinkingOpt struct {
	Type string `json:"type"`
}

type wireMsg struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []wireCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type wireTool struct {
	Type     string   `json:"type"` // always "function"
	Function wireFunc `json:"function"`
}

type wireFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type wireCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"` // always "function"
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

// toWire translates the domain into the OpenAI format. The tool name travels in "name" on
// result messages because some compatible providers still require it.
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
		Thinking:    d.thinkingParam(),
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
		return Response{}, fmt.Errorf("deepseek: invalid response (HTTP %d): %s", resp.StatusCode, string(body))
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if cr.Error != nil {
			msg = cr.Error.Message
		}
		return Response{}, fmt.Errorf("deepseek HTTP %d: %s", resp.StatusCode, msg)
	}
	if len(cr.Choices) == 0 {
		return Response{}, fmt.Errorf("deepseek: response has no choices")
	}
	return Response{
		Text:         cr.Choices[0].Message.Content,
		Model:        cr.Model,
		Usage:        cr.Usage,
		ToolCalls:    fromWireCalls(cr.Choices[0].Message.ToolCalls),
		FinishReason: cr.Choices[0].FinishReason,
	}, nil
}
