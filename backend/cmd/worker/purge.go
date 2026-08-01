package main

// The only thing in the system that actually destroys a mailbox.
//
// Everything upstream of this is reversible: suspending removes credentials, deleting parks the
// address and starts a clock. This is where the clock runs out. It runs in the worker rather than
// in a request because nobody should be waiting on it, and because a destruction that happens on a
// user's click is one that happens on a double click too.
//
// IT DESTROYS ONLY WHAT IT RE-READS. The list is fetched, and each row is checked again at the
// moment of deletion (the DELETE carries `status = 'deleted'`): a mailbox restored in the seconds
// between the two is left alone. Anything else and a restore would race a purge, once a month,
// silently, on somebody's correspondence.
//
// LAW 2.8: a purge that fails every time looks exactly like a purge with nothing to do. Both are
// silent. So failures are counted and, past a threshold, said out loud -- and saying it again when
// it recovers, because an alarm nobody sees stop is an alarm nobody trusts.

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
	"mailin/backend/internal/httpapi"
	"mailin/backend/internal/stalwart"
)

const (
	purgeInterval = 10 * time.Minute
	// How many mailboxes to destroy per pass. A tenant closing down could queue hundreds; doing them
	// in batches keeps one bad row from holding up the rest and keeps the log readable.
	purgeBatch = 20
	// Consecutive failed passes before this stops being bad luck.
	purgeFailuresBeforeAlarm = 3
)

// startPurgeWorker forgets mailboxes whose recovery window has closed.
func startPurgeWorker(q *db.Queries, sw *stalwart.Client) {
	go func() {
		ticker := time.NewTicker(purgeInterval)
		defer ticker.Stop()
		consecutive := 0
		for range ticker.C {
			if err := purgePass(context.Background(), q, sw); err != nil {
				consecutive++
				if consecutive == purgeFailuresBeforeAlarm {
					log.Printf("purge: FAILING every pass (%d in a row): %v — mailboxes past their "+
						"recovery window are NOT being deleted, which is a retention promise we are breaking",
						consecutive, err)
				} else if consecutive < purgeFailuresBeforeAlarm {
					log.Printf("purge: pass failed (%d): %v", consecutive, err)
				}
				continue
			}
			if consecutive >= purgeFailuresBeforeAlarm {
				log.Printf("purge: working again after %d failed passes", consecutive)
			}
			consecutive = 0
		}
	}()
	log.Printf("mailbox purge worker started (recovery window %s, every %s)", httpapi.RecoveryWindow, purgeInterval)
}

func purgePass(ctx context.Context, q *db.Queries, sw *stalwart.Client) error {
	cutoff := time.Now().Add(-httpapi.RecoveryWindow)
	rows, err := q.MailUsersToPurge(ctx, db.MailUsersToPurgeParams{
		DeletedAt: pgtype.Timestamptz{Time: cutoff, Valid: true}, Limit: purgeBatch,
	})
	if err != nil {
		return err
	}
	for _, m := range rows {
		// Stalwart first. If our row went first and this failed, the mailbox would exist forever with
		// nothing left pointing at it -- invisible to the panel, invisible to the purge, and still
		// holding somebody's mail.
		if sw != nil && m.StalwartAccountID != nil {
			if err := sw.DeleteAccount(ctx, *m.StalwartAccountID); err != nil {
				log.Printf("purge: %s: deleting the account failed, leaving the row for the next pass: %v", m.Email, err)
				continue
			}
		}
		if err := q.PurgeMailUser(ctx, m.ID); err != nil {
			log.Printf("purge: %s: the account is gone but the row remains: %v", m.Email, err)
			continue
		}
		log.Printf("purge: %s deleted permanently (requested %s)", m.Email, m.DeletedAt.Time.Format(time.RFC3339))
	}
	return nil
}
