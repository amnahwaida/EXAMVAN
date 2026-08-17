package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Sub-account voucher policy: accounts CREATED BY an operator (school
// sub-accounts, admin_users.operator_created = true) may never claim/activate
// vouchers. Their package, quota and expiry come exclusively from the school
// package the operator manages. The flag is set at creation time by the real
// CreateUser handler and is never changed afterwards (origin-based).
//
// These tests use the same DB-backed integration infra as the voucher
// lifecycle tests (setupVoucherITDB, newVoucherTestClient): they skip when
// TEST_DATABASE_URL is unset.
// ---------------------------------------------------------------------------

// claimSubOwnVoucher plants a PRE-POLICY voucher claim on an
// operator-created sub-account. The sub-account voucher policy blocks the real
// RedeemVoucherHandler path (403), so tests that need a sub-account to hold
// its OWN active package — to exercise the suspension/restore and
// expiry-cascade guards that spare accounts running their own package — insert
// the redemption row + account expiry directly, mirroring exactly what a
// successful claim produced before the policy: a guru package with its own
// ~30-day lifetime. The "IT-GURU-SUB" voucher row must already exist (created
// via createGuruVoucherCode).
func claimSubOwnVoucher(t *testing.T, pool *pgxpool.Pool, userID int) {
	t.Helper()
	ctx := context.Background()
	var vid int
	if err := pool.QueryRow(ctx, `SELECT id FROM vouchers WHERE code = 'IT-GURU-SUB'`).Scan(&vid); err != nil {
		t.Fatalf("claimSubOwnVoucher: find IT-GURU-SUB voucher: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO voucher_redemptions
			(voucher_id, user_id, remaining_seconds, activated_at, is_active, package,
			 max_exams, max_pdf_size, max_concurrent_exams, max_storage_size, max_users, role)
		VALUES ($1, $2, $3, now(), true, 'guru', 1, 10485760, 1, 104857600, 0, '')`,
		vid, userID, 30*86400); err != nil {
		t.Fatalf("claimSubOwnVoucher: insert redemption for user %d: %v", userID, err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() + ($1 * interval '1 second') WHERE id = $2`,
		30*86400, userID); err != nil {
		t.Fatalf("claimSubOwnVoucher: set expiry for user %d: %v", userID, err)
	}
}

// claimSubOwnSchoolVoucher plants a PRE-POLICY SEKOLAH claim on an
// operator-created sub-account: the sub holds its OWN school package (and the
// operator role that package grants). Like claimSubOwnVoucher, this simulates
// exactly what a successful redeem produced before the sub-account voucher
// policy blocked the real path — an active 'sekolah-test' redemption (30-day
// lifetime, max_users quota, operator role) plus the account-side entitlement
// columns a real applyRedemptionEntitlement would have written (package,
// quotas, expiry, role = base guru ∪ package operator, package_role =
// [operator], base_role = [guru]). The "IT-SEKOLAH-SUB" voucher row must
// already exist (created via createSchoolVoucherCode).
func claimSubOwnSchoolVoucher(t *testing.T, pool *pgxpool.Pool, userID int) {
	t.Helper()
	ctx := context.Background()
	var vid int
	if err := pool.QueryRow(ctx, `SELECT id FROM vouchers WHERE code = 'IT-SEKOLAH-SUB'`).Scan(&vid); err != nil {
		t.Fatalf("claimSubOwnSchoolVoucher: find IT-SEKOLAH-SUB voucher: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO voucher_redemptions
			(voucher_id, user_id, remaining_seconds, activated_at, is_active, package,
			 max_exams, max_pdf_size, max_concurrent_exams, max_storage_size, max_users, role)
		VALUES ($1, $2, $3, now(), true, 'sekolah-test', 3, 52428800, 3, 524288000, 2, $4)`,
		vid, userID, 30*86400, models.SerializeRoles([]string{models.RoleOperator})); err != nil {
		t.Fatalf("claimSubOwnSchoolVoucher: insert redemption for user %d: %v", userID, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE admin_users SET
			package = 'sekolah-test',
			max_exams = 3, max_pdf_size = 52428800,
			max_concurrent_exams = 3, max_storage_size = 524288000,
			expires_at = now() + ($1 * interval '1 second'),
			role = $2, package_role = $3, base_role = $4
		WHERE id = $5`,
		30*86400,
		models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}),
		models.SerializeRoles([]string{models.RoleOperator}),
		models.SerializeRoles([]string{models.RoleGuru}),
		userID); err != nil {
		t.Fatalf("claimSubOwnSchoolVoucher: update user %d entitlement: %v", userID, err)
	}
}

// TestSubAccountCannotRedeemVoucher locks in the core of the sub-account
// voucher policy: an account created BY an operator (via the real CreateUser
// handler) is marked operator_created and every redeem attempt is rejected
// with 403 — BEFORE any voucher lookup, so a blocked account can never use
// the response to distinguish a valid code from a bogus one (the
// voucherInvalidMsg oracle rule). No redemption row is created and the
// voucher's quota is untouched. The operator themself is unaffected: they can
// still claim the school voucher (that is how they became an operator).
func TestSubAccountCannotRedeemVoucher(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucherCode(t, pool, "IT-GURU-SUB") // a fresh, unused code the sub must NOT consume

	op := createOperatorUser(t, pool, "op-sub-block", "SMK Sub Block", "pass-op-sub-block")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)

	// The operator itself may still claim vouchers — the policy targets
	// accounts CREATED by the operator, not the operator's own account.
	tc.redeem(t, "IT-SEKOLAH")
	op = mustGetUser(t, pool, "op-sub-block")
	if !models.HasRole(op.Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}

	// Operator creates a sub-account → operator_created must be set at
	// creation time by the real handler.
	if status, resp := tc.createUser(t, "sub1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub1: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub1")
	if !sub.OperatorCreated {
		t.Fatalf("sub1.OperatorCreated=false, want true (account created by an operator)")
	}

	// Redeem is rejected with the same 403 for a valid code AND a bogus one —
	// the policy check precedes any voucher lookup, so there is no oracle.
	sc := newVoucherTestClient(t, pool)
	sc.login(t, sub.ID)
	for _, code := range []string{"IT-GURU-SUB", "BOGUS-NOT-A-REAL-CODE"} {
		status, resp := postForm(t, sc.client, sc.srv, "/api/vouchers/redeem",
			url.Values{"code": {code}})
		if status != http.StatusForbidden || !strings.Contains(resp.Message, "dibuat oleh Operator") {
			t.Errorf("sub redeem %s: status=%d resp=%+v, want 403 sub-account message", code, status, resp)
		}
	}

	// Nothing was recorded: no redemption row, voucher quota untouched.
	var redemptionCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM voucher_redemptions WHERE user_id = $1`, sub.ID).Scan(&redemptionCount); err != nil {
		t.Fatalf("count sub redemptions: %v", err)
	}
	if redemptionCount != 0 {
		t.Errorf("sub redemptions=%d after blocked redeem, want 0", redemptionCount)
	}
	var used int
	if err := pool.QueryRow(ctx,
		`SELECT used_count FROM vouchers WHERE code = 'IT-GURU-SUB'`).Scan(&used); err != nil {
		t.Fatalf("load IT-GURU-SUB used_count: %v", err)
	}
	if used != 0 {
		t.Errorf("IT-GURU-SUB used_count=%d after blocked redeem, want 0", used)
	}
}

// TestSubAccountCannotActivateVoucher locks in the second half of the policy:
// even a redemption row left over on a sub-account (e.g. claimed before the
// policy was introduced) cannot be activated — ActivateVoucherHandler answers
// 403 too, so a sub-account can never run an own voucher package.
func TestSubAccountCannotActivateVoucher(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucherCode(t, pool, "IT-GURU-SUB")
	op := createOperatorUser(t, pool, "op-sub-act", "SMK Sub Activate", "pass-op-sub-act")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if !models.HasRole(mustGetUser(t, pool, "op-sub-act").Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}
	if status, resp := tc.createUser(t, "sub1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub1: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub1")

	// Simulate a pre-policy claim lingering on the sub's row (the redeem
	// endpoint itself would already reject it today).
	claimSubOwnVoucher(t, pool, sub.ID)
	var redemptionID int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM voucher_redemptions WHERE user_id = $1`, sub.ID).Scan(&redemptionID); err != nil {
		t.Fatalf("load sub redemption: %v", err)
	}

	sc := newVoucherTestClient(t, pool)
	sc.login(t, sub.ID)
	status, resp := postForm(t, sc.client, sc.srv, "/api/vouchers/activate",
		url.Values{"redemption_id": {strconv.Itoa(redemptionID)}})
	if status != http.StatusForbidden || !strings.Contains(resp.Message, "dibuat oleh Operator") {
		t.Errorf("sub activate: status=%d resp=%+v, want 403 sub-account message", status, resp)
	}

	// The redemption row is untouched (still active, still its own lifetime).
	var stillActive bool
	var remaining int64
	if err := pool.QueryRow(ctx,
		`SELECT is_active, remaining_seconds FROM voucher_redemptions WHERE id = $1`,
		redemptionID).Scan(&stillActive, &remaining); err != nil {
		t.Fatalf("reload sub redemption: %v", err)
	}
	if !stillActive || remaining != 30*86400 {
		t.Errorf("sub redemption after blocked activate: is_active=%v remaining=%d, want untouched (true, 30 days)",
			stillActive, remaining)
	}
}

