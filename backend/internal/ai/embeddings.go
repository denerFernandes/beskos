package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	// dims: dimensão com que gravamos. A coluna é `vector(1536)` e o índice ivfflat do pgvector não
	// passa das 2000 dimensões — com os 4096 nativos do qwen3-embedding-8b, ou o INSERT rebenta, ou
	// perdíamos o índice e a busca passava a varrer a tabela toda.
	//
	// Cortamos aqui, e não no pedido: a TensorX recusa o parâmetro `dimensions` com 400 (confirmado
	// contra o endpoint real). Truncar é o mecanismo previsto — o Qwen3 é treinado com Matryoshka,
	// tal como o text-embedding-3 da OpenAI, e guarda o significado nas primeiras componentes.
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
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
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
		// A dimensão TEM de bater com a coluna: um vetor mais curto que o esperado é sinal de modelo
		// trocado, e misturar modelos na mesma tabela dá uma busca semântica que devolve lixo em
		// silêncio — pior do que falhar.
		if len(d.Embedding) < e.dims {
			return nil, fmt.Errorf("embeddings: model %q returned %d dimensions, need at least %d",
				e.model, len(d.Embedding), e.dims)
		}
		vecs[d.Index] = shorten(d.Embedding, e.dims)
	}
	return vecs, nil
}

// shorten corta o vetor às primeiras `dims` componentes e volta a normalizá-lo (norma 1).
// A renormalização não é cosmética: cortado, o vetor perde comprimento, e a busca compara por
// produto interno — sem isto os textos longos ficariam sistematicamente à frente dos curtos.
func shorten(v []float32, dims int) []float32 {
	if dims <= 0 || len(v) <= dims {
		return v
	}
	out := v[:dims]
	var sum float64
	for _, x := range out {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return out
	}
	norm := float32(math.Sqrt(sum))
	for i, x := range out {
		out[i] = x / norm
	}
	return out
}
