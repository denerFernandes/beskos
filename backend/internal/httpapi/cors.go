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

// publicCORS opens an endpoint to any origin, WITHOUT credentials.
//
// The credentialed `cors` above echoes a known origin and allows cookies, which is right for the
// application API and wrong here: the mail audit is unauthenticated, returns only records that are
// already published in public DNS, and is meant to be called from the marketing site -- a different
// origin from the API. Listing that site's domains here would couple the API to the landing and
// would have to grow every time a domain is added.
//
// "*" is also the safer of the two: a browser refuses to send credentials to a wildcard origin, so
// this can never turn into a way to make an authenticated request from somebody else's page.
func publicCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
