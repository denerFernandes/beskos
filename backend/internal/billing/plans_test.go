package billing

import "testing"

func TestPlanLookup(t *testing.T) {
	if _, ok := PlanByID("base"); !ok {
		t.Error("base should exist")
	}
	if _, ok := PlanByID("inexistente"); ok {
		t.Error("nonexistent plan should not exist")
	}
	if PlanOrDefault("xpto").ID != DefaultPlanID {
		t.Error("unknown plan should fall back to trial (fail-safe)")
	}
}

func TestPlansAreOrderedByCapacity(t *testing.T) {
	ps := Plans()
	for i := 1; i < len(ps); i++ {
		if ps[i].MaxMailboxes < ps[i-1].MaxMailboxes {
			t.Errorf("plans out of capacity order: %s < %s", ps[i].ID, ps[i-1].ID)
		}
		if ps[i].MaxStorageBytes < ps[i-1].MaxStorageBytes {
			t.Errorf("storage out of order: %s", ps[i].ID)
		}
	}
}

func TestTrialIsFreeAndLimited(t *testing.T) {
	trial := PlanOrDefault("trial")
	if trial.PriceCentsPerUser != 0 {
		t.Error("trial must be free")
	}
	if trial.MaxMailboxes <= 0 || trial.MaxStorageBytes <= 0 {
		t.Error("trial needs positive limits")
	}
}

func TestIsValidPlan(t *testing.T) {
	if !IsValidPlan("business") || IsValidPlan("") {
		t.Error("incorrect plan validation")
	}
}

// A plan with a zero limit (the self-hosted plan) must mean "no cap," never "zero allowed" — the
// caps here previously defaulted to blocking the very first mailbox, upload, or meeting attendee.
func TestZeroLimitsMeanUnlimited(t *testing.T) {
	unlimited := Plan{}
	if unlimited.MailboxesExceeded(0) {
		t.Error("MaxMailboxes 0 must not block the first mailbox")
	}
	if unlimited.MailboxesExceeded(1000) {
		t.Error("MaxMailboxes 0 must not cap mailbox count at all")
	}
	if unlimited.StorageExceeded(0, 5*GB) {
		t.Error("MaxStorageBytes 0 must not block the first upload")
	}
	if unlimited.MeetParticipantsExceeded(0) {
		t.Error("MaxMeetParticipants 0 must not block the first non-host participant")
	}
}

func TestPositiveLimitsAreEnforced(t *testing.T) {
	p := Plan{MaxMailboxes: 3, MaxStorageBytes: 10 * GB, MaxMeetParticipants: 5}
	if p.MailboxesExceeded(2) {
		t.Error("2 of 3 mailboxes must still be allowed")
	}
	if !p.MailboxesExceeded(3) {
		t.Error("3 of 3 mailboxes must be exceeded")
	}
	if p.StorageExceeded(9*GB, 1*GB) {
		t.Error("exactly at the storage cap must still be allowed")
	}
	if !p.StorageExceeded(9*GB, 2*GB) {
		t.Error("over the storage cap must be exceeded")
	}
	if !p.MeetParticipantsExceeded(5) {
		t.Error("5 of 5 participants must be exceeded")
	}
}
