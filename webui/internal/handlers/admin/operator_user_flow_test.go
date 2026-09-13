package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/examvan/webui/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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
	if gotMax, gotUsed, _ := loadOperatorAccountQuota(ctx, pool, op.ID, true, models.InstansiScope{ID: op.InstansiID, Name: op.Instansi}); gotMax != 2 || gotUsed != 0 {
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

	// opA becomes the school's FIRST (and only) operator via the real redeem
	// path — the one-operator-per-school policy requires the redeem to happen
	// before any peer operator exists.
	opA := createOperatorUser(t, pool, "op-verify-a", "SMK Verify", "pass-op-verify-a")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, opA.ID)
	tc.redeem(t, "IT-SEKOLAH")

	// Two more accounts in the SAME instansi: a peer operator (planted
	// directly — a second operator can no longer be created via the app) and
	// a pending guru. Both start pending_otp so the verify flow is exercised.
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
// account. A NULL operator code is inherited as raw NULL — never the ”
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
		Username:     "filler-audit",
		PasswordHash: "pass-filler-audit",
		Status:       models.UserStatusActive,
		Instansi:     "SMK Audit",
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
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
	// An operator-created account is marked as a sub-account in the detail,
	// so the trail distinguishes it from SuperAdmin-created / self-registered
	// accounts at a glance.
	if !strings.Contains(detail, "sub-akun, dibuat operator") {
		t.Errorf("user_created audit detail = %q, want the sub-account marker", detail)
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

// TestUserDeleteAudited locks in the user_deleted audit trail: every
// successful account DELETE by an admin/operator appends an immutable
// admin_audit_logs row per removed account (exam_id NULL), with the acting
// username snapshotted. When the target is an operator with a real instansi,
// ONE row is written for the operator AND every sub-account the cascade
// removes, so the school-wide wipe leaves a complete trail. A rejected delete
// (target in another instansi) must NOT leak an audit row.
func TestUserDeleteAudited(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-del-audit", "SMK Del Audit", "pass-op-del-audit")
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

	// Operator creates two sub-accounts (user_created rows, quota 2).
	if status, resp := tc.createUser(t, "sub-del-1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-del-1: status=%d resp=%+v", status, resp)
	}
	if status, resp := tc.createUser(t, "sub-del-2"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-del-2: status=%d resp=%+v", status, resp)
	}
	sub1 := mustGetUser(t, pool, "sub-del-1")
	sub2 := mustGetUser(t, pool, "sub-del-2")

	// (a) Operator deletes its OWN sub-account → one user_deleted row, actor
	//     = operator, detail snapshots username + role, exam_id NULL.
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub1.ID)+"/delete", nil); status != http.StatusOK || !resp.Success {
		t.Fatalf("operator delete sub-del-1: status=%d resp=%+v", status, resp)
	}
	if n := countAudit(models.ActionUserDeleted); n != 1 {
		t.Fatalf("user_deleted rows after operator delete = %d, want 1", n)
	}
	var actor, detail string
	var examID *int
	if err := pool.QueryRow(ctx,
		`SELECT username, detail, exam_id FROM admin_audit_logs WHERE action = $1 ORDER BY id DESC LIMIT 1`,
		models.ActionUserDeleted).Scan(&actor, &detail, &examID); err != nil {
		t.Fatalf("read user_deleted audit row: %v", err)
	}
	if actor != "op-del-audit" {
		t.Errorf("user_deleted audit actor = %q, want the operator's username", actor)
	}
	if !strings.Contains(detail, "sub-del-1") || !strings.Contains(detail, "guru") {
		t.Errorf("user_deleted audit detail = %q, want the deleted account's username and role", detail)
	}
	if strings.Contains(detail, "ikut terhapus") {
		t.Errorf("user_deleted audit detail = %q, must NOT carry the cascade marker for a plain sub delete", detail)
	}
	if examID != nil {
		t.Errorf("user_deleted audit exam_id = %v, want NULL (no exam involved)", examID)
	}

	// (b) SuperAdmin deletes the OPERATOR → one user_deleted row per account
	//     the cascade removes: the operator + sub-del-2 (sub-del-1 is already
	//     gone). Cascade rows carry the marker.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-del-audit", Name: "Root Del Audit",
		PasswordHash: "pass-root-del-audit", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	if status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/delete", nil); status != http.StatusOK || !resp.Success {
		t.Fatalf("superadmin delete operator: status=%d resp=%+v", status, resp)
	}
	if n := countAudit(models.ActionUserDeleted); n != 3 { // sub-del-1 + operator + sub-del-2
		t.Fatalf("user_deleted rows after cascade = %d, want 3 (1 direct + 2 cascade)", n)
	}
	var opDetail, subDetail string
	if err := pool.QueryRow(ctx,
		`SELECT detail FROM admin_audit_logs WHERE action = $1 AND detail LIKE '%op-del-audit%' ORDER BY id DESC LIMIT 1`,
		models.ActionUserDeleted).Scan(&opDetail); err != nil {
		t.Fatalf("read operator user_deleted row: %v", err)
	}
	if strings.Contains(opDetail, "ikut terhapus") {
		t.Errorf("operator's own user_deleted detail = %q, must NOT carry the cascade marker", opDetail)
	}
	if err := pool.QueryRow(ctx,
		`SELECT detail FROM admin_audit_logs WHERE action = $1 AND detail LIKE '%sub-del-2%' ORDER BY id DESC LIMIT 1`,
		models.ActionUserDeleted).Scan(&subDetail); err != nil {
		t.Fatalf("read cascade sub user_deleted row: %v", err)
	}
	if !strings.Contains(subDetail, "sub-del-2") || !strings.Contains(subDetail, "ikut terhapus dengan operator") {
		t.Errorf("cascade sub user_deleted detail = %q, want username + cascade marker", subDetail)
	}
	if _, err := models.GetUserByID(ctx, pool, sub2.ID); err == nil {
		t.Error("sub-del-2 must be gone after the operator cascade delete")
	}

	// (c) A rejected delete (operator targets an account in ANOTHER instansi)
	//     writes NO user_deleted row. Create a third operator (own school
	//     voucher, so it holds the operator role) and a sub-account in a
	//     different school, then try the cross-instansi delete.
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-3")
	opThird := createOperatorUser(t, pool, "op-third-school", "SMK Ketiga", "pass-op-third")
	thirdTc := newVoucherTestClient(t, pool)
	thirdTc.login(t, opThird.ID)
	thirdTc.redeem(t, "IT-SEKOLAH-3")
	if _, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "sub-other", Name: "Sub Other",
		PasswordHash: "pass-sub-other", Status: models.UserStatusActive,
		Instansi: "SMK Lain",
		Role:     models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		OperatorCreated: true, CreatedBy: &opThird.ID,
	}); err != nil {
		t.Fatalf("create sub-other: %v", err)
	}
	otherSub := mustGetUser(t, pool, "sub-other")
	before := countAudit(models.ActionUserDeleted)
	if status, resp := postJSON(t, thirdTc.client, thirdTc.srv, "/api/users/"+strconv.Itoa(otherSub.ID)+"/delete", nil); status != http.StatusBadRequest {
		t.Fatalf("cross-instansi delete: status=%d resp=%+v, want 400", status, resp)
	}
	if after := countAudit(models.ActionUserDeleted); after != before {
		t.Errorf("user_deleted rows after rejected delete = %d, want %d (no audit leak)", after, before)
	}
}

