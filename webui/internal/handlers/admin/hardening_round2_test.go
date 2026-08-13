package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Round-2 hardening tests (test-first for the review findings):
//
//  1. AuthRequired revalidates role / superadmin / instansi from the DB on
//     every request — a demoted account loses its old powers immediately,
//     even though the cookie session is still valid
//     (TestAuthRevalidatesOperatorRole, TestAuthRevalidatesSuperadminLock).
//  2. ListUsers fails CLOSED when an operator's instansi cannot be resolved —
//     an anomalous account must never silently see every instansi
//     (TestListUsersOperatorFailsClosedOnUnknownInstansi).
//  3. DeleteUser rejects operator targets when the actor is an operator —
//     deleting a peer operator would otherwise cascade-wipe the whole school
//     (TestDeleteUserRejectsOperatorPeer).
//  4. ToggleUserStatus serializes concurrent toggles on the locked account
//     row — the reactivation freeze can never be applied twice with the same
//     suspended_at (TestToggleStatusSerializesConcurrentToggles).
//  5. The renewal (expired-account) reactivation clears the
//     suspended_by_cascade marker — a later cascade restore must not
//     re-suspend an account an admin explicitly revived
//     (TestToggleRenewalClearsCascadeMarker).
//  6. EditUser activation (with an explicit expiry) clears the suspension
//     markers left by a previous suspension — no dangling markers, no
//     double-freeze later (TestEditUserActivationClearsSuspendMarkers).
//  7. Voucher custom duration is capped — absurd day counts can no longer
//     produce a poisoned duration that overflows the redemption arithmetic
//     (TestVoucherCustomDaysBoundRejected).
//  8. Voucher custom quota MB inputs are capped — a huge float64 can no
//     longer silently convert into a negative int64 quota
//     (TestVoucherCustomQuotaOverflowRejected).
//  9. The expiry reconciliation pass runs under a PostgreSQL advisory lock —
//     a second replica/instance never runs a concurrent pass
//     (TestExpiryJobPassSkipsWhenLocked).
// 10. Exam R2 object names are unique per call — two uploads in the same
//     second can no longer overwrite each other (TestExamObjectNameUnique).
// ---------------------------------------------------------------------------

// TestAuthRevalidatesOperatorRole locks in per-request role revalidation: an
// operator account DEMOTED in the database (role written back to plain guru)
// must lose operator powers on the very next request — the still-valid cookie
// session alone must not keep granting AdminManagementRequired access. Before
// the fix the session's role was trusted for the whole session lifetime, so a
// demoted operator kept managing users until the session expired (1 day).
func TestAuthRevalidatesOperatorRole(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-demote", "SMK Demote", "pass-op-demote")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH") // grants the operator role
	opAfterRedeem := mustGetUser(t, pool, "op-demote")
	if !opAfterRedeem.IsOperator() {
		t.Fatal("fixture: operator must hold the operator role after redeem")
	}

	// Sanity: while the role is operator, the /api/users list is reachable.
	if status, _ := getAPIJSON(t, tc.client, tc.srv, "/api/users"); status != http.StatusOK {
		t.Fatalf("fixture: /api/users before demotion = %d, want 200", status)
	}

	// Demote in the DB: operator role stripped, session untouched.
	if _, err := pool.Exec(ctx, `UPDATE admin_users SET role = '["guru"]' WHERE id = $1`, op.ID); err != nil {
		t.Fatalf("demote operator role: %v", err)
	}

	// Next request must be refused: the cookie still says operator, but the
	// DB row is now a plain guru, so AdminManagementRequired must fail.
	status, _ := getAPIJSON(t, tc.client, tc.srv, "/api/users")
	if status != http.StatusForbidden {
		t.Errorf("/api/users after DB demotion = %d, want 403 (stale session must not keep operator powers)", status)
	}
}

