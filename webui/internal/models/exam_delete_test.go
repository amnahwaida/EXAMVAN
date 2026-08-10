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

// A failure in the middle of BulkDeleteExams' transaction must roll back ALL
// deletes — not just stop after the failing statement. This is what makes the
// bulk path atomic (reviewer follow-up): an error deleting submissions (here
// forced with a guard trigger) must restore the approvals/pengawas/access-log
// rows already deleted earlier in the same tx, and keep the exams alive.
func TestBulkDeleteExamsRollsBackOnFailure(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	creator, err := CreateUser(ctx, pool, &AdminUser{
		Username: "bulk_rb_owner", Name: "Bulk Rb Owner", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	examID := insertDeleteTestExam(t, pool, creator.ID, "Bulk Rollback")
	insertDeleteTestSubmission(t, pool, examID)
	if _, err := pool.Exec(ctx, `INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2)`, examID, creator.ID); err != nil {
		t.Fatalf("insert pengawas: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO student_access_logs (exam_id, student_identifier, event)
		VALUES ($1, 'AA:BB:CC:DD:EE:00', 'login')`, examID); err != nil {
		t.Fatalf("insert access log: %v", err)
	}

	// Guard trigger: any DELETE on submissions raises, so BulkDeleteExams fails
	// at the submissions step — after approvals, pengawas, and access logs were
	// already deleted inside the transaction. The whole tx must roll back.
	//
	// Setup is idempotent (DROP IF EXISTS before CREATE): the per-package test
	// schema survives TRUNCATE-only resets, and TRUNCATE does NOT drop triggers
	// or functions — a hard-killed previous run would otherwise leave stale
	// objects behind and break the next run with "function already exists".
	const fnName = "fn_block_submissions_delete_rollback_test"
	const tgName = "trg_block_submissions_delete_rollback_test"
	if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+tgName+` ON submissions`); err != nil {
		t.Fatalf("reset guard trigger: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP FUNCTION IF EXISTS `+fnName+`()`); err != nil {
		t.Fatalf("reset guard function: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION `+fnName+`() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'bulk delete rollback test: submissions delete blocked';
		END;
		$$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create guard function: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER `+tgName+`
		BEFORE DELETE ON submissions FOR EACH ROW EXECUTE FUNCTION `+fnName+`()`); err != nil {
		t.Fatalf("create guard trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+tgName+` ON submissions`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS `+fnName+`()`)
	})

	// The delete must FAIL (the trigger aborts the transaction).
	if _, err := BulkDeleteExams(ctx, pool, []int{examID}); err == nil {
		t.Fatalf("BulkDeleteExams unexpectedly succeeded despite guard trigger")
	}

	// Rollback proof: every row must still exist — approvals (deleted first in
	// the tx), pengawas + access log (deleted in between), submissions (the
	// failing step), and the exam itself.
	if got := countApprovals(t, pool, examID); got != 3 {
		t.Errorf("approvals after failed bulk delete = %d, want 3 (rolled back)", got)
	}
	if got := countSubmissions(t, pool, examID); got != 1 {
		t.Errorf("submissions after failed bulk delete = %d, want 1 (rolled back)", got)
	}
	var pengawas int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM exam_pengawas WHERE exam_id = $1`, examID).Scan(&pengawas); err != nil {
		t.Fatalf("count pengawas: %v", err)
	}
	if pengawas != 1 {
		t.Errorf("pengawas after failed bulk delete = %d, want 1 (rolled back)", pengawas)
	}
	var logs int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM student_access_logs WHERE exam_id = $1`, examID).Scan(&logs); err != nil {
		t.Fatalf("count access logs: %v", err)
	}
	if logs != 1 {
		t.Errorf("access logs after failed bulk delete = %d, want 1 (rolled back)", logs)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM exams WHERE id = $1`, examID).Scan(&n); err != nil {
		t.Fatalf("count exams: %v", err)
	}
	if n != 1 {
		t.Errorf("exam after failed bulk delete = %d, want 1 (still alive)", n)
	}

	// Sanity: after dropping the guard trigger, the same delete succeeds and the
	// exam goes away (the trigger, not the delete logic, was the blocker). The
	// trigger is dropped explicitly here — t.Cleanup would run too late.
	if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+tgName+` ON submissions`); err != nil {
		t.Fatalf("drop guard trigger: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP FUNCTION IF EXISTS `+fnName+`()`); err != nil {
		t.Fatalf("drop guard function: %v", err)
	}
	if _, err := BulkDeleteExams(ctx, pool, []int{examID}); err != nil {
		t.Fatalf("BulkDeleteExams after guard removed: %v", err)
	}
	if got := countApprovals(t, pool, examID); got != 0 {
		t.Errorf("approvals after retry = %d, want 0", got)
	}
}

// A failure in the middle of DeleteExam's transaction must roll back ALL
// deletes — not just stop after the failing statement. DeleteExam cleans
// exam_pengawas, exam_approvals, student_access_logs and submissions
// explicitly before deleting the exam row. A guard trigger on submissions
// forces the failure at the 4th child delete (after pengawas, approvals and
// access logs were already deleted in the same tx); every earlier delete must
// be restored and the exam must stay alive.
func TestDeleteExamRollsBackOnFailure(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	creator, err := CreateUser(ctx, pool, &AdminUser{
		Username: "single_rb_owner", Name: "Single Rb Owner", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	examID := insertDeleteTestExam(t, pool, creator.ID, "Single Rollback")
	insertDeleteTestSubmission(t, pool, examID)
	if _, err := pool.Exec(ctx, `INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2)`, examID, creator.ID); err != nil {
		t.Fatalf("insert pengawas: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO student_access_logs (exam_id, student_identifier, event)
		VALUES ($1, 'AA:BB:CC:DD:EE:00', 'login')`, examID); err != nil {
		t.Fatalf("insert access log: %v", err)
	}

	// Guard trigger: any DELETE on submissions raises, so DeleteExam fails at
	// the submissions step — after pengawas, approvals, and access logs were
	// already deleted inside the transaction. The whole tx must roll back.
	//
	// Names are unique per test (the bulk test uses a different pair) and setup
	// is idempotent (DROP IF EXISTS before CREATE): the per-package test schema
	// survives TRUNCATE-only resets, and TRUNCATE does NOT drop triggers or
	// functions — a hard-killed previous run would otherwise leave stale
	// objects behind and break the next run with "function already exists".
	const fnName = "fn_block_submissions_delete_exam_rollback_test"
	const tgName = "trg_block_submissions_delete_exam_rollback_test"
	if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+tgName+` ON submissions`); err != nil {
		t.Fatalf("reset guard trigger: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP FUNCTION IF EXISTS `+fnName+`()`); err != nil {
		t.Fatalf("reset guard function: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION `+fnName+`() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'delete exam rollback test: submissions delete blocked';
		END;
		$$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create guard function: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER `+tgName+`
		BEFORE DELETE ON submissions FOR EACH ROW EXECUTE FUNCTION `+fnName+`()`); err != nil {
		t.Fatalf("create guard trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+tgName+` ON submissions`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS `+fnName+`()`)
	})

	// The delete must FAIL (the trigger aborts the transaction).
	if _, err := DeleteExam(ctx, pool, examID); err == nil {
		t.Fatalf("DeleteExam unexpectedly succeeded despite guard trigger")
	}

	// Rollback proof: every row must still exist — pengawas + approvals +
	// access logs (deleted before the failing step), submissions (the failing
	// step), and the exam itself.
	var pengawas int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM exam_pengawas WHERE exam_id = $1`, examID).Scan(&pengawas); err != nil {
		t.Fatalf("count pengawas: %v", err)
	}
	if pengawas != 1 {
		t.Errorf("pengawas after failed DeleteExam = %d, want 1 (rolled back)", pengawas)
	}
	if got := countApprovals(t, pool, examID); got != 3 {
		t.Errorf("approvals after failed DeleteExam = %d, want 3 (rolled back)", got)
	}
	var logs int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM student_access_logs WHERE exam_id = $1`, examID).Scan(&logs); err != nil {
		t.Fatalf("count access logs: %v", err)
	}
	if logs != 1 {
		t.Errorf("access logs after failed DeleteExam = %d, want 1 (rolled back)", logs)
	}
	if got := countSubmissions(t, pool, examID); got != 1 {
		t.Errorf("submissions after failed DeleteExam = %d, want 1 (rolled back)", got)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM exams WHERE id = $1`, examID).Scan(&n); err != nil {
		t.Fatalf("count exams: %v", err)
	}
	if n != 1 {
		t.Errorf("exam after failed DeleteExam = %d, want 1 (still alive)", n)
	}

	// Sanity: after dropping the guard trigger, the same delete succeeds and
	// the exam goes away (the trigger, not the delete logic, was the blocker).
	// The trigger is dropped explicitly here — t.Cleanup would run too late.
	if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+tgName+` ON submissions`); err != nil {
		t.Fatalf("drop guard trigger: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP FUNCTION IF EXISTS `+fnName+`()`); err != nil {
		t.Fatalf("drop guard function: %v", err)
	}
	if _, err := DeleteExam(ctx, pool, examID); err != nil {
		t.Fatalf("DeleteExam after guard removed: %v", err)
	}
	if got := countApprovals(t, pool, examID); got != 0 {
		t.Errorf("approvals after retry = %d, want 0", got)
	}
	if got := countSubmissions(t, pool, examID); got != 0 {
		t.Errorf("submissions after retry = %d, want 0", got)
	}
	for _, q := range []struct {
		name string
		sql  string
	}{
		{"pengawas", `SELECT COUNT(*) FROM exam_pengawas WHERE exam_id = $1`},
		{"access logs", `SELECT COUNT(*) FROM student_access_logs WHERE exam_id = $1`},
		{"exams", `SELECT COUNT(*) FROM exams WHERE id = $1`},
	} {
		var c int
		if err := pool.QueryRow(ctx, q.sql, examID).Scan(&c); err != nil {
			t.Fatalf("count %s after retry: %v", q.name, err)
		}
		if c != 0 {
			t.Errorf("%s after retry = %d, want 0", q.name, c)
		}
	}
}

// A failure in the middle of DeleteUser's cascade transaction must roll back
// ALL deletes — not just stop after the failing statement. DeleteUser for an
// operator deletes the whole school (cascaded sub-accounts) along with every
// user's exams, approvals, submissions, pengawas and access logs — all inside
// one transaction. A guard trigger on admin_users forces the failure at the
// cascaded-user delete step (after the exams were already deleted in the same
// tx); every earlier delete must be restored.
func TestDeleteUserRollsBackOnFailure(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	// School operator + a sub-account in the same instansi (cascaded by the
	// delete) + a voucher redemption for the sub (child row of admin_users).
	op, err := CreateUser(ctx, pool, &AdminUser{
		Username: "user_rb_op", Name: "User Rb Op", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleOperator}),
		Instansi: "SMA Rollback", MaxExams: 3, MaxPDFSize: 1048576,
		MaxConcurrentExams: 1, MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create operator: %v", err)
	}
	sub, err := CreateUser(ctx, pool, &AdminUser{
		Username: "user_rb_sub", Name: "User Rb Sub", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleGuru}),
		Instansi: "SMA Rollback", MaxExams: 3, MaxPDFSize: 1048576,
		MaxConcurrentExams: 1, MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create sub: %v", err)
	}

	opExam := insertDeleteTestExam(t, pool, op.ID, "Op Rollback")
	subExam := insertDeleteTestExam(t, pool, sub.ID, "Sub Rollback")
	insertDeleteTestSubmission(t, pool, opExam)
	insertDeleteTestSubmission(t, pool, subExam)
	if _, err := pool.Exec(ctx, `INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2)`, opExam, sub.ID); err != nil {
		t.Fatalf("insert pengawas: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO student_access_logs (exam_id, student_identifier, event)
		VALUES ($1, 'AA:BB:CC:DD:EE:00', 'login')`, opExam); err != nil {
		t.Fatalf("insert access log: %v", err)
	}
	// A claimed voucher on the sub — the child row that must also roll back
	// with the failed user delete (voucher_redemptions.user_id CASCADE).
	voucher, err := CreateVoucher(ctx, pool, &Voucher{Code: "VCRB" + fmt.Sprintf("%05d", time.Now().UnixNano()%100000), Package: "sekolah_kecil"})
	if err != nil {
		t.Fatalf("create voucher: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO voucher_redemptions (voucher_id, user_id) VALUES ($1, $2)`, voucher.ID, sub.ID); err != nil {
		t.Fatalf("insert redemption: %v", err)
	}

	// Guard trigger: any DELETE on admin_users raises, so DeleteUser fails at
	// the cascaded-user delete step — after exams, approvals, submissions,
	// pengawas and access logs were already deleted inside the transaction.
	// Idempotent setup (DROP IF EXISTS before CREATE): the per-package test
	// schema survives TRUNCATE-only resets, and TRUNCATE does NOT drop
	// triggers/functions — a hard-killed previous run would otherwise leave
	// stale objects behind and break the next run.
	const fnName = "fn_block_admin_users_delete_rollback_test"
	const tgName = "trg_block_admin_users_delete_rollback_test"
	if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+tgName+` ON admin_users`); err != nil {
		t.Fatalf("reset guard trigger: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP FUNCTION IF EXISTS `+fnName+`()`); err != nil {
		t.Fatalf("reset guard function: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION `+fnName+`() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'delete user rollback test: admin_users delete blocked';
		END;
		$$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create guard function: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER `+tgName+`
		BEFORE DELETE ON admin_users FOR EACH ROW EXECUTE FUNCTION `+fnName+`()`); err != nil {
		t.Fatalf("create guard trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+tgName+` ON admin_users`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS `+fnName+`()`)
	})

	// The delete must FAIL (the trigger aborts the transaction).
	if _, err := DeleteUser(ctx, pool, op.ID); err == nil {
		t.Fatalf("DeleteUser unexpectedly succeeded despite guard trigger")
	}

	// Rollback proof: every row must still exist — both users, both exams, all
	// approvals, submissions, the pengawas assignment, the access log, and the
	// voucher redemption.
	for _, uid := range []int{op.ID, sub.ID} {
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users WHERE id = $1`, uid).Scan(&n); err != nil {
			t.Fatalf("count user %d: %v", uid, err)
		}
		if n != 1 {
			t.Errorf("user %d after failed DeleteUser = %d, want 1 (rolled back)", uid, n)
		}
	}
	for _, eid := range []int{opExam, subExam} {
		if got := countApprovals(t, pool, eid); got != 3 {
			t.Errorf("exam %d approvals after failed DeleteUser = %d, want 3 (rolled back)", eid, got)
		}
		if got := countSubmissions(t, pool, eid); got != 1 {
			t.Errorf("exam %d submissions after failed DeleteUser = %d, want 1 (rolled back)", eid, got)
		}
	}
	var pengawas int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM exam_pengawas WHERE exam_id = $1 AND user_id = $2`, opExam, sub.ID).Scan(&pengawas); err != nil {
		t.Fatalf("count pengawas: %v", err)
	}
	if pengawas != 1 {
		t.Errorf("pengawas after failed DeleteUser = %d, want 1 (rolled back)", pengawas)
	}
	var logs int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM student_access_logs WHERE exam_id = $1`, opExam).Scan(&logs); err != nil {
		t.Fatalf("count access logs: %v", err)
	}
	if logs != 1 {
		t.Errorf("access logs after failed DeleteUser = %d, want 1 (rolled back)", logs)
	}
	var redemptions int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM voucher_redemptions WHERE user_id = $1`, sub.ID).Scan(&redemptions); err != nil {
		t.Fatalf("count redemptions: %v", err)
	}
	if redemptions != 1 {
		t.Errorf("voucher redemptions after failed DeleteUser = %d, want 1 (rolled back)", redemptions)
	}
	for _, eid := range []int{opExam, subExam} {
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM exams WHERE id = $1`, eid).Scan(&n); err != nil {
			t.Fatalf("count exam %d: %v", eid, err)
		}
		if n != 1 {
			t.Errorf("exam %d after failed DeleteUser = %d, want 1 (still alive)", eid, n)
		}
	}

	// Sanity: after dropping the guard trigger, the same delete succeeds and
	// everything goes away (the trigger, not the delete logic, was the
	// blocker). Dropped explicitly here — t.Cleanup would run too late.
	if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+tgName+` ON admin_users`); err != nil {
		t.Fatalf("drop guard trigger: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP FUNCTION IF EXISTS `+fnName+`()`); err != nil {
		t.Fatalf("drop guard function: %v", err)
	}
	if _, err := DeleteUser(ctx, pool, op.ID); err != nil {
		t.Fatalf("DeleteUser after guard removed: %v", err)
	}
	if got := countApprovals(t, pool, opExam, subExam); got != 0 {
		t.Errorf("approvals after retry = %d, want 0", got)
	}
	if got := countSubmissions(t, pool, opExam, subExam); got != 0 {
		t.Errorf("submissions after retry = %d, want 0", got)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users WHERE id = $1 OR id = $2`, op.ID, sub.ID).Scan(&n); err != nil {
		t.Fatalf("count users after retry: %v", err)
	}
	if n != 0 {
		t.Errorf("users after retry = %d, want 0", n)
	}
	for _, q := range []struct {
		name string
		sql  string
		arg  int
	}{
		{"pengawas", `SELECT COUNT(*) FROM exam_pengawas WHERE exam_id = $1`, opExam},
		{"access logs", `SELECT COUNT(*) FROM student_access_logs WHERE exam_id = $1`, opExam},
		{"redemptions", `SELECT COUNT(*) FROM voucher_redemptions WHERE user_id = $1`, sub.ID},
	} {
		var c int
		if err := pool.QueryRow(ctx, q.sql, q.arg).Scan(&c); err != nil {
			t.Fatalf("count %s after retry: %v", q.name, err)
		}
		if c != 0 {
			t.Errorf("%s after retry = %d, want 0", q.name, c)
		}
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
