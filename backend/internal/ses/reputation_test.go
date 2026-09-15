package ses

import "testing"

// The rates decide whether a customer gets suspended. A bug here suspends someone who shouldn't
// be, or lets through someone who should — and in both cases the damage is to the whole account.
func TestRates(t *testing.T) {
	cases := []struct {
		name          string
		r             Reputation
		bounceRate    float64
		complaintRate float64
	}{
		{"normal", Reputation{Sent: 1000, Bounced: 20, Complaints: 1}, 0.02, 0.001},
		{"high bounces", Reputation{Sent: 200, Bounced: 30, Complaints: 0}, 0.15, 0},
		// With no sends there is no rate. Dividing by zero would give NaN, and NaN compared against
		// the threshold is always false — a customer who never sent anything would slip through
		// unnoticed or get suspended, depending on which side of the comparison it lands on. Zero is
		// the correct, explicit answer.
		{"never sent", Reputation{}, 0, 0},
	}
	for _, c := range cases {
		if got := c.r.BounceRate(); got != c.bounceRate {
			t.Errorf("%s: bounce %v, want %v", c.name, got, c.bounceRate)
		}
		if got := c.r.ComplaintRate(); got != c.complaintRate {
			t.Errorf("%s: complaint %v, want %v", c.name, got, c.complaintRate)
		}
	}
}

// Permanent and transient bounces both count: an address that bounces for a full mailbox counts
// toward reputation the same as one that doesn't exist.
func TestBouncesSumBothTypes(t *testing.T) {
	r := Reputation{Sent: 100, Bounced: 3 + 2}
	if got := r.BounceRate(); got != 0.05 {
		t.Fatalf("rate %v, want 0.05", got)
	}
}