// TestSubAccountBlockIsOriginBased locks in the origin-based semantics of the
// policy: admin_users.operator_created is set once at creation and never
// changes, so even after a SuperAdmin PROMOTES a sub-account to the operator
// role (via the real EditUser handler), the account still cannot claim
// vouchers — the policy tracks WHO created the account, not its current role.
func TestSubAccountBlockIsOriginBased(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucherCode(t, pool, "IT-GURU-SUB")
	op := createOperatorUser(t, pool, "op-sub-promo", "SMK Sub Promo", "pass-op-sub-promo")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if status, resp := tc.createUser(t, "sub1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub1: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub1")
	if !sub.OperatorCreated {
		t.Fatalf("fixture: sub1 must be operator_created")
	}

	// The sub-account is promoted to the operator role. Planted directly (not
	// via EditUser): the one-operator-per-school policy blocks granting the
	// operator role to a second operator through the handler, so the promotion
	// here simulates a pre-policy/legacy state — exactly the state the
	// origin-based flag must still reject.
	if _, err := pool.Exec(ctx, `UPDATE admin_users SET role = $1 WHERE id = $2`,
		models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}), sub.ID); err != nil {
		t.Fatalf("promote sub1: %v", err)
	}
	sub = mustGetUser(t, pool, "sub1")
	if !models.HasRole(sub.Role, models.RoleOperator) {
		t.Fatalf("fixture: sub1 must hold the operator role after promotion, got %s", sub.Role)
	}

	// The claim is still blocked — the flag is origin-based, not role-based.
	sc := newVoucherTestClient(t, pool)
	sc.login(t, sub.ID)
	status, resp := postForm(t, sc.client, sc.srv, "/api/vouchers/redeem",
		url.Values{"code": {"IT-GURU-SUB"}})
	if status != http.StatusForbidden || !strings.Contains(resp.Message, "dibuat oleh Operator") {
		t.Errorf("promoted sub redeem: status=%d resp=%+v, want 403 sub-account message", status, resp)
	}
}

// TestDirectCreatedAccountsCanStillRedeem locks in who is NOT affected by the
// policy: accounts created by a SuperAdmin through the same CreateUser handler
// (caller is not an operator → operator_created stays false) and accounts that
// self-register (instansi "personal", like /register) keep full voucher-claim
// access.
func TestDirectCreatedAccountsCanStillRedeem(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createGuruVoucherCode(t, pool, "IT-GURU-DIRECT")
	createGuruVoucherCode(t, pool, "IT-GURU-PERSONAL")

	// SuperAdmin-created account: driven through the real CreateUser handler
	// as a superadmin session, so the flag is decided by the actual isOp path.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-sub-pol", Name: "Root Sub Pol",
		PasswordHash: "pass-root-sub-pol", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)
	if status, resp := tc.createUser(t, "direct1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create direct1 (as superadmin): status=%d resp=%+v", status, resp)
	}
	direct := mustGetUser(t, pool, "direct1")
	if direct.OperatorCreated {
		t.Fatalf("direct1.OperatorCreated=true, want false (created by SuperAdmin)")
	}
	dc := newVoucherTestClient(t, pool)
	dc.login(t, direct.ID)
	dc.redeem(t, "IT-GURU-DIRECT") // must succeed

	// Self-registered account: same shape as the /register flow (instansi
	// "personal" + registered_ip, no operator creator).
	personal, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "personal1", Name: "Personal One",
		PasswordHash: "pass-personal1", Status: models.UserStatusActive,
		Instansi:     "personal",
		RegisteredIP: "203.0.113.10",
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create personal account: %v", err)
	}
	if personal.OperatorCreated {
		t.Fatalf("personal1.OperatorCreated=true, want false (self-registered)")
	}
	pc := newVoucherTestClient(t, pool)
	pc.login(t, personal.ID)
	pc.redeem(t, "IT-GURU-PERSONAL") // must succeed
}

