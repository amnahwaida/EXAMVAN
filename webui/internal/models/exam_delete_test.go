package models

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
)

// ---------------------------------------------------------------------------
// Orphan-free exam deletion: exam_approvals must go away with their exam
// ---------------------------------------------------------------------------
//
// exam_approvals rows are child rows of exams (the per-device entry gate). The
// schema's ON DELETE CASCADE covers them, but DeleteExam / BulkDeleteExams /
// DeleteUser also delete them explicitly (defense-in-depth + pre-FK-migration
// databases). These tests lock in the explicit behavior: after any exam
// deletion path, no orphan approval row survives — regardless of whether the
// FK cascade exists.

// countApprovals counts approval rows for the given exams (all statuses).
func countApprovals(t *testing.T, pool *pgxpool.Pool, examIDs ...int) int {
	t.Helper()
	if len(examIDs) == 0 {
		return 0
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id = ANY($1)`, examIDs).Scan(&n); err != nil {
		t.Fatalf("count approvals: %v", err)
	}
	return n
}

// countSubmissions counts submission rows for the given exams.
func countSubmissions(t *testing.T, pool *pgxpool.Pool, examIDs ...int) int {
	t.Helper()
	if len(examIDs) == 0 {
		return 0
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM submissions WHERE exam_id = ANY($1)`, examIDs).Scan(&n); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	return n
}

// insertDeleteTestSubmission inserts one submission row for the exam (the
// durable student record that must also disappear with its exam on bulk
// delete — mirroring the DeleteExam explicit-cleanup contract).
func insertDeleteTestSubmission(t *testing.T, pool *pgxpool.Pool, examID int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class)
		VALUES ($1, 'Siswa', '01', 'XII A')`, examID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}
}

// insertDeleteTestExam inserts an exam owned by creatorID plus approval rows
// (one pending, one approved, one rejected) and returns its id.
func insertDeleteTestExam(t *testing.T, pool *pgxpool.Pool, creatorID int, name string) int {
	t.Helper()
	ctx := context.Background()
	token := fmt.Sprintf("DEL%05d", time.Now().UnixNano()%100000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ($1, 'del.pdf', 1024, $2, $2, 'active', 'medium', $3)
		RETURNING id`, name, token, creatorID).Scan(&examID); err != nil {
		t.Fatalf("insert exam %s: %v", name, err)
	}
	for i, status := range []string{"pending", "approved", "rejected"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO exam_approvals (exam_id, mac_address, student_name, status)
			VALUES ($1, $2, 'Siswa', $3)`,
			examID, fmt.Sprintf("AA:BB:CC:DD:EE:%02d", i), status); err != nil {
			t.Fatalf("insert approval %s: %v", status, err)
		}
	}
	return examID
}

// DeleteExam must remove the exam's approval rows (all statuses, including
// rejected — the whole exam is gone, so its decisions die with it).
func TestDeleteExamRemovesApprovals(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	creator, err := CreateUser(ctx, pool, &AdminUser{
		Username: "del_owner", Name: "Del Owner", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	examID := insertDeleteTestExam(t, pool, creator.ID, "Del Single")
	if got := countApprovals(t, pool, examID); got != 3 {
		t.Fatalf("fixture approvals = %d, want 3", got)
	}

	if _, err := DeleteExam(ctx, pool, examID); err != nil {
		t.Fatalf("DeleteExam: %v", err)
	}
	if got := countApprovals(t, pool, examID); got != 0 {
		t.Errorf("approvals after DeleteExam = %d, want 0 (no orphans)", got)
	}
	// The exam itself is gone too.
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM exams WHERE id = $1`, examID).Scan(&n); err != nil {
		t.Fatalf("count exams: %v", err)
	}
	if n != 0 {
		t.Errorf("exam still exists after DeleteExam")
	}
}

// BulkDeleteExams must remove approval rows for every deleted exam, while
// leaving another exam's approvals untouched.
func TestBulkDeleteExamsRemovesApprovals(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	creator, err := CreateUser(ctx, pool, &AdminUser{
		Username: "bulk_del_owner", Name: "Bulk Owner", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	del1 := insertDeleteTestExam(t, pool, creator.ID, "Bulk Del 1")
	del2 := insertDeleteTestExam(t, pool, creator.ID, "Bulk Del 2")
	keep := insertDeleteTestExam(t, pool, creator.ID, "Bulk Keep")
	// Submissions on the deleted exams must go too (bulk path now cleans them
	// explicitly, same contract as DeleteExam).
	insertDeleteTestSubmission(t, pool, del1)
	insertDeleteTestSubmission(t, pool, del2)
	insertDeleteTestSubmission(t, pool, keep)

	if _, err := BulkDeleteExams(ctx, pool, []int{del1, del2}); err != nil {
		t.Fatalf("BulkDeleteExams: %v", err)
	}
	if got := countApprovals(t, pool, del1, del2); got != 0 {
		t.Errorf("approvals after bulk delete = %d, want 0 (no orphans)", got)
	}
	if got := countApprovals(t, pool, keep); got != 3 {
		t.Errorf("kept exam approvals = %d, want 3 (untouched)", got)
	}
	if got := countSubmissions(t, pool, del1, del2); got != 0 {
		t.Errorf("submissions after bulk delete = %d, want 0 (no orphans)", got)
	}
	if got := countSubmissions(t, pool, keep); got != 1 {
		t.Errorf("kept exam submissions = %d, want 1 (untouched)", got)
	}
}

// DeleteUser removes the user's exams — and their approval rows — even though
// DeleteUser itself never touches exam_approvals directly in the exam path.
func TestDeleteUserRemovesExamApprovals(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	creator, err := CreateUser(ctx, pool, &AdminUser{
		Username: "user_del_owner", Name: "User Del Owner", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	examID := insertDeleteTestExam(t, pool, creator.ID, "User Del Exam")
	if got := countApprovals(t, pool, examID); got != 3 {
		t.Fatalf("fixture approvals = %d, want 3", got)
	}

	if _, err := DeleteUser(ctx, pool, creator.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if got := countApprovals(t, pool, examID); got != 0 {
		t.Errorf("approvals after DeleteUser = %d, want 0 (no orphans)", got)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM exams WHERE id = $1`, examID).Scan(&n); err != nil {
		t.Fatalf("count exams: %v", err)
	}
	if n != 0 {
		t.Errorf("exam still exists after DeleteUser")
	}
}