// TestOperatorAccountQuotaFailClosed locks in the fail-closed rule: a REAL
// database error while reading the sub-account quota must surface as an error
// (never as a silent unlimited 0), so the CreateUser enforcement rejects the
// request instead of letting the operator create unlimited accounts on a
// transient glitch. The legitimate "no active redemption" outcome (ErrNoRows)
// still falls back to package_settings.
func TestOperatorAccountQuotaFailClosed(t *testing.T) {
	ctx := context.Background()

	// (a) A real DB error on the redemption query → error returned, never 0.
	if _, _, err := loadOperatorAccountQuota(ctx, &stagedQuerier{err: fmt.Errorf("simulated connection drop")}, 1, true, models.InstansiScope{Name: "SMK X"}); err == nil {
		t.Error("loadOperatorAccountQuota(real DB error) = nil, want a fail-closed error")
	}

	// (b) A real DB error on the package_settings fallback (after the
	// redemption read returns ErrNoRows) → error returned, never 0.
	errQuerier := &stagedQuerier{firstErr: pgx.ErrNoRows, err: fmt.Errorf("simulated connection drop")}
	if _, _, err := loadOperatorAccountQuota(ctx, errQuerier, 1, true, models.InstansiScope{Name: "SMK X"}); err == nil {
		t.Error("loadOperatorAccountQuota(fallback DB error) = nil, want a fail-closed error")
	}

	// (c) The legitimate "no active redemption" outcome (ErrNoRows on the
	// redemption query) falls back to package_settings WITHOUT an error — the
	// fail-closed rule must not break the legacy fallback path. The staged
	// querier returns ErrNoRows on both queries, so the fallback itself
	// yields the ErrNoRows → 0 (unlimited) anomaly handling, not an error.
	noRowsQuerier := &stagedQuerier{err: pgx.ErrNoRows}
	maxUsers, used, err := loadOperatorAccountQuota(ctx, noRowsQuerier, 1, true, models.InstansiScope{Name: "SMK X"})
	if err != nil {
		t.Errorf("loadOperatorAccountQuota(ErrNoRows) = err %v, want nil (legacy fallback still works)", err)
	}
	if maxUsers != 0 || used != 0 {
		t.Errorf("loadOperatorAccountQuota(ErrNoRows) = (%d,%d), want (0,0) — no package row → unlimited", maxUsers, used)
	}
}

// stagedQuerier returns a programmable error for the FIRST QueryRow and
// pgx.ErrNoRows for later ones (or vice versa via err), so tests can exercise
// the redemption-query vs package-fallback branches of
// loadOperatorAccountQuota deterministically.
type stagedQuerier struct {
	firstErr error
	err      error
	calls    int
}

func (q *stagedQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	q.calls++
	if q.calls == 1 && q.firstErr != nil {
		return errRow{err: q.firstErr}
	}
	return errRow{err: q.err}
}

type errRow struct{ err error }

func (r errRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return fmt.Errorf("simulated connection drop")
}

// TestOperatorCreateEmptyInstansiFailClosed locks in the empty-instansi guard:
// an operator whose DB row has an EMPTY instansi (anomalous data — a healthy
// operator always has "personal" or a real school) must NOT be able to create
// accounts. Without the guard, the quota check was skipped (instansi == ""
// short-circuits loadOperatorAccountQuota to 0,0 = unlimited) and the account
// silently landed in the shared "personal" bucket outside the school quota.
func TestOperatorCreateEmptyInstansiFailClosed(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "op-empty-inst", Name: "Op Empty Inst",
		PasswordHash: "pass-op-empty", Status: models.UserStatusActive,
		Instansi: "", // anomalous — no instansi at all
		Role:     models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create empty-instansi operator: %v", err)
	}

	// Promote to operator role so the create path treats it as an operator.
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET role = $1 WHERE id = $2`,
		models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}), op.ID); err != nil {
		t.Fatalf("promote to operator: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)

	// The handler must fail CLOSED (500), never create into "personal".
	status, resp := tc.createUser(t, "sub-empty-inst")
	if status != http.StatusInternalServerError {
		t.Fatalf("create as empty-instansi operator: status=%d resp=%+v, want 500 fail-closed", status, resp)
	}
	if _, err := models.GetUserByUsername(ctx, pool, "sub-empty-inst"); err == nil {
		t.Error("sub-empty-inst must NOT exist — the create must have aborted")
	}
}

// TestSubAccountQuotaForcedFreeDefaults locks in the sub-account quota
// policy: the per-account quota columns of an operator-created account are
// FORCED to the free defaults (3 exams / 1 MB PDF / 2 concurrent / 50 MB
// storage — the same values a fresh self-registered account carries) no
// matter what the create request sends. A hand-crafted request posting huge
// values (99 concurrent, 20 MB PDF, 200 MB storage) must not be able to grant
// a sub-account a bigger per-account quota than the free tier: those columns
// are the ONLY gate when the school pool is inactive (shared "personal"
// bucket, legacy school without an active redemption), and there they must
// stay at the free defaults. The billing page relies on the same contract
// (it shows the school pool for pool-active sub-accounts and the free
// defaults otherwise).
func TestSubAccountQuotaForcedFreeDefaults(t *testing.T) {
	pool := setupVoucherITDB(t)

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-forced-quota", "SMK Forced Quota", "pass-op-forced")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")

	// Tampered create: per-account values far above the free defaults (kept
	// below the global upload cap and any plausible free-disk cap so the
	// request passes the pre-tx validators and reaches the operator path).
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username":             "sub-tampered",
		"password":             "pass-sub-tampered",
		"name":                 "Sub Tampered",
		"roles":                []string{models.RoleGuru},
		"max_exams":            99,
		"max_concurrent_exams": 99,
		"max_pdf_size_mb":      20.0,
		"max_storage_size_mb":  200.0,
	})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-tampered: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub-tampered")
	if sub.Package != "free" || sub.MaxExams != 3 || sub.MaxPDFSize != 1048576 ||
		sub.MaxConcurrentExams != 2 || sub.MaxStorageSize != 50*1024*1024 {
		t.Fatalf("sub-tampered row must hold the forced free defaults, got package=%q exams=%d pdf=%d concurrent=%d storage=%d",
			sub.Package, sub.MaxExams, sub.MaxPDFSize, sub.MaxConcurrentExams, sub.MaxStorageSize)
	}
}

// TestSecondOperatorSchoolQuotaShared locks in the school-wide quota for a
// real school instansi: the sub-account cap comes from the SCHOOL's operators
// (the active redemption(s) of the instansi, MAXed like schoolPoolQuota
// ForInstansi), NOT from the acting operator's own redemption. Before this
// rule, a SECOND operator of the same school WITHOUT an active redemption
// fell back to its own package label (free → 0 = unlimited) and could create
// accounts past the school's max_users. Here opA holds the active school
// package (2 accounts) and opB is a redemption-less co-operator: opB must be
// capped by the same school quota — it may not create once the school is at
// its cap.
func TestSecondOperatorSchoolQuotaShared(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool) // max_users = 2
	opA := createOperatorUser(t, pool, "op-quota-a", "SMK Shared Quota", "pass-op-quota-a")
	tcA := newVoucherTestClient(t, pool)
	tcA.login(t, opA.ID)
	// opA must redeem FIRST — the one-operator-per-school policy blocks a
	// redeem while a peer operator already exists in the school.
	tcA.redeem(t, "IT-SEKOLAH")

	// opB: a second operator of the same school, planted directly (a
	// pre-policy state — the app now blocks creating one). No active
	// redemption, free package label.
	opB, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "op-quota-b", Name: "Op Quota B",
		PasswordHash: "pass-op-quota-b", Status: models.UserStatusActive,
		Instansi: "SMK Shared Quota", // same school as opA
		Role:     models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free", // no redemption, free label
	})
	if err != nil {
		t.Fatalf("create co-operator op-quota-b: %v", err)
	}

	// opB occupies one slot of the school's 2-account quota (the count is
	// every account in the instansi except the acting operator), so opA can
	// create exactly one sub-account before the school is at its cap.
	if status, resp := tcA.createUser(t, "sub-shared-1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("opA create sub-shared-1: status=%d resp=%+v", status, resp)
	}

	// opB (no active redemption) must be bound by the SAME school cap: with
	// the school at max_users, its create is rejected. Before the school-wide
	// rule it fell back to its free package label (0 = unlimited) and sailed
	// through.
	tcB := newVoucherTestClient(t, pool)
	tcB.login(t, opB.ID)
	if status, resp := tcB.createUser(t, "sub-shared-2"); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Kuota akun") {
		t.Fatalf("opB create at school cap: status=%d resp=%+v, want 400 quota rejection", status, resp)
	}
	if _, err := models.GetUserByUsername(ctx, pool, "sub-shared-2"); err == nil {
		t.Error("sub-shared-2 must NOT exist — the second operator bypassed the school quota")
	}

	// And opA itself is equally capped now: a third create is rejected too.
	if status, resp := tcA.createUser(t, "sub-shared-3"); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Kuota akun") {
		t.Fatalf("opA create at school cap: status=%d resp=%+v, want 400 quota rejection", status, resp)
	}
}

// TestConcurrentCreateAcrossOperatorsSameSchool locks in the cross-operator
// serialization of the sub-account quota: concurrent creates by DIFFERENT
// operators of the same school must never overshoot the shared max_users.
// Each operator's FOR UPDATE row lock alone only serializes same-operator
// creates — two operators could both count the school's free slots and both
// insert. The create transaction therefore also takes a transaction-scoped
// advisory lock keyed on the instansi, so ALL of the school's creates
// serialize on one counter. 8 concurrent creates (4 via opA, 4 via opB)
// against a 2-account school (with opB occupying one slot) must yield exactly
// one new account.
func TestConcurrentCreateAcrossOperatorsSameSchool(t *testing.T) {
	pool := setupVoucherITDB(t)

	createSchoolVoucher(t, pool) // max_users = 2
	opA := createOperatorUser(t, pool, "op-race-a", "SMK Cross Race", "pass-op-race-a")
	tcA := newVoucherTestClient(t, pool)
	tcA.login(t, opA.ID)
	// opA must redeem FIRST — the one-operator-per-school policy blocks a
	// redeem while a peer operator already exists in the school.
	tcA.redeem(t, "IT-SEKOLAH")

	// opB: a second operator of the same school, planted directly (pre-
	// policy state — the app now blocks creating one).
	opB, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: "op-race-b", Name: "Op Race B",
		PasswordHash: "pass-op-race-b", Status: models.UserStatusActive,
		Instansi: "SMK Cross Race",
		Role:     models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create co-operator op-race-b: %v", err)
	}
	tcB := newVoucherTestClient(t, pool)
	tcB.login(t, opB.ID)

	const attempts = 8 // 4 per operator, far above the 1 free slot (2 - opB)
	var wg sync.WaitGroup
	created := 0
	var mu sync.Mutex
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tc := tcA
			if i%2 == 1 {
				tc = tcB
			}
			status, resp := tc.createUser(t, fmt.Sprintf("xrace%d", i))
			if status == http.StatusOK && resp.Success {
				mu.Lock()
				created++
				mu.Unlock()
				return
			}
			if status != http.StatusBadRequest || !strings.Contains(resp.Message, "Kuota akun") {
				t.Errorf("create %d: status=%d resp=%+v, want 200 or the quota 400", i, status, resp)
			}
		}(i)
	}
	wg.Wait()

	if created != 1 {
		t.Errorf("cross-operator concurrent creates: %d succeeded, want exactly 1 (school at 2/2 with opB counted) — the shared quota raced", created)
	}
	var total int64
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_users WHERE username LIKE 'xrace%'`).Scan(&total); err != nil {
		t.Fatalf("count cross-race accounts: %v", err)
	}
	if total != 1 {
		t.Errorf("DB has %d xrace* accounts, want exactly 1 (no overshoot across operators)", total)
	}
}

