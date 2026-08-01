package httpapi

// Cross-cutting request handling: what every route passes through, rather than what any one route
// does. Kept apart from server.go so the route table there stays readable as a table.

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"mailin/backend/internal/rbac"
)

const sessionTTL = 7 * 24 * time.Hour

// redactSensitiveQuery devolve a URL com os parâmetros de credencial mascarados (para logs).
func redactSensitiveQuery(u *url.URL) string {
	if u.RawQuery == "" {
		return u.Path
	}
	q := u.Query()
	redacted := false
	for _, k := range []string{"access_token", "token"} {
		if q.Has(k) {
			q.Set(k, "REDACTED")
			redacted = true
		}
	}
	if !redacted {
		return u.Path + "?" + u.RawQuery
	}
	return u.Path + "?" + q.Encode()
}

// csrfGuard protege as rotas de sessão contra CSRF. Requisições que mudam estado precisam ou de um
// Authorization: Bearer (app nativo — token não-ambiente, sem risco de CSRF) ou do header custom
// X-Mailin-Client que só o nosso cliente envia. Um POST cross-site "simples" (form) não consegue
// setar header custom sem preflight, e o preflight bate na allow-list do CORS (auditoria M5).
func (s *Server) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if r.Header.Get("Authorization") == "" && r.Header.Get("X-Mailin-Client") == "" {
				writeErr(w, http.StatusForbidden, "csrf.blocked")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// publicRateLimit limita por IP os endpoints sem login (Meet público, download por token). Trava
// enumeração de salas/tokens e abuso desses endpoints anônimos (auditoria M).
func (s *Server) publicRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.pubLimiter.allow("pub:"+clientIP(r), time.Now()) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// presentationRateLimit limita o PDF da apresentação num balde próprio e generoso (ver presLimiter).
func (s *Server) presentationRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.presLimiter.allow("pres:"+clientIP(r), time.Now()) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLogger loga método, URL (com credenciais redigidas), status e duração.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		defer func() {
			log.Printf("%s %s -> %d (%s)", r.Method, redactSensitiveQuery(r.URL), ww.Status(), time.Since(start))
		}()
		next.ServeHTTP(ww, r)
	})
}

// requirePerm exige que o usuário autenticado (sessão de painel) tenha a permissão. Fonte de
// verdade é o papel salvo (nunca vindo do request). Fail-closed (lei §5.2).
func (s *Server) requirePerm(perm rbac.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := mailUserFrom(r.Context())
			// Efetivas = papel ± exceções da pessoa. É uma leitura a mais por pedido, mas só nas
			// rotas de administração (baixo tráfego) e é o que torna as exceções reais: sem isto
			// seriam decoração na UI, com o backend a continuar a decidir só pelo papel.
			allow, deny := s.memberOverrides(r.Context(), u.TenantID, u.ID)
			if !rbac.HasEffective(rbac.Role(u.Role), allow, deny, perm) {
				writeErr(w, http.StatusForbidden, "rbac.forbidden")
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}

// --- respostas ---

// orEmpty troca slice nil por vazio: em JSON, nil vira `null` e o frontend faz .map/.length em
// cima disso. Lista vazia é `[]`.
func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr returns an error with a STABLE code (the frontend translates it via i18n — law §4.4)
// plus an English fallback message derived from the code. The code is the source of truth for
// the translation; the message is only a backup and is always English (law §4.6).
func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error_code": code, "error": humanizeCode(code)})
}

// humanizeCode turns "auth.invalid_credentials" into "Invalid credentials" (English fallback).
func humanizeCode(code string) string {
	s := code
	if i := strings.LastIndex(code, "."); i >= 0 {
		s = code[i+1:]
	}
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return "Error"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
