package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// TestDeactivatePackageRevertsToFreeTrial locks the no-fallback path: an
// operator with an ACTIVE school package and no other claimed voucher is
// deactivated by a SuperAdmin. The active redemption must be BURNED
// (remaining_seconds = 0, is_active = false — the same terminal state the
// expiry job produces), the account must revert to the free tier (free
// defaults, package-granted operator role clawed back, fresh
// default_active_days trial), the Kelola User list flag must flip to false,
// and a voucher_deactivated audit row must be appended.
func TestDeactivatePackageRevertsToFreeTrial(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucherCode(t, pool, "IT-DEACT-1")
	op := createOperatorUser(t, pool, "op-deact-1", "SMK Deact 1", "pass-op-deact-1")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-DEACT-1") // school package active, operator role granted

	opBefore := mustGetUser(t, pool, "op-deact-1")
	if !models.HasRole(opBefore.Role, models.RoleOperator) {
		t.Fatalf("fixture: op must hold operator role after redeem, got %q", opBefore.Role)
	}

	root := createRootUser(t, pool, "root-deact-1")
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	// The Kelola User list must flag the account as having an active package
	// BEFORE the deactivation.
	if found, flag := listUsersActivePackageFlag(t, rt.client, rt.srv, "op-deact-1"); !found || !flag {
		t.Fatalf("fixture: list flag = (found=%v, active=%v), want (true, true)", found, flag)
	}

	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/deactivate-package", map[string]interface{}{})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("deactivate package: status=%d resp=%+v", status, resp)
	}

	after := mustGetUser(t, pool, "op-deact-1")
	if after.Package != "free" {
		t.Errorf("package = %q, want free", after.Package)
	}
	if after.MaxExams != subAccountFreeMaxExams || after.MaxPDFSize != subAccountFreeMaxPDFSize ||
		after.MaxConcurrentExams != subAccountFreeMaxConcurrentExams || after.MaxStorageSize != subAccountFreeMaxStorageSize {
		t.Errorf("quotas = (%d,%d,%d,%d), want free defaults (%d,%d,%d,%d)",
			after.MaxExams, after.MaxPDFSize, after.MaxConcurrentExams, after.MaxStorageSize,
			subAccountFreeMaxExams, subAccountFreeMaxPDFSize, subAccountFreeMaxConcurrentExams, subAccountFreeMaxStorageSize)
	}
	if models.HasRole(after.Role, models.RoleOperator) {
		t.Errorf("role = %q — operator must be clawed back on the free tier", after.Role)
	}

	// Active redemption burned: none active, remaining zeroed.
	var active int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM voucher_redemptions WHERE user_id=$1 AND is_active`, op.ID).Scan(&active); err != nil {
		t.Fatalf("count active redemptions: %v", err)
	}
	if active != 0 {
		t.Errorf("active redemptions after deactivate = %d, want 0 (burned)", active)
	}
	var rem int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(remaining_seconds, 0) FROM voucher_redemptions WHERE user_id=$1`, op.ID).Scan(&rem); err != nil {
		t.Fatalf("read remaining_seconds: %v", err)
	}
	if rem != 0 {
		t.Errorf("remaining_seconds = %d, want 0 (burned, not re-activatable)", rem)
	}

	// Fresh free trial: expiry ≈ now + default_active_days.
	trialDays := models.GetSaasSettingInt(ctx, pool, models.SettingDefaultActiveDays, 14)
	if after.ExpiresAt == nil {
		t.Fatal("expires_at is nil after revert to free trial")
	}
	want := time.Now().UTC().AddDate(0, 0, trialDays)
	if diff := after.ExpiresAt.Sub(want); diff > 2*time.Minute || diff < -2*time.Minute {
		t.Errorf("expires_at = %v, want ≈ now+%dd (%v)", after.ExpiresAt, trialDays, want)
	}

	// Kelola User list flag flipped to false.
	if found, flag := listUsersActivePackageFlag(t, rt.client, rt.srv, "op-deact-1"); !found || flag {
		t.Errorf("list flag after deactivate = (found=%v, active=%v), want (true, false)", found, flag)
	}

	// Append-only audit trail recorded the deactivation.
	var auditCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'voucher_deactivated' AND user_id = $1`, root.ID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("voucher_deactivated audit rows = %d, want 1", auditCount)
	}
}

// TestDeactivatePackageActivatesFallback locks the fallback path: the account
// holds an active guru package AND a paused school package with remaining
// lifetime. Deactivating the active one must BURN it and ACTIVATE the best
// paused claimed voucher instead — the account keeps access via the fallback,
// its operator role returns, and only the deactivated package is zeroed.
func TestDeactivatePackageActivatesFallback(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucherCode(t, pool, "IT-DEACT-A") // grants operator role
	createGuruVoucherCode(t, pool, "IT-DEACT-B")   // grants no role
	op := createOperatorUser(t, pool, "op-deact-2", "SMK Deact 2", "pass-op-deact-2")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-DEACT-A") // A active (operator role granted)
	tc.redeem(t, "IT-DEACT-B") // B active; A auto-paused with lifetime preserved

	opMid := mustGetUser(t, pool, "op-deact-2")
	if models.HasRole(opMid.Role, models.RoleOperator) {
		t.Fatalf("fixture: after guru redeem the operator role must be gone, got %q", opMid.Role)
	}
	var aRem int64
	if err := pool.QueryRow(ctx, `
		SELECT r.remaining_seconds FROM voucher_redemptions r
		JOIN vouchers v ON v.id = r.voucher_id
		WHERE v.code = 'IT-DEACT-A' AND r.user_id = $1`, op.ID).Scan(&aRem); err != nil {
		t.Fatalf("fixture: read paused A remaining: %v", err)
	}
	if aRem <= 0 {
		t.Fatalf("fixture: paused A must still have remaining lifetime, got %d", aRem)
	}

	root := createRootUser(t, pool, "root-deact-2")
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/deactivate-package", map[string]interface{}{})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("deactivate package: status=%d resp=%+v", status, resp)
	}

	after := mustGetUser(t, pool, "op-deact-2")
	if !models.HasRole(after.Role, models.RoleOperator) {
		t.Errorf("role = %q — the fallback school package must re-grant operator", after.Role)
	}
	if after.Package != "sekolah-test" {
		t.Errorf("package = %q, want sekolah-test (the fallback)", after.Package)
	}

	// A active again; B burned (inactive, zeroed).
	var aActive, bActive bool
	var bRem int64
	if err := pool.QueryRow(ctx, `
		SELECT r.is_active FROM voucher_redemptions r
		JOIN vouchers v ON v.id = r.voucher_id
		WHERE v.code = 'IT-DEACT-A' AND r.user_id = $1`, op.ID).Scan(&aActive); err != nil {
		t.Fatalf("read A active: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT r.is_active, COALESCE(r.remaining_seconds, 0) FROM voucher_redemptions r
		JOIN vouchers v ON v.id = r.voucher_id
		WHERE v.code = 'IT-DEACT-B' AND r.user_id = $1`, op.ID).Scan(&bActive, &bRem); err != nil {
		t.Fatalf("read B state: %v", err)
	}
	if !aActive {
		t.Error("fallback A must be active after deactivation")
	}
	if bActive {
		t.Error("deactivated package B must not be active")
	}
	if bRem != 0 {
		t.Errorf("burned B remaining_seconds = %d, want 0", bRem)
	}
}

