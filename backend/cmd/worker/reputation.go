package main

import (
	"context"
	"log"
	"time"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/ses"
)

// Reputation sentinel: suspends one customer's sending before AWS suspends everyone's.
//
// THE PROBLEM. Every customer's identities live in the same AWS account. A customer with a
// purchased list drives up the ACCOUNT's bounce rate, and AWS throttles or suspends the whole
// account — everyone's mail stops because of one. Until now, faced with that, the options were to
// do nothing or suspend the account ourselves.
//
// With a per-domain configuration set (see internal/ses), SES measures each customer separately
// and lets us turn off sending for one without touching the others. This sentinel is what
// connects the two.
//
// THRESHOLDS: AWS's own — 5% bounces and 0.1% complaints are the marks past which it starts
// looking at the account. Suspending before that is precisely about stopping it from doing so.
//
// SUSPENDS BUT DOES NOT REINSTATE. Reactivating a customer who was burning reputation is a
// decision with consequences for everyone else, and must come from a person who looked at the
// case. A sentinel that reinstates on its own would end up switching the same customer on and
// off in a loop.
//
// THIS SENTINEL IS NOT ENOUGH, and it is worth saying so: it is our own code, so it can be down
// exactly when it is needed. The second line of defense is AWS's automatic pause, which works
// without us — but it requires the X-SES-TENANT header on every message, which means touching the
// mail path.

const (
	reputationInterval = time.Hour
	// Analysis window. Twenty-four hours smooths out the noise of a small send: with ten
	// messages, one bounce gives 10% and would suspend a customer for nothing.
	reputationWindow = 24 * time.Hour
	// Minimum sends for the rate to mean anything.
	minSends = 100

	bounceLimit    = 0.05  // 5%
	complaintLimit = 0.001 // 0,1%

	// Three blind passes is three hours. Long enough that a one-off AWS hiccup stays quiet, short
	// enough that a broken watchdog is caught the same working day instead of the same week.
	blindPassesBeforeAlarm = 3
)

func startReputation(q *db.Queries, relay *ses.Client) {
	if relay == nil {
		log.Printf("reputation watchdog disabled (SES off)")
		return
	}
	go func() {
		time.Sleep(2 * time.Minute) // lets startup breathe
		// Consecutive passes where NOT ONE domain could be measured. Law 2.8: a single failing
		// domain is ordinary noise, but every read failing, pass after pass, means the watchdog is
		// blind -- and a blind watchdog looks exactly like a happy one.
		var blind int
		for {
			measured, failed := checkReputation(context.Background(), q, relay)
			was := blind
			blind = blindAfter(blind, measured, failed)
			switch {
			case blind == 0 && was >= blindPassesBeforeAlarm:
				log.Printf("reputation: RECOVERED, measuring again after %d blind passes", was)
			case blind == blindPassesBeforeAlarm:
				log.Printf("reputation: BLIND for %d passes — not one domain could be measured. "+
					"Nobody is being watched; a customer can burn the account's reputation unnoticed. "+
					"Check the SES credentials and the metrics window.", blind)
			case blind > blindPassesBeforeAlarm && blind%6 == 0:
				log.Printf("reputation: still blind after %d passes", blind)
			}
			time.Sleep(reputationInterval)
		}
	}()
	log.Printf("reputation watchdog started (every %s, bounces>%.0f%% or complaints>%.1f%% suspend the customer)",
		reputationInterval, bounceLimit*100, complaintLimit*100)
}

// blindAfter folds one pass into the blind-pass counter.
//
// Blind means NOT ONE domain could be read while at least one read was attempted. A single failing
// domain is ordinary noise; measuring nothing at all, pass after pass, means the watchdog is down
// and looks exactly like a watchdog with nothing to report. Zero attempts (no domains, SES off) is
// not blindness -- there was nothing to see.
func blindAfter(blind, measured, failed int) int {
	if measured > 0 || failed == 0 {
		return 0
	}
	return blind + 1
}

// checkReputation runs one pass and reports how many domains it could read and how many it could
// not. The caller needs both to tell "one flaky domain" from "the whole thing is broken" (law 2.8).
func checkReputation(ctx context.Context, q *db.Queries, relay *ses.Client) (measured, failed int) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	domains, err := q.ListAllDomains(ctx)
	if err != nil {
		log.Printf("reputation: list domains: %v", err)
		// Not blindness: without the list there was nothing to measure, so it does not count as a
		// failed read. Saying otherwise would raise the alarm for an empty database.
		return 0, 0
	}

	// ALIGNED TO UTC MIDNIGHT, and not rolling from now. SES only returns the aggregated daily
	// data when the interval runs midnight to midnight; with partial-day marks it answers HTTP
	// 400 and returns no number at all. This was a bug that only showed up in production: the
	// sentinel failed on EVERY pass since it was installed, and nobody was being measured.
	//
	// The price is that the window ends at the last midnight, i.e. up to 24 h of lag behind what
	// is happening right now. That is acceptable because this sentinel was never the real-time
	// defense — that is SES's per-tenant automatic pause, which acts without us. This one is the
	// net that catches the trend.
	until := time.Now().UTC().Truncate(24 * time.Hour)
	from := until.Add(-reputationWindow)

	for _, d := range domains {
		configSet := ses.TenantName(d.Domain)
		rep, err := relay.ConfigSetReputation(ctx, configSet, from, until)
		if err != nil {
			// Does not suspend for failing to measure: a read error is not proof of anything, and
			// suspending a customer by mistake is the same damage for the wrong reason.
			log.Printf("reputation: %s: %v", d.Domain, err)
			failed++
			continue
		}
		measured++
		if rep.Sent < minSends {
			continue
		}

		bounceRate, complaintRate := rep.BounceRate(), rep.ComplaintRate()
		if bounceRate <= bounceLimit && complaintRate <= complaintLimit {
			continue
		}

		log.Printf("reputation: SUSPENDING %s — %.0f sent, %.2f%% bounced (limit %.0f%%), %.3f%% complained (limit %.1f%%)",
			d.Domain, rep.Sent, bounceRate*100, bounceLimit*100, complaintRate*100, complaintLimit*100)

		if err := relay.SetSendingEnabled(ctx, configSet, false); err != nil {
			log.Printf("reputation: %s: SUSPENSION FAILED, the account is still exposed: %v", d.Domain, err)
			continue
		}
		log.Printf("reputation: %s suspended; reinstating is a manual decision (ses-tenants has no --reactivate on purpose)",
			d.Domain)
	}
	return measured, failed
}