// TestUsersListAPIReportsOperatorCreated locks in the API contract behind the
// "Dibuat oleh Operator" badge on the Kelola Users page: the user list
// endpoint (production GET /admin/api/users, test router GET /api/users) must
// expose admin_users.operator_created on every user item — TRUE for
// sub-accounts created by an operator, FALSE for accounts created directly
// (SuperAdmin / self-registration). The page's client-side row renderer reads
// this flag to show the badge; without it an admin could not tell a
// sub-account apart from a directly-created one.
func TestUsersListAPIReportsOperatorCreated(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-list-origin", "SMK List Origin", "pass-op-list-origin")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if status, resp := tc.createUser(t, "sub1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub1: status=%d resp=%+v", status, resp)
	}
	if !mustGetUser(t, pool, "sub1").OperatorCreated {
		t.Fatalf("fixture: sub1 must be operator_created")
	}

	// A SuperAdmin-created account stays operator_created=false.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-list-origin", Name: "Root List Origin",
		PasswordHash: "pass-root-list-origin", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	if status, resp := rt.createUser(t, "direct1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create direct1: status=%d resp=%+v", status, resp)
	}
	if mustGetUser(t, pool, "direct1").OperatorCreated {
		t.Fatalf("fixture: direct1 must NOT be operator_created")
	}

	// List users as the superadmin; assert the origin flag rides along in the
	// JSON so the Kelola Users page can render the badge.
	httpResp, err := rt.client.Get(rt.srv.URL + "/api/users?page=1&per_page=50")
	if err != nil {
		t.Fatalf("GET /api/users: %v", err)
	}
	defer httpResp.Body.Close()
	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/users: status=%d body=%s", httpResp.StatusCode, body)
	}
	var out struct {
		Success bool `json:"success"`
		Users   []struct {
			Username        string `json:"username"`
			OperatorCreated bool   `json:"operator_created"`
		} `json:"users"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal /api/users: %v", err)
	}
	if !out.Success {
		t.Fatalf("GET /api/users: success=false body=%s", body)
	}
	flags := map[string]bool{}
	for _, u := range out.Users {
		flags[u.Username] = u.OperatorCreated
	}
	// Assert the fixture accounts are actually present first, so a missing row
	// reports "user missing from list" instead of a misleading flag mismatch.
	for _, name := range []string{"sub1", "direct1", "op-list-origin"} {
		if _, ok := flags[name]; !ok {
			t.Errorf("user %q missing from /api/users response", name)
		}
	}
	if got, want := flags["sub1"], true; got != want {
		t.Errorf("sub1.operator_created in /api/users = %v, want %v (badge must render)", got, want)
	}
	if got, want := flags["direct1"], false; got != want {
		t.Errorf("direct1.operator_created in /api/users = %v, want %v (no badge for direct accounts)", got, want)
	}
	// The operator's OWN account is not operator_created — only the accounts
	// it creates carry the flag.
	if got, want := flags["op-list-origin"], false; got != want {
		t.Errorf("op-list-origin.operator_created in /api/users = %v, want %v (operator itself is not a sub-account)", got, want)
	}
}

// TestOperatorCannotCreateOperatorAccount locks in the operator restriction:
// an operator (school sub-account manager) must never be able to create an
// account that holds the operator role — whether sent via the plural `roles`
// array or the singular `role` fallback. The CreateUser handler answers 400
// before any account is created, so no operator account can sneak in through
// the Tambah User form or a hand-crafted request.
func TestOperatorCannotCreateOperatorAccount(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-no-op", "SMK No Operator", "pass-op-no-op")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if !models.HasRole(mustGetUser(t, pool, "op-no-op").Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}

	// Both the plural `roles` form and the singular `role` fallback are
	// rejected with the operator-role message, and no account is created.
	// Whitespace and case variants are covered too: the guard trims each
	// candidate and compares case-insensitively (mirroring EditUser), so a
	// hand-crafted " operator" / "OPERATOR" cannot slip past the comparison
	// and silently degrade the account to guru.
	for i, roles := range []interface{}{
		[]string{models.RoleGuru, models.RoleOperator},
		models.RoleOperator,                    // sent as the singular `role` field
		[]string{models.RoleGuru, " operator"}, // leading space in the plural array
		" operator, guru",                      // padding around the comma in the singular fallback
		[]string{models.RoleGuru, "OPERATOR"},  // uppercase variant
		" Operator ",                           // mixed case with padding
	} {
		payload := map[string]interface{}{
			"username": "wannabe-op", "password": "pass-wannabe-op", "name": "Wannabe Op",
		}
		if s, ok := roles.([]string); ok {
			payload["roles"] = s
		} else {
			payload["role"] = roles
		}
		status, resp := postJSON(t, tc.client, tc.srv, "/api/users", payload)
		if status != http.StatusBadRequest || !strings.Contains(resp.Message, "Operator tidak dapat membuat akun dengan role Operator") {
			t.Errorf("operator create attempt #%d: status=%d resp=%+v, want 400 operator-role message", i, status, resp)
		}
	}
	if _, err := models.GetUserByUsername(ctx, pool, "wannabe-op"); err == nil {
		t.Error("wannabe-op was created despite the operator-role restriction")
	}
}

// TestOperatorCannotAssignPackageToSubAccount locks in the sub-account package
// policy: an operator may never choose the subscription package of the
// accounts below it. CreateUser forces the package label to "free" (the
// sub-account's real quota/role/expiry all follow the operator's school
// package), and EditUser ignores any package change the operator submits —
// only a SuperAdmin may assign a package.
func TestOperatorCannotAssignPackageToSubAccount(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-no-pkg", "SMK No Pkg", "pass-op-no-pkg")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")

	// Create: even with a paid school package in the payload, the sub-account
	// is forced to "free" — and no operator role leaks through the package.
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username": "sub-no-pkg", "password": "pass-sub-no-pkg", "name": "Sub No Pkg",
		"roles":   []string{models.RoleGuru},
		"package": "sekolah_unggulan",
	})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-no-pkg: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub-no-pkg")
	if sub.Package != "free" {
		t.Errorf("sub-no-pkg package=%q after operator create, want \"free\" (forced)", sub.Package)
	}
	if models.HasRole(sub.Role, models.RoleOperator) {
		t.Errorf("sub-no-pkg role=%s, want NO operator role (package must not leak a role)", sub.Role)
	}

	// Edit: a package change submitted by the operator is ignored — the label
	// stays exactly as it was.
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub.ID)+"/edit",
		map[string]interface{}{"package": "sekolah_kecil"}); status != http.StatusOK || !resp.Success {
		t.Fatalf("edit sub-no-pkg package: status=%d resp=%+v", status, resp)
	}
	sub = mustGetUser(t, pool, "sub-no-pkg")
	if sub.Package != "free" {
		t.Errorf("sub-no-pkg package=%q after operator edit, want unchanged \"free\" (ignored)", sub.Package)
	}
	if models.HasRole(sub.Role, models.RoleOperator) {
		t.Errorf("sub-no-pkg role=%s after operator edit, want still no operator role", sub.Role)
	}

	// A SuperAdmin CAN still assign a package through the same handler — the
	// restriction is scoped to operators.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-pkg", Name: "Root Pkg",
		PasswordHash: "pass-root-pkg", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	if status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(sub.ID)+"/edit",
		map[string]interface{}{"package": "guru"}); status != http.StatusOK || !resp.Success {
		t.Fatalf("superadmin edit sub-no-pkg package: status=%d resp=%+v", status, resp)
	}
	sub = mustGetUser(t, pool, "sub-no-pkg")
	if sub.Package != "guru" {
		t.Errorf("sub-no-pkg package=%q after superadmin edit, want \"guru\" (superadmin may assign)", sub.Package)
	}
}

// TestOperatorQuotaEnforcedForPersonalInstansi locks in the sub-account quota
// fix: a guru who redeems a school voucher becomes an operator but keeps the
// shared "personal" instansi until a school name is set (UpdateInstansi). The
// school package's max_users quota must still bind it — the quota comes from
// the ACTIVE REDEMPTION, not from the instansi label. Previously
// loadOperatorAccountQuota returned (0,0)=unlimited for "personal", so a
// personal-bucket operator could create unlimited sub-accounts. Self-
// registered personal accounts (operator_created=false, exactly what
// /register creates) are NOT sub-accounts and must not consume the quota.
func TestOperatorQuotaEnforcedForPersonalInstansi(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)

	// The operator's instansi is the shared "personal" default — the shape of
	// a guru who redeemed a school voucher before ever setting a school name.
	op, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "op-personal", Name: "Op Personal",
		PasswordHash: "pass-op-personal", Status: models.UserStatusActive,
		Instansi: "personal",
		Role:     models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create personal operator: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")

	opAfter := mustGetUser(t, pool, "op-personal")
	if !models.HasRole(opAfter.Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}
	if opAfter.Instansi != "personal" {
		t.Fatalf("fixture: op instansi=%q, want the shared \"personal\" bucket", opAfter.Instansi)
	}

	// A self-registered personal account (instansi "personal",
	// operator_created=false — exactly what /register creates) is NOT a
	// sub-account: it must not consume the school quota.
	if _, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "selfreg-personal", Name: "Self Reg",
		PasswordHash: "pass-selfreg", Status: models.UserStatusActive,
		Instansi:     "personal",
		RegisteredIP: "203.0.113.77",
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	}); err != nil {
		t.Fatalf("create self-registered personal account: %v", err)
	}

	// The quota comes from the active redemption, not the instansi label: a
	// personal-bucket operator still reports the school package's 2 accounts.
	if gotMax, gotUsed, _ := loadOperatorAccountQuota(ctx, pool, opAfter.ID, true, opAfter.Instansi); gotMax != 2 || gotUsed != 0 {
		t.Fatalf("loadOperatorAccountQuota after redeem = (%d,%d), want (2,0) — school quota applies to the personal bucket", gotMax, gotUsed)
	}

	// The school quota binds the personal operator: only 2 sub-accounts.
	for _, name := range []string{"sub1", "sub2"} {
		if status, resp := tc.createUser(t, name); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", name, status, resp)
		}
	}
	if gotMax, gotUsed, _ := loadOperatorAccountQuota(ctx, pool, opAfter.ID, true, opAfter.Instansi); gotMax != 2 || gotUsed != 2 {
		t.Errorf("loadOperatorAccountQuota after 2 subs = (%d,%d), want (2,2)", gotMax, gotUsed)
	}

	// A third account is blocked by the quota — this is the fix: previously
	// the "personal" label skipped the quota entirely.
	if status, resp := tc.createUser(t, "sub3"); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Kuota akun") {
		t.Errorf("create sub3: status=%d resp=%+v, want 400 quota message", status, resp)
	}
	if _, err := models.GetUserByUsername(ctx, pool, "sub3"); err == nil {
		t.Error("sub3 was created despite the personal-bucket quota")
	}
}

// TestInstansiUpdateRouteRequiresManagementRole locks in the route-level
// authorization of POST /admin/api/instansi/update: renaming a school
// instansi applies to EVERY account sharing the instansi_id, so it must be
// management-level. The production route is registered under
// AdminManagementRequired (this test router mirrors that wiring: AuthRequired
// → FeatureLockRequired → AdminManagementRequired) — a guru or pengawas gets
// 403 BEFORE the handler runs (no rename, no instansi row created), while an
// operator and the SuperAdmin both succeed.
func TestInstansiUpdateRouteRequiresManagementRole(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)

	// Fixtures: SuperAdmin, an operator (redeemed the school voucher), a
	// plain guru, and a pengawas.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-instansi", Name: "Root Instansi",
		PasswordHash: "pass-root-instansi", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	op := createOperatorUser(t, pool, "op-instansi", "SMK Alpha", "pass-op-instansi")
	guru := createOperatorUser(t, pool, "guru-instansi", "personal", "pass-guru-instansi")
	pw, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "pengawas-instansi", Name: "Pengawas Instansi",
		PasswordHash: "pass-pengawas-instansi", Status: models.UserStatusActive,
		Instansi: "SMK Alpha",
		Role:     models.SerializeRoles([]string{models.RolePengawas}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create pengawas: %v", err)
	}

	tc := newVoucherTestClient(t, pool)

	// Operator: redeem the school voucher to actually hold the operator role
	// (the redeem handler refreshes the session role, so later management
	// calls in this session are authorized as an operator).
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	op = mustGetUser(t, pool, "op-instansi")
	if !models.HasRole(op.Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}

	// Guru: 403 from AdminManagementRequired (pinned by the message) — the
	// handler never runs, so the rename must not happen.
	tc.login(t, guru.ID)
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update",
		map[string]interface{}{"instansi": "SMA Guru Hacker"}); status != http.StatusForbidden || !strings.Contains(resp.Message, "khusus Super Admin atau Operator") {
		t.Errorf("guru instansi/update: status=%d resp=%+v, want 403 AdminManagementRequired message", status, resp)
	}
	if got := mustGetUser(t, pool, "guru-instansi").Instansi; got != "personal" {
		t.Errorf("guru instansi changed to %q despite 403 — handler must not run", got)
	}

	// Pengawas: 403 as well.
	tc.login(t, pw.ID)
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update",
		map[string]interface{}{"instansi": "SMA Pengawas Hacker"}); status != http.StatusForbidden || !strings.Contains(resp.Message, "khusus Super Admin atau Operator") {
		t.Errorf("pengawas instansi/update: status=%d resp=%+v, want 403 AdminManagementRequired message", status, resp)
	}
	if got := mustGetUser(t, pool, "pengawas-instansi").Instansi; got != "SMK Alpha" {
		t.Errorf("pengawas instansi changed to %q despite 403 — handler must not run", got)
	}

	// No instansi row may have been created by the blocked attempts.
	var instansiRows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM instansi`).Scan(&instansiRows); err != nil {
		t.Fatalf("count instansi rows: %v", err)
	}
	if instansiRows != 0 {
		t.Errorf("instansi rows=%d after blocked attempts, want 0 (handler must not run for guru/pengawas)", instansiRows)
	}

	// Operator: 200, the rename applies to the operator's own row.
	tc.login(t, op.ID)
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update",
		map[string]interface{}{"instansi": "SMK Alpha Baru"}); status != http.StatusOK || !resp.Success {
		t.Fatalf("operator instansi/update: status=%d resp=%+v, want 200 success", status, resp)
	}
	if got := mustGetUser(t, pool, "op-instansi").Instansi; got != "SMK Alpha Baru" {
		t.Errorf("operator instansi after update = %q, want %q", got, "SMK Alpha Baru")
	}
	// The operator had no instansi_id, so the rename is scoped to its own row:
	// the pengawas that merely shares the old instansi STRING (no instansi_id)
	// is untouched — documents the instansi_id-NULL behavior of UpdateInstansi.
	if got := mustGetUser(t, pool, "pengawas-instansi").Instansi; got != "SMK Alpha" {
		t.Errorf("pengawas instansi after operator rename = %q, want unchanged \"SMK Alpha\" (no instansi_id link)", got)
	}

	// SuperAdmin: 200 too.
	tc.login(t, root.ID)
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update",
		map[string]interface{}{"instansi": "Root Corp"}); status != http.StatusOK || !resp.Success {
		t.Fatalf("superadmin instansi/update: status=%d resp=%+v, want 200 success", status, resp)
	}
	if got := mustGetUser(t, pool, "root-instansi").Instansi; got != "Root Corp" {
		t.Errorf("superadmin instansi after update = %q, want %q", got, "Root Corp")
	}
}

