package admin

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Integration tests: PurgeStaleExamApprovals + the background job pass
// ---------------------------------------------------------------------------
//
// The purge has two conservative staleness rules (see models/approval_cleanup.go):
//   1. pending/approved rows on exams whose end_time passed (by the grace);
//   2. pending/approved rows on exams inactive longer than the TTL.
// Rejected rows are never touched; submissions are never touched (the durable
// student record behind the monitoring page and public results).

// insertCleanupExam inserts an exam with the given status and schedule.
func insertCleanupExam(t *testing.T, pool *pgxpool.Pool, name, status string, startAt, endAt *time.Time) int {
	t.Helper()
	token := fmt.Sprintf("CLN%05d", time.Now().UnixNano()%100000)
	var examID int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, exam_started_at, start_time, end_time)
		VALUES ($1, 'cleanup.pdf', 1024, $2, $2, $3, 'medium', $4, $5, $6)
		RETURNING id`, name, token, status, startAt, startAt, endAt).Scan(&examID); err != nil {
		t.Fatalf("insert cleanup exam %s: %v", name, err)
	}
	return examID
}

// insertCleanupApproval inserts an approval row with an explicit created_at.
func insertCleanupApproval(t *testing.T, pool *pgxpool.Pool, examID int, mac, status string, createdAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status, created_at)
		VALUES ($1, $2, 'Siswa', '01', 'XII A', $3, $4)`,
		examID, mac, status, createdAt); err != nil {
		t.Fatalf("insert approval %s: %v", mac, err)
	}
}

