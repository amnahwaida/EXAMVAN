package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/examvan/webui/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Creator-family shared quota: exams created by an operator-created
// sub-account spend the creator-operator's quota (the operator's quota is
// the reference), counted family-wide (creator + direct sub-accounts).
// Without this, a pool-less sub-account (shared "personal" bucket, legacy
// school without a redemption, or a label-drifted row) fell back to its
// private forced-free columns counted created_by=self — N sub-accounts ×
// free quota never touched the operator's budget.
//
// These tests drive the real handlers over the DB-backed integration infra
// (setupVoucherITDB, skipped when TEST_DATABASE_URL is unset), mirroring
// school_shared_quota_test.go.
// ---------------------------------------------------------------------------

// markFamilySub flags an existing account as an operator-created sub-account
// of the given operator (what CreateUser does on the operator path).
func markFamilySub(t *testing.T, pool *pgxpool.Pool, subID, opID int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE admin_users SET operator_created = true, created_by = $1 WHERE id = $2`, opID, subID); err != nil {
		t.Fatalf("mark sub %d of operator %d: %v", subID, opID, err)
	}
}

// TestFamilyBudgetCapsTotalExamsAcrossSubs pins the reported bug: with no
// school pool anywhere (shared "personal" bucket, no redemption), two
// sub-accounts share the operator's max_exams budget — the third upload,
// by either sub, is rejected, and the operator's own upload is capped by
// the same family budget (symmetric sharing).
func TestFamilyBudgetCapsTotalExamsAcrossSubs(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createSchoolQuotaUser(t, pool, "op-fam-exams", "personal", []string{models.RoleOperator}, 2)
	sub1 := createSchoolQuotaUser(t, pool, "sub-fam-exams-1", "personal", []string{models.RoleGuru}, 3)
	sub2 := createSchoolQuotaUser(t, pool, "sub-fam-exams-2", "personal", []string{models.RoleGuru}, 3)
	markFamilySub(t, pool, sub1, op)
	markFamilySub(t, pool, sub2, op)

	tc, _ := newQuotaTestClient(t, pool)
	pdf := schoolTestPDF(64)

	// Two sub uploads fill the operator's 2-exam family budget.
	tc.login(t, sub1)
	if status, body := tc.upload(t, "fam-exam-1", pdf); status != http.StatusOK {
		t.Fatalf("sub1 upload 1: status=%d body=%s", status, body)
	}
	tc.login(t, sub2)
	if status, body := tc.upload(t, "fam-exam-2", pdf); status != http.StatusOK {
		t.Fatalf("sub2 upload 1: status=%d body=%s", status, body)
	}

	if n, err := models.CountExamsByFamily(context.Background(), pool, op); err != nil || n != 2 {
		t.Fatalf("family usage: got %d err=%v, want 2", n, err)
	}

	// Third upload by either sub → 403: the family budget (operator's quota)
	// is spent.
	tc.login(t, sub1)
	if status, body := tc.upload(t, "fam-exam-3", pdf); status != http.StatusForbidden ||
		!strings.Contains(body, "Batas pembuatan ujian") {
		t.Fatalf("sub1 upload 3: status=%d body=%s, want 403 family cap", status, body)
	}

	// Symmetric: the operator's own upload is capped by the same budget.
	tc.login(t, op)
	if status, body := tc.upload(t, "fam-exam-op", pdf); status != http.StatusForbidden ||
		!strings.Contains(body, "Batas pembuatan ujian") {
		t.Fatalf("operator upload over family: status=%d body=%s, want 403", status, body)
	}
}

// TestFamilyBudgetFromCreatorPoolAfterDrift pins the drift case: the
// operator runs a real school with an active package while its sub-account
// still carries the old "personal" label. The sub rejoins the creator's
// pool numbers (family-scoped usage) instead of its private free quota.
func TestFamilyBudgetFromCreatorPoolAfterDrift(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-fam-drift", "SMK Fam Drift", []string{models.RoleGuru, models.RoleOperator}, 10)
	sub := createSchoolQuotaUser(t, pool, "sub-fam-drift", "personal", []string{models.RoleGuru}, 3)
	markFamilySub(t, pool, sub, op)
	// School package: 1 exam. Without the family fallback the drifted sub
	// would spend its private 3-exam free quota instead.
	plantSchoolRedemption(t, pool, op, 1, 50*1024*1024, 1, 500*1024*1024)

	tc, _ := newQuotaTestClient(t, pool)
	pdf := schoolTestPDF(64)

	tc.login(t, sub)
	if status, body := tc.upload(t, "fam-drift-1", pdf); status != http.StatusOK {
		t.Fatalf("drifted sub upload 1: status=%d body=%s", status, body)
	}
	if status, body := tc.upload(t, "fam-drift-2", pdf); status != http.StatusForbidden ||
		!strings.Contains(body, "Batas pembuatan ujian") {
		t.Fatalf("drifted sub upload 2: status=%d body=%s, want 403 family cap", status, body)
	}

	if n, err := models.CountExamsByFamily(ctx, pool, op); err != nil || n != 1 {
		t.Fatalf("family usage: got %d err=%v, want 1", n, err)
	}
}

// TestFamilyBudgetCapsConcurrentStarts pins the concurrent dimension: with
// the operator's max_concurrent_exams = 1, a sub-account can run only one
// exam at a time across the whole family.
func TestFamilyBudgetCapsConcurrentStarts(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-fam-conc", "personal", []string{models.RoleOperator}, 10)
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET max_concurrent_exams = 1 WHERE id = $1`, op); err != nil {
		t.Fatalf("pin operator max_concurrent: %v", err)
	}
	sub := createSchoolQuotaUser(t, pool, "sub-fam-conc", "personal", []string{models.RoleGuru}, 10)
	markFamilySub(t, pool, sub, op)

	tc, _ := newQuotaTestClient(t, pool)
	pdf := schoolTestPDF(64)

	tc.login(t, sub)
	if status, body := tc.upload(t, "fam-conc-1", pdf); status != http.StatusOK {
		t.Fatalf("sub upload 1: status=%d body=%s", status, body)
	}
	if status, body := tc.upload(t, "fam-conc-2", pdf); status != http.StatusOK {
		t.Fatalf("sub upload 2: status=%d body=%s", status, body)
	}
	var e1, e2 int
	if err := pool.QueryRow(ctx, `SELECT id FROM exams WHERE name = 'fam-conc-1'`).Scan(&e1); err != nil {
		t.Fatalf("find exam 1: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM exams WHERE name = 'fam-conc-2'`).Scan(&e2); err != nil {
		t.Fatalf("find exam 2: %v", err)
	}

	if status, resp := tc.start(t, e1); status != http.StatusOK || !resp.Success {
		t.Fatalf("start exam 1: status=%d resp=%+v", status, resp)
	}
	if status, resp := tc.start(t, e2); status != http.StatusForbidden {
		t.Fatalf("start exam 2: status=%d resp=%+v, want 403 family concurrent cap", status, resp)
	}
}