// TestUniqueViolationMessage locks in the duplicate-key → friendly-400 mapping
// used when the admin_users INSERT in CreateUser hits a unique constraint the
// pre-tx checks raced past (two concurrent requests for the same username /
// email both pass GetUserByUsername / GetUserByEmail, the loser lands here).
func TestUniqueViolationMessage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "username duplicate (concurrent create loser)",
			err:  fmt.Errorf("create user: %w", &pgconn.PgError{Code: "23505", ConstraintName: "admin_users_username_key"}),
			want: "Username sudah digunakan",
		},
		{
			name: "email duplicate (concurrent create loser)",
			err:  fmt.Errorf("create user: %w", &pgconn.PgError{Code: "23505", ConstraintName: "uq_admin_users_email"}),
			want: "Email sudah terdaftar. Gunakan email lain atau biarkan kosong.",
		},
		{
			name: "real DB error is NOT a duplicate",
			err:  fmt.Errorf("create user: %w", &pgconn.PgError{Code: "08006"}), // connection failure
			want: "",
		},
		{
			name: "non-PgError is NOT a duplicate",
			err:  fmt.Errorf("create user: boom"),
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := uniqueViolationMessage(tc.err); got != tc.want {
				t.Errorf("uniqueViolationMessage(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestConcurrentDuplicateCreateFriendly400 locks in the concurrent-duplicate
// outcome of CreateUser: when two requests race for the same username (or the
// same email) and both pass the pre-tx uniqueness checks, the loser must get
// the SAME friendly 400 as a sequential duplicate — never a raw 500 unique-
// violation. Two operators in DIFFERENT schools keep the per-instansi
// advisory lock from serializing them, so the race genuinely reaches the DB
// constraint (the same username is unique across the whole table).
func TestConcurrentDuplicateCreateFriendly400(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// School vouchers with UNLIMITED sub-account quota (CustomMaxUsers = 0),
	// so the ONLY failure mode of the race is the duplicate key — the school
	// cap must not interfere with counting winners/losers.
	for _, code := range []string{"IT-SEKOLAH-U1", "IT-SEKOLAH-U2"} {
		if _, err := models.CreateVoucher(ctx, pool, &models.Voucher{
			Code: code, Package: "sekolah-test", DurationType: "bulanan",
			MaxUsage: 1, IsActive: true,
			IsCustom: true, CustomLabel: "sekolah-test",
			CustomMaxExams: 3, CustomMaxPDFSize: 50 * 1024 * 1024,
			CustomMaxConcurrentExams: 3, CustomMaxStorageSize: 500 * 1024 * 1024,
			CustomMaxUsers: 0, CustomRole: models.SerializeRoles([]string{models.RoleOperator}),
		}); err != nil {
			t.Fatalf("create unlimited school voucher %s: %v", code, err)
		}
	}
	opA := createOperatorUser(t, pool, "op-dup-a", "SMK Dup A", "pass-op-dup-a")
	opB := createOperatorUser(t, pool, "op-dup-b", "SMK Dup B", "pass-op-dup-b")
	tcA := newVoucherTestClient(t, pool)
	tcA.login(t, opA.ID)
	tcA.redeem(t, "IT-SEKOLAH-U1")
	tcB := newVoucherTestClient(t, pool)
	tcB.login(t, opB.ID)
	tcB.redeem(t, "IT-SEKOLAH-U2")

	// (a) Same username, different schools: exactly one may win; every loser
	//     must see the friendly 400 (pre-check OR constraint path — the same
	//     message either way).
	run := func(makePayload func(i int) map[string]interface{}, wantMsg string) (ok, friendly, unexpected int) {
		t.Helper()
		const attempts = 8
		var wg sync.WaitGroup
		var mu sync.Mutex
		for i := 0; i < attempts; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				tc := tcA
				if i%2 == 1 {
					tc = tcB
				}
				status, resp := postJSON(t, tc.client, tc.srv, "/api/users", makePayload(i))
				mu.Lock()
				defer mu.Unlock()
				switch {
				case status == http.StatusOK && resp.Success:
					ok++
				case status == http.StatusBadRequest && strings.Contains(resp.Message, wantMsg):
					friendly++
				default:
					unexpected++
					t.Errorf("create attempt %d: status=%d resp=%+v, want 200 or 400 %q", i, status, resp, wantMsg)
				}
			}(i)
		}
		wg.Wait()
		return
	}

	// (a) Same username, different schools: exactly one may win; every loser
	//     must see the friendly 400 (pre-check OR constraint path — the same
	//     message either way).
	ok, friendly, unexpected := run(func(i int) map[string]interface{} {
		return map[string]interface{}{
			"username": "dup-user",
			"password": "pass-dup-user",
			"name":     "Dup User",
			"roles":    []string{models.RoleGuru},
		}
	}, "Username sudah digunakan")
	if ok != 1 || friendly != 7 || unexpected != 0 {
		t.Errorf("same-username race: ok=%d friendly400=%d unexpected=%d, want 1/7/0", ok, friendly, unexpected)
	}

	// (b) Same email, different (unique) usernames: the unique partial index
	//     uq_admin_users_email is the only collision — same contract.
	ok, friendly, unexpected = run(func(i int) map[string]interface{} {
		return map[string]interface{}{
			"username": fmt.Sprintf("dup-email-%d", i),
			"password": "pass-dup-email",
			"name":     "Dup Email",
			"roles":    []string{models.RoleGuru},
			"email":    "dup-race@example.com",
		}
	}, "Email sudah terdaftar")
	if ok != 1 || friendly != 7 || unexpected != 0 {
		t.Errorf("same-email race: ok=%d friendly400=%d unexpected=%d, want 1/7/0", ok, friendly, unexpected)
	}
}

// TestMultiOperatorDowngradeKeepsSchoolAlive locks in the multi-operator
// behavior of the redeem/activate cascade. The one-operator-per-school policy
// now blocks the REAL redeem path for a second operator, but a multi-operator
// school can still exist from legacy/pre-policy rows (planted here directly,
// mirroring the pre-policy claims), and the school pool deliberately MAXes
// the operators' active redemptions (schoolPoolQuotaForInstansi). When ONE of
// them downgrades to a non-school package (redeeming a guru voucher drops the
// operator role), the school must NOT be torn down while ANOTHER operator
// still runs an active school package — the sub-accounts must keep working
// and the school pool must stay live. Before the fix the suspend branch of
// syncInstansiWithOperatorRole suspended every non-operator account of the
// instansi whenever the acting operator lost the role, even though opA still
// covered the school.
func TestMultiOperatorDowngradeKeepsSchoolAlive(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-1")   // opA's school voucher (max_users = 2)
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-SUB") // opB's planted state
	createGuruVoucherCode(t, pool, "IT-GURU-MULTI")    // opB's downgrade target
	opA := createOperatorUser(t, pool, "op-multi-a", "SMK Multi Op", "pass-op-multi-a")
	opB := createOperatorUser(t, pool, "op-multi-b", "SMK Multi Op", "pass-op-multi-b")
	tcA := newVoucherTestClient(t, pool)
	tcA.login(t, opA.ID)
	tcA.redeem(t, "IT-SEKOLAH-1")
	// opB is a SECOND operator planted directly (pre-policy state): the
	// one-operator-per-school policy now blocks the real redeem path, so a
	// multi-operator school can only arise from legacy/pre-policy rows. The
	// downgrade cascade must still tolerate that state — opA covers the
	// school after opB's downgrade.
	claimSubOwnSchoolVoucher(t, pool, opB.ID)
	if !models.HasRole(mustGetUser(t, pool, "op-multi-b").Role, models.RoleOperator) {
		t.Fatal("fixture: opB must hold the operator role (planted pre-policy state)")
	}
	tcB := newVoucherTestClient(t, pool)
	tcB.login(t, opB.ID)

	// opA creates one sub-account (opB occupies one of the 2 quota slots, so
	// the school has exactly one slot left).
	if status, resp := tcA.createUser(t, "sub-multi"); status != http.StatusOK || !resp.Success {
		t.Fatalf("opA create sub-multi: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub-multi")
	if status, cascade, _ := subFlags(t, pool, sub.ID); status != models.UserStatusActive || cascade {
		t.Fatalf("fixture: sub-multi must be active and not cascade-suspended, got status=%s cascade=%v", status, cascade)
	}

	// opB downgrades to a guru package → its operator role is lost.
	tcB.redeem(t, "IT-GURU-MULTI")
	if models.HasRole(mustGetUser(t, pool, "op-multi-b").Role, models.RoleOperator) {
		t.Fatal("opB must have lost the operator role after switching to the guru package")
	}
	if !models.HasRole(mustGetUser(t, pool, "op-multi-a").Role, models.RoleOperator) {
		t.Fatal("opA must KEEP the operator role — it still runs the school package")
	}

	// The school pool must stay live via opA's active redemption.
	if _, _, _, _, _, ok := schoolPoolQuota(ctx, pool, opA.ID); !ok {
		t.Error("school pool must stay active via opA — the school package did not expire")
	}

	// And the sub-account must NOT have been cascade-suspended: opA still
	// covers the school, so opB's downgrade must not tear the school down.
	if status, cascade, _ := subFlags(t, pool, sub.ID); status != models.UserStatusActive || cascade {
		t.Errorf("sub-multi must stay active after opB's downgrade (opA still runs the school), got status=%s cascade=%v", status, cascade)
	}
	// The school's unstarted exam must not be tombstoned either — same reason.
	var tombstoned int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM exams e JOIN admin_users u ON e.created_by = u.id
		  WHERE u.instansi = $1 AND e.tombstoned_at IS NOT NULL`, "SMK Multi Op").Scan(&tombstoned); err != nil {
		t.Fatalf("count tombstoned exams: %v", err)
	}
	if tombstoned != 0 {
		t.Errorf("no school exam must be tombstoned by opB's downgrade while opA covers the school, got %d", tombstoned)
	}
}

// oneOperatorPolicyMsg is the user-facing rejection message of the
// one-operator-per-school policy, asserted by TestOneOperatorPerSchoolPolicy.
const oneOperatorPolicyMsg = "Satu sekolah hanya dapat memiliki satu operator"

// TestOneOperatorPerSchoolPolicy locks in the ONE-OPERATOR-PER-SCHOOL policy:
// every path that would add a second operator to a school is rejected with a
// clear 400 — redeeming/activating a school package in a school that already
// has an operator, a SuperAdmin create/edit granting the operator role, and a
// personal-bucket operator claiming a school that already has one. The
// school's OWN operator renewing with a new code, a school WITHOUT an
// operator, the shared "personal" bucket, and legacy operator-created
// sub-accounts holding their own operator role are all never blocked.
func TestOneOperatorPerSchoolPolicy(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	const school = "SMK Satu Operator"
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-A")   // opA's first code
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-B")   // renewal + guru-B's blocked claim
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-C")   // personal opC's code
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-G")   // legacy-sub school guru
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-SUB") // claimSubOwnSchoolVoucher
	createGuruVoucherCode(t, pool, "IT-GURU-MULTI")

	// opA: the school's first (and only) operator.
	opA := createOperatorUser(t, pool, "op-satu-a", school, "pass-op-satu-a")
	tcA := newVoucherTestClient(t, pool)
	tcA.login(t, opA.ID)
	tcA.redeem(t, "IT-SEKOLAH-A")

	// guruB: a plain guru in the same school.
	guruB, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "guru-satu-b", Name: "Guru Satu B",
		PasswordHash: "pass-guru-satu-b", Status: models.UserStatusActive,
		Instansi: school, Role: models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create guruB: %v", err)
	}
	tcB := newVoucherTestClient(t, pool)
	tcB.login(t, guruB.ID)

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-satu", Name: "Root Satu",
		PasswordHash: "pass-root-satu", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tcRoot := newVoucherTestClient(t, pool)
	tcRoot.login(t, root.ID)

	t.Run("redeem second operator rejected", func(t *testing.T) {
		status, resp := postForm(t, tcB.client, tcB.srv, "/api/vouchers/redeem", url.Values{"code": {"IT-SEKOLAH-B"}})
		if status != http.StatusBadRequest || !strings.Contains(resp.Message, oneOperatorPolicyMsg) {
			t.Fatalf("guruB redeem school in school-with-operator: status=%d resp=%+v, want 400 policy message", status, resp)
		}
		bAfter := mustGetUser(t, pool, "guru-satu-b")
		if models.HasRole(bAfter.Role, models.RoleOperator) {
			t.Error("guruB must NOT have gained the operator role")
		}
		var redemptions int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM voucher_redemptions WHERE user_id = $1`, guruB.ID).Scan(&redemptions); err != nil {
			t.Fatalf("count guruB redemptions: %v", err)
		}
		if redemptions != 0 {
			t.Errorf("guruB must have no redemption row after the rejected claim, got %d", redemptions)
		}
	})

	t.Run("school's own operator renewal allowed", func(t *testing.T) {
		// opA redeeming a NEW school code is the school's operator renewing —
		// excluded from the check, never blocked.
		tcA.redeem(t, "IT-SEKOLAH-B")
		if !models.HasRole(mustGetUser(t, pool, "op-satu-a").Role, models.RoleOperator) {
			t.Fatal("opA must keep the operator role after renewing")
		}
	})

	t.Run("activate second operator rejected", func(t *testing.T) {
		// Plant a PAUSED school-package redemption for guruB (pre-policy claim
		// state) and try to activate it: activation would grant the operator
		// role in a school that already has one → rejected.
		var vid int
		if err := pool.QueryRow(ctx, `SELECT id FROM vouchers WHERE code = 'IT-SEKOLAH-B'`).Scan(&vid); err != nil {
			t.Fatalf("find IT-SEKOLAH-B voucher: %v", err)
		}
		var rid int
		if err := pool.QueryRow(ctx, `
			INSERT INTO voucher_redemptions
				(voucher_id, user_id, remaining_seconds, activated_at, is_active, package,
				 max_exams, max_pdf_size, max_concurrent_exams, max_storage_size, max_users, role)
			VALUES ($1, $2, $3, now(), false, 'sekolah-test', 3, 52428800, 3, 524288000, 2, $4)
			RETURNING id`,
			vid, guruB.ID, 30*86400, models.SerializeRoles([]string{models.RoleOperator})).Scan(&rid); err != nil {
			t.Fatalf("plant paused school redemption for guruB: %v", err)
		}
		status, resp := postForm(t, tcB.client, tcB.srv, "/api/vouchers/activate",
			url.Values{"redemption_id": {strconv.Itoa(rid)}})
		if status != http.StatusBadRequest || !strings.Contains(resp.Message, oneOperatorPolicyMsg) {
			t.Fatalf("guruB activate school package in school-with-operator: status=%d resp=%+v, want 400 policy message", status, resp)
		}
	})

	t.Run("superadmin create second operator rejected", func(t *testing.T) {
		status, resp := postJSON(t, tcRoot.client, tcRoot.srv, "/api/users", map[string]interface{}{
			"username": "op-satu-x", "password": "pass-op-satu-x",
			"name": "Op Satu X", "instansi": school,
			"roles": []string{models.RoleOperator},
		})
		if status != http.StatusBadRequest || !strings.Contains(resp.Message, oneOperatorPolicyMsg) {
			t.Fatalf("superadmin create operator in school-with-operator: status=%d resp=%+v, want 400 policy message", status, resp)
		}
		if _, err := models.GetUserByUsername(ctx, pool, "op-satu-x"); err == nil {
			t.Error("op-satu-x must NOT exist")
		}
	})

	t.Run("superadmin create operator in empty school allowed", func(t *testing.T) {
		status, resp := postJSON(t, tcRoot.client, tcRoot.srv, "/api/users", map[string]interface{}{
			"username": "op-satu-new", "password": "pass-op-satu-new",
			"name": "Op Satu New", "instansi": "SMK Baru",
			"roles": []string{models.RoleOperator},
		})
		if status != http.StatusOK || !resp.Success {
			t.Fatalf("superadmin create operator in new school: status=%d resp=%+v", status, resp)
		}
	})

	t.Run("superadmin edit grant operator rejected", func(t *testing.T) {
		status, resp := postJSON(t, tcRoot.client, tcRoot.srv, "/api/users/"+strconv.Itoa(guruB.ID)+"/edit", map[string]interface{}{
			"roles": []string{models.RoleGuru, models.RoleOperator},
		})
		if status != http.StatusBadRequest || !strings.Contains(resp.Message, oneOperatorPolicyMsg) {
			t.Fatalf("superadmin grant operator to guru in school-with-operator: status=%d resp=%+v, want 400 policy message", status, resp)
		}
		if models.HasRole(mustGetUser(t, pool, "guru-satu-b").Role, models.RoleOperator) {
			t.Error("guruB must NOT have gained the operator role")
		}
	})

	t.Run("superadmin edit grant pengawas allowed", func(t *testing.T) {
		status, resp := postJSON(t, tcRoot.client, tcRoot.srv, "/api/users/"+strconv.Itoa(guruB.ID)+"/edit", map[string]interface{}{
			"roles": []string{models.RoleGuru, models.RolePengawas},
		})
		if status != http.StatusOK || !resp.Success {
			t.Fatalf("superadmin grant pengawas: status=%d resp=%+v", status, resp)
		}
	})

	t.Run("personal operator claiming occupied school rejected", func(t *testing.T) {
		// opC: a personal-bucket operator (redeemed a school voucher but has
		// not claimed a school yet). Claiming a school that already has an
		// operator would make it operator #2 → rejected.
		opC := createOperatorUser(t, pool, "op-satu-c", "personal", "pass-op-satu-c")
		tcC := newVoucherTestClient(t, pool)
		tcC.login(t, opC.ID)
		tcC.redeem(t, "IT-SEKOLAH-C")
		if !models.HasRole(mustGetUser(t, pool, "op-satu-c").Role, models.RoleOperator) {
			t.Fatal("fixture: opC must hold the operator role in the personal bucket")
		}
		status, resp := postJSON(t, tcC.client, tcC.srv, "/api/instansi/update", map[string]interface{}{
			"instansi": school,
		})
		if status != http.StatusBadRequest || !strings.Contains(resp.Message, oneOperatorPolicyMsg) {
			t.Fatalf("personal operator claims occupied school: status=%d resp=%+v, want 400 policy message", status, resp)
		}
	})

	t.Run("legacy operator-created sub does not block a new operator", func(t *testing.T) {
		// A legacy operator-created sub holding its own operator role (pre-
		// policy claim, planted directly) is NOT the school's operator: it
		// spares itself from cascades but must not block a real operator from
		// taking over the school.
		const legacySchool = "SMK Legacy Sub"
		legacySub, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: "legacy-sub", Name: "Legacy Sub",
			PasswordHash: "pass-legacy-sub", Status: models.UserStatusActive,
			Instansi: legacySchool, Role: models.SerializeRoles([]string{models.RoleGuru}),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
			OperatorCreated: true,
		})
		if err != nil {
			t.Fatalf("create legacy sub: %v", err)
		}
		claimSubOwnSchoolVoucher(t, pool, legacySub.ID) // own operator role + active redemption
		guruD, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: "guru-satu-d", Name: "Guru Satu D",
			PasswordHash: "pass-guru-satu-d", Status: models.UserStatusActive,
			Instansi: legacySchool, Role: models.SerializeRoles([]string{models.RoleGuru}),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		})
		if err != nil {
			t.Fatalf("create guruD: %v", err)
		}
		tcD := newVoucherTestClient(t, pool)
		tcD.login(t, guruD.ID)
		status, resp := postForm(t, tcD.client, tcD.srv, "/api/vouchers/redeem", url.Values{"code": {"IT-SEKOLAH-G"}})
		if status != http.StatusOK || !resp.Success {
			t.Fatalf("guruD redeem school in legacy-sub school: status=%d resp=%+v, want allowed", status, resp)
		}
	})
}

