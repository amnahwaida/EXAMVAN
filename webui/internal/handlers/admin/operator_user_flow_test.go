package admin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Operator account-creation flow hardening tests.
//
// These lock in the operator-path guarantees reviewed across CreateUser /
// EditUser / VerifyUser:
//
//  1. A sub-account's expiry ALWAYS follows the operator — the operator can
//     never grant a longer lifetime or an unlimited one via EditUser
//     (TestOperatorCannotSetSubAccountExpiry).
//  2. The sub-account quota check and the INSERT are atomic (FOR UPDATE lock
//     on the operator row inside one transaction) — concurrent creates can
//     never overshoot max_users (TestCreateUserQuotaAtomicUnderConcurrency).
//  3. The instansi is always taken from the operator's DB row, never from the
//     request body (TestCreateUserForcesOperatorInstansiOverBody).
//  4. VerifyUser rejects operator targets, mirroring Edit/Toggle
//     (TestVerifyUserCannotActOnOperatorPeer).
//  5. instansi_id + instansi_code are inherited inside the same INSERT,
//     including a raw NULL code (TestCreateUserInheritsInstansiIdentity).
//  6. The server enforces the 8-character password minimum on both create and
//     edit (TestCreateUserPasswordMinLengthServer).
//  7. Management actions (create / edit) leave an append-only audit trail
//     (TestUserCreateEditAudited).
// ---------------------------------------------------------------------------