// TestDeactivatePackageGuards locks the rejection paths: no active package →
// 400, suspended target → 400 (a suspended account must not silently receive a
// fresh trial), unknown target → 404.
func TestDeactivatePackageGuards(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	root := createRootUser(t, pool, "root-deact-g")
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	// (a) Account without any active package → 400.
	plain := createOperatorUser(t, pool, "guru-deact-none", "SMK None", "pass-x")
	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(plain.ID)+"/deactivate-package", map[string]interface{}{})
	if status != http.StatusBadRequest {
		t.Errorf("no-active-package: status=%d resp=%+v, want 400", status, resp)
	}
	if !strings.Contains(resp.Message, "tidak memiliki paket aktif") {
		t.Errorf("no-active-package message = %q", resp.Message)
	}

	// (b) Suspended target → 400.
	createSchoolVoucherCode(t, pool, "IT-DEACT-S")
	op := createOperatorUser(t, pool, "op-deact-susp", "SMK Susp", "pass-x")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-DEACT-S")
	if _, err := pool.Exec(ctx, `UPDATE admin_users SET status = 'suspended' WHERE id = $1`, op.ID); err != nil {
		t.Fatalf("suspend target: %v", err)
	}
	status, resp = postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/deactivate-package", map[string]interface{}{})
	if status != http.StatusBadRequest {
		t.Errorf("suspended target: status=%d resp=%+v, want 400", status, resp)
	}
	if !strings.Contains(resp.Message, "suspended") {
		t.Errorf("suspended target message = %q", resp.Message)
	}
	// The suspended account's package must be untouched.
	var active int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM voucher_redemptions WHERE user_id=$1 AND is_active`, op.ID).Scan(&active); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 1 {
		t.Errorf("active redemptions after rejected deactivate = %d, want 1", active)
	}

	// (c) Unknown target → 404.
	status, resp = postJSON(t, rt.client, rt.srv, "/api/users/999999/deactivate-package", map[string]interface{}{})
	if status != http.StatusNotFound {
		t.Errorf("unknown target: status=%d, want 404", status)
	}
}

// TestDeactivatePackageSuperAdminOnly locks the authorization boundary: an
// operator (even one with a school package of its own) must get 403 — manual
// package deactivation is strictly more powerful than package assignment,
// which is already SuperAdmin-only. The target package stays untouched.
func TestDeactivatePackageSuperAdminOnly(t *testing.T) {
	pool := setupVoucherITDB(t)

	createSchoolVoucherCode(t, pool, "IT-DEACT-OP")
	op := createOperatorUser(t, pool, "op-deact-owner", "SMK Owner", "pass-x")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-DEACT-OP")

	op2 := createOperatorUser(t, pool, "op-deact-intruder", "personal", "pass-x")
	tc2 := newVoucherTestClient(t, pool)
	tc2.login(t, op2.ID)

	status, resp := postJSON(t, tc2.client, tc2.srv, "/api/users/"+strconv.Itoa(op.ID)+"/deactivate-package", map[string]interface{}{})
	if status != http.StatusForbidden {
		t.Errorf("operator caller: status=%d resp=%+v, want 403", status, resp)
	}
	var active int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM voucher_redemptions WHERE user_id=$1 AND is_active`, op.ID).Scan(&active); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 1 {
		t.Errorf("active redemptions after rejected 403 = %d, want 1 (untouched)", active)
	}
}