// TestOperatorEditSubAccountQuotaNotForcedFree probes the EDIT side of the
// sub-account quota policy. CreateUser forces the per-account quota columns of
// an operator-created account to the free defaults (see
// TestSubAccountQuotaForcedFreeDefaults), but EditUser historically applied
// whatever quota values the request sent — so an operator could EDIT a
// sub-account it created and hand it max_storage_size_mb: 0 (= unlimited), 99
// concurrent, etc. Those columns are the ONLY gate when the school pool is
// inactive (shared "personal" bucket, legacy school without an active
// redemption), which makes the edit-time hole exactly as exploitable as the
// create-time one that was already fixed. The fix: on the operator edit path
// the four quota fields are IGNORED — the columns stay EXACTLY as they are
// (never forced to the free defaults, because forcing would silently destroy a
// quota a SuperAdmin deliberately raised). For a fresh sub the columns are the
// create-time free defaults, so the tamper is neutralized either way; a
// SuperAdmin grant must survive an unrelated operator edit.
func TestOperatorEditSubAccountQuotaNotForcedFree(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-edit-quota", "SMK Edit Quota", "pass-op-edit-quota")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if status, resp := tc.createUser(t, "sub-edit-quota"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-edit-quota: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub-edit-quota")
	if sub.MaxStorageSize != 50*1024*1024 || sub.MaxExams != 3 {
		t.Fatalf("fixture: sub-edit-quota must start at free defaults, got storage=%d exams=%d", sub.MaxStorageSize, sub.MaxExams)
	}

	// (a) Tampered edit: storage 0 = unlimited, 99 exams, 20 MB PDF, 99
	//     concurrent. The fields are ignored → columns stay free defaults.
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub.ID)+"/edit", map[string]interface{}{
		"max_exams":            99,
		"max_concurrent_exams": 99,
		"max_pdf_size_mb":      20.0,
		"max_storage_size_mb":  0.0, // 0 = tidak terbatas
	})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("operator edit sub-edit-quota quota: status=%d resp=%+v", status, resp)
	}
	if !strings.Contains(resp.Message, "tidak dapat diubah oleh operator") {
		t.Errorf("operator quota-block edit message = %q, want the blocked-quota note", resp.Message)
	}
	subAfter := mustGetUser(t, pool, "sub-edit-quota")
	if subAfter.MaxExams != 3 || subAfter.MaxPDFSize != 1048576 ||
		subAfter.MaxConcurrentExams != 2 || subAfter.MaxStorageSize != 50*1024*1024 {
		t.Fatalf("operator edit must leave the free defaults intact, got exams=%d pdf=%d concurrent=%d storage=%d",
			subAfter.MaxExams, subAfter.MaxPDFSize, subAfter.MaxConcurrentExams, subAfter.MaxStorageSize)
	}

	// (b) A SuperAdmin-raised quota must SURVIVE an unrelated operator edit:
	//     the edit modal always sends the quota fields, so a name-only edit by
	//     the operator would otherwise silently clobber the grant. The
	//     operator's request carries storage=200MB (what the modal would read
	//     from the row) and the grant must stay put.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-edit-quota", Name: "Root Edit Quota",
		PasswordHash: "pass-root-edit-quota", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	if status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(sub.ID)+"/edit", map[string]interface{}{
		"max_storage_size_mb": 200.0,
	}); status != http.StatusOK || !resp.Success {
		t.Fatalf("superadmin raise sub storage: status=%d resp=%+v", status, resp)
	}
	if mustGetUser(t, pool, "sub-edit-quota").MaxStorageSize != 200*1024*1024 {
		t.Fatalf("fixture: superadmin grant of 200MB must land, got %d", mustGetUser(t, pool, "sub-edit-quota").MaxStorageSize)
	}

	// Operator edits the NAME only; the modal also sends the quota fields
	// (pre-filled with the row's 200MB). The columns must stay 200MB — the
	// grant survives, only the name changes.
	status, resp = postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub.ID)+"/edit", map[string]interface{}{
		"name":                 "Sub Edit Quota Renamed",
		"max_exams":            3,
		"max_concurrent_exams": 2,
		"max_pdf_size_mb":      1.0,
		"max_storage_size_mb":  200.0,
	})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("operator name edit with modal quota fields: status=%d resp=%+v", status, resp)
	}
	subRenamed := mustGetUser(t, pool, "sub-edit-quota")
	if subRenamed.Name != "Sub Edit Quota Renamed" {
		t.Errorf("name must change, got %q", subRenamed.Name)
	}
	if subRenamed.MaxStorageSize != 200*1024*1024 {
		t.Errorf("superadmin-raised storage must survive the operator edit, got %d (was 200MB)", subRenamed.MaxStorageSize)
	}
	if subRenamed.MaxExams != 3 || subRenamed.MaxPDFSize != 1048576 || subRenamed.MaxConcurrentExams != 2 {
		t.Errorf("other quota columns must stay at their values, got exams=%d pdf=%d concurrent=%d",
			subRenamed.MaxExams, subRenamed.MaxPDFSize, subRenamed.MaxConcurrentExams)
	}
}

