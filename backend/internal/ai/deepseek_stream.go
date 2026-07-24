package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// streamChunk é um pedaço do fio SSE do formato OpenAI. O texto chega em `delta.content`; um pedido
// de ferramenta chega FRAGMENTADO em `delta.tool_calls`, com o índice a dizer a que chamada
// pertence cada pedaço — é por isso que há um acumulador em vez de uma simples cópia.
type streamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []streamCallFrag `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

type streamCallFrag struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Stream faz uma completude token-a-token. onDelta recebe cada pedaço de TEXTO à medida que chega;
// os pedidos de ferramenta não são transmitidos a meio porque um nome ou uns argumentos pela metade
// não dizem nada a ninguém — só o fim da chamada é informação útil.
//
// A Response devolvida é equivalente à de Complete: quem chama não precisa de saber qual usou.
func (d *DeepSeek) Stream(ctx context.Context, req Request, onDelta func(string)) (Response, error) {
	payload, err := json.Marshal(chatRequest{
		Model:       d.model,
		Messages:    toWire(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
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
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("deepseek: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return Response{}, fmt.Errorf("deepseek HTTP %d: %s", resp.StatusCode, string(body))
	}
	return readSSE(resp.Body, onDelta)
}

// readSSE consome o fio e reconstrói a resposta. Isolado da chamada HTTP para ser testável sem rede.
func readSSE(body io.Reader, onDelta func(string)) (Response, error) {
	var (
		out   Response
		text  strings.Builder
		calls = map[int]*ToolCall{}
		order []int
	)
	sc := bufio.NewScanner(body)
	// Uma linha de SSE pode ser grande (argumentos de ferramenta num só pedaço). O default do
	// Scanner são 64 KiB e estourar a meio deixaria a resposta truncada em silêncio.
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ch streamChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			// Um pedaço ilegível não deita fora o que já veio: o fio continua.
			continue
		}
		if ch.Model != "" {
			out.Model = ch.Model
		}
		if ch.Usage != nil {
			out.Usage = *ch.Usage
		}
		if len(ch.Choices) == 0 {
			continue
		}
		c := ch.Choices[0]
		if c.FinishReason != "" {
			out.FinishReason = c.FinishReason
		}
		if c.Delta.Content != "" {
			text.WriteString(c.Delta.Content)
			if onDelta != nil {
				onDelta(c.Delta.Content)
			}
		}
		for _, frag := range c.Delta.ToolCalls {
			acc, ok := calls[frag.Index]
			if !ok {
				acc = &ToolCall{}
				calls[frag.Index] = acc
				order = append(order, frag.Index)
			}
			if frag.ID != "" {
				acc.ID = frag.ID
			}
			if frag.Function.Name != "" {
				acc.Name = frag.Function.Name
			}
			acc.Arguments += frag.Function.Arguments
		}
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("deepseek: stream: %w", err)
	}

	out.Text = text.String()
	for _, idx := range order {
		tc := *calls[idx]
		// Reverte o nome da ferramenta do formato de fio (mail_search) para o domínio (mail.search).
		tc.Name = domainToolName(tc.Name)
		out.ToolCalls = append(out.ToolCalls, tc)
	}
	return out, nil
}
