package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Embedder gera embeddings de texto num endpoint OpenAI-compatível (POST {base}/embeddings).
// Provedor escolhido para o RAG da wiki (docs/wiki-features.md §7.2): OpenAI text-embedding-3-small,
// dimensão 1536. Config só por ambiente (lei 2.6); sem EMBEDDINGS_API_KEY o RAG fica desligado.
type Embedder struct {
	apiKey  string
	baseURL string
	model   string
	// dims: dimensão pedida ao servidor. O `qwen3-embedding-8b` devolve 4096 por omissão, mas a
	// coluna é `vector(1536)` e o índice ivfflat do pgvector não passa das 2000 dimensões — sem
	// isto, ou o INSERT falha, ou perdíamos o índice e a busca passava a varrer a tabela toda.
	// Modelos com Matryoshka (é o caso do Qwen3) truncam sem estragar a qualidade.
	dims int
	hc   *http.Client
}

// NewEmbedderFromEnv lê a config do ambiente e devolve nil quando desligado (sem chave):
//
//	EMBEDDINGS_API_KEY     (obrigatório para ligar)
//	EMBEDDINGS_BASE_URL    (default "https://api.tensorx.ai/v1")
//	EMBEDDINGS_MODEL       (default "qwen/qwen3-embedding-8b")
//	EMBEDDINGS_DIMENSIONS  (default 1536 — tem de bater com a coluna vector(N))
func NewEmbedderFromEnv() *Embedder {
	key := os.Getenv("EMBEDDINGS_API_KEY")
	if key == "" {
		return nil
	}
	dims := 1536
	if v, err := strconv.Atoi(os.Getenv("EMBEDDINGS_DIMENSIONS")); err == nil && v > 0 {
		dims = v
	}
	return &Embedder{
		apiKey:  key,
		baseURL: getenv("EMBEDDINGS_BASE_URL", "https://api.tensorx.ai/v1"),
		model:   getenv("EMBEDDINGS_MODEL", "qwen/qwen3-embedding-8b"),
		dims:    dims,
		hc:      &http.Client{Timeout: 60 * time.Second},
	}
}

// Embed devolve um embedding por texto, na mesma ordem do input.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	payload := map[string]any{"model": e.model, "input": texts}
	if e.dims > 0 {
		payload["dimensions"] = e.dims
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("embeddings: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embeddings: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		return nil, fmt.Errorf("embeddings: provider returned %d: %s", resp.StatusCode, apiErr.Error.Message)
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embeddings: decode response: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: provider returned %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) {
			return nil, fmt.Errorf("embeddings: provider returned out-of-range index %d", d.Index)
		}
		// A dimensão TEM de bater com a coluna: um vetor de 4096 num vector(1536) rebenta no INSERT
		// com um erro obscuro, e pior — se algum dia a coluna aceitasse, misturar dimensões (ou
		// modelos) na mesma tabela dá uma busca semântica que devolve lixo em silêncio.
		if e.dims > 0 && len(d.Embedding) != e.dims {
			return nil, fmt.Errorf("embeddings: provider returned %d dimensions, expected %d (model %q ignores the `dimensions` parameter?)",
				len(d.Embedding), e.dims, e.model)
		}
		vecs[d.Index] = d.Embedding
	}
	return vecs, nil
}
