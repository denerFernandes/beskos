package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
	"mailin/backend/internal/storage"
)

// Metering do manager (M3, docs/manager.md §3.5): rollup diário idempotente + retenção de
// gravações conforme o plano do tenant.

// scanDailyUsage congela o uso por tenant para HOJE e ONTEM (upsert). Rodar de hora em hora cobre
// mudanças ao longo do dia; refazer ontem no primeiro tick depois da meia-noite fecha o dia certo.
func scanDailyUsage(ctx context.Context, q *db.Queries) {
	now := time.Now()
	for _, d := range []time.Time{now, now.AddDate(0, 0, -1)} {
		day := pgtype.Date{Time: d, Valid: true}
		if err := q.RollupTenantDailyUsage(ctx, day); err != nil {
			log.Printf("metering: rollup %s: %v", d.Format("2006-01-02"), err)
		}
	}
}

// scanRecordingRetention apaga gravações além da retenção do plano: primeiro o objeto no
// storage, depois a linha (nesta ordem — se o storage falhar, a linha fica e tentamos de novo).
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

// meteringStorage monta o cliente MinIO para a retenção (mesmas envs do api). nil = sem storage
// (a retenção só apaga linhas cujo blob esteja vazio).
func meteringStorage() *storage.Service {
	ep := os.Getenv("MINIO_ENDPOINT")
	if ep == "" {
		return nil
	}
	bucket := os.Getenv("MINIO_BUCKET")
	if bucket == "" {
		bucket = "mailin"
	}
	st, err := storage.New(ep, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"),
		bucket, os.Getenv("MINIO_REGION"), os.Getenv("MINIO_SSL") == "true")
	if err != nil {
		log.Printf("metering: storage disabled: %v", err)
		return nil
	}
	return st
}

// startMetering liga o rollup + retenção num ticker horário (primeira passada já no arranque).
func startMetering(q *db.Queries) {
	st := meteringStorage()
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