func countApprovalsByStatus(t *testing.T, pool *pgxpool.Pool, examID int, status string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id = $1 AND status = $2`, examID, status).Scan(&n); err != nil {
		t.Fatalf("count approvals (%s): %v", status, err)
	}
	return n
}

// Rows on an exam whose end_time passed (beyond the 1h grace) are purged —
// pending (can never be decided) and approved (device approved but never
// submitted, holding a cap slot). Rejected rows survive: they are explicit
// pengawas decisions.
func TestApprovalCleanupEndedExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	now := time.Now().UTC()
	end := now.Add(-3 * time.Hour)
	examID := insertCleanupExam(t, pool, "Ended", "active", &now, &end)

	insertCleanupApproval(t, pool, examID, "EE:00:00:00:00:01", "pending", now.Add(-1*time.Hour))
	insertCleanupApproval(t, pool, examID, "EE:00:00:00:00:02", "approved", now.Add(-2*time.Hour))
	insertCleanupApproval(t, pool, examID, "EE:00:00:00:00:03", "rejected", now.Add(-2*time.Hour))

	stats, err := models.PurgeStaleExamApprovals(context.Background(), pool, 1, 24)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if stats.PendingEnded != 1 || stats.ApprovedEnded != 1 {
		t.Errorf("stats = %+v, want pending_ended=1 approved_ended=1", stats)
	}
	if got := countApprovalsByStatus(t, pool, examID, "pending"); got != 0 {
		t.Errorf("pending rows after purge = %d, want 0", got)
	}
	if got := countApprovalsByStatus(t, pool, examID, "approved"); got != 0 {
		t.Errorf("approved rows after purge = %d, want 0", got)
	}
	if got := countApprovalsByStatus(t, pool, examID, "rejected"); got != 1 {
		t.Errorf("rejected rows after purge = %d, want 1 (explicit decision must survive)", got)
	}
}

// The safety margin: an exam whose end_time passed but is still within the
// cleanup grace (the API rejects joins/subs only 60s past end_time, the purge
// waits 1h) must keep its rows — this covers the "teacher extends end_time
// shortly after it passed" scenario without stranding anyone.
func TestApprovalCleanupKeepsRowsWithinEndedGrace(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	now := time.Now().UTC()
	end := now.Add(-30 * time.Minute) // past the 60s submit grace, inside the 1h cleanup grace
	examID := insertCleanupExam(t, pool, "Grace", "active", &now, &end)

	insertCleanupApproval(t, pool, examID, "EE:00:00:05:00:01", "pending", now.Add(-20*time.Minute))
	insertCleanupApproval(t, pool, examID, "EE:00:00:05:00:02", "approved", now.Add(-20*time.Minute))

	stats, err := models.PurgeStaleExamApprovals(context.Background(), pool, 1, 24)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if stats.Total() != 0 {
		t.Errorf("stats = %+v, want nothing purged while within the ended grace", stats)
	}
	if got := countApprovalsByStatus(t, pool, examID, "pending"); got != 1 {
		t.Errorf("pending rows after purge = %d, want 1 (within ended grace)", got)
	}
	if got := countApprovalsByStatus(t, pool, examID, "approved"); got != 1 {
		t.Errorf("approved rows after purge = %d, want 1 (within ended grace)", got)
	}
}

// Rows on a LIVE exam (active, started, end_time in the future) are never
// touched — deleting them would strand a queued device or revoke a working
// approval mid-exam.
func TestApprovalCleanupKeepsLiveExamRows(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	now := time.Now().UTC()
	end := now.Add(2 * time.Hour)
	examID := insertCleanupExam(t, pool, "Live", "active", &now, &end)

	insertCleanupApproval(t, pool, examID, "EE:00:00:10:00:01", "pending", now.Add(-10*time.Minute))
	insertCleanupApproval(t, pool, examID, "EE:00:00:10:00:02", "approved", now.Add(-10*time.Minute))

	stats, err := models.PurgeStaleExamApprovals(context.Background(), pool, 1, 24)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if stats.Total() != 0 {
		t.Errorf("stats = %+v, want nothing purged for a live exam", stats)
	}
	if got := countApprovalsByStatus(t, pool, examID, "pending"); got != 1 {
		t.Errorf("pending rows after purge = %d, want 1 (live exam)", got)
	}
	if got := countApprovalsByStatus(t, pool, examID, "approved"); got != 1 {
		t.Errorf("approved rows after purge = %d, want 1 (live exam)", got)
	}
}

// Rows on an INACTIVE exam are purged once they pass the TTL; younger rows
// survive (the teacher may still restart the exam). Rejected rows survive
// regardless of age.
func TestApprovalCleanupInactiveExamTTL(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	now := time.Now().UTC()
	examID := insertCleanupExam(t, pool, "Dormant", "inactive", nil, nil)

	insertCleanupApproval(t, pool, examID, "EE:00:00:20:00:01", "pending", now.Add(-48*time.Hour))   // old pending → purge
	insertCleanupApproval(t, pool, examID, "EE:00:00:20:00:02", "approved", now.Add(-48*time.Hour))  // old approved → purge
	insertCleanupApproval(t, pool, examID, "EE:00:00:20:00:03", "approved", now.Add(-10*time.Hour))  // young approved → keep
	insertCleanupApproval(t, pool, examID, "EE:00:00:20:00:04", "rejected", now.Add(-48*time.Hour))  // old rejected → keep

	stats, err := models.PurgeStaleExamApprovals(context.Background(), pool, 1, 24)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if stats.PendingInactive != 1 || stats.ApprovedInactive != 1 {
		t.Errorf("stats = %+v, want pending_inactive=1 approved_inactive=1", stats)
	}
	if got := countApprovalsByStatus(t, pool, examID, "pending"); got != 0 {
		t.Errorf("pending rows after purge = %d, want 0 (older than TTL)", got)
	}
	if got := countApprovalsByStatus(t, pool, examID, "approved"); got != 1 {
		t.Errorf("approved rows after purge = %d, want 1 (younger than TTL kept)", got)
	}
	if got := countApprovalsByStatus(t, pool, examID, "rejected"); got != 1 {
		t.Errorf("rejected rows after purge = %d, want 1 (never touched)", got)
	}
}

// The job wrapper (runApprovalCleanupPass) must honor the SuperAdmin-tunable
// saas_settings: with default keys absent it uses 1h grace / 24h TTL, but once
// the settings are tightened (grace=0, TTL=0) rows that previously survived
// (within the default windows) get purged — no restart needed. saas_settings
// survives TRUNCATE, so the test removes its keys afterwards to restore the
// defaults for sibling tests.
func TestApprovalCleanupPassReadsSaaSConfig(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()
	now := time.Now().UTC()

	cleanupKeys := []string{
		models.SettingApprovalCleanupEndedGraceHours,
		models.SettingApprovalCleanupInactiveTTLHours,
	}
	t.Cleanup(func() {
		for _, k := range cleanupKeys {
			_, _ = pool.Exec(context.Background(), `DELETE FROM saas_settings WHERE key = $1`, k)
		}
	})
	for _, k := range cleanupKeys {
		if _, err := pool.Exec(ctx, `DELETE FROM saas_settings WHERE key = $1`, k); err != nil {
			t.Fatalf("reset key %s: %v", k, err)
		}
	}

	// Exam ended 30 minutes ago — WITHIN the default 1h grace, so its rows must
	// survive the pass while the default config is in effect.
	end := now.Add(-30 * time.Minute)
	examID := insertCleanupExam(t, pool, "EndedGrace", "active", &now, &end)
	insertCleanupApproval(t, pool, examID, "EE:00:00:00:50:01", "pending", now.Add(-10*time.Minute))

	// Exam inactive with a row 2 hours old — WITHIN the default 24h TTL. Its
	// end_time is in the FUTURE so only the inactive-TTL rule (not the
	// ended-exam rule) can touch it: the two staleness windows must be tested
	// independently.
	var inactiveID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, start_time, end_time)
		VALUES ('Dormant', 'cleanup.pdf', 1024, $1, $1, 'inactive', 'medium', $2, $3)
		RETURNING id`, fmt.Sprintf("CLND%05d", time.Now().UnixNano()%100000),
		now.Add(-3*time.Hour), now.Add(1*time.Hour)).Scan(&inactiveID); err != nil {
		t.Fatalf("insert inactive exam: %v", err)
	}
	insertCleanupApproval(t, pool, inactiveID, "EE:00:00:00:50:02", "approved", now.Add(-2*time.Hour))

	// Pass 1 — defaults: both rows survive (inside the default windows).
	runApprovalCleanupPass(ctx, pool)
	if got := countApprovalsByStatus(t, pool, examID, "pending"); got != 1 {
		t.Fatalf("ended-exam row after default pass = %d, want 1 (within 1h grace)", got)
	}
	if got := countApprovalsByStatus(t, pool, inactiveID, "approved"); got != 1 {
		t.Fatalf("inactive row after default pass = %d, want 1 (within 24h TTL)", got)
	}

	// Tighten via saas_settings (the panel's knobs) — grace 0, TTL 0.
	if err := models.SetSaasSetting(ctx, pool, models.SettingApprovalCleanupEndedGraceHours, "0"); err != nil {
		t.Fatalf("set grace: %v", err)
	}
	if err := models.SetSaasSetting(ctx, pool, models.SettingApprovalCleanupInactiveTTLHours, "0"); err != nil {
		t.Fatalf("set ttl: %v", err)
	}

	// Pass 2 — tightened: both rows now purged by the SAME job wrapper.
	runApprovalCleanupPass(ctx, pool)
	if got := countApprovalsByStatus(t, pool, examID, "pending"); got != 0 {
		t.Errorf("ended-exam row after tightened pass = %d, want 0 (grace=0)", got)
	}
	if got := countApprovalsByStatus(t, pool, inactiveID, "approved"); got != 0 {
		t.Errorf("inactive row after tightened pass = %d, want 0 (TTL=0)", got)
	}
}

