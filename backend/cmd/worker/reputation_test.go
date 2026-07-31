package main

import "testing"

// Law 2.8: a loop that swallows errors has to escalate when it keeps failing.
//
// What broke in production was never the sleeping between passes; it was the counting. Every read
// returned HTTP 400, every pass, for days, and nothing said so -- the watchdog looked identical to
// one with nothing to report. So the tests drive blindAfter, which is the decision the loop makes,
// and not the loop, which would need a fake clock to test and would prove less.

func TestOneBadDomainIsNotBlindness(t *testing.T) {
	// Nine domains read, one failed: ordinary noise. Alarming here trains everyone to ignore the
	// alarm, which costs more than the one missing reading.
	if got := blindAfter(0, 9, 1); got != 0 {
		t.Fatalf("blindAfter(0, 9, 1) = %d, want 0", got)
	}
}

func TestNothingToMeasureIsNotBlindness(t *testing.T) {
	// No domains at all (empty database, SES off): nothing was attempted, so nothing is wrong.
	// Counting this as blindness would raise the alarm on a fresh install.
	if got := blindAfter(2, 0, 0); got != 0 {
		t.Fatalf("blindAfter(2, 0, 0) = %d, want 0", got)
	}
}

func TestTotalFailureCountsUp(t *testing.T) {
	// The production failure: every domain fails to read, pass after pass.
	blind := 0
	for pass := 1; pass <= 5; pass++ {
		blind = blindAfter(blind, 0, 3)
		if blind != pass {
			t.Fatalf("after %d failing passes blind = %d, want %d", pass, blind, pass)
		}
	}
}

func TestAlarmThresholdIsReachable(t *testing.T) {
	// The counter has to actually hit the threshold the loop compares against, or the alarm is
	// unreachable code and the watchdog stays as silent as it was before this was written.
	blind := 0
	for i := 0; i < blindPassesBeforeAlarm; i++ {
		blind = blindAfter(blind, 0, 1)
	}
	if blind != blindPassesBeforeAlarm {
		t.Fatalf("blind = %d after %d failing passes, want %d",
			blind, blindPassesBeforeAlarm, blindPassesBeforeAlarm)
	}
}

func TestRecoveryResetsTheCounter(t *testing.T) {
	blind := 0
	for i := 0; i < 4; i++ {
		blind = blindAfter(blind, 0, 2)
	}
	if got := blindAfter(blind, 1, 1); got != 0 {
		t.Fatalf("blindAfter(%d, 1, 1) = %d after one readable domain, want 0", blind, got)
	}
}