// TestSuperAdminMoveOperatorIntoOccupiedSchool probes the one-operator-per-
// school policy on the EDIT-INSTANSI path. A SuperAdmin editing an existing
// operator and changing its instansi to a school that ALREADY has an operator
// moves a second operator in — the same policy violation the redeem/activate/
// create/edit-role/claim paths already reject. The fix must apply
// schoolAlreadyHasOperator against the DESTINATION school before the move
// (with the acting target excluded, so a school rename of the school's own
// operator stays allowed).
func TestSuperAdminMoveOperatorIntoOccupiedSchool(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucherCode(t, pool, "IT-A")
	createSchoolVoucherCode(t, pool, "IT-B")
	opA := createOperatorUser(t, pool, "op-move-a", "Sekolah A", "pass-op-move-a")
	opB := createOperatorUser(t, pool, "op-move-b", "Sekolah B", "pass-op-move-b")
	tcA := newVoucherTestClient(t, pool)
	tcA.login(t, opA.ID)
	tcA.redeem(t, "IT-A") // opA becomes the operator of Sekolah A
	tcB := newVoucherTestClient(t, pool)
	tcB.login(t, opB.ID)
	tcB.redeem(t, "IT-B") // opB becomes the operator of Sekolah B

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-move", Name: "Root Move",
		PasswordHash: "pass-root-move", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	// SuperAdmin moves opA into Sekolah B, which already has operator opB.
	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(opA.ID)+"/edit", map[string]interface{}{
		"instansi": "Sekolah B",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("move opA into occupied Sekolah B: status=%d resp=%+v, want 400 (one-operator-per-school)", status, resp)
	}
	if !strings.Contains(resp.Message, "satu operator") {
		t.Errorf("move opA into occupied Sekolah B: message=%q, want the one-operator-per-school message", resp.Message)
	}
	opAAfter := mustGetUser(t, pool, "op-move-a")
	if opAAfter.Instansi != "Sekolah A" {
		t.Errorf("opA must stay in Sekolah A after the rejected move, got %q", opAAfter.Instansi)
	}
}

