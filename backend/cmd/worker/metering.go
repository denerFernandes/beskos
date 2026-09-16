package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// scanDailyUsage refreshes today and yesterday, including changes around midnight.
func scanDailyUsage(ctx context.Context, q *db.Queries, now time.Time) error {
	var failures []error
	for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
		if err := q.RollupTenantDailyUsage(ctx, pgtype.Date{Time: day, Valid: true}); err != nil {
			failures = append(failures, fmt.Errorf("rollup %s: %w", day.Format("2006-01-02"), err))
		}
	}
	return errors.Join(failures...)
}

// scanRecordingRetention preserves rows until their storage objects have been removed.
func scanRecordingRetention(ctx context.Context, q *db.Queries, remove func(context.Context, string) error) error {
	rows, err := q.ExpiredRecordings(ctx)
	if err != nil {
		return fmt.Errorf("list expired recordings: %w", err)
	}
	var failures []error
	removed := 0
	for _, row := range rows {
		if err := removeExpiredRecording(ctx, q, row, remove); err != nil {
			failures = append(failures, fmt.Errorf("recording %s: %w", row.ID, err))
			continue
		}
		removed++
	}
	if removed > 0 {
		log.Printf("metering: recording retention removed %d recording(s)", removed)
	}
	return errors.Join(failures...)
}

func removeExpiredRecording(ctx context.Context, q *db.Queries, row db.ExpiredRecordingsRow, remove func(context.Context, string) error) error {
	if row.BlobKey != "" {
		if remove == nil {
			return fmt.Errorf("storage unavailable for blob %s; retaining database row", row.BlobKey)
		}
		if err := remove(ctx, row.BlobKey); err != nil {
			return fmt.Errorf("remove blob %s: %w", row.BlobKey, err)
		}
	}
	if err := q.DeleteRecordingRow(ctx, db.DeleteRecordingRowParams{ID: row.ID, TenantID: row.TenantID}); err != nil {
		return fmt.Errorf("delete database row: %w", err)
	}
	return nil
}

type meteringHealth struct {
	consecutive int
}

func (h *meteringHealth) report(job string, err error) {
	if err == nil {
		if h.consecutive > 0 {
			log.Printf("metering: %s RECOVERED after %d failed passes", job, h.consecutive)
		}
		h.consecutive = 0
		return
	}
	h.consecutive++
	if h.consecutive >= failuresBeforeAlarm {
		log.Printf("metering: %s UNHEALTHY after %d consecutive failed passes: %v", job, h.consecutive, err)
		return
	}
	log.Printf("metering: %s failed (pass %d): %v", job, h.consecutive, err)
}

// startMetering runs only in the private edition, with independent failure counters per job.
func startMetering(ctx context.Context, q *db.Queries, st *storage.Service) {
	var remove func(context.Context, string) error
	if st != nil {
		remove = st.Remove
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		var usage, retention meteringHealth
		for {
			if ctx.Err() != nil {
				return
			}
			passCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			usage.report("daily usage", scanDailyUsage(passCtx, q, time.Now()))
			retention.report("recording retention", scanRecordingRetention(passCtx, q, remove))
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	log.Printf("metering worker started (hourly rollup + recording retention)")
}