// TestOperatorCannotSetSubAccountExpiry locks in the sub-account expiry
// policy for EditUser (mirror of CreateUser's force-expiry inheritance): an
// operator may never set an expiry on a sub-account that differs from its
// own — whether a longer one, a shorter one, or the "" unlimited attempt.
// Whatever the operator submits is replaced with the operator's CURRENT
// expiry (including NULL → unlimited when the operator itself is unlimited),
// and the success message says so. Only a SuperAdmin may grant a different
// expiry.
func TestOperatorCannotSetSubAccountExpiry(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)

	// --- Fixture 1: operator with a CONCRETE expiry ------------------------
	opExpiry := time.Date(2030, 1, 1, 9, 30, 0, 0, time.UTC)
	op := createOperatorUser(t, pool, "op-fix-exp", "SMK Force Expiry", "pass-op-fix-exp")
	if _, err := pool.Exec(ctx, `UPDATE admin_users SET expires_at = $1 WHERE id = $2`, opExpiry, op.ID); err != nil {
		t.Fatalf("pin operator expiry: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")

	// The operator's expiry stays the pinned one (redeem sets a finite
	// package lifetime for the OPERATOR itself — the guarantees below only
	// concern the sub-account edits).
	opAfter := mustGetUser(t, pool, "op-fix-exp")
	if opAfter.ExpiresAt == nil {
		t.Fatal("fixture: operator must have a concrete expiry")
	}
	wantExp := opAfter.ExpiresAt.UTC().Format("2006-01-02 15:04:05")

	// Create a sub-account — its expiry is inherited from the operator.
	if status, resp := tc.createUser(t, "sub-exp1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-exp1: status=%d resp=%+v", status, resp)
	}
	sub1 := mustGetUser(t, pool, "sub-exp1")
	if sub1.ExpiresAt == nil || !sub1.ExpiresAt.Truncate(time.Second).Equal(opAfter.ExpiresAt.Truncate(time.Second)) {
		t.Fatalf("fixture: sub-exp1 expiry=%v, want operator's %v", sub1.ExpiresAt, opAfter.ExpiresAt)
	}

	// (a) Far-future expiry attempt → forced back to the operator's expiry.
	future := "2099-12-31 23:59:59"
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub1.ID)+"/edit",
		map[string]interface{}{"expires_at": future})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("operator edit sub expiry (future): status=%d resp=%+v", status, resp)
	}
	if !strings.Contains(resp.Message, "dipaksa mengikuti operator") {
		t.Errorf("edit expiry message = %q, want the forced-follow note", resp.Message)
	}
	sub1 = mustGetUser(t, pool, "sub-exp1")
	if sub1.ExpiresAt == nil || sub1.ExpiresAt.UTC().Format("2006-01-02 15:04:05") != wantExp {
		t.Errorf("sub-exp1 expiry after future attempt = %v, want forced operator %s", sub1.ExpiresAt, wantExp)
	}

	// (b) Unlimited attempt (expires_at "") → still the operator's expiry,
	// NEVER NULL: a sub-account cannot be made unlimited while the operator
	// has a finite lifetime (the expiry-cascade guards rely on this).
	status, resp = postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub1.ID)+"/edit",
		map[string]interface{}{"expires_at": ""})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("operator edit sub expiry (unlimited attempt): status=%d resp=%+v", status, resp)
	}
	sub1 = mustGetUser(t, pool, "sub-exp1")
	if sub1.ExpiresAt == nil || sub1.ExpiresAt.UTC().Format("2006-01-02 15:04:05") != wantExp {
		t.Errorf("sub-exp1 expiry after unlimited attempt = %v, want forced operator %s (never NULL)", sub1.ExpiresAt, wantExp)
	}

	// A SuperAdmin CAN set any expiry through the same handler — the
	// restriction is scoped to operators.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-fix-exp", Name: "Root Fix Exp",
		PasswordHash: "pass-root-fix-exp", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	if status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(sub1.ID)+"/edit",
		map[string]interface{}{"expires_at": future}); status != http.StatusOK || !resp.Success {
		t.Fatalf("superadmin edit sub expiry: status=%d resp=%+v", status, resp)
	}
	sub1 = mustGetUser(t, pool, "sub-exp1")
	if sub1.ExpiresAt == nil || sub1.ExpiresAt.UTC().Format("2006-01-02 15:04:05") != future {
		t.Errorf("sub-exp1 expiry after superadmin edit = %v, want %s (superadmin may override)", sub1.ExpiresAt, future)
	}

	// --- Fixture 2: operator with NO expiry (unlimited) ---------------------
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-2") // fixture 1 used IT-SEKOLAH
	opUnl := createOperatorUser(t, pool, "op-unl-exp", "SMK Unlimited", "pass-op-unl-exp")
	tc2 := newVoucherTestClient(t, pool)
	tc2.login(t, opUnl.ID)
	tc2.redeem(t, "IT-SEKOLAH-2")
	// The operator is cleared to unlimited AFTER the redeem (the redeem gave
	// it a finite package lifetime; an admin clearing the expiry represents
	// the real-world "unlimited operator" state).
	if _, err := pool.Exec(ctx, `UPDATE admin_users SET expires_at = NULL WHERE id = $1`, opUnl.ID); err != nil {
		t.Fatalf("clear operator expiry: %v", err)
	}
	if mustGetUser(t, pool, "op-unl-exp").ExpiresAt != nil {
		t.Fatal("fixture: operator must be unlimited")
	}

	// Create a sub-account — unlimited by inheritance (inheritUnlimited).
	if status, resp := tc2.createUser(t, "sub-unl1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-unl1: status=%d resp=%+v", status, resp)
	}
	subUnl := mustGetUser(t, pool, "sub-unl1")
	if subUnl.ExpiresAt != nil {
		t.Fatalf("fixture: sub-unl1 must be unlimited, got %v", subUnl.ExpiresAt)
	}

	// A concrete expiry attempt by the unlimited operator → forced back to
	// unlimited (NULL); the message explains the follow rule.
	status, resp = postJSON(t, tc2.client, tc2.srv, "/api/users/"+strconv.Itoa(subUnl.ID)+"/edit",
		map[string]interface{}{"expires_at": future})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("unlimited operator edit sub expiry: status=%d resp=%+v", status, resp)
	}
	if !strings.Contains(resp.Message, "unlimited") {
		t.Errorf("unlimited-operator edit message = %q, want the unlimited-follow note", resp.Message)
	}
	subUnl = mustGetUser(t, pool, "sub-unl1")
	if subUnl.ExpiresAt != nil {
		t.Errorf("sub-unl1 expiry after concrete attempt = %v, want NULL (forced unlimited)", subUnl.ExpiresAt)
	}
}