// TestEditRoleAndInstansiCombinedBypassesPolicy probes a second gap in the
// EditUser one-operator-per-school guard: the role-grant check runs against
// the target's CURRENT instansi, so a single request that BOTH moves a guru
// into a school that already has an operator AND grants the operator role
// checks the OLD school (no operator → passes) and lands the guru as a second
// operator of the destination. The fix must resolve the instansi BEFORE the
// role check: when the request also changes instansi, the check runs against
// the destination.
func TestEditRoleAndInstansiCombinedBypassesPolicy(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucherCode(t, pool, "IT-C")
	createSchoolVoucherCode(t, pool, "IT-D")
	opB := createOperatorUser(t, pool, "op-comb-b", "Sekolah D", "pass-op-comb-b")
	tcB := newVoucherTestClient(t, pool)
	tcB.login(t, opB.ID)
	tcB.redeem(t, "IT-D") // opB becomes the operator of Sekolah D

	// A guru in Sekolah C (no operator there).
	guru, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "guru-comb-c", Name: "Guru Comb C",
		PasswordHash: "pass-guru-comb-c", Status: models.UserStatusActive,
		Instansi: "Sekolah C", Role: models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create guru-comb-c: %v", err)
	}

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-comb", Name: "Root Comb",
		PasswordHash: "pass-root-comb", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	// One request: move the guru into occupied Sekolah D AND grant operator.
	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(guru.ID)+"/edit", map[string]interface{}{
		"instansi": "Sekolah D",
		"roles":    []string{models.RoleOperator},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("guru into occupied Sekolah D + operator role: status=%d resp=%+v, want 400 (one-operator-per-school)", status, resp)
	}
	if !strings.Contains(resp.Message, "satu operator") {
		t.Errorf("combined edit: message=%q, want the one-operator-per-school message", resp.Message)
	}
	guruAfter := mustGetUser(t, pool, "guru-comb-c")
	if guruAfter.Instansi != "Sekolah C" || models.HasRole(guruAfter.Role, models.RoleOperator) {
		t.Errorf("guru must stay a non-operator in Sekolah C after the rejected edit, got instansi=%q role=%q", guruAfter.Instansi, guruAfter.Role)
	}
}

