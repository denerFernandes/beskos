package managerapi

import (
	"testing"
	"time"
)

func TestHumanizeCode(t *testing.T) {
	cases := map[string]string{
		"manager.invalid_credentials": "Invalid credentials",
		"manager.totp_code_required":  "Totp code required",
		"failed":                      "Failed",
		"":                            "Error",
	}
	for in, want := range cases {
		if got := humanizeCode(in); got != want {
			t.Errorf("humanizeCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRateLimiterWindow(t *testing.T) {
	rl := newRateLimiter(2, time.Minute)
	now := time.Now()
	if !rl.allow("k", now) || !rl.allow("k", now) {
		t.Fatal("first two attempts should pass")
	}
	if rl.allow("k", now) {
		t.Fatal("third attempt within window should be blocked")
	}
	// Fora da janela, a chave volta a passar.
	if !rl.allow("k", now.Add(2*time.Minute)) {
		t.Fatal("attempt after window should pass")
	}
	// Chaves independentes não interferem.
	if !rl.allow("other", now) {
		t.Fatal("different key should pass")
	}
}
