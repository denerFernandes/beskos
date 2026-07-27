package httpapi

import "net/http"

// cors permite o app nativo (Tauri, origem tauri://localhost / https://tauri.localhost) e outras
// origens configuradas a chamar a API. Sem origens configuradas, é no-op (web same-origin não
// precisa). Ecoa a origem exata (nunca "*") porque enviamos credenciais/permitimos o cookie.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				// `Range` é preciso para o app nativo (Tauri, cross-origin): o pdf.js pede intervalos
				// em PDFs grandes e o browser faz preflight — sem `Range` aqui, a apresentação de PDF
				// não carregava no desktop (os pequenos, que não usam Range, funcionavam).
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Locale, Range")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// Deixa o cliente ler os headers de range/tamanho numa resposta cross-origin (pdf.js).
			h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges")
		}
		next.ServeHTTP(w, r)
	})
}
