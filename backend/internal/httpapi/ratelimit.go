package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiter é um limitador em memória (janela deslizante por chave). Trava brute-force/credential
// stuffing no login e no 2º fator numa instância. Multi-instância exigiria um store partilhado
// (Redis) — registado como limitação conhecida, não como bloqueio deste fix (auditoria H5).
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]int64 // chave -> instantes (unix nanos) ainda dentro da janela
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: make(map[string][]int64), limit: limit, window: window}
}

// allow regista uma tentativa para a chave e devolve false se ela excede o limite na janela.
func (rl *rateLimiter) allow(key string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := now.Add(-rl.window).UnixNano()
	// Filtra in-place os instantes ainda válidos.
	kept := rl.hits[key][:0]
	for _, t := range rl.hits[key] {
		if t >= cutoff {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.limit {
		rl.hits[key] = kept
		return false
	}
	rl.hits[key] = append(kept, now.UnixNano())
	// Poda preguiçosa: se o mapa cresceu muito, remove chaves totalmente expiradas.
	if len(rl.hits) > 10000 {
		for k, ts := range rl.hits {
			if len(ts) == 0 || ts[len(ts)-1] < cutoff {
				delete(rl.hits, k)
			}
		}
	}
	return true
}

// clientIP devolve o IP do cliente. middleware.RealIP já resolve X-Forwarded-For quando atrás do
// nosso proxy (Caddy); fora dele, cai no RemoteAddr.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
