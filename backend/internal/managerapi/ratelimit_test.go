package managerapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

// TestClientIPIgnoresSpoofedPrefix mirrors httpapi's equivalent test: exactly one trusted reverse
// proxy (Caddy) sits in front of this service, so clientIP must trust only the X-Forwarded-For
// entry that proxy itself appended -- never a leftmost entry a client can forge. This is the
// vulnerability the now-deprecated middleware.RealIP had (GHSA-3fxj-6jh8-hvhx).
func TestClientIPIgnoresSpoofedPrefix(t *testing.T) {
	var got string
	h := middleware.ClientIPFromXFFTrustedProxies(1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = clientIP(r)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.7")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got == "198.51.100.1" {
		t.Fatal("clientIP trusted the attacker-forged leftmost X-Forwarded-For entry")
	}
	if got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want 203.0.113.7 (the entry our own proxy appended)", got)
	}
}

func TestClientIPFallsBackToRemoteAddrWithoutProxy(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:54321"

	got := clientIP(req)
	if got != "127.0.0.1" {
		t.Errorf("clientIP = %q, want 127.0.0.1 from RemoteAddr", got)
	}
}