// TestCreateUserQuotaAtomicUnderConcurrency locks in the atomicity of the
// sub-account quota: the operator's admin_users row is locked FOR UPDATE
// inside the create transaction, so ANY number of concurrent create requests
// serialize on it — exactly max_users succeed, the rest get the quota 400.
// Before the fix, count-then-insert ran outside a lock and concurrent
// requests could all read used < max_users and overshoot the package quota.
func TestCreateUserQuotaAtomicUnderConcurrency(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool) // max_users = 2 (CustomMaxUsers)
	op := createOperatorUser(t, pool, "op-race", "SMK Race", "pass-op-race")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if gotMax, gotUsed := loadOperatorAccountQuota(ctx, pool, op.ID, true, op.Instansi); gotMax != 2 || gotUsed != 0 {
		t.Fatalf("fixture: quota after redeem = (%d,%d), want (2,0)", gotMax, gotUsed)
	}

	const attempts = 8 // 6 above the 2-account quota: at most 2 may succeed
	var wg sync.WaitGroup
	results := make([]int, attempts)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, resp := tc.createUser(t, fmt.Sprintf("racer%d", i))
			if status == http.StatusOK && resp.Success {
				results[i] = 1
				return
			}
			if status == http.StatusBadRequest && strings.Contains(resp.Message, "Kuota akun") {
				results[i] = 0 // correctly rejected by the quota
				return
			}
			results[i] = 2 // unexpected status/message
		}(i)
	}
	wg.Wait()

	created, rejected, unexpected := 0, 0, 0
	for _, r := range results {
		switch r {
		case 1:
			created++
		case 0:
			rejected++
		default:
			unexpected++
		}
	}
	if unexpected > 0 {
		t.Fatalf("concurrent creates: %d unexpected responses (must be either 200 or the quota 400)", unexpected)
	}
	if created != 2 {
		t.Errorf("concurrent creates: %d succeeded, want exactly %d (max_users=2) — the quota count raced", created, 2)
	}
	if rejected != attempts-2 {
		t.Errorf("concurrent creates: %d rejected by quota, want %d", rejected, attempts-2)
	}

	var total int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users WHERE username LIKE 'racer%'`).Scan(&total); err != nil {
		t.Fatalf("count sub-accounts: %v", err)
	}
	if total != 2 {
		t.Errorf("DB has %d racer* accounts, want exactly 2 (no partial/overshoot rows)", total)
	}
}

// TestCreateUserForcesOperatorInstansiOverBody locks in that the sub-account's
// instansi ALWAYS comes from the operator's DB row — a hand-crafted
// instansi in the request body (tamper) can neither label the new account
// with another instansi nor dodge the quota (which counts by the operator's
// instansi). Covers both a real school instansi and the shared "personal"
// bucket.
func TestCreateUserForcesOperatorInstansiOverBody(t *testing.T) {
	pool := setupVoucherITDB(t)

	createSchoolVoucher(t, pool)

	// Operator with a REAL school instansi: body instansi must lose.
	op := createOperatorUser(t, pool, "op-tamper", "SMK Tamper", "pass-op-tamper")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username": "sub-tamper1", "password": "pass-sub-tamper1", "name": "Sub Tamper",
		"roles":    []string{models.RoleGuru},
		"instansi": "SEKOLAH-EVIL",
	})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("create with tampered instansi: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub-tamper1")
	if sub.Instansi != "SMK Tamper" {
		t.Errorf("sub-tamper1 instansi=%q, want the operator's \"SMK Tamper\" (body %q must be ignored)", sub.Instansi, "SEKOLAH-EVIL")
	}

	// Personal-bucket operator: body instansi must lose to "personal" too.
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-2") // first code already used above
	opP := createOperatorUser(t, pool, "op-tamper-p", "personal", "pass-op-tamper-p")
	tcP := newVoucherTestClient(t, pool)
	tcP.login(t, opP.ID)
	tcP.redeem(t, "IT-SEKOLAH-2")
	status, resp = postJSON(t, tcP.client, tcP.srv, "/api/users", map[string]interface{}{
		"username": "sub-tamper2", "password": "pass-sub-tamper2", "name": "Sub Tamper 2",
		"roles":    []string{models.RoleGuru},
		"instansi": "SEKOLAH-EVIL",
	})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("create with tampered instansi (personal op): status=%d resp=%+v", status, resp)
	}
	sub2 := mustGetUser(t, pool, "sub-tamper2")
	if sub2.Instansi != "personal" {
		t.Errorf("sub-tamper2 instansi=%q, want \"personal\" (the operator's DB instansi, body ignored)", sub2.Instansi)
	}
}

// TestVerifyUserCannotActOnOperatorPeer locks in the VerifyUser guard: an
// operator may verify pending accounts in its own instansi but NEVER another
// operator (mirror of the Edit/Toggle guards) — peer operators stay out of
// each other's account lifecycle.
func TestVerifyUserCannotActOnOperatorPeer(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// opA needs the operator role to act; the school voucher grants it.
	createSchoolVoucher(t, pool)

	// Two operators in the SAME instansi: one acts, one is the pending target.
	opA := createOperatorUser(t, pool, "op-verify-a", "SMK Verify", "pass-op-verify-a")
	opB, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "op-verify-b", Name: "Op Verify B",
		PasswordHash: "pass-op-verify-b", Status: models.UserStatusPendingOTP,
		Instansi: "SMK Verify",
		Role:     models.SerializeRoles([]string{models.RoleOperator}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create operator peer: %v", err)
	}
	guru, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "guru-verify", Name: "Guru Verify",
		PasswordHash: "pass-guru-verify", Status: models.UserStatusPendingOTP,
		Instansi: "SMK Verify",
		Role:     models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create pending guru: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, opA.ID)
	// opA gains the operator role by redeeming the school voucher (the same
	// grant real operators hold) — without it the actor is a plain guru and
	// the peer-target guard would not apply.
	tc.redeem(t, "IT-SEKOLAH")

	// Peer operator target → 400, status untouched.
	status2, resp2 := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(opB.ID)+"/verify", map[string]interface{}{})
	if status2 != http.StatusBadRequest || !strings.Contains(resp2.Message, "role Operator") {
		t.Errorf("verify peer operator: status=%d resp=%+v, want 400 operator-role message", status2, resp2)
	}
	opBAfter := mustGetUser(t, pool, "op-verify-b")
	if opBAfter.Status != models.UserStatusPendingOTP {
		t.Errorf("peer operator status after blocked verify = %q, want unchanged pending_otp", opBAfter.Status)
	}

	// Guru target in the same instansi → verified normally.
	status3, resp3 := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(guru.ID)+"/verify", map[string]interface{}{})
	if status3 != http.StatusOK || !resp3.Success {
		t.Fatalf("verify guru: status=%d resp=%+v, want 200", status3, resp3)
	}
	guruAfter := mustGetUser(t, pool, "guru-verify")
	if guruAfter.Status != models.UserStatusActive {
		t.Errorf("guru status after verify = %q, want active", guruAfter.Status)
	}
}

// TestCreateUserInheritsInstansiIdentity locks in the school-identity
// inheritance of operator-created sub-accounts: instansi_id and instansi_code
// are copied from the operator INSIDE the same INSERT that creates the
// account. A NULL operator code is inherited as raw NULL — never the ''
// interpolation that used to drift from the school's canonical "unset"
// representation.
func TestCreateUserInheritsInstansiIdentity(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)

	// --- Variant A: operator WITH a real instansi row (id + code) -----------
	var instID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMK Ident Code', 'SCH-AAAA-BBBB') RETURNING id`).Scan(&instID); err != nil {
		t.Fatalf("create instansi row: %v", err)
	}
	opA := createOperatorUser(t, pool, "op-ident-a", "SMK Ident Code", "pass-op-ident-a")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET instansi_id = $1, instansi_code = 'SCH-AAAA-BBBB' WHERE id = $2`, instID, opA.ID); err != nil {
		t.Fatalf("link operator to instansi: %v", err)
	}
	tcA := newVoucherTestClient(t, pool)
	tcA.login(t, opA.ID)
	tcA.redeem(t, "IT-SEKOLAH")
	if status, resp := tcA.createUser(t, "sub-ident-a"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-ident-a: status=%d resp=%+v", status, resp)
	}
	var gotID *int
	var gotCode *string
	if err := pool.QueryRow(ctx,
		`SELECT instansi_id, instansi_code FROM admin_users WHERE username = 'sub-ident-a'`).Scan(&gotID, &gotCode); err != nil {
		t.Fatalf("read sub identity: %v", err)
	}
	if gotID == nil || *gotID != instID {
		t.Errorf("sub-ident-a instansi_id = %v, want %d (inherited from the operator)", gotID, instID)
	}
	if gotCode == nil || *gotCode != "SCH-AAAA-BBBB" {
		t.Errorf("sub-ident-a instansi_code = %v, want SCH-AAAA-BBBB (inherited)", gotCode)
	}

	// --- Variant B: operator with NO instansi row (legacy shape) -------------
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-B") // first code already used above
	opB := createOperatorUser(t, pool, "op-ident-b", "personal", "pass-op-ident-b")
	tcB := newVoucherTestClient(t, pool)
	tcB.login(t, opB.ID)
	tcB.redeem(t, "IT-SEKOLAH-B")
	if status, resp := tcB.createUser(t, "sub-ident-b"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-ident-b: status=%d resp=%+v", status, resp)
	}
	var gotIDB *int
	var gotCodeB *string
	if err := pool.QueryRow(ctx,
		`SELECT instansi_id, instansi_code FROM admin_users WHERE username = 'sub-ident-b'`).Scan(&gotIDB, &gotCodeB); err != nil {
		t.Fatalf("read sub identity B: %v", err)
	}
	if gotIDB != nil || gotCodeB != nil {
		t.Errorf("sub-ident-b identity = (%v, %v), want (NULL, NULL) — the operator's raw NULL code must not become ''", gotIDB, gotCodeB)
	}
}

// TestCreateUserPasswordMinLengthServer locks in the server-side password
// minimum on both create and edit: the 8-character rule the UI enforces with
// minlength must not be bypassable by a hand-crafted request.
func TestCreateUserPasswordMinLengthServer(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-pwlen", Name: "Root Pw Len",
		PasswordHash: "pass-root-pwlen", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	// Create: 7 characters → 400, no account created.
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username": "pw-short", "password": "1234567", "name": "Pw Short",
		"roles": []string{models.RoleGuru},
	})
	if status != http.StatusBadRequest || !strings.Contains(resp.Message, "Password minimal 8 karakter") {
		t.Errorf("create with 7-char password: status=%d resp=%+v, want 400 min-length message", status, resp)
	}
	if _, err := models.GetUserByUsername(ctx, pool, "pw-short"); err == nil {
		t.Error("pw-short was created despite the min-length rule")
	}

	// Create: 8 characters → accepted.
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username": "pw-ok", "password": "12345678", "name": "Pw Ok",
		"roles": []string{models.RoleGuru},
	}); status != http.StatusOK || !resp.Success {
		t.Fatalf("create with 8-char password: status=%d resp=%+v", status, resp)
	}
	pwOK := mustGetUser(t, pool, "pw-ok")

	// Edit: resetting to 7 characters → 400 and the old password survives.
	status, resp = postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(pwOK.ID)+"/edit",
		map[string]interface{}{"password": "1234567"})
	if status != http.StatusBadRequest || !strings.Contains(resp.Message, "Password minimal 8 karakter") {
		t.Errorf("edit with 7-char password: status=%d resp=%+v, want 400", status, resp)
	}
	if after := mustGetUser(t, pool, "pw-ok"); !models.CheckPassword("12345678", after.PasswordHash) {
		t.Error("edit with a too-short password must not change the existing password")
	}

	// Edit: resetting to 8 characters → accepted.
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(pwOK.ID)+"/edit",
		map[string]interface{}{"password": "87654321"}); status != http.StatusOK || !resp.Success {
		t.Fatalf("edit with 8-char password: status=%d resp=%+v", status, resp)
	}
	if after := mustGetUser(t, pool, "pw-ok"); !models.CheckPassword("87654321", after.PasswordHash) {
		t.Error("the 8-char password reset should have applied")
	}
}

// TestUserCreateEditAudited locks in the management audit trail: every
// successful account CREATE and EDIT by an admin/operator appends an
// immutable admin_audit_logs row (exam_id NULL — no exam is involved), with
// the actor's username snapshotted. Rejected creates (short password, quota
// full, invalid role) must NOT leak an audit row.
func TestUserCreateEditAudited(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-audit", "SMK Audit", "pass-op-audit")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")

	countAudit := func(action string) int {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM admin_audit_logs WHERE action = $1`, action).Scan(&n); err != nil {
			t.Fatalf("count audit %s: %v", action, err)
		}
		return n
	}

	// (a) A rejected create (short password) writes NO audit row.
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username": "sub-audit-fail", "password": "1234567", "name": "Fail",
		"roles": []string{models.RoleGuru},
	}); status != http.StatusBadRequest {
		t.Fatalf("rejected create: status=%d resp=%+v", status, resp)
	}
	if n := countAudit(models.ActionUserCreated); n != 0 {
		t.Errorf("audit rows after rejected create = %d, want 0", n)
	}

	// (b) A quota-rejected create also writes no audit row. max_users = 0 is
	// UNLIMITED (fail-open), so pin a real quota: one existing account in the
	// instansi + max_users = 1 → used(1) >= max(1) → rejection.
	if _, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "filler-audit",
		PasswordHash: "pass-filler-audit",
		Status:   models.UserStatusActive,
		Instansi: "SMK Audit",
		Role:     models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	}); err != nil {
		t.Fatalf("create filler-audit: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE voucher_redemptions SET max_users = 1 WHERE user_id = $1 AND is_active`, op.ID); err != nil {
		t.Fatalf("pin the quota: %v", err)
	}
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username": "sub-audit-quota", "password": "pass-sub-audit-quota", "name": "Quota",
		"roles": []string{models.RoleGuru},
	}); status != http.StatusBadRequest {
		t.Fatalf("quota-rejected create: status=%d resp=%+v", status, resp)
	}
	if n := countAudit(models.ActionUserCreated); n != 0 {
		t.Errorf("audit rows after quota-rejected create = %d, want 0", n)
	}

	// (c) A successful create → one user_created row with the actor snapshot.
	if _, err := pool.Exec(ctx,
		`UPDATE voucher_redemptions SET max_users = 2 WHERE user_id = $1 AND is_active`, op.ID); err != nil {
		t.Fatalf("restore the quota: %v", err)
	}
	if status, resp := tc.createUser(t, "sub-audit-ok"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-audit-ok: status=%d resp=%+v", status, resp)
	}
	var actor, detail string
	var examID *int
	if err := pool.QueryRow(ctx,
		`SELECT username, detail, exam_id FROM admin_audit_logs WHERE action = $1 ORDER BY id DESC LIMIT 1`,
		models.ActionUserCreated).Scan(&actor, &detail, &examID); err != nil {
		t.Fatalf("read user_created audit row: %v", err)
	}
	if actor != "op-audit" {
		t.Errorf("user_created audit actor = %q, want the operator's username", actor)
	}
	if !strings.Contains(detail, "sub-audit-ok") || !strings.Contains(detail, "guru") {
		t.Errorf("user_created audit detail = %q, want the new account's username and role", detail)
	}
	if examID != nil {
		t.Errorf("user_created audit exam_id = %v, want NULL (no exam involved)", examID)
	}

	// (d) A successful edit → one user_edited row listing the changed field.
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(mustGetUser(t, pool, "sub-audit-ok").ID)+"/edit",
		map[string]interface{}{"name": "Sub Audit Renamed"}); status != http.StatusOK || !resp.Success {
		t.Fatalf("edit sub-audit-ok: status=%d resp=%+v", status, resp)
	}
	if err := pool.QueryRow(ctx,
		`SELECT detail FROM admin_audit_logs WHERE action = $1 ORDER BY id DESC LIMIT 1`,
		models.ActionUserEdited).Scan(&detail); err != nil {
		t.Fatalf("read user_edited audit row: %v", err)
	}
	if !strings.Contains(detail, "name") {
		t.Errorf("user_edited audit detail = %q, want the changed field listed", detail)
	}
}