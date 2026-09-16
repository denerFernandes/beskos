package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// Manager metering: idempotent daily rollup, plus recording retention per the tenant's plan.

// scanDailyUsage freezes per-tenant usage for TODAY and YESTERDAY (upsert). Running hourly covers
// changes throughout the day; redoing yesterday on the first tick after midnight closes the right day.
func scanDailyUsage(ctx context.Context, q *db.Queries) {
	now := time.Now()
	for _, d := range []time.Time{now, now.AddDate(0, 0, -1)} {
		day := pgtype.Date{Time: d, Valid: true}
		if err := q.RollupTenantDailyUsage(ctx, day); err != nil {
			log.Printf("metering: rollup %s: %v", d.Format("2006-01-02"), err)
		}
	}
}

// scanRecordingRetention deletes recordings past the plan's retention: first the storage object,
// then the row (in this order — if storage fails, the row stays and we try again).
func scanRecordingRetention(ctx context.Context, q *db.Queries, st *storage.Service) {
	rows, err := q.ExpiredRecordings(ctx)
	if err != nil {
		log.Printf("metering: expired recordings: %v", err)
		return
	}
	for _, r := range rows {
		if r.BlobKey != "" && st != nil {
			if err := st.Remove(ctx, r.BlobKey); err != nil {
				log.Printf("metering: remove recording blob %s: %v", r.BlobKey, err)
				continue
			}
		}
		if err := q.DeleteRecordingRow(ctx, r.ID); err != nil {
			log.Printf("metering: delete recording row %s: %v", r.ID, err)
		}
	}
	if len(rows) > 0 {
		log.Printf("metering: recording retention removed %d recording(s)", len(rows))
	}
}

// meteringStorage builds the MinIO client for retention (same envs as the api). nil = no storage
// (retention only deletes rows whose blob is empty).
func meteringStorage(q *db.Queries) *storage.Service {
	ep := os.Getenv("MINIO_ENDPOINT")
	if ep == "" {
		return nil
	}
	bucket := os.Getenv("MINIO_BUCKET")
	if bucket == "" {
		bucket = "beskos"
	}
	st, err := storage.New(ep, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"),
		bucket, os.Getenv("MINIO_REGION"), os.Getenv("MINIO_SSL") == "true")
	if err != nil {
		log.Printf("metering: storage disabled: %v", err)
		return nil
	}
	// Recording assembly writes through here: without this, the final MP4 would go out in
	// plaintext while Drive's files went out encrypted. See the file-encryption design doc §3a.
	if err := st.EnableEncryption(q, os.Getenv("FILE_ENC_KEYS")); err != nil {
		log.Fatalf("metering: storage encryption: %v", err)
	}
	return st
}

// startMetering wires up rollup + retention on an hourly ticker (first pass already at startup).
func startMetering(q *db.Queries) {
	st := meteringStorage(q)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		scanDailyUsage(context.Background(), q)
		scanRecordingRetention(context.Background(), q, st)
		for range t.C {
			scanDailyUsage(context.Background(), q)
			scanRecordingRetention(context.Background(), q, st)
		}
	}()
	log.Printf("metering worker started (hourly rollup + recording retention)")
}
