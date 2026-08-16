package queue

import (
	"context"
	"fmt"
	"sync"
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

// TestUpsertSubmissionRowRetryIsIdempotent locks in the retry/dedup guarantee:
// when the same job (same exam + device + exam_number) is upserted a second
// time — e.g. the client retried because it never saw the first response, or
// the queue worker re-processed a re-enqueued job — the EXISTING submitted row
// is updated in place instead of inserting a duplicate row. The old
// implementation only matched the open (un-submitted) placeholder row, so a
// retry after a persisted submit silently created a second submission row that
// showed the same student twice in the admin monitoring table with duplicate
// scores.
func TestUpsertSubmissionRowRetryIsIdempotent(t *testing.T) {
	pool := setupQueueTestPool(t)
	ctx := context.Background()
	ownerID := insertQueueOwner(t, pool, "q-retry-guru")
	examID := insertQueueTestExam(t, pool, ownerID)

	job := &SubmissionJob{
		StudentName: "Siswa Retry", ExamNumber: "R1", ExamID: examID,
		MACAddress: "DEVICE:retry1", StartTime: "2026-08-09 07:00:00",
		Answers: map[string]interface{}{"1": "a", "2": "b"},
	}
	score := 85.0

	sp1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	firstID, err := upsertSubmissionRow(ctx, sp1, job, &score)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	_ = sp1.Commit(ctx)

	// Simulate the client retrying because the response was lost: same job,
	// same device + exam_number, arguably different (latest) answers.
	job.Answers = map[string]interface{}{"1": "a", "2": "c"}
	newScore := 90.0
	sp2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	secondID, err := upsertSubmissionRow(ctx, sp2, job, &newScore)
	if err != nil {
		t.Fatalf("retry upsert: %v", err)
	}
	_ = sp2.Commit(ctx)

	if secondID != firstID {
		t.Errorf("retry upsert returned id %d, want same row %d (retry must not insert)", secondID, firstID)
	}

	var total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:retry1'`,
		examID).Scan(&total); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if total != 1 {
		t.Errorf("rows for device = %d, want 1 (retry must not create a duplicate)", total)
	}

	// The retried job's answers and score must be the ones persisted.
	var gotAnswers *string
	var gotScore *float64
	if err := pool.QueryRow(ctx,
		`SELECT answers_json, score FROM submissions WHERE id=$1`, firstID).Scan(&gotAnswers, &gotScore); err != nil {
		t.Fatalf("read persisted row: %v", err)
	}
	if gotAnswers == nil || *gotAnswers != `{"1":"a","2":"c"}` {
		t.Errorf("answers = %v, want retried answers", gotAnswers)
	}
	if gotScore == nil || *gotScore != newScore {
		t.Errorf("score = %v, want %v", gotScore, newScore)
	}
}

// TestUpsertSubmissionRowConcurrentSameDeviceNoDuplicates locks in the
// cross-path idempotency guarantee: N CONCURRENT upserts of the SAME
// (exam, device, exam_number) with no pre-existing row — e.g. the queue
// worker's async job racing a sync-path recovery resubmit, or two worker
// instances — must end with exactly ONE row. The per-(exam, device) advisory
// lock serialises the UPDATE-then-INSERT check-then-act; without it both
// writers could observe "no row yet" and both INSERT, duplicating the student
// in the monitoring table / hasil page (the sync-path analogue is covered by
// TestSubmitSyncConcurrentSameDeviceNoDuplicates).
func TestUpsertSubmissionRowConcurrentSameDeviceNoDuplicates(t *testing.T) {
	pool := setupQueueTestPool(t)
	ctx := context.Background()
	ownerID := insertQueueOwner(t, pool, "q-dup-guru")
	examID := insertQueueTestExam(t, pool, ownerID)

	const n = 12
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // barrier: all upserts leave together so the first-attempt
			// UPDATEs overlap instead of finishing before later ones start
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Errorf("begin: %v", err)
				return
			}
			if _, err := upsertSubmissionRow(ctx, tx, &SubmissionJob{
				StudentName: "Siswa Dup", ExamNumber: "D1", ExamID: examID,
				MACAddress: "DEVICE:dup1", StartTime: "2026-08-09 07:00:00",
				Answers: map[string]interface{}{"1": "a"},
			}, nil); err != nil {
				_ = tx.Rollback(ctx)
				t.Errorf("upsert: %v", err)
				return
			}
			if err := tx.Commit(ctx); err != nil {
				t.Errorf("commit: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	var total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:dup1'`,
		examID).Scan(&total); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if total != 1 {
		t.Errorf("rows after concurrent upserts = %d, want 1 (must not duplicate)", total)
	}
}