// Purging an approved row on an ended exam must NOT delete the submission row
// that EnsureFreshSubmissionOnApproval created — submissions are the durable
// student record (monitoring page + public results), approvals are just the
// entry gate. Also exercises the job wrapper (runApprovalCleanupPass).
func TestApprovalCleanupPreservesSubmissions(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()
	now := time.Now().UTC()
	end := now.Add(-3 * time.Hour)
	examID := insertCleanupExam(t, pool, "EndedSub", "active", &now, &end)

	mac := "EE:00:00:30:00:01"
	insertCleanupApproval(t, pool, examID, mac, "approved", now.Add(-2*time.Hour))
	if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure submission: %v", err)
	}

	var subsBefore int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND mac_address = $2`, examID, mac).Scan(&subsBefore); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if subsBefore != 1 {
		t.Fatalf("submissions before purge = %d, want 1 (fixture)", subsBefore)
	}

	// Run through the job wrapper itself (not just the model function).
	runApprovalCleanupPass(ctx, pool)

	if got := countApprovalsByStatus(t, pool, examID, "approved"); got != 0 {
		t.Errorf("approved rows after purge = %d, want 0 (exam ended)", got)
	}
	var subsAfter int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND mac_address = $2`, examID, mac).Scan(&subsAfter); err != nil {
		t.Fatalf("count submissions after purge: %v", err)
	}
	if subsAfter != 1 {
		t.Errorf("submissions after purge = %d, want 1 (student record must survive)", subsAfter)
	}
}