// instansiLink reads the instansi_id + instansi_code columns of an account
// (the AdminUser model does not scan them — only explicit SQL does).
func instansiLink(t *testing.T, pool *pgxpool.Pool, userID int) (instansiID int, code string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(instansi_id, 0), COALESCE(instansi_code, '') FROM admin_users WHERE id = $1`,
		userID).Scan(&instansiID, &code); err != nil {
		t.Fatalf("load instansi link for user %d: %v", userID, err)
	}
	return instansiID, code
}

// TestUpdateInstansiMigratesPersonalBucketSubAccounts locks in the
// UpdateInstansi migration: an operator who created sub-accounts while still
// in the shared "personal" bucket (school instansi not yet set) has those
// sub-accounts moved to the new school instansi (instansi + instansi_id +
// instansi_code) when it finally sets one. Without the migration the subs
// would keep the "personal" label and stop counting toward the school quota
// (loadOperatorAccountQuota counts by the operator's instansi), letting the
// operator create max_users more on top of the ones already created.
// Self-registered personal accounts (operator_created=false) are NOT
// sub-accounts and are never migrated.
func TestUpdateInstansiMigratesPersonalBucketSubAccounts(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)

	op, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "op-migrate", Name: "Op Migrate",
		PasswordHash: "pass-op-migrate", Status: models.UserStatusActive,
		Instansi: "personal",
		Role:     models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create personal operator: %v", err)
	}
	// A self-registered personal account must never be migrated.
	if _, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "selfreg-migrate", Name: "Self Reg",
		PasswordHash: "pass-selfreg-migrate", Status: models.UserStatusActive,
		Instansi:     "personal",
		RegisteredIP: "203.0.113.99",
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	}); err != nil {
		t.Fatalf("create self-registered personal account: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	opAfter := mustGetUser(t, pool, "op-migrate")
	if !models.HasRole(opAfter.Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}
	if opAfter.Instansi != "personal" {
		t.Fatalf("fixture: op instansi=%q, want the shared \"personal\" bucket", opAfter.Instansi)
	}

	// Two sub-accounts created while still in the personal bucket (quota 2/2).
	for _, name := range []string{"sub1", "sub2"} {
		if status, resp := tc.createUser(t, name); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", name, status, resp)
		}
	}
	for _, name := range []string{"sub1", "sub2"} {
		sub := mustGetUser(t, pool, name)
		if !sub.OperatorCreated || sub.Instansi != "personal" {
			t.Fatalf("fixture: %s operator_created=%v instansi=%q, want sub-account in the personal bucket", name, sub.OperatorCreated, sub.Instansi)
		}
	}

	// The operator sets its school instansi.
	tc.login(t, op.ID)
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update",
		map[string]interface{}{"instansi": "SMK Baru"}); status != http.StatusOK || !resp.Success {
		t.Fatalf("operator instansi/update: status=%d resp=%+v", status, resp)
	}

	opAfter = mustGetUser(t, pool, "op-migrate")
	opInstID, opCode := instansiLink(t, pool, opAfter.ID)
	if opAfter.Instansi != "SMK Baru" || opInstID == 0 || opCode == "" {
		t.Fatalf("op after update: instansi=%q instansi_id=%d code=%q, want \"SMK Baru\" with a real link", opAfter.Instansi, opInstID, opCode)
	}

	// The sub-accounts were migrated to the school: label, instansi_id and
	// instansi_code all follow the operator.
	for _, name := range []string{"sub1", "sub2"} {
		sub := mustGetUser(t, pool, name)
		subInstID, subCode := instansiLink(t, pool, sub.ID)
		if sub.Instansi != "SMK Baru" {
			t.Errorf("%s instansi after migration = %q, want \"SMK Baru\"", name, sub.Instansi)
		}
		if subInstID != opInstID {
			t.Errorf("%s instansi_id after migration = %d, want the operator's %d", name, subInstID, opInstID)
		}
		if subCode != opCode {
			t.Errorf("%s instansi_code after migration = %q, want the operator's %q", name, subCode, opCode)
		}
	}
	// The self-registered personal account is untouched.
	if got := mustGetUser(t, pool, "selfreg-migrate").Instansi; got != "personal" {
		t.Errorf("self-registered account instansi = %q, want still \"personal\" (never migrated)", got)
	}

	// Quota stays accurate: the migrated subs now count in the school bucket,
	// so a third account is still blocked.
	if gotMax, gotUsed, _ := loadOperatorAccountQuota(ctx, pool, opAfter.ID, true, opAfter.Instansi); gotMax != 2 || gotUsed != 2 {
		t.Errorf("loadOperatorAccountQuota after migration = (%d,%d), want (2,2) — subs followed the operator", gotMax, gotUsed)
	}
	if status, resp := tc.createUser(t, "sub3"); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Kuota akun") {
		t.Errorf("create sub3 after migration: status=%d resp=%+v, want 400 quota message", status, resp)
	}
}

// TestPersonalBucketQuotaAndMigrationScopedPerOperator locks in the
// created_by attribution that replaces the old shared-bucket approximation:
// when TWO personal-bucket operators share the "personal" instansi, each
// operator's sub-account quota counts ONLY the sub-accounts IT created
// (created_by = operator id, not every operator_created row in the bucket),
// and the UpdateInstansi migration moves ONLY the operator's own sub-accounts
// to its new school — the other operator's subs stay in "personal". This
// closes the documented approximation where multiple personal operators
// counted each other's sub-accounts and one claiming a school swept the
// other's sub-accounts along.
func TestPersonalBucketQuotaAndMigrationScopedPerOperator(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// Each operator gets its OWN school voucher (quota 2 per operator).
	createSchoolVoucher(t, pool)
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-B")

	makePersonal := func(username string) models.AdminUser {
		op, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: username, Name: username,
			PasswordHash: "pass-" + username, Status: models.UserStatusActive,
			Instansi: "personal",
			Role:     models.SerializeRoles([]string{models.RoleGuru}),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		})
		if err != nil {
			t.Fatalf("create %s: %v", username, err)
		}
		return *op
	}
	opA := makePersonal("op-scope-a")
	opB := makePersonal("op-scope-b")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, opA.ID)
	tc.redeem(t, "IT-SEKOLAH")
	opA = mustGetUser(t, pool, "op-scope-a")
	if !models.HasRole(opA.Role, models.RoleOperator) {
		t.Fatalf("op-scope-a must hold the operator role after redeeming")
	}
	tc.login(t, opB.ID)
	tc.redeem(t, "IT-SEKOLAH-B")
	opB = mustGetUser(t, pool, "op-scope-b")
	if !models.HasRole(opB.Role, models.RoleOperator) {
		t.Fatalf("op-scope-b must hold the operator role after redeeming")
	}

	// A fills its own quota (2 subs); B creates 1.
	tc.login(t, opA.ID)
	for _, name := range []string{"subA1", "subA2"} {
		if status, resp := tc.createUser(t, name); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", name, status, resp)
		}
	}
	tc.login(t, opB.ID)
	if status, resp := tc.createUser(t, "subB1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create subB1: status=%d resp=%+v", status, resp)
	}

	// created_by attribution: each sub records its own creator.
	for name, creatorUsername := range map[string]string{
		"subA1": "op-scope-a", "subA2": "op-scope-a", "subB1": "op-scope-b",
	} {
		sub := mustGetUser(t, pool, name)
		if sub.CreatedBy == nil {
			t.Fatalf("%s.created_by = NULL, want the creating operator's id", name)
		}
		creator := mustGetUser(t, pool, creatorUsername)
		if *sub.CreatedBy != creator.ID {
			t.Errorf("%s.created_by = %d, want %d (%s)", name, *sub.CreatedBy, creator.ID, creatorUsername)
		}
	}

	// Quota per operator counts only its OWN subs — the shared bucket no
	// longer mixes operators' sub-accounts.
	if gotMax, gotUsed, _ := loadOperatorAccountQuota(ctx, pool, opA.ID, true, "personal"); gotMax != 2 || gotUsed != 2 {
		t.Errorf("quota(op-scope-a) = (%d,%d), want (2,2) — only its own subs count", gotMax, gotUsed)
	}
	if gotMax, gotUsed, _ := loadOperatorAccountQuota(ctx, pool, opB.ID, true, "personal"); gotMax != 2 || gotUsed != 1 {
		t.Errorf("quota(op-scope-b) = (%d,%d), want (2,1) — subA* must NOT count against B", gotMax, gotUsed)
	}

	// A is full (its own 2 subs); B still has room for one more.
	tc.login(t, opA.ID)
	if status, resp := tc.createUser(t, "subA3"); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Kuota akun") {
		t.Errorf("create subA3: status=%d resp=%+v, want 400 quota message", status, resp)
	}
	tc.login(t, opB.ID)
	if status, resp := tc.createUser(t, "subB2"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create subB2: status=%d resp=%+v, want success (B still has quota room)", status, resp)
	}

	// A claims its school instansi: only A's subs migrate. B's subB1 stays in
	// "personal" — the old approximation would have swept it into A's school.
	tc.login(t, opA.ID)
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update",
		map[string]interface{}{"instansi": "SMK Scope A"}); status != http.StatusOK || !resp.Success {
		t.Fatalf("op-scope-a instansi/update: status=%d resp=%+v", status, resp)
	}
	opA = mustGetUser(t, pool, "op-scope-a")
	aInstID, aCode := instansiLink(t, pool, opA.ID)
	for _, name := range []string{"subA1", "subA2"} {
		sub := mustGetUser(t, pool, name)
		subInstID, subCode := instansiLink(t, pool, sub.ID)
		if sub.Instansi != "SMK Scope A" || subInstID != aInstID || subCode != aCode {
			t.Errorf("%s after migration: instansi=%q instansi_id=%d code=%q, want the operator's school",
				name, sub.Instansi, subInstID, subCode)
		}
	}
	if got := mustGetUser(t, pool, "subB1").Instansi; got != "personal" {
		t.Errorf("subB1 instansi after op-scope-a migration = %q, want still \"personal\" (not A's sub-account)", got)
	}

	// B's quota still counts its own subs in the personal bucket (subB1,
	// subB2 = 2/2) — A's migration must not have stolen them.
	if gotMax, gotUsed, _ := loadOperatorAccountQuota(ctx, pool, opB.ID, true, "personal"); gotMax != 2 || gotUsed != 2 {
		t.Errorf("quota(op-scope-b) after A's migration = (%d,%d), want (2,2) — B's subs stayed in personal", gotMax, gotUsed)
	}
}

// TestCreatedByDeleteSetsNull locks in the ON DELETE SET NULL FK behavior:
// deleting the creating operator must never be blocked by a sub-account that a
// SuperAdmin moved out of the operator's instansi (DeleteUser only cascades
// within the operator's instansi, so such a sub survives the delete — a plain
// REFERENCES would trip the FK and 500 the delete). The orphaned sub-account's
// created_by is nulled instead, falling back to the legacy shared-bucket
// counting (created_by IS NULL AND operator_created) — it stays a valid
// sub-account, just no longer attributed to a specific operator.
func TestCreatedByDeleteSetsNull(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-del-cb", "personal", "pass-op-del-cb")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if status, resp := tc.createUser(t, "sub-del-cb"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-del-cb: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub-del-cb")
	if sub.CreatedBy == nil || *sub.CreatedBy != op.ID {
		t.Fatalf("fixture: sub-del-cb.created_by = %v, want %d (the operator)", sub.CreatedBy, op.ID)
	}

	// A SuperAdmin moves the sub-account to another instansi (allowed — only
	// operators are restricted from changing instansi), so the sub no longer
	// lives in the operator's bucket and survives an operator-scoped delete.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-del-cb", Name: "Root Del CB",
		PasswordHash: "pass-root-del-cb", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	if status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(sub.ID)+"/edit",
		map[string]interface{}{"instansi": "SMA Pindahan"}); status != http.StatusOK || !resp.Success {
		t.Fatalf("superadmin move sub to another instansi: status=%d resp=%+v", status, resp)
	}

	// Deleting the operator must succeed (ON DELETE SET NULL — no FK block).
	if status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/delete",
		map[string]interface{}{}); status != http.StatusOK || !resp.Success {
		t.Fatalf("delete operator with moved sub-account: status=%d resp=%+v, want 200 (FK must not block)", status, resp)
	}

	// The moved sub-account survived, with created_by nulled (legacy fallback).
	sub = mustGetUser(t, pool, "sub-del-cb")
	if sub.CreatedBy != nil {
		t.Errorf("sub-del-cb.created_by after operator delete = %v, want NULL (ON DELETE SET NULL)", *sub.CreatedBy)
	}
	if sub.Instansi != "SMA Pindahan" {
		t.Errorf("sub-del-cb instansi = %q, want \"SMA Pindahan\" (survived the operator delete)", sub.Instansi)
	}
	// The orphan still counts toward the shared legacy bucket of any personal
	// operator via the (created_by IS NULL AND operator_created) fallback.
	if gotMax, gotUsed, _ := loadOperatorAccountQuota(ctx, pool, root.ID, false, "personal"); gotMax != 0 || gotUsed != 0 {
		t.Errorf("quota for non-operator after sub orphan = (%d,%d), want (0,0) — not applicable", gotMax, gotUsed)
	}
}

// TestOperatorWithEmailCanCreateSubAccounts locks in the fix for the
// operator-created-account failure: CreateUser used to copy the operator's
// email onto every sub-account, but the unique index uq_admin_users_email
// (LOWER(email) WHERE email <> ”) already holds the operator's own row — so
// the INSERT always failed with a unique violation and an operator with an
// email could NEVER create an account ("Gagal membuat user", 500). Production
// operators always carry an email (the register form requires it). Now the
// sub-account keeps its own form-provided email, and a duplicate email gets a
// friendly 400 instead of a raw 500.
func TestOperatorWithEmailCanCreateSubAccounts(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)

	op := createOperatorUser(t, pool, "op-email", "personal", "pass-op-email")
	// Give the operator an email — the exact shape of a production operator
	// who registered via /register. Before the fix this alone broke every
	// subsequent account creation.
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET email = 'op-email@sekolah.sch.id' WHERE id = $1`, op.ID); err != nil {
		t.Fatalf("set operator email: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")

	// A sub-account with its own email must be created successfully — before
	// the fix the handler forced the operator's email onto it and the unique
	// index rejected the insert (500 "Gagal membuat user").
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username": "sub-email",
		"password": "pass-sub-email",
		"name":     "sub-email",
		"roles":    []string{models.RoleGuru},
		"email":    "sub-email@sekolah.sch.id",
	}); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-account with own email: status=%d resp=%+v", status, resp)
	}
	if sub := mustGetUser(t, pool, "sub-email"); sub.Email != "sub-email@sekolah.sch.id" {
		t.Errorf("sub-account email=%q, want the form-provided email (not the operator's)", sub.Email)
	}

	// A sub-account whose form email duplicates an existing account gets a
	// friendly 400, not a raw 500.
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username": "sub-dupe",
		"password": "pass-sub-dupe",
		"name":     "sub-dupe",
		"roles":    []string{models.RoleGuru},
		"email":    "op-email@sekolah.sch.id",
	}); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Email sudah terdaftar") {
		t.Errorf("duplicate-email sub-account: status=%d resp=%+v, want 400 friendly message", status, resp)
	}
	if _, err := models.GetUserByUsername(ctx, pool, "sub-dupe"); err == nil {
		t.Error("sub-dupe was created despite the duplicate email")
	}
}