// TestFlushBatchRevokesApprovalOnlyAfterCommit locks in the durable-first
// approval lifecycle of the async path: the approval row of a submitting device
// must NOT be revoked until the submission batch has actually committed to
// PostgreSQL. If the worker process dies before commit (or the commit fails and
// the job is re-enqueued), the approval survives so the device can retry its
// submit — otherwise a student whose job never persisted would be stranded with
// a cancelled approval, a cleared answer sheet, and no way back in.
func TestFlushBatchRevokesApprovalAfterCommit(t *testing.T) {
	pool := setupQueueTestPool(t)
	ctx := context.Background()
	ownerID := insertQueueOwner(t, pool, "q-revoke-guru")
	examID := insertQueueTestExam(t, pool, ownerID)

	// Approved device sitting in the join queue.
	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, status)
		VALUES ($1, 'DEVICE:revoke1', 'Siswa Revoke', 'V1', 'approved')`, examID); err != nil {
		t.Fatalf("insert approval: %v", err)
	}
	var approvalsBefore int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id=$1 AND mac_address='DEVICE:revoke1'`,
		examID).Scan(&approvalsBefore); err != nil {
		t.Fatalf("count approvals before: %v", err)
	}
	if approvalsBefore != 1 {
		t.Fatalf("approvals before = %d, want 1 (fixture mis-set)", approvalsBefore)
	}

	score := 75.0
	w := &Worker{rdb: nil, pool: pool}
	w.flushBatch(ctx, []SubmissionResult{{
		Job: SubmissionJob{
			StudentName: "Siswa Revoke", ExamNumber: "V1", ExamID: examID,
			MACAddress: "DEVICE:revoke1", StartTime: "2026-08-09 07:00:00",
			Answers: map[string]interface{}{"1": "a"},
		},
		Score: &score,
	}})

	var approvalsAfter int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id=$1 AND mac_address='DEVICE:revoke1'`,
		examID).Scan(&approvalsAfter); err != nil {
		t.Fatalf("count approvals after: %v", err)
	}
	if approvalsAfter != 0 {
		t.Errorf("approvals after flush = %d, want 0 (revoked only after durable commit)", approvalsAfter)
	}

	// And the submission itself must be durable.
	var total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:revoke1'`,
		examID).Scan(&total); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if total != 1 {
		t.Errorf("submission rows = %d, want 1 (flush must persist the job)", total)
	}
}

// TestFlushBatchKeepsApprovalWhenRowFails locks in the failure side of the
// same contract: when a job's row cannot be persisted (here: a job whose exam
// id does not exist violates the submissions FK), the failed job is re-enqueued
// for retry and its approval row is left untouched. Approval is only revoked on
// the success path after the batch commit — so a transient DB failure never
// cancels a device's re-entry entitlement.
func TestFlushBatchKeepsApprovalWhenRowFails(t *testing.T) {
	pool := setupQueueTestPool(t)
	ctx := context.Background()
	ownerID := insertQueueOwner(t, pool, "q-keep-guru")
	examID := insertQueueTestExam(t, pool, ownerID)

	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, status)
		VALUES ($1, 'DEVICE:keep1', 'Siswa Keep', 'K1', 'approved')`, examID); err != nil {
		t.Fatalf("insert approval: %v", err)
	}

	// The job references a non-existent exam → the per-row savepoint fails, the
	// row is re-enqueued for retry (rdb nil ⇒ re-enqueue is a no-op), and the
	// approval must survive.
	w := &Worker{rdb: nil, pool: pool}
	w.flushBatch(ctx, []SubmissionResult{{
		Job: SubmissionJob{
			StudentName: "Siswa Keep", ExamNumber: "K1", ExamID: 999999,
			MACAddress: "DEVICE:keep1", StartTime: "2026-08-09 07:00:00",
			Answers: map[string]interface{}{"1": "a"},
		},
		Score: nil,
	}})

	var approvals int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id=$1 AND mac_address='DEVICE:keep1'`,
		examID).Scan(&approvals); err != nil {
		t.Fatalf("count approvals: %v", err)
	}
	if approvals != 1 {
		t.Errorf("approvals after failed row = %d, want 1 (never revoked before durable commit)", approvals)
	}

	var total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:keep1'`,
		examID).Scan(&total); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if total != 0 {
		t.Errorf("submission rows = %d, want 0 (failing job must not persist)", total)
	}
}
