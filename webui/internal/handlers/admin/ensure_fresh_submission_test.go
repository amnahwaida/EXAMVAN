package admin

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Direct integration tests: models.EnsureFreshSubmissionOnApproval
// ---------------------------------------------------------------------------
//
// The function is the shared entry-row bookkeeping behind BOTH the manual
// approval endpoint (SetApprovalStatus) and the server-side auto-approve
// (RequestApproval). These tests drive it directly against a real database
// (no HTTP layer) so each invariant — row creation from the approval identity,
// idempotency on an open row, a fresh attempt row after a completed submit,
// the silent no-op when the approval row is missing, and the advisory-lock
// concurrency guarantee — is pinned at the unit boundary.

func TestEnsureFreshSubmissionOnApprovalCreatesRowFromApproval(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()
	mac := "AA:BB:CC:DD:EE:0B"

	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, identity_data, status)
		VALUES ($1, $2, 'Siswa Fresh', '07', 'XI C', '{"student_name":"Siswa Fresh","exam_number":"07"}', 'approved')`,
		fx.ExamID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}

	if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, fx.ExamID, mac); err != nil {
		t.Fatalf("EnsureFreshSubmissionOnApproval: %v", err)
	}

	// The monitoring row must exist and carry the identity the device supplied
	// in its approval request (copied from exam_approvals), plus a start_time
	// so the pengawas table shows it as a real (in-progress) device.
	var name, number, cls string
	var idData, startTime *string
	if err := pool.QueryRow(ctx, `
		SELECT student_name, exam_number, student_class, identity_data, start_time
		FROM submissions WHERE exam_id = $1 AND mac_address = $2`,
		fx.ExamID, mac).Scan(&name, &number, &cls, &idData, &startTime); err != nil {
		t.Fatalf("read submission: %v", err)
	}
	if name != "Siswa Fresh" || number != "07" || cls != "XI C" {
		t.Errorf("submission identity = %q/%q/%q, want Siswa Fresh/07/XI C", name, number, cls)
	}
	if idData == nil || !strings.Contains(*idData, "Siswa Fresh") {
		t.Errorf("identity_data = %v, want the approval identity copied", idData)
	}
	if startTime == nil || *startTime == "" {
		t.Error("start_time must be set on the fresh row")
	}
}

func TestEnsureFreshSubmissionOnApprovalIdempotentOnOpenRow(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()
	mac := "AA:BB:CC:DD:EE:0C"

	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, status)
		VALUES ($1, $2, 'Siswa Idem', 'approved')`, fx.ExamID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}

	// Repeated calls — e.g. the client's 5s approval poll — must never create
	// duplicate open rows for the same device.
	for i := 0; i < 3; i++ {
		if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, fx.ExamID, mac); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	var cnt int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND mac_address = $2`,
		fx.ExamID, mac).Scan(&cnt); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if cnt != 1 {
		t.Errorf("submissions = %d, want exactly 1 (idempotent on open row)", cnt)
	}
}

func TestEnsureFreshSubmissionOnApprovalCreatesNewAttemptAfterSubmit(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()
	mac := "AA:BB:CC:DD:EE:0D"

	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, status)
		VALUES ($1, $2, 'Siswa Ulang', 'approved')`, fx.ExamID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}

	// First approval → one open row.
	if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, fx.ExamID, mac); err != nil {
		t.Fatalf("first approval: %v", err)
	}

	// Simulate the student submitting: the open row gets its answers filled
	// (SubmitExam's CreateSubmission updates the open row in place).
	if _, err := pool.Exec(ctx,
		`UPDATE submissions SET answers_json = '{}' WHERE exam_id = $1 AND mac_address = $2`,
		fx.ExamID, mac); err != nil {
		t.Fatalf("simulate submit: %v", err)
	}

	// A second approval (new session) must start a NEW attempt row — the
	// completed row is not reused.
	if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, fx.ExamID, mac); err != nil {
		t.Fatalf("second approval: %v", err)
	}

	var cnt int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND mac_address = $2`,
		fx.ExamID, mac).Scan(&cnt); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if cnt != 2 {
		t.Errorf("submissions = %d, want 2 (completed row + fresh attempt row)", cnt)
	}
}

func TestEnsureFreshSubmissionOnApprovalNoApprovalRowNoOp(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()
	mac := "AA:BB:CC:DD:EE:0E" // never requested approval — no exam_approvals row

	if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, fx.ExamID, mac); err != nil {
		t.Fatalf("EnsureFreshSubmissionOnApproval: %v", err)
	}

	// Without an approval row there is nothing to copy from: the INSERT …
	// SELECT writes zero rows and must NOT invent an entry.
	var cnt int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND mac_address = $2`,
		fx.ExamID, mac).Scan(&cnt); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if cnt != 0 {
		t.Errorf("submissions = %d, want 0 (no approval row to copy from)", cnt)
	}
}

func TestEnsureFreshSubmissionOnApprovalConcurrentSingleRow(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()
	mac := "AA:BB:CC:DD:EE:0F"

	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, status)
		VALUES ($1, $2, 'Siswa Race', 'approved')`, fx.ExamID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}

	// The per-device advisory lock serialises the check-then-insert, so no
	// matter how many requests land at the same instant, exactly one open row
	// survives. Without the lock this test fails intermittently.
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, fx.ExamID, mac); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent call: %v", err)
	}

	var cnt int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND mac_address = $2`,
		fx.ExamID, mac).Scan(&cnt); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if cnt != 1 {
		t.Errorf("submissions = %d, want exactly 1 (advisory lock must prevent duplicates)", cnt)
	}
}
