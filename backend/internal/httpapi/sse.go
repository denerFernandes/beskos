package httpapi

import (
	"encoding/json"
	"net/http"
)

// sseWriter transmite eventos Server-Sent Events. Cada evento é uma linha `data: <json>`, seguida
// de linha em branco — o JSON serializado nunca contém quebras de linha cruas, portanto uma linha
// por evento chega.
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

// newSSE escreve os cabeçalhos e devolve o transmissor. Falha quando a ligação não permite
// flush — sem flush, tudo ficaria em buffer e chegaria de uma vez no fim, que é o oposto de
// streaming.
func newSSE(w http.ResponseWriter) (*sseWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Diz ao nginx para não fazer buffer da resposta; sem isto o proxy anula o streaming.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &sseWriter{w: w, f: f}, true
}

// send emite um evento. Um valor que não serialize é ignorado em silêncio: matar o fio a meio por
// causa de um evento seria pior do que perder esse evento.
func (s *sseWriter) send(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	if _, err := s.w.Write(append(append([]byte("data: "), payload...), '\n', '\n')); err != nil {
		return
	}
	s.f.Flush()
}
