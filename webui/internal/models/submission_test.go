package models

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// DB-backed CreateSubmission regression guards (skipped when TEST_DATABASE_URL
// is unset — same infra as user_create_db_test.go via setupAuthTestDB).
//
// The retry-duplicate bug class: CreateSubmission only updated the open
// (un-submitted) placeholder row, so when a client retried a submit whose first
// attempt HAD already been persisted (the response was lost, or the user tapped
// twice), a brand-new completed row was inserted — the same student showed up
// twice in the admin monitoring table with duplicate scores. These tests pin
// the idempotent-retry contract.
// ---------------------------------------------------------------------------

// setupSubmissionTestExam inserts a minimal active/started exam for
// CreateSubmission to reference (submissions.exam_id has an FK to exams).
func setupSubmissionTestExam(t *testing.T, pool *pgxpool.Pool, ownerID int) int {
	t.Helper()
	ctx := context.Background()
	token := fmt.Sprintf("M%07d", time.Now().UnixNano()%10000000)
	var id int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at)
		VALUES ('Ujian Models', 'm.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP)
		RETURNING id`, token, ownerID).Scan(&id); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return id
}

// TestCreateSubmissionRetryIsIdempotent locks in the sync-path dedup
// guarantee: a second CreateSubmission with the same (exam, device,
// exam_number) updates the already-submitted row instead of inserting a
// duplicate. The score and answers of the retried attempt must win.
func TestCreateSubmissionRetryIsIdempotent(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	owner, err := CreateUser(ctx, pool, &AdminUser{
		Username: "sub-retry-guru", Name: "Guru Submission",
		PasswordHash: "pass", Status: UserStatusActive,
		Role: SerializeRoles([]string{RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	examID := setupSubmissionTestExam(t, pool, owner.ID)

	answers := `{"1":"jakarta"}`
	score := 100.0
	startTime := "2026-08-09 07:00:00"
	first, err := CreateSubmission(ctx, pool, &Submission{
		ExamID:       examID,
		StudentName:  "Siswa Retry",
		ExamNumber:   "N9",
		StudentClass: "XII-A",
		AnswersJSON:  &answers,
		Score:        &score,
		StartTime:    &startTime,
		MACAddress:   "DEVICE:model-retry",
	})
	if err != nil {
		t.Fatalf("first CreateSubmission: %v", err)
	}

	// Retry with updated answers (as if the first response never arrived).
	answers2 := `{"1":"bogor"}`
	score2 := 0.0
	retry, err := CreateSubmission(ctx, pool, &Submission{
		ExamID:       examID,
		StudentName:  "Siswa Retry",
		ExamNumber:   "N9",
		StudentClass: "XII-A",
		AnswersJSON:  &answers2,
		Score:        &score2,
		StartTime:    &startTime,
		MACAddress:   "DEVICE:model-retry",
	})
	if err != nil {
		t.Fatalf("retry CreateSubmission: %v", err)
	}
	if retry.ID != first.ID {
		t.Errorf("retry returned id %d, want %d (retry must update, not insert)", retry.ID, first.ID)
	}

	var total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:model-retry'`,
		examID).Scan(&total); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if total != 1 {
		t.Errorf("rows for device = %d, want 1 (retry must not create a duplicate)", total)
	}

	var gotAnswers *string
	var gotScore *float64
	if err := pool.QueryRow(ctx,
		`SELECT answers_json, score FROM submissions WHERE id=$1`, first.ID).Scan(&gotAnswers, &gotScore); err != nil {
		t.Fatalf("read persisted row: %v", err)
	}
	if gotAnswers == nil || *gotAnswers != answers2 {
		t.Errorf("answers = %v, want retried answers %q", gotAnswers, answers2)
	}
	if gotScore == nil || *gotScore != score2 {
		t.Errorf("score = %v, want %v", gotScore, score2)
	}
}
