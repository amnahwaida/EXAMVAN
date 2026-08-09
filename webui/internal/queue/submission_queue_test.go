package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
)

// setupQueueTestPool connects to the package's isolated schema, skipping when
// TEST_DATABASE_URL is unset.
func setupQueueTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return database.NewPackageTestPool(t, "queue")
}

// insertQueueOwner creates a minimal admin_users row (created_by FK).
func insertQueueOwner(t *testing.T, pool *pgxpool.Pool, username string) int {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO admin_users (username, name, password_hash, status, role)
		VALUES ($1, $1, 'x', 'active', '["guru"]')`, username)
	if err != nil {
		t.Fatalf("insert owner %s: %v", username, err)
	}
	var id int
	if err := pool.QueryRow(context.Background(), `SELECT id FROM admin_users WHERE username=$1`, username).Scan(&id); err != nil {
		t.Fatalf("get owner %s: %v", username, err)
	}
	return id
}

// insertQueueTestExam inserts a minimal active/started exam for submissions to reference.
func insertQueueTestExam(t *testing.T, pool *pgxpool.Pool, createdBy int) int {
	t.Helper()
	token := fmt.Sprintf("Q%07d", time.Now().UnixNano()%10000000)
	var id int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at)
		VALUES ('Ujian Queue', 'queue.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP)
		RETURNING id`, token, createdBy).Scan(&id); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return id
}

// TestUpsertSubmissionRowTwoStudentsShareDevice is the fix-#4 regression: with
// one device (mac_address = "DEVICE:abc") and two distinct exam numbers,
// upserting a submission for each student must create TWO rows — the second
// must not overwrite the first student's answers.
func TestUpsertSubmissionRowTwoStudentsShareDevice(t *testing.T) {
	pool := setupQueueTestPool(t)
	ctx := context.Background()
	ownerID := insertQueueOwner(t, pool, "q-guru")
	examID := insertQueueTestExam(t, pool, ownerID)

	// Student A joins (open placeholder row, exam_number N1).
	sp1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	if _, err := upsertSubmissionRow(ctx, sp1, &SubmissionJob{
		StudentName: "Siswa A", ExamNumber: "N1", ExamID: examID,
		MACAddress: "DEVICE:abc", StartTime: "2026-08-09 07:00:00",
	}, nil); err != nil {
		t.Fatalf("upsert student A placeholder: %v", err)
	}
	_ = sp1.Commit(ctx)

	// Student B joins the same device, different exam_number — must NOT merge
	// into A's placeholder.
	sp2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	if _, err := upsertSubmissionRow(ctx, sp2, &SubmissionJob{
		StudentName: "Siswa B", ExamNumber: "N2", ExamID: examID,
		MACAddress: "DEVICE:abc", StartTime: "2026-08-09 07:05:00",
	}, nil); err != nil {
		t.Fatalf("upsert student B: %v", err)
	}
	_ = sp2.Commit(ctx)

	// Student A submits: must update ONLY A's row (exam_number N1).
	scoreA := 80.0
	sp3, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx3: %v", err)
	}
	if _, err := upsertSubmissionRow(ctx, sp3, &SubmissionJob{
		StudentName: "Siswa A", ExamNumber: "N1", ExamID: examID,
		MACAddress: "DEVICE:abc", Answers: map[string]interface{}{"1": "a"},
		StartTime: "2026-08-09 07:00:00",
	}, &scoreA); err != nil {
		t.Fatalf("upsert student A submit: %v", err)
	}
	_ = sp3.Commit(ctx)

	// Assert: two rows total, exactly one submitted (A), and B's row has no score.
	var total, submitted int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN answers_json IS NOT NULL AND answers_json != '' THEN 1 ELSE 0 END), 0)
		FROM submissions WHERE exam_id=$1`, examID).Scan(&total, &submitted); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if total != 2 {
		t.Errorf("rows = %d, want 2 (students sharing a device must not merge)", total)
	}
	if submitted != 1 {
		t.Errorf("submitted = %d, want 1 (only student A submitted)", submitted)
	}

	var aSubmitted int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM submissions
		WHERE exam_id=$1 AND exam_number='N1' AND answers_json IS NOT NULL AND answers_json != ''`, examID).Scan(&aSubmitted); err != nil {
		t.Fatalf("count A submitted: %v", err)
	}
	if aSubmitted != 1 {
		t.Errorf("A submitted rows = %d, want 1", aSubmitted)
	}

	var bAnswers *string
	if err := pool.QueryRow(ctx, `SELECT answers_json FROM submissions WHERE exam_id=$1 AND exam_number='N2'`, examID).Scan(&bAnswers); err != nil {
		t.Fatalf("get B row: %v", err)
	}
	if bAnswers != nil && *bAnswers != "" {
		t.Errorf("student B must keep an empty (open) row, got answers %q", *bAnswers)
	}
}
