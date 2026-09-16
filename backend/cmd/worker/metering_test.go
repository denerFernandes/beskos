package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

type meteringDB struct {
	db.DBTX
	exec     func(string, ...any) error
	rows     []db.ExpiredRecordingsRow
	queryErr error
}

func (m meteringDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("DELETE 1"), m.exec(sql, args...)
}

func (m meteringDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &meteringRows{rows: m.rows}, m.queryErr
}

type meteringRows struct {
	pgx.Rows
	rows []db.ExpiredRecordingsRow
	next int
}

func (r *meteringRows) Next() bool { r.next++; return r.next <= len(r.rows) }
func (r *meteringRows) Close()     {}
func (r *meteringRows) Err() error { return nil }
func (r *meteringRows) Scan(dest ...any) error {
	row := r.rows[r.next-1]
	*dest[0].(*uuid.UUID) = row.ID
	*dest[1].(*uuid.UUID) = row.TenantID
	*dest[2].(*string) = row.BlobKey
	return nil
}

func TestDailyUsageAttemptsBothDaysAndPreservesFailures(t *testing.T) {
	failure := errors.New("database unavailable")
	var dates []string
	q := db.New(meteringDB{exec: func(_ string, args ...any) error {
		dates = append(dates, args[0].(pgtype.Date).Time.Format("2006-01-02"))
		return failure
	}})
	err := scanDailyUsage(context.Background(), q, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, failure) || !reflect.DeepEqual(dates, []string{"2024-03-01", "2024-02-29"}) {
		t.Fatalf("dates=%v error=%v", dates, err)
	}
	for _, date := range dates {
		if !strings.Contains(err.Error(), date) {
			t.Errorf("missing failed date %s: %v", date, err)
		}
	}
}

func TestRetentionPreservesRowsUntilBlobRemovalSucceeds(t *testing.T) {
	failure := errors.New("storage unavailable")
	for _, name := range []string{"storage absent", "storage failure", "success", "empty blob", "database failure"} {
		t.Run(name, func(t *testing.T) {
			row := db.ExpiredRecordingsRow{ID: uuid.New(), TenantID: uuid.New(), BlobKey: "recording.mp4"}
			var operations []string
			q := db.New(meteringDB{rows: []db.ExpiredRecordingsRow{row}, exec: func(sql string, args ...any) error {
				operations = append(operations, "database")
				if !strings.Contains(sql, "tenant_id = $2") || !reflect.DeepEqual(args, []any{row.ID, row.TenantID}) {
					t.Fatalf("unscoped delete: %s %v", sql, args)
				}
				if name == "database failure" {
					return failure
				}
				return nil
			}})
			var remove func(context.Context, string) error
			if name != "storage absent" {
				remove = func(_ context.Context, key string) error {
					operations = append(operations, "blob")
					if key != row.BlobKey {
						t.Fatalf("unexpected key %s", key)
					}
					if name == "storage failure" {
						return failure
					}
					return nil
				}
			}
			if name == "empty blob" {
				row.BlobKey = ""
			}
			err := removeExpiredRecording(context.Background(), q, row, remove)
			want := []string{"blob", "database"}
			wantError := false
			switch name {
			case "storage absent":
				want = nil
				wantError = true
			case "storage failure":
				want = []string{"blob"}
				wantError = true
			case "empty blob":
				want = []string{"database"}
			case "database failure":
				wantError = true
			}
			if (err != nil) != wantError || !reflect.DeepEqual(operations, want) {
				t.Fatalf("operations=%v want=%v error=%v", operations, want, err)
			}
		})
	}
}

func TestRetentionContinuesAfterFailedBlobAndReportsActualCount(t *testing.T) {
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(old) })
	rows := []db.ExpiredRecordingsRow{
		{ID: uuid.New(), TenantID: uuid.New(), BlobKey: "failed.mp4"},
		{ID: uuid.New(), TenantID: uuid.New(), BlobKey: "removed.mp4"},
	}
	var deleted []any
	q := db.New(meteringDB{rows: rows, exec: func(_ string, args ...any) error {
		deleted = append(deleted, args[0])
		return nil
	}})
	failure := errors.New("storage failure")
	err := scanRecordingRetention(context.Background(), q, func(_ context.Context, key string) error {
		if key == "failed.mp4" {
			return failure
		}
		return nil
	})
	if !errors.Is(err, failure) || !reflect.DeepEqual(deleted, []any{rows[1].ID}) {
		t.Fatalf("deleted=%v error=%v", deleted, err)
	}
	if !strings.Contains(output.String(), "removed 1 recording(s)") {
		t.Fatalf("incorrect deletion count: %s", output.String())
	}
}

func TestRetentionReportsQueryFailure(t *testing.T) {
	failure := errors.New("query failed")
	err := scanRecordingRetention(context.Background(), db.New(meteringDB{queryErr: failure}), nil)
	if !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
}

func TestMeteringEscalatesAndAnnouncesRecovery(t *testing.T) {
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(old) })
	var health meteringHealth
	for i := 0; i < failuresBeforeAlarm-1; i++ {
		health.report("retention", errors.New("failed"))
	}
	if strings.Contains(output.String(), "UNHEALTHY") {
		t.Fatal("alarmed before threshold")
	}
	health.report("retention", errors.New("failed"))
	if !strings.Contains(output.String(), "UNHEALTHY") {
		t.Fatal("repeated failures did not escalate")
	}
	health.report("retention", nil)
	if health.consecutive != 0 || !strings.Contains(output.String(), "RECOVERED") {
		t.Fatalf("recovery missing: %s", output.String())
	}
	output.Reset()
	health.report("retention", nil)
	if output.Len() != 0 {
		t.Fatal("recovery was announced twice")
	}
}
