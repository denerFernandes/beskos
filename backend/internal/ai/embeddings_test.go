package ai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// O modelo devolve 4096 dimensões e a coluna é vector(1536): o corte tem de acontecer aqui, e o
// vetor cortado tem de sair normalizado. Também não podemos pedir `dimensions` — a TensorX recusa
// o parâmetro com 400.
func TestEmbedTruncatesAndNormalizes(t *testing.T) {
	var sentDims bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if _, ok := in["dimensions"]; ok {
			sentDims = true
		}
		n := 4096
		if r.URL.Query().Get("short") != "" {
			n = 768 // simula o modelo trocado por um mais pequeno
		}
		vec := make([]float32, n)
		for i := range vec {
			vec[i] = 0.5 // norma != 1 de propósito, para se ver a normalização
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": vec}},
		})
	}))
	defer srv.Close()

	e := &Embedder{apiKey: "k", baseURL: srv.URL, model: "qwen/qwen3-embedding-8b", dims: 1536, hc: srv.Client()}
	vecs, err := e.Embed(context.Background(), []string{"olá"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if sentDims {
		t.Error("não devíamos enviar `dimensions` — a TensorX responde 400")
	}
	if len(vecs[0]) != 1536 {
		t.Fatalf("vetor com %d dimensões, quero 1536", len(vecs[0]))
	}
	var sum float64
	for _, x := range vecs[0] {
		sum += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(sum)-1) > 1e-5 {
		t.Errorf("vetor não normalizado: norma = %v", math.Sqrt(sum))
	}

	// Vetor mais curto do que a coluna: erro claro, não escrita de lixo.
	e2 := &Embedder{apiKey: "k", baseURL: srv.URL + "?short=1", model: "outro", dims: 1536, hc: srv.Client()}
	if _, err := e2.Embed(context.Background(), []string{"olá"}); err == nil {
		t.Error("dimensão insuficiente devia dar erro")
	} else if !strings.Contains(err.Error(), "need at least 1536") {
		t.Errorf("erro pouco claro: %v", err)
	}
}