// TestConcurrentSchoolClaimSerialized locks in the UpdateInstansi race fix:
// two personal-bucket operators claiming the SAME school name concurrently
// serialize on the school-claim advisory lock, so exactly ONE becomes the
// school's operator and the other is rejected with the one-operator-per-school
// 400. Before the fix the guard ran outside any lock/transaction, so both
// concurrent requests read the pre-claim snapshot and BOTH became operators of
// one school — a second operator with a full school package, exactly what the
// policy forbids.
func TestConcurrentSchoolClaimSerialized(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucherCode(t, pool, "IT-CLAIM-A")
	createSchoolVoucherCode(t, pool, "IT-CLAIM-B")
	opA := createOperatorUser(t, pool, "op-claim-a", "personal", "pass-op-claim-a")
	opB := createOperatorUser(t, pool, "op-claim-b", "personal", "pass-op-claim-b")
	tcA := newVoucherTestClient(t, pool)
	tcA.login(t, opA.ID)
	tcA.redeem(t, "IT-CLAIM-A") // opA becomes an operator (still in the personal bucket)
	tcB := newVoucherTestClient(t, pool)
	tcB.login(t, opB.ID)
	tcB.redeem(t, "IT-CLAIM-B") // opB becomes an operator (still in the personal bucket)

	const school = "SMK Race Claim"
	statuses := make([]int, 2)
	messages := make([]string, 2)
	var wg sync.WaitGroup
	for i, tc := range []*voucherTestClient{tcA, tcB} {
		wg.Add(1)
		go func(i int, tc *voucherTestClient) {
			defer wg.Done()
			status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update", map[string]interface{}{"instansi": school})
			statuses[i] = status
			messages[i] = resp.Message
		}(i, tc)
	}
	wg.Wait()

	// Exactly one claim succeeds; the loser gets the policy 400.
	successes, rejections, other := 0, 0, 0
	for i := range statuses {
		switch {
		case statuses[i] == http.StatusOK:
			successes++
		case statuses[i] == http.StatusBadRequest && strings.Contains(messages[i], "satu operator"):
			rejections++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent claims: %d unexpected responses (statuses=%v messages=%v)", other, statuses, messages)
	}
	if successes != 1 {
		t.Errorf("concurrent claims: %d succeeded, want exactly 1 — the claim raced and both became operators", successes)
	}
	if rejections != 1 {
		t.Errorf("concurrent claims: %d rejected by the policy, want exactly 1", rejections)
	}

	// The school ends up with exactly one operator.
	var ops int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_users
		 WHERE LOWER(instansi) = LOWER($1) AND role ILIKE '%"operator"%' AND NOT operator_created`,
		school).Scan(&ops); err != nil {
		t.Fatalf("count school operators: %v", err)
	}
	if ops != 1 {
		t.Errorf("school %q has %d operators, want exactly 1", school, ops)
	}
}

// TestSuperAdminConcurrentOperatorMoveSerialized locks in the
// one-operator-per-school serialization on the SuperAdmin EDIT move-operator
// path: two concurrent edits moving two operators (each from its own school)
// into the SAME empty destination school serialize on the school-claim
// advisory lock (the check and the UPDATE share one transaction), so exactly
// ONE lands as the destination's operator and the other gets the policy 400
// while staying in its own school. Before the fix the check ran outside any
// lock, so both concurrent moves read the pre-move snapshot and the
// destination ended up with two operators.
func TestSuperAdminConcurrentOperatorMoveSerialized(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	ops := make([]models.AdminUser, 2)
	for i := range ops {
		op, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: fmt.Sprintf("op-move-race-%d", i), Name: fmt.Sprintf("Op Move Race %d", i),
			PasswordHash: fmt.Sprintf("pass-op-move-race-%d", i), Status: models.UserStatusActive,
			Instansi: fmt.Sprintf("SMK Origin %d", i),
			Role:     models.SerializeRoles([]string{models.RoleOperator}),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		})
		if err != nil {
			t.Fatalf("create operator %d: %v", i, err)
		}
		ops[i] = *op
	}

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-move-race", Name: "Root Move Race",
		PasswordHash: "pass-root-move-race", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	const dest = "SMK Move Dest"
	statuses := make([]int, len(ops))
	messages := make([]string, len(ops))
	var wg sync.WaitGroup
	for i, op := range ops {
		wg.Add(1)
		go func(i int, op models.AdminUser) {
			defer wg.Done()
			status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/edit",
				map[string]interface{}{"instansi": dest})
			statuses[i] = status
			messages[i] = resp.Message
		}(i, op)
	}
	wg.Wait()

	successes, rejections, other := 0, 0, 0
	for i := range statuses {
		switch {
		case statuses[i] == http.StatusOK:
			successes++
		case statuses[i] == http.StatusBadRequest && strings.Contains(messages[i], "satu operator"):
			rejections++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent operator moves: %d unexpected responses (statuses=%v messages=%v)", other, statuses, messages)
	}
	if successes != 1 {
		t.Errorf("concurrent operator moves: %d succeeded, want exactly 1 — the check raced and two operators landed", successes)
	}
	if rejections != 1 {
		t.Errorf("concurrent operator moves: %d rejected by the policy, want exactly 1", rejections)
	}

	// The destination school ends up with exactly one operator.
	var opsInDest int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_users
		 WHERE LOWER(instansi) = LOWER($1) AND role ILIKE '%"operator"%' AND NOT operator_created`,
		dest).Scan(&opsInDest); err != nil {
		t.Fatalf("count destination operators: %v", err)
	}
	if opsInDest != 1 {
		t.Errorf("destination %q has %d operators, want exactly 1", dest, opsInDest)
	}
}

// TestConcurrentActivateDifferentPackagesSerialized locks in the
// one-operator-per-school serialization on the ACTIVATE path for DIFFERENT
// packages: several gurus of the SAME school, each holding a claimed-but-
// inactive school-package redemption, activating those redemptions
// concurrently serialize on the school-claim advisory lock (the redemption
// row lock alone only serializes same-row operations — every guru has its OWN
// row), so exactly ONE becomes the school's operator and every other
// activation gets the policy 400. Before the fix the guard ran inside each tx
// against the pre-commit snapshot, so the concurrent activations all passed
// and the school ended up with multiple operators. Four actors make the
// pre-fix race reliably observable (with two, natural timing can serialize
// them and mask the bug).
func TestConcurrentActivateDifferentPackagesSerialized(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucherCode(t, pool, "IT-ACT-1")
	createSchoolVoucherCode(t, pool, "IT-ACT-2")
	createSchoolVoucherCode(t, pool, "IT-ACT-3")
	createSchoolVoucherCode(t, pool, "IT-ACT-4")

	const school = "SMK Race Activate"
	const actors = 4
	gurus := make([]models.AdminUser, actors)
	redemptionIDs := make([]int, actors)
	for i := range gurus {
		g, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: fmt.Sprintf("guru-act-%d", i), Name: fmt.Sprintf("Guru Act %d", i),
			PasswordHash: fmt.Sprintf("pass-guru-act-%d", i), Status: models.UserStatusActive,
			Instansi: school, Role: models.SerializeRoles([]string{models.RoleGuru}),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		})
		if err != nil {
			t.Fatalf("create guru %d: %v", i, err)
		}
		gurus[i] = *g
		redemptionIDs[i] = plantInactiveSchoolRedemption(t, pool, g.ID, fmt.Sprintf("IT-ACT-%d", i+1))
	}

	clients := make([]*voucherTestClient, actors)
	for i := range clients {
		clients[i] = newVoucherTestClient(t, pool)
		clients[i].login(t, gurus[i].ID)
	}

	statuses := make([]int, actors)
	messages := make([]string, actors)
	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, resp := postForm(t, clients[i].client, clients[i].srv, "/api/vouchers/activate",
				url.Values{"redemption_id": {strconv.Itoa(redemptionIDs[i])}})
			statuses[i] = status
			messages[i] = resp.Message
		}(i)
	}
	wg.Wait()

	successes, rejections, other := 0, 0, 0
	for i := range statuses {
		switch {
		case statuses[i] == http.StatusOK:
			successes++
		case statuses[i] == http.StatusBadRequest && strings.Contains(messages[i], "satu operator"):
			rejections++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent activations: %d unexpected responses (statuses=%v messages=%v)", other, statuses, messages)
	}
	if successes != 1 {
		t.Errorf("concurrent activations: %d succeeded, want exactly 1 — the guard raced and multiple operators landed", successes)
	}
	if rejections != actors-1 {
		t.Errorf("concurrent activations: %d rejected by the policy, want %d", rejections, actors-1)
	}

	// The school ends up with exactly one operator.
	var ops int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_users
		 WHERE LOWER(instansi) = LOWER($1) AND role ILIKE '%"operator"%' AND NOT operator_created`,
		school).Scan(&ops); err != nil {
		t.Fatalf("count school operators: %v", err)
	}
	if ops != 1 {
		t.Errorf("school %q has %d operators, want exactly 1", school, ops)
	}
}

// TestSuperAdminConcurrentOperatorCreateSerialized locks in the
// one-operator-per-school serialization on the SuperAdmin CREATE path: two
// concurrent creates of operator accounts in the SAME empty school serialize
// on the school-claim advisory lock, so exactly ONE becomes the school's
// operator and the rest get the policy 400. Before the fix the check ran
// outside any lock/transaction, so concurrent requests all read the pre-insert
// snapshot and MULTIPLE operators landed in one school — the exact state the
// policy forbids.
func TestSuperAdminConcurrentOperatorCreateSerialized(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-race-op", Name: "Root Race Op",
		PasswordHash: "pass-root-race-op", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	const school = "SMK Race Op"
	const attempts = 4
	statuses := make([]int, attempts)
	messages := make([]string, attempts)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, resp := postJSON(t, rt.client, rt.srv, "/api/users", map[string]interface{}{
				"username": fmt.Sprintf("op-race-%d", i),
				"password": fmt.Sprintf("pass-op-race-%d", i),
				"name":     fmt.Sprintf("Op Race %d", i),
				"roles":    []string{models.RoleOperator},
				"instansi": school,
			})
			statuses[i] = status
			messages[i] = resp.Message
		}(i)
	}
	wg.Wait()

	successes, rejections, other := 0, 0, 0
	for i := range statuses {
		switch {
		case statuses[i] == http.StatusOK:
			successes++
		case statuses[i] == http.StatusBadRequest && strings.Contains(messages[i], "satu operator"):
			rejections++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent operator creates: %d unexpected responses (statuses=%v messages=%v)", other, statuses, messages)
	}
	if successes != 1 {
		t.Errorf("concurrent operator creates: %d succeeded, want exactly 1 — the check raced and multiple operators landed", successes)
	}
	if rejections != attempts-1 {
		t.Errorf("concurrent operator creates: %d rejected by the policy, want %d", rejections, attempts-1)
	}

	var ops int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_users
		 WHERE LOWER(instansi) = LOWER($1) AND role ILIKE '%"operator"%' AND NOT operator_created`,
		school).Scan(&ops); err != nil {
		t.Fatalf("count school operators: %v", err)
	}
	if ops != 1 {
		t.Errorf("school %q has %d operators, want exactly 1", school, ops)
	}
}