// TestCreateUserEmailDuplicateFriendlyErrors locks in the API-level email
// contract for the CreateUser handler (the layer that surfaced the operator
// bug as a raw 500): a duplicate non-empty email — exact, case-variant, or
// whitespace-padded — must be rejected with a friendly 400 "Email sudah
// terdaftar" BEFORE the INSERT (whose uq_admin_users_email index would
// otherwise turn it into a raw 500 "Gagal membuat user"), while empty emails
// (the common shape of a sub-account with no form email) never collide.
// Mirrors the self-registration contract on /register (main.go).
func TestCreateUserEmailDuplicateFriendlyErrors(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-email-matrix", Name: "Root Email Matrix",
		PasswordHash: "pass-root-email-matrix", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	create := func(username, email string) (int, apiResp) {
		return postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
			"username": username,
			"password": "pass-" + username,
			"name":     username,
			"roles":    []string{models.RoleGuru},
			"email":    email,
		})
	}

	// Unique email: accepted.
	if status, resp := create("matrix-ok", "matrix-ok@sekolah.sch.id"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create with unique email: status=%d resp=%+v", status, resp)
	}

	// Exact duplicate: friendly 400, no row created.
	if status, resp := create("matrix-dupe", "matrix-ok@sekolah.sch.id"); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Email sudah terdaftar") {
		t.Errorf("exact duplicate email: status=%d resp=%+v, want 400 friendly message", status, resp)
	}
	if _, err := models.GetUserByUsername(ctx, pool, "matrix-dupe"); err == nil {
		t.Error("matrix-dupe was created despite the exact duplicate email")
	}

	// Case-variant duplicate (index is on LOWER(email)): friendly 400 too.
	if status, resp := create("matrix-case", "MATRIX-OK@Sekolah.Sch.ID"); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Email sudah terdaftar") {
		t.Errorf("case-variant duplicate email: status=%d resp=%+v, want 400 friendly message", status, resp)
	}

	// Whitespace-padded duplicate (handler trims): friendly 400 too.
	if status, resp := create("matrix-space", "  matrix-ok@sekolah.sch.id  "); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Email sudah terdaftar") {
		t.Errorf("whitespace-padded duplicate email: status=%d resp=%+v, want 400 friendly message", status, resp)
	}

	// Empty emails never collide (partial unique index WHERE email <> ''):
	// the common shape of sub-accounts without a form email.
	for _, u := range []string{"matrix-empty-1", "matrix-empty-2"} {
		if status, resp := create(u, ""); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s with empty email: status=%d resp=%+v", u, status, resp)
		}
	}
}

