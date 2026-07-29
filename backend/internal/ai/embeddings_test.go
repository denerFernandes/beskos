package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// O servidor tem de receber a dimensão pedida, e a resposta tem de bater certo com ela: um vetor
// de 4096 numa coluna vector(1536) rebenta no INSERT, e misturar dimensões estraga a busca.
func TestEmbedSendsDimensionsAndValidatesResponse(t *testing.T) {
	var gotDims float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		gotDims, _ = in["dimensions"].(float64)
		n := int(gotDims)
		if r.URL.Query().Get("wrong") != "" {
			n = 4096 // simula um servidor que ignora o parâmetro
		}
		vec := make([]float32, n)
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
	if gotDims != 1536 {
		t.Errorf("dimensions enviado = %v, quero 1536", gotDims)
	}
	if len(vecs[0]) != 1536 {
		t.Errorf("vetor com %d dimensões", len(vecs[0]))
	}

	// Servidor que ignora o parâmetro: tem de dar erro, não de escrever lixo.
	e2 := &Embedder{apiKey: "k", baseURL: srv.URL + "?wrong=1", model: "m", dims: 1536, hc: srv.Client()}
	if _, err := e2.Embed(context.Background(), []string{"olá"}); err == nil {
		t.Error("dimensão errada devia dar erro")
	} else if !strings.Contains(err.Error(), "expected 1536") {
		t.Errorf("erro pouco claro: %v", err)
	}
}