// TestSuperAdminConcurrentOperatorRoleGrantSerialized locks in the
// one-operator-per-school serialization on the SuperAdmin EDIT role-grant
// path: two concurrent grants of the operator role to two gurus in the SAME
// empty school serialize on the school-claim advisory lock (the check and the
// UPDATE share one transaction), so exactly ONE becomes the school's operator
// and the other gets the policy 400.
func TestSuperAdminConcurrentOperatorRoleGrantSerialized(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	const school = "SMK Race Edit"
	gurus := make([]models.AdminUser, 2)
	for i := range gurus {
		g, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: fmt.Sprintf("guru-race-%d", i), Name: fmt.Sprintf("Guru Race %d", i),
			PasswordHash: fmt.Sprintf("pass-guru-race-%d", i), Status: models.UserStatusActive,
			Instansi: school, Role: models.SerializeRoles([]string{models.RoleGuru}),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		})
		if err != nil {
			t.Fatalf("create guru %d: %v", i, err)
		}
		gurus[i] = *g
	}

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "root-race-edit", Name: "Root Race Edit",
		PasswordHash: "pass-root-race-edit", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	statuses := make([]int, len(gurus))
	messages := make([]string, len(gurus))
	var wg sync.WaitGroup
	for i, g := range gurus {
		wg.Add(1)
		go func(i int, g models.AdminUser) {
			defer wg.Done()
			status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(g.ID)+"/edit",
				map[string]interface{}{"roles": []string{models.RoleOperator}})
			statuses[i] = status
			messages[i] = resp.Message
		}(i, g)
	}
	wg.Wait()

	successes, rejections, other := 0, 0, 0
	for i := range statuses {
		switch {
		case statuses[i] == http.StatusOK:
			successes++
		case statuses[i] == http.StatusBadRequest && strings.Contains(messages[i], "satu operator"):
			rejections++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent role grants: %d unexpected responses (statuses=%v messages=%v)", other, statuses, messages)
	}
	if successes != 1 {
		t.Errorf("concurrent role grants: %d succeeded, want exactly 1 — the check raced and two operators landed", successes)
	}
	if rejections != 1 {
		t.Errorf("concurrent role grants: %d rejected by the policy, want exactly 1", rejections)
	}

	var ops int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_users
		 WHERE LOWER(instansi) = LOWER($1) AND role ILIKE '%"operator"%' AND NOT operator_created`,
		school).Scan(&ops); err != nil {
		t.Fatalf("count school operators: %v", err)
	}
	if ops != 1 {
		t.Errorf("school %q has %d operators, want exactly 1", school, ops)
	}
}

// plantInactiveSchoolRedemption inserts a claimed-but-inactive school-package
// redemption (operator-role snapshot) on a user, mirroring the state a real
// redeem-then-pause produces (see claimPausedVoucher in auth_expiry_test.go).
// The voucher row must already exist under the given code. Returns the new
// redemption ID (the one the activate endpoint expects).
func plantInactiveSchoolRedemption(t *testing.T, pool *pgxpool.Pool, userID int, code string) int {
	t.Helper()
	ctx := context.Background()
	var vid int
	if err := pool.QueryRow(ctx, `SELECT id FROM vouchers WHERE code = $1`, code).Scan(&vid); err != nil {
		t.Fatalf("plantInactiveSchoolRedemption: find voucher %s: %v", code, err)
	}
	var rid int
	if err := pool.QueryRow(ctx, `
		INSERT INTO voucher_redemptions
			(voucher_id, user_id, remaining_seconds, is_active, package,
			 max_exams, max_pdf_size, max_concurrent_exams, max_storage_size, max_users, role)
		VALUES ($1, $2, $3, false, 'sekolah-test', 3, 52428800, 3, 524288000, 2, $4)
		RETURNING id`,
		vid, userID, 30*86400, models.SerializeRoles([]string{models.RoleOperator})).Scan(&rid); err != nil {
		t.Fatalf("plantInactiveSchoolRedemption: insert redemption for user %d: %v", userID, err)
	}
	return rid
}

// TestConcurrentRedeemDifferentCodesSerialized locks in the one-operator-per-
// school serialization on the REDEEM path for DIFFERENT codes: two gurus of
// the SAME school redeeming two different school-package codes concurrently
// serialize on the school-claim advisory lock (the voucher row lock alone
// only serializes SAME-code redeems), so exactly ONE becomes the school's
// operator and the other gets the policy 400.
func TestConcurrentRedeemDifferentCodesSerialized(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucherCode(t, pool, "IT-RACE-1")
	createSchoolVoucherCode(t, pool, "IT-RACE-2")

	const school = "SMK Race Redeem"
	gurus := make([]models.AdminUser, 2)
	for i := range gurus {
		g, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: fmt.Sprintf("guru-red-%d", i), Name: fmt.Sprintf("Guru Red %d", i),
			PasswordHash: fmt.Sprintf("pass-guru-red-%d", i), Status: models.UserStatusActive,
			Instansi: school, Role: models.SerializeRoles([]string{models.RoleGuru}),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		})
		if err != nil {
			t.Fatalf("create guru %d: %v", i, err)
		}
		gurus[i] = *g
	}

	clients := []*voucherTestClient{
		newVoucherTestClient(t, pool),
		newVoucherTestClient(t, pool),
	}
	clients[0].login(t, gurus[0].ID)
	clients[1].login(t, gurus[1].ID)

	codes := []string{"IT-RACE-1", "IT-RACE-2"}
	statuses := make([]int, len(clients))
	messages := make([]string, len(clients))
	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, resp := postForm(t, clients[i].client, clients[i].srv, "/api/vouchers/redeem",
				url.Values{"code": {codes[i]}})
			statuses[i] = status
			messages[i] = resp.Message
		}(i)
	}
	wg.Wait()

	successes, rejections, other := 0, 0, 0
	for i := range statuses {
		switch {
		case statuses[i] == http.StatusOK:
			successes++
		case statuses[i] == http.StatusBadRequest && strings.Contains(messages[i], "satu operator"):
			rejections++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent redeems: %d unexpected responses (statuses=%v messages=%v)", other, statuses, messages)
	}
	if successes != 1 {
		t.Errorf("concurrent redeems: %d succeeded, want exactly 1 — the guard raced and two operators landed", successes)
	}
	if rejections != 1 {
		t.Errorf("concurrent redeems: %d rejected by the policy, want exactly 1", rejections)
	}

	var ops int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_users
		 WHERE LOWER(instansi) = LOWER($1) AND role ILIKE '%"operator"%' AND NOT operator_created`,
		school).Scan(&ops); err != nil {
		t.Fatalf("count school operators: %v", err)
	}
	if ops != 1 {
		t.Errorf("school %q has %d operators, want exactly 1", school, ops)
	}
}