// TestDeactivatePackageCascadesToSubAccounts locks the school cascade: an
// operator of a real school with sub-accounts is deactivated with no fallback
// and no other operator covering the school → the operator loses the role and
// the sub-accounts are cascade-suspended (suspended_by_cascade), exactly as a
// natural package loss behaves.
func TestDeactivatePackageCascadesToSubAccounts(t *testing.T) {
	pool := setupVoucherITDB(t)

	createSchoolVoucherCode(t, pool, "IT-DEACT-C")
	op := createOperatorUser(t, pool, "op-deact-c", "SMK Deact C", "pass-op-deact-c")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-DEACT-C")
	for _, u := range []string{"sub-deact-c-1", "sub-deact-c-2"} {
		if status, resp := tc.createUser(t, u); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", u, status, resp)
		}
	}

	root := createRootUser(t, pool, "root-deact-c")
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/deactivate-package", map[string]interface{}{})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("deactivate package: status=%d resp=%+v", status, resp)
	}

	opAfter := mustGetUser(t, pool, "op-deact-c")
	if models.HasRole(opAfter.Role, models.RoleOperator) {
		t.Errorf("operator role must be clawed back, got %q", opAfter.Role)
	}
	for _, u := range []string{"sub-deact-c-1", "sub-deact-c-2"} {
		sub := mustGetUser(t, pool, u)
		st, cascade, _ := subFlags(t, pool, sub.ID)
		if st != models.UserStatusSuspended || !cascade {
			t.Errorf("sub %s: status=%s cascade=%v, want suspended+cascade", u, st, cascade)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// createRootUser creates a SuperAdmin account (the actor for deactivation).
func createRootUser(t *testing.T, pool *pgxpool.Pool, username string) models.AdminUser {
	t.Helper()
	root, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: username, Name: username,
		PasswordHash: "pass-" + username, Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin %s: %v", username, err)
	}
	return *root
}

// listUsersActivePackageFlag fetches the Kelola User list as the logged-in
// client and returns whether the named account appears and its
// has_active_package flag.
func listUsersActivePackageFlag(t *testing.T, client *http.Client, srv *httptest.Server, username string) (found, active bool) {
	t.Helper()
	resp, err := client.Get(srv.URL + "/api/users?page=1&per_page=100")
	if err != nil {
		t.Fatalf("GET /api/users: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Success bool `json:"success"`
		Users   []struct {
			Username         string `json:"username"`
			HasActivePackage bool   `json:"has_active_package"`
		} `json:"users"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal users list: %v — body=%s", err, body)
	}
	for _, u := range out.Users {
		if u.Username == username {
			return true, u.HasActivePackage
		}
	}
	return false, false
}