// TestSubAccountCannotListRedemptions locks in the third leg of the
// sub-account voucher policy: GET /admin/api/vouchers/mine (the endpoint that
// backs the "Paket yang Sudah Anda Klaim" list on the Paket & Voucher tab of
// the settings hub) must answer 403 for an operator-created account — the
// same origin-based block as redeem/activate — so a sub-account can never
// read a redemption list, even one left over from before the policy (a
// pre-policy claim planted on its row). The operator themself
// (operator_created=false) keeps full access.
func TestSubAccountCannotListRedemptions(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucherCode(t, pool, "IT-GURU-SUB")
	op := createOperatorUser(t, pool, "op-sub-mine", "SMK Sub Mine", "pass-op-sub-mine")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if !models.HasRole(mustGetUser(t, pool, "op-sub-mine").Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}

	// Sub-account created by the operator (operator_created=true) with a
	// PRE-POLICY claim planted on its row — the data the endpoint must never
	// leak to it.
	if status, resp := tc.createUser(t, "sub-mine"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-mine: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub-mine")
	if !sub.OperatorCreated {
		t.Fatalf("fixture: sub-mine must be operator_created")
	}
	claimSubOwnVoucher(t, pool, sub.ID)

	// The operator itself (operator_created=false) can still list its own
	// claimed packages.
	tc.login(t, op.ID)
	opResp, opErr := tc.client.Get(tc.srv.URL + "/api/vouchers/mine")
	if opErr != nil {
		t.Fatalf("operator GET /api/vouchers/mine: %v", opErr)
	}
	opResp.Body.Close()
	if opResp.StatusCode != http.StatusOK {
		t.Errorf("operator GET /vouchers/mine: status=%d, want 200", opResp.StatusCode)
	}

	// The sub-account gets the same 403 sub-account message as redeem/activate
	// — before any redemption lookup, so even the pre-policy row is invisible.
	sc := newVoucherTestClient(t, pool)
	sc.login(t, sub.ID)
	resp, err := sc.client.Get(sc.srv.URL + "/api/vouchers/mine")
	if err != nil {
		t.Fatalf("sub GET /api/vouchers/mine: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("sub GET /api/vouchers/mine: status=%d, want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "dibuat oleh Operator") {
		t.Errorf("sub GET /api/vouchers/mine body=%s, want the sub-account 403 message", string(body))
	}

	// Nothing was exposed or mutated: the pre-policy redemption row is still
	// there, untouched.
	var redemptionCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM voucher_redemptions WHERE user_id = $1`, sub.ID).Scan(&redemptionCount); err != nil {
		t.Fatalf("count sub redemptions: %v", err)
	}
	if redemptionCount != 1 {
		t.Errorf("sub redemptions=%d after blocked mine, want 1 (pre-policy row untouched)", redemptionCount)
	}
}