// TestAuthRevalidatesSuperadminLock locks in per-request revalidation of the
// superadmin flag — the same mechanism that exempts the platform owner from
// the feature lock. A superadmin account demoted in the DB (role downgraded)
// AND expired must become feature-locked on the next request: the session's
// stale is_super_admin=true must not keep it exempt (and its expiry must not
// be ignored anymore).
func TestAuthRevalidatesSuperadminLock(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-reval", Name: "Root Reval",
		PasswordHash: "pass-root-reval", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	// Sanity: superadmin passes the feature-gated probe.
	if status, _ := getAPIJSON(t, tc.client, tc.srv, "/api/auth-ping"); status != http.StatusOK {
		t.Fatalf("fixture: auth-ping before demotion = %d, want 200", status)
	}

	// Demote + expire in the DB: role downgraded, expiry in the past.
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET role = '["guru"]', expires_at = now() - interval '1 second' WHERE id = $1`,
		root.ID); err != nil {
		t.Fatalf("demote superadmin: %v", err)
	}

	// The stale session says superadmin (feature-lock exempt); the DB row is
	// now a plain expired guru → the request must be feature-locked (403).
	status, _ := getAPIJSON(t, tc.client, tc.srv, "/api/auth-ping")
	if status != http.StatusForbidden {
		t.Errorf("auth-ping after DB demotion = %d, want 403 (stale is_super_admin must not keep the feature-lock exemption)", status)
	}
}

// TestListUsersOperatorFailsClosedOnUnknownInstansi locks in the fail-CLOSED
// behavior of the operator user list: when the operator's instansi cannot be
// resolved (DB error or an anomalous empty instansi row), the list must NOT
// silently fall back to "no instansi filter" — that would return every
// account of every tenant. Before the fix getInstansiForOperator returned ""
// on error, ListUsers skipped the instansi filter, and a broken operator
// session could read the whole platform's user list.
func TestListUsersOperatorFailsClosedOnUnknownInstansi(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-2")
	op := createOperatorUser(t, pool, "op-empty", "SMK Empty", "pass-op-empty")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH") // grants the operator role
	opAfterRedeem := mustGetUser(t, pool, "op-empty")
	if !opAfterRedeem.IsOperator() {
		t.Fatal("fixture: operator must hold the operator role after redeem")
	}

	// Sanity: while the instansi is intact, the scoped list works.
	if status, _ := getAPIJSON(t, tc.client, tc.srv, "/api/users"); status != http.StatusOK {
		t.Fatalf("fixture: /api/users before instansi wipe = %d, want 200", status)
	}

	// Anomalous row: operator instansi wiped (legacy/corrupt state). A normal
	// operator always has an instansi, so this is never a discardable case.
	if _, err := pool.Exec(ctx, `UPDATE admin_users SET instansi = '' WHERE id = $1`, op.ID); err != nil {
		t.Fatalf("wipe operator instansi: %v", err)
	}

	status, _ := getAPIJSON(t, tc.client, tc.srv, "/api/users")
	if status != http.StatusInternalServerError {
		t.Errorf("/api/users with unresolved operator instansi = %d, want 500 (must fail closed, never list cross-tenant users)", status)
	}

	// Contrast: a healthy operator (concrete instansi) still gets its own
	// filtered list.
	op2 := createOperatorUser(t, pool, "op-ok", "SMK Ok", "pass-op-ok")
	tc2 := newVoucherTestClient(t, pool)
	tc2.login(t, op2.ID)
	tc2.redeem(t, "IT-SEKOLAH-2")
	if status, _ := getAPIJSON(t, tc2.client, tc2.srv, "/api/users"); status != http.StatusOK {
		t.Errorf("/api/users with resolvable instansi = %d, want 200", status)
	}
}

// TestDeleteUserRejectsOperatorPeer locks in the missing guard on DeleteUser:
// an operator must never delete a PEER operator of the same instansi — the
// delete of an operator account cascades over its whole instansi (every
// sub-account and exam goes with it), so a peer deletion would wipe the
// entire school, including the acting operator themself. EditUser /
// ToggleUserStatus / VerifyUser already reject operator targets; DeleteUser
// did not. Deleting a plain sub-account stays allowed.
func TestDeleteUserRejectsOperatorPeer(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-del", "SMK Del", "pass-op-del")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	// Sanity: the redeem granted the operator role (the peer-operator guard
	// relies on the target's ROLE, not on the actor's view of itself).
	opAfter := mustGetUser(t, pool, "op-del")
	if !opAfter.IsOperator() {
		t.Fatal("fixture: operator must hold the operator role after redeem")
	}

	// Peer operator in the SAME instansi (role set directly, mirroring an
	// admin-granted operator account).
	peer := createOperatorUser(t, pool, "peer-del", "SMK Del", "pass-peer-del")
	if _, err := pool.Exec(ctx, `UPDATE admin_users SET role = '["guru","operator"]' WHERE id = $1`, peer.ID); err != nil {
		t.Fatalf("grant peer operator role: %v", err)
	}
	peerAfter := mustGetUser(t, pool, "peer-del")
	if !peerAfter.IsOperator() {
		t.Fatal("fixture: peer must hold the operator role")
	}

	// A sub-account in the same instansi (created by the acting operator).
	if status, resp := tc.createUser(t, "sub-del1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-del1: status=%d resp=%+v", status, resp)
	}

	// Deleting the PEER operator must be refused — before the fix this
	// returned 200 and cascade-deleted the whole instansi (peer, sub-account,
	// and even the acting operator itself).
	status, _ := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(peer.ID)+"/delete", nil)
	if status != http.StatusBadRequest {
		t.Errorf("operator delete peer operator = %d, want 400", status)
	}
	for _, username := range []string{"op-del", "peer-del", "sub-del1"} {
		if !userExists(pool, username) {
			t.Errorf("user %q was deleted — the peer-operator delete must not cascade the instansi", username)
		}
	}

	// Deleting a PLAIN sub-account remains allowed: only the target goes away.
	sub := mustGetUser(t, pool, "sub-del1")
	status, _ = postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub.ID)+"/delete", nil)
	if status != http.StatusOK {
		t.Fatalf("operator delete sub-account = %d, want 200", status)
	}
	if userExists(pool, "sub-del1") {
		t.Error("sub-account survived the allowed delete")
	}
}

// TestToggleStatusSerializesConcurrentToggles locks in the atomicity of
// ToggleUserStatus: the account row is locked FOR UPDATE inside one
// transaction, so concurrent toggles serialize on the LATEST committed state
// instead of deciding from a stale read. The specific hazard: two concurrent
// reactivations of the same suspended account both applied the suspension
// freeze with the SAME suspended_at — extending expires_at TWICE (double
// freeze). Under the lock the second toggle sees the reactivated account and
// suspends it again (2 toggles = activate then suspend → final suspended,
// expiry frozen exactly once).
func TestToggleStatusSerializesConcurrentToggles(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	expiry := now.Add(30 * 24 * time.Hour)
	target, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "race-sus", Name: "Race Sus",
		PasswordHash: "pass-race-sus", Status: models.UserStatusActive,
		Role:      models.SerializeRoles([]string{models.RoleGuru}),
		Instansi:  "SMK Race",
		ExpiresAt: &expiry,
	})
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}

	// Suspended state, suspended_at pinned 10s in the past, cascade marker set
	// (mirrors a school-operator cascade suspension).
	suspendedAt := now.Add(-10 * time.Second)
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET status = 'suspended', suspended_at = $1, suspended_by_cascade = TRUE WHERE id = $2`,
		suspendedAt, target.ID); err != nil {
		t.Fatalf("prepare suspended target: %v", err)
	}

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-race", Name: "Root Race",
		PasswordHash: "pass-root-race", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	// Two concurrent toggle requests (double-click).
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			statuses[i], _ = postJSON(t, tc.client, tc.srv,
				"/api/users/"+strconv.Itoa(target.ID)+"/toggle-status", nil)
		}(i)
	}
	wg.Wait()
	for i, s := range statuses {
		if s != http.StatusOK {
			t.Errorf("concurrent toggle %d = %d, want 200", i, s)
		}
	}

	// Serialized outcome of two toggles: activate (with one freeze), then
	// suspend again → final state SUSPENDED with a CLEAN marker.
	var status string
	var marker bool
	var exp, susAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT status, suspended_by_cascade, expires_at, suspended_at FROM admin_users WHERE id = $1`,
		target.ID).Scan(&status, &marker, &exp, &susAt); err != nil {
		t.Fatalf("read final state: %v", err)
	}
	if status != models.UserStatusSuspended {
		t.Fatalf("final status = %q, want suspended (2 serialized toggles: activate then suspend); batch had %v", status, statuses)
	}
	if marker {
		t.Error("suspended_by_cascade = true, want false (manual reactivation must clear the marker)")
	}
	if susAt == nil {
		t.Error("suspended_at = NULL, want set (the second toggle re-suspended the account)")
	}

	// The freeze must have been applied EXACTLY ONCE: the final expiry can
	// reach at most original + (time since suspended_at); a double freeze
	// would overshoot that bound.
	if exp != nil {
		bound := time.Now().UTC().Sub(suspendedAt) + 5*time.Second
		if gained := exp.Sub(*target.ExpiresAt); gained > bound {
			t.Errorf("expiry gained %s, want <= %s — the suspension freeze was applied more than once", gained, bound)
		}
	}
}

// TestToggleRenewalClearsCascadeMarker locks in the marker cleanup of the
// renewal branch: reactivating a SUSPENDED-EXPIRED account (renewal grants a
// fresh default_active_days period) must clear suspended_by_cascade — the
// marker exists only to let a later school-restore re-suspend cascade
// accounts, and a manually revived account must never be re-suspended by a
// stale marker (it would be immediately re-suspended by the next restore).
func TestToggleRenewalClearsCascadeMarker(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	target, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "renew-sus", Name: "Renew Sus",
		PasswordHash: "pass-renew-sus", Status: models.UserStatusActive,
		Role:     models.SerializeRoles([]string{models.RoleGuru}),
		Instansi: "SMK Renew",
	})
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}
	// Suspended-EXPIRED account carrying the cascade marker.
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET status = 'suspended', suspended_by_cascade = TRUE,
		        suspended_at = now() - interval '10 seconds', expires_at = now() - interval '1 day'
		 WHERE id = $1`, target.ID); err != nil {
		t.Fatalf("prepare suspended-expired target: %v", err)
	}

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-renew", Name: "Root Renew",
		PasswordHash: "pass-root-renew", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(target.ID)+"/toggle-status", nil)
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("toggle reactivate: status=%d resp=%+v", status, resp)
	}

	var dbStatus string
	var marker bool
	var susAt *time.Time
	var exp *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT status, suspended_by_cascade, suspended_at, expires_at FROM admin_users WHERE id = $1`,
		target.ID).Scan(&dbStatus, &marker, &susAt, &exp); err != nil {
		t.Fatalf("read final state: %v", err)
	}
	if dbStatus != models.UserStatusActive {
		t.Errorf("final status = %q, want active", dbStatus)
	}
	if marker {
		t.Error("suspended_by_cascade = true, want false (renewal reactivation must clear the marker)")
	}
	if susAt != nil {
		t.Errorf("suspended_at = %v, want NULL", susAt)
	}
	if exp == nil || !exp.After(now) {
		t.Errorf("expires_at = %v, want a future renewal expiry", exp)
	}
}

// TestEditUserActivationClearsSuspendMarkers locks in the marker cleanup of
// the EditUser activation path: when an admin sets status=active with an
// EXPLICIT expiry (the freeze path is skipped because a fresh expiry
// supersedes it), the suspension markers must still be cleared. Before the
// fix the markers were left dangling — a later cascade restore would have
// re-suspended the explicitly revived account.
func TestEditUserActivationClearsSuspendMarkers(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	expiry := now.Add(30 * 24 * time.Hour)
	target, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "edit-sus", Name: "Edit Sus",
		PasswordHash: "pass-edit-sus", Status: models.UserStatusActive,
		Role:      models.SerializeRoles([]string{models.RoleGuru}),
		Instansi:  "SMK Edit",
		ExpiresAt: &expiry,
	})
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET status = 'suspended', suspended_by_cascade = TRUE,
		        suspended_at = now() - interval '10 seconds'
		 WHERE id = $1`, target.ID); err != nil {
		t.Fatalf("prepare suspended target: %v", err)
	}

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-edit", Name: "Root Edit",
		PasswordHash: "pass-root-edit", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	// Explicit reactivation WITH a fresh expiry (freeze path skipped).
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(target.ID)+"/edit",
		map[string]interface{}{"status": "active", "expires_at": "2030-01-01"})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("edit reactivate: status=%d resp=%+v", status, resp)
	}

	var dbStatus string
	var marker bool
	var susAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT status, suspended_by_cascade, suspended_at FROM admin_users WHERE id = $1`,
		target.ID).Scan(&dbStatus, &marker, &susAt); err != nil {
		t.Fatalf("read final state: %v", err)
	}
	if dbStatus != models.UserStatusActive {
		t.Errorf("final status = %q, want active", dbStatus)
	}
	if marker {
		t.Error("suspended_by_cascade = true, want false (explicit activation must clear the marker)")
	}
	if susAt != nil {
		t.Errorf("suspended_at = %v, want NULL (explicit activation must clear the marker)", susAt)
	}
}

// TestVoucherCustomDaysBoundRejected locks in the custom duration cap: an
// absurd-but-parseable day count (e.g. 999.999.999 hari) must be rejected
// instead of writing a poisoned duration into the voucher. The day count is
// multiplied into seconds at redemption time (durationDays × 86400) and later
// converted into a time.Duration in the entitlement math — values beyond the
// duration range would wrap the expiry computation.
func TestVoucherCustomDaysBoundRejected(t *testing.T) {
	pool := setupVoucherITDB(t)

	root, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: "root-vb1", Name: "Root VB1",
		PasswordHash: "pass-root-vb1", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	// Huge but valid integer → must be rejected by the bound.
	status, resp := postForm(t, tc.client, tc.srv, "/api/vouchers", map[string][]string{
		"code":          {"V-BOUNDS-1"},
		"package":       {"guru"},
		"duration_type": {"custom"},
		"custom_days":   {"999999999"},
	})
	if status != http.StatusBadRequest {
		t.Errorf("custom_days=999999999 = %d, want 400 (absurd duration must be rejected), resp=%+v", status, resp)
	}

	// A sane custom duration still works and lands as a day count.
	status, _ = postForm(t, tc.client, tc.srv, "/api/vouchers", map[string][]string{
		"code":          {"V-BOUNDS-2"},
		"package":       {"guru"},
		"duration_type": {"custom"},
		"custom_days":   {"100"},
	})
	if status != http.StatusOK {
		t.Fatalf("custom_days=100 = %d, want 200", status)
	}
	var durationType string
	if err := pool.QueryRow(context.Background(),
		`SELECT duration_type FROM vouchers WHERE code = 'V-BOUNDS-2'`).Scan(&durationType); err != nil {
		t.Fatalf("read created voucher: %v", err)
	}
	if durationType != "100" {
		t.Errorf("created voucher duration_type = %q, want %q", durationType, "100")
	}
}

// TestVoucherCustomQuotaOverflowRejected locks in the hard cap on the MB
// quota inputs of a custom voucher: a huge float64 (e.g. 1e15 MB) must be
// rejected by an explicit ceiling instead of flowing into
// int64(mb*1024*1024), where an out-of-range conversion would silently
// produce a NEGATIVE quota (MinInt64) — poisoning every account that later
// redeemed the voucher. The ceiling applies before and independently of the
// free-disk check (which fails open when the disk cannot be determined).
func TestVoucherCustomQuotaOverflowRejected(t *testing.T) {
	pool := setupVoucherITDB(t)

	root, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: "root-vb2", Name: "Root VB2",
		PasswordHash: "pass-root-vb2", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	status, resp := postForm(t, tc.client, tc.srv, "/api/vouchers", map[string][]string{
		"code":                   {"V-BOUNDS-3"},
		"package":                {"custom"},
		"duration_type":          {"bulanan"},
		"custom_label":           {"quota-bounds"},
		"custom_role":            {"guru"},
		"custom_max_pdf_size_mb": {"1e15"},
	})
	if status != http.StatusBadRequest {
		t.Errorf("custom_max_pdf_size_mb=1e15 = %d, want 400 (out-of-range quota must be rejected), resp=%+v", status, resp)
	}
}

// TestExpiryJobPassSkipsWhenLocked locks in the advisory-lock guard of the
// expiry reconciliation pass: only ONE pass runs at a time across the whole
// cluster. A second call (another replica, a double-started job) must skip
// immediately while the lock is held — two concurrent passes would each
// select the same exhausted packages and fight over the redemption rows.
func TestExpiryJobPassSkipsWhenLocked(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-joblock", "SMK JobLock", "pass-op-joblock")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	expireActivePackage(t, pool, op.ID) // active redemption is now exhausted

	var redemptionID int
	var isActive bool
	if err := pool.QueryRow(ctx,
		`SELECT id, is_active FROM voucher_redemptions WHERE user_id = $1 AND is_active`,
		op.ID).Scan(&redemptionID, &isActive); err != nil {
		t.Fatalf("fixture: read active redemption: %v", err)
	}

	// Hold the advisory lock on a DEDICATED connection (advisory locks are
	// per-backend-session — the pass takes its lock on another pooled
	// backend, which is exactly the cross-replica scenario), then run the
	// pass: it must skip.
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("fixture: acquire lock connection: %v", err)
	}
	defer lockConn.Release()
	var got bool
	if err := lockConn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, expiryJobAdvisoryLockKey).Scan(&got); err != nil || !got {
		t.Fatalf("fixture: acquire advisory lock: got=%v err=%v", got, err)
	}

	if acquired := runPackageExpiryPass(ctx, pool); acquired {
		t.Fatal("runPackageExpiryPass acquired the lock while it was held — pass must skip")
	}
	var isActiveAfterSkip bool
	if err := pool.QueryRow(ctx,
		`SELECT is_active FROM voucher_redemptions WHERE id = $1`, redemptionID).Scan(&isActiveAfterSkip); err != nil {
		t.Fatalf("read redemption after skipped pass: %v", err)
	}
	if !isActiveAfterSkip {
		t.Error("skipped pass still reconciled the redemption — the pass must not run under a held lock")
	}

	// Release the lock on the same dedicated connection: the next pass runs
	// and reconciles the package.
	var released bool
	if err := lockConn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, expiryJobAdvisoryLockKey).Scan(&released); err != nil || !released {
		t.Fatalf("release advisory lock: released=%v err=%v", released, err)
	}
	if acquired := runPackageExpiryPass(ctx, pool); !acquired {
		t.Fatal("runPackageExpiryPass did not acquire the lock after release")
	}
	var isActiveAfterPass bool
	if err := pool.QueryRow(ctx,
		`SELECT is_active FROM voucher_redemptions WHERE id = $1`, redemptionID).Scan(&isActiveAfterPass); err != nil {
		t.Fatalf("read redemption after pass: %v", err)
	}
	if isActiveAfterPass {
		t.Error("redemption still active after the unlocked pass — the exhausted package must be paused")
	}
}

// TestExamObjectNameUnique locks in object-name uniqueness: two PDFs uploaded
// in the SAME second with the SAME original filename must produce different
// object names. Before the fix the name was timestamp + sanitized filename —
// the second upload overwrote the first object in R2.
func TestExamObjectNameUnique(t *testing.T) {
	ts := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	a := examObjectNameAt(ts, "ujian.pdf")
	b := examObjectNameAt(ts, "ujian.pdf")

	if a == b {
		t.Fatalf("two uploads of ujian.pdf in the same second produced the same object name %q — collision must be impossible", a)
	}

	// Shape stays compatible with the R2 "pdfs/<name>" layout and the
	// timestamp prefix used for human-readable ordering.
	for _, n := range []string{a, b} {
		if !strings.HasPrefix(n, "20260814_120000_") {
			t.Errorf("object name %q lost the timestamp prefix", n)
		}
		if !strings.HasSuffix(n, "_ujian.pdf") {
			t.Errorf("object name %q lost the sanitized filename suffix", n)
		}
		if strings.Contains(n, "/") || strings.Contains(n, " ") {
			t.Errorf("object name %q contains path separators or spaces", n)
		}
	}

	// Different filenames must stay distinguishable.
	c := examObjectNameAt(ts, "ujian2.pdf")
	if c == a || c == b {
		t.Errorf("different filenames collided: %q vs %q vs %q", a, b, c)
	}
}

// ---------------------------------------------------------------------------
// Small local helpers (kept local to this file).
// ---------------------------------------------------------------------------

// getAPIJSON performs an AJAX-style GET (X-Requested-With set, so the
// middlewares take the 403-JSON branch instead of the HTML redirect branch).
func getAPIJSON(t *testing.T, client *http.Client, srv *httptest.Server, path string) (int, apiResp) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out apiResp
	_ = json.Unmarshal(body, &out)
	out.Body = string(body)
	return resp.StatusCode, out
}

// userExists reports whether an admin_users row with the username exists.
func userExists(pool *pgxpool.Pool, username string) bool {
	_, err := models.GetUserByUsername(context.Background(), pool, username)
	return err == nil
}
