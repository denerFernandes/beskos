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
	for i := 1; i <= 2; i++ {
		if !rl.allow("k", now) {
			t.Fatalf("attempt %d of 2 should pass", i)
		}
	}
	if rl.allow("k", now) {
		t.Fatal("third attempt within window should be blocked")
	}
	// Outside the window, the key passes again.
	if !rl.allow("k", now.Add(2*time.Minute)) {
		t.Fatal("attempt after window should pass")
	}
	// Independent keys don't interfere with each other.
	if !rl.allow("other", now) {
		t.Fatal("different key should pass")
	}
}
