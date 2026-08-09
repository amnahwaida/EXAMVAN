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

	// A superadmin promotes the sub-account to the operator role.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-promo", Name: "Root Promo",
		PasswordHash: "pass-root-promo", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc.login(t, root.ID)
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub.ID)+"/edit",
		map[string]interface{}{"roles": []string{models.RoleGuru, models.RoleOperator}}); status != http.StatusOK || !resp.Success {
		t.Fatalf("promote sub1: status=%d resp=%+v", status, resp)
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
