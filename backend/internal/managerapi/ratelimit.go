package managerapi

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiter is an in-memory sliding window per key — the same implementation as httpapi's, kept
// local because the packages are deliberately isolated. Enough to stop brute-force login attempts
// on a single instance, which is the manager's deployment case.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]int64
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
	if len(rl.hits) > 10000 {
		for k, ts := range rl.hits {
			if len(ts) == 0 || ts[len(ts)-1] < cutoff {
				delete(rl.hits, k)
			}
		}
	}
	return true
}

// clientIP devolve o IP do cliente (middleware.RealIP já resolveu X-Forwarded-For atrás do Caddy).
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
