package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// createCascadeUser seeds an active account for the cascade regression tests.
func createCascadeUser(t *testing.T, pool *pgxpool.Pool, username, instansi string, instansiID *int, expiresAt *time.Time, roles ...string) models.AdminUser {
	t.Helper()
	u, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username:     username,
		Name:         username,
		PasswordHash: "pass-" + username,
		Status:       models.UserStatusActive,
		Instansi:     instansi,
		InstansiID:   instansiID,
		ExpiresAt:    expiresAt,
		Role:         models.SerializeRoles(roles),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return *u
}

func createCascadeSuperadmin(t *testing.T, pool *pgxpool.Pool, username string) models.AdminUser {
	t.Helper()
	return createCascadeUser(t, pool, username, "owner", nil, nil, models.RoleSuperAdmin)
}

// TestDeletePersonalOperatorDoesNotCascadePersonalBucket pins the bucket guard
// in models.DeleteUser: deleting a personal-bucket operator must remove only
// that operator, never every account carrying the shared "personal" label.
func TestDeletePersonalOperatorDoesNotCascadePersonalBucket(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createCascadeUser(t, pool, "cascade-pb-op", "personal", nil, nil, models.RoleOperator)
	opID := op.ID
	ownSub, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "cascade-pb-sub", Name: "Cascade PB Sub",
		PasswordHash: "pass-cascade-pb-sub", Status: models.UserStatusActive,
		Instansi: "personal", Role: models.SerializeRoles([]string{models.RoleGuru}),
		CreatedBy: &opID, OperatorCreated: true,
	})
	if err != nil {
		t.Fatalf("create own sub: %v", err)
	}
	other := createCascadeUser(t, pool, "cascade-pb-other", "personal", nil, nil, models.RoleGuru)
	root := createCascadeSuperadmin(t, pool, "cascade-pb-root")

	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/delete", nil)
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("delete personal operator: status=%d resp=%+v", status, resp)
	}

	if _, err := models.GetUserByID(ctx, pool, op.ID); err == nil {
		t.Error("personal operator must be deleted")
	}
	for _, id := range []int{ownSub.ID, other.ID} {
		if _, err := models.GetUserByID(ctx, pool, id); err != nil {
			t.Errorf("user %d must survive deleting a personal operator: %v", id, err)
		}
	}
}

// TestSuspendPersonalOperatorDoesNotCascadePersonalBucket pins the bucket guard
// in ToggleUserStatus: suspending a personal operator must not suspend every
// unrelated account in the shared bucket.
func TestSuspendPersonalOperatorDoesNotCascadePersonalBucket(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createCascadeUser(t, pool, "cascade-pb-suspend-op", "personal", nil, nil, models.RoleOperator)
	other := createCascadeUser(t, pool, "cascade-pb-suspend-other", "personal", nil, nil, models.RoleGuru)
	root := createCascadeSuperadmin(t, pool, "cascade-pb-suspend-root")

	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/toggle-status", nil)
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("suspend personal operator: status=%d resp=%+v", status, resp)
	}

	if got, _, _ := subFlags(t, pool, op.ID); got != models.UserStatusSuspended {
		t.Errorf("personal operator status = %q, want suspended", got)
	}
	if got, _, _ := subFlags(t, pool, other.ID); got != models.UserStatusActive {
		t.Errorf("unrelated personal account status = %q, want active (no bucket cascade)", got)
	}
}

// TestMovePersonalOperatorDoesNotRelabelPersonalBucket pins the bucket guard
// in EditUser's instansi-rename cascade: moving an operator out of "personal"
// must not rewrite every unrelated personal account's label.
func TestMovePersonalOperatorDoesNotRelabelPersonalBucket(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createCascadeUser(t, pool, "cascade-pb-move-op", "personal", nil, nil, models.RoleOperator)
	createCascadeUser(t, pool, "cascade-pb-move-other", "personal", nil, nil, models.RoleGuru)
	root := createCascadeSuperadmin(t, pool, "cascade-pb-move-root")

	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/edit", map[string]interface{}{
		"instansi": "SMK Tujuan Move",
	})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("move personal operator: status=%d resp=%+v", status, resp)
	}

	if got := mustGetUser(t, pool, "cascade-pb-move-op").Instansi; got != "SMK Tujuan Move" {
		t.Errorf("operator instansi = %q, want destination", got)
	}
	if got := mustGetUser(t, pool, "cascade-pb-move-other").Instansi; got != "personal" {
		t.Errorf("unrelated personal instansi = %q, want unchanged \"personal\" (no bucket cascade)", got)
	}
}

// TestEditPersonalOperatorExpiryDoesNotCascadePersonalBucket pins the bucket
// guard in EditUser's expiry cascade: only the operator's own clock changes.
func TestEditPersonalOperatorExpiryDoesNotCascadePersonalBucket(t *testing.T) {
	pool := setupVoucherITDB(t)

	opExpiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	otherExpiry := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	op := createCascadeUser(t, pool, "cascade-pb-exp-op", "personal", nil, &opExpiry, models.RoleOperator)
	createCascadeUser(t, pool, "cascade-pb-exp-other", "personal", nil, &otherExpiry, models.RoleGuru)
	root := createCascadeSuperadmin(t, pool, "cascade-pb-exp-root")

	newExpiry := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/edit", map[string]interface{}{
		"expires_at": newExpiry.Format("2006-01-02 15:04:05"),
	})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("edit personal operator expiry: status=%d resp=%+v", status, resp)
	}

	gotOther := mustGetUser(t, pool, "cascade-pb-exp-other")
	if gotOther.ExpiresAt == nil || !gotOther.ExpiresAt.Equal(otherExpiry) {
		t.Errorf("unrelated personal expiry = %v, want unchanged %v", gotOther.ExpiresAt, otherExpiry)
	}
	gotOp := mustGetUser(t, pool, "cascade-pb-exp-op")
	if gotOp.ExpiresAt == nil || !gotOp.ExpiresAt.Equal(newExpiry) {
		t.Errorf("operator expiry = %v, want %v", gotOp.ExpiresAt, newExpiry)
	}
}

// TestSaveQuestionsPengawasRejectsTwinSchool pins the canonical roster check:
// two schools share a name but have different instansi_ids, so the picker's
// same-named twin must not be assignable through a hand-crafted request.
func TestSaveQuestionsPengawasRejectsTwinSchool(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	var instA, instB int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Pengawas', 'PENGAWASA') RETURNING id`).Scan(&instA); err != nil {
		t.Fatalf("seed instansi A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Pengawas', 'PENGAWASB') RETURNING id`).Scan(&instB); err != nil {
		t.Fatalf("seed instansi B: %v", err)
	}

	opA := createCascadeUser(t, pool, "cascade-pengawas-op-a", "SMA Kembar Pengawas", &instA, nil, models.RoleOperator)
	pengawasB := createCascadeUser(t, pool, "cascade-pengawas-b", "SMA Kembar Pengawas", &instB, nil, models.RolePengawas)

	tc, _ := newQuotaTestClient(t, pool)
	tc.login(t, opA.ID)
	if status, body := tc.upload(t, "twin-pengawas-exam", schoolTestPDF(64)); status != http.StatusOK {
		t.Fatalf("operator upload: status=%d body=%s", status, body)
	}
	var examID int
	if err := pool.QueryRow(ctx, `SELECT id FROM exams WHERE name = 'twin-pengawas-exam'`).Scan(&examID); err != nil {
		t.Fatalf("find exam: %v", err)
	}

	status, resp := postJSON(t, tc.client, tc.srv, "/admin/api/exams/"+strconv.Itoa(examID)+"/questions",
		map[string]interface{}{
			"pengawas_ids": []int{pengawasB.ID},
			"start_time":   "2030-01-01 09:00",
			"end_time":     "2030-01-02 15:04",
		})
	if status != http.StatusBadRequest || !strings.Contains(resp.Message, "tidak berada dalam instansi yang sama") {
		t.Fatalf("twin-school pengawas: status=%d resp=%+v, want cross-instansi 400", status, resp)
	}
}

// TestSuperAdminCreateRejectsReservedInstansi pins the reserved-bucket guard
// on the SuperAdmin create path: "owner" is the bootstrap superadmin's home,
// never a valid school for a newly created account.
func TestSuperAdminCreateRejectsReservedInstansi(t *testing.T) {
	pool := setupVoucherITDB(t)
	root := createCascadeSuperadmin(t, pool, "cascade-reserved-root")

	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	status, resp := postJSON(t, rt.client, rt.srv, "/api/users", map[string]interface{}{
		"username": "cascade-reserved-owner",
		"password": "pass-cascade-reserved-owner",
		"name":     "Cascade Reserved Owner",
		"instansi": "owner",
		"roles":    []string{models.RoleGuru},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("create account in owner bucket: status=%d resp=%+v, want 400", status, resp)
	}
	if _, err := models.GetUserByUsername(context.Background(), pool, "cascade-reserved-owner"); err == nil {
		t.Error("reserved-instansi account must not be created")
	}
}

// TestOperatorCannotForcePendingOTP pins the EditUser status whitelist:
// pending_otp is an internal verification state, not an operator-settable
// status that would lock a sub-account out waiting for an unsent OTP.
func TestOperatorCannotForcePendingOTP(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createCascadeUser(t, pool, "cascade-otp-op", "SMK OTP", nil, nil, models.RoleOperator)
	sub := createCascadeUser(t, pool, "cascade-otp-sub", "SMK OTP", nil, nil, models.RoleGuru)

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub.ID)+"/edit", map[string]interface{}{
		"status": models.UserStatusPendingOTP,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("operator set pending_otp: status=%d resp=%+v, want 400", status, resp)
	}
	if got := mustGetUser(t, pool, "cascade-otp-sub").Status; got != models.UserStatusActive {
		t.Errorf("sub-account status = %q, want active", got)
	}
}

// TestSchoolPackageLabelIsTenantScoped pins the display label lookup to the
// canonical instansi_id: a twin school with no package must not inherit the
// label of the other school that happens to share its name.
func TestSchoolPackageLabelIsTenantScoped(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	var instA, instB int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Paket', 'PAKETA') RETURNING id`).Scan(&instA); err != nil {
		t.Fatalf("seed instansi A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Paket', 'PAKETB') RETURNING id`).Scan(&instB); err != nil {
		t.Fatalf("seed instansi B: %v", err)
	}

	opA := createCascadeUser(t, pool, "cascade-paket-op-a", "SMA Kembar Paket", &instA, nil, models.RoleOperator)
	plantSchoolRedemption(t, pool, opA.ID, 3, 50*1024*1024, 3, 500*1024*1024)
	if _, err := pool.Exec(ctx, `UPDATE voucher_redemptions SET package = 'sekolah_kecil' WHERE user_id = $1 AND is_active`, opA.ID); err != nil {
		t.Fatalf("label redemption package: %v", err)
	}

	scopeA := models.InstansiScope{ID: &instA, Name: "SMA Kembar Paket"}
	scopeB := models.InstansiScope{ID: &instB, Name: "SMA Kembar Paket"}
	if got := schoolPackageLabel(ctx, pool, scopeA); got != "Paket Sekolah Kecil" {
		t.Errorf("label for instansi A = %q, want Paket Sekolah Kecil", got)
	}
	if got := schoolPackageLabel(ctx, pool, scopeB); got != "Paket Sekolah" {
		t.Errorf("label for twin instansi B = %q, want Paket Sekolah", got)
	}
}

// TestSuperAdminMoveOperatorMovesCanonicalInstansi pins the true-move
// semantics: changing an operator's instansi in EditUser must migrate the
// canonical instansi_id/code for the operator AND every member of its source
// school, not merely relabel them.
func TestSuperAdminMoveOperatorMovesCanonicalInstansi(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	var instA, instB int
	var codeB string
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Asal Move', 'MOVEASAL') RETURNING id`).Scan(&instA); err != nil {
		t.Fatalf("seed source instansi: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Tujuan Move', 'MOVETUJUAN') RETURNING id, code`).Scan(&instB, &codeB); err != nil {
		t.Fatalf("seed destination instansi: %v", err)
	}

	op := createCascadeUser(t, pool, "cascade-move-op", "SMA Asal Move", &instA, nil, models.RoleOperator)
	createCascadeUser(t, pool, "cascade-move-sub", "SMA Asal Move", &instA, nil, models.RoleGuru)
	root := createCascadeSuperadmin(t, pool, "cascade-move-root")
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/edit",
		map[string]interface{}{"instansi": "SMA Tujuan Move"})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("move operator to existing school: status=%d resp=%+v", status, resp)
	}

	for _, username := range []string{"cascade-move-op", "cascade-move-sub"} {
		got := mustGetUser(t, pool, username)
		if got.Instansi != "SMA Tujuan Move" {
			t.Errorf("%s instansi = %q, want SMA Tujuan Move", username, got.Instansi)
		}
		if got.InstansiID == nil || *got.InstansiID != instB {
			t.Errorf("%s instansi_id = %v, want %d", username, got.InstansiID, instB)
		}
		if got.InstansiCode == nil || *got.InstansiCode != codeB {
			t.Errorf("%s instansi_code = %v, want %q", username, got.InstansiCode, codeB)
		}
	}
}

// TestSuperAdminMoveSubAccountUsesCanonicalDestination pins the non-operator
// move path: a guru moved to an existing school adopts that school's
// instansi_id/code instead of keeping the source identity under a new label.
func TestSuperAdminMoveSubAccountUsesCanonicalDestination(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	var instA, instB int
	var codeB string
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Asal Guru', 'GURUASAL') RETURNING id`).Scan(&instA); err != nil {
		t.Fatalf("seed source instansi: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Tujuan Guru', 'GURUTUJUAN') RETURNING id, code`).Scan(&instB, &codeB); err != nil {
		t.Fatalf("seed destination instansi: %v", err)
	}

	guru := createCascadeUser(t, pool, "cascade-move-guru", "SMA Asal Guru", &instA, nil, models.RoleGuru)
	root := createCascadeSuperadmin(t, pool, "cascade-move-guru-root")
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(guru.ID)+"/edit",
		map[string]interface{}{"instansi": "SMA Tujuan Guru"})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("move guru to existing school: status=%d resp=%+v", status, resp)
	}
	got := mustGetUser(t, pool, "cascade-move-guru")
	if got.Instansi != "SMA Tujuan Guru" {
		t.Errorf("guru instansi = %q, want SMA Tujuan Guru", got.Instansi)
	}
	if got.InstansiID == nil || *got.InstansiID != instB {
		t.Errorf("guru instansi_id = %v, want %d", got.InstansiID, instB)
	}
	if got.InstansiCode == nil || *got.InstansiCode != codeB {
		t.Errorf("guru instansi_code = %v, want %q", got.InstansiCode, codeB)
	}
}

// TestSuperAdminMoveOperatorCreatesDestinationSchool pins the other half of
// true-move semantics: a destination name that does not exist yet gets a new
// canonical instansi row, and the moved operator adopts its id/code.
func TestSuperAdminMoveOperatorCreatesDestinationSchool(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	var instA int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Asal Baru', 'BARUASAL') RETURNING id`).Scan(&instA); err != nil {
		t.Fatalf("seed source instansi: %v", err)
	}
	op := createCascadeUser(t, pool, "cascade-move-new-op", "SMA Asal Baru", &instA, nil, models.RoleOperator)
	root := createCascadeSuperadmin(t, pool, "cascade-move-new-root")
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/edit",
		map[string]interface{}{"instansi": "SMA Tujuan Baru"})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("move operator to new school: status=%d resp=%+v", status, resp)
	}

	got := mustGetUser(t, pool, "cascade-move-new-op")
	if got.InstansiID == nil || *got.InstansiID <= 0 {
		t.Fatalf("moved operator instansi_id = %v, want a new positive id", got.InstansiID)
	}
	if got.InstansiCode == nil || strings.TrimSpace(*got.InstansiCode) == "" {
		t.Fatalf("moved operator instansi_code = %v, want a generated code", got.InstansiCode)
	}
	var storedName, storedCode string
	if err := pool.QueryRow(ctx, `SELECT name, code FROM instansi WHERE id = $1`, *got.InstansiID).Scan(&storedName, &storedCode); err != nil {
		t.Fatalf("lookup new destination school: %v", err)
	}
	if storedName != "SMA Tujuan Baru" || storedCode != *got.InstansiCode {
		t.Errorf("new school row = (%q,%q), want (SMA Tujuan Baru,%q)", storedName, storedCode, *got.InstansiCode)
	}
}

// TestSuperAdminMoveOperatorRejectsAmbiguousDestination pins the duplicate-name
// guard: when two schools share the destination name, the request has no id to
// choose one, so the move is rejected instead of silently joining the wrong
// school.
func TestSuperAdminMoveOperatorRejectsAmbiguousDestination(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	var instA, instX, instY int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Asal Ambigu', 'AMBIGASAL') RETURNING id`).Scan(&instA); err != nil {
		t.Fatalf("seed source instansi: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Ambigu', 'AMBIGX') RETURNING id`).Scan(&instX); err != nil {
		t.Fatalf("seed twin X: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Ambigu', 'AMBIGY') RETURNING id`).Scan(&instY); err != nil {
		t.Fatalf("seed twin Y: %v", err)
	}
	_ = instX
	_ = instY

	op := createCascadeUser(t, pool, "cascade-move-ambig-op", "SMA Asal Ambigu", &instA, nil, models.RoleOperator)
	root := createCascadeSuperadmin(t, pool, "cascade-move-ambig-root")
	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)

	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(op.ID)+"/edit",
		map[string]interface{}{"instansi": "SMA Kembar Ambigu"})
	if status != http.StatusBadRequest || !strings.Contains(resp.Message, "ambigu") {
		t.Fatalf("ambiguous move: status=%d resp=%+v, want 400 ambiguity", status, resp)
	}
	got := mustGetUser(t, pool, "cascade-move-ambig-op")
	if got.Instansi != "SMA Asal Ambigu" || got.InstansiID == nil || *got.InstansiID != instA {
		t.Errorf("operator must stay in source school, got instansi=%q id=%v", got.Instansi, got.InstansiID)
	}
}

// TestSchoolAlreadyHasOperatorIsTenantScoped pins the one-operator guard to
// the canonical instansi_id: a twin school sharing the name must not be seen
// as already occupied by the other school's operator.
func TestSchoolAlreadyHasOperatorIsTenantScoped(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	var instA, instB int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Guard', 'GUARDA') RETURNING id`).Scan(&instA); err != nil {
		t.Fatalf("seed instansi A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Guard', 'GUARDB') RETURNING id`).Scan(&instB); err != nil {
		t.Fatalf("seed instansi B: %v", err)
	}

	createCascadeUser(t, pool, "cascade-guard-op-a", "SMA Kembar Guard", &instA, nil, models.RoleOperator)

	scopeA := models.InstansiScope{ID: &instA, Name: "SMA Kembar Guard"}
	scopeB := models.InstansiScope{ID: &instB, Name: "SMA Kembar Guard"}
	if has, err := schoolAlreadyHasOperator(ctx, pool, scopeA, 0); err != nil || !has {
		t.Fatalf("school A has operator = %v, err=%v, want true", has, err)
	}
	if has, err := schoolAlreadyHasOperator(ctx, pool, scopeB, 0); err != nil || has {
		t.Fatalf("twin school B has operator = %v, err=%v, want false", has, err)
	}
}

// TestOperatorAccountQuotaIsTenantScoped pins the id-first quota lookup: two
// schools share a name but have different instansi_ids, so the package cap and
// used-account count must come only from the acting operator's school.
func TestOperatorAccountQuotaIsTenantScoped(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	var instA, instB int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Kuota', 'QUOTAA') RETURNING id`).Scan(&instA); err != nil {
		t.Fatalf("seed instansi A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Kuota', 'QUOTAB') RETURNING id`).Scan(&instB); err != nil {
		t.Fatalf("seed instansi B: %v", err)
	}

	opA := createCascadeUser(t, pool, "cascade-quota-op-a", "SMA Kembar Kuota", &instA, nil, models.RoleOperator)
	opB := createCascadeUser(t, pool, "cascade-quota-op-b", "SMA Kembar Kuota", &instB, nil, models.RoleOperator)
	createCascadeUser(t, pool, "cascade-quota-guru-a", "SMA Kembar Kuota", &instA, nil, models.RoleGuru)
	createCascadeUser(t, pool, "cascade-quota-guru-b1", "SMA Kembar Kuota", &instB, nil, models.RoleGuru)
	createCascadeUser(t, pool, "cascade-quota-guru-b2", "SMA Kembar Kuota", &instB, nil, models.RoleGuru)

	var voucherID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO vouchers (code, package, duration_type, max_usage, is_active)
		VALUES ('CASCADE-QUOTA', 'sekolah-test', 'bulanan', 2, true)
		RETURNING id`).Scan(&voucherID); err != nil {
		t.Fatalf("seed voucher: %v", err)
	}
	for _, pair := range []struct {
		userID   int
		maxUsers int
	}{{opA.ID, 5}, {opB.ID, 9}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO voucher_redemptions
				(voucher_id, user_id, is_active, package, max_users, role, remaining_seconds, activated_at)
			VALUES ($1, $2, true, 'sekolah-test', $3, '["operator"]', 3600, now())`,
			voucherID, pair.userID, pair.maxUsers); err != nil {
			t.Fatalf("seed redemption for user %d: %v", pair.userID, err)
		}
	}

	scopeA := models.InstansiScope{ID: &instA, Name: "SMA Kembar Kuota"}
	maxUsers, used, err := loadOperatorAccountQuota(ctx, pool, opA.ID, true, scopeA)
	if err != nil {
		t.Fatalf("load operator account quota: %v", err)
	}
	if maxUsers != 5 {
		t.Errorf("max_users = %d, want 5 from operator A's own redemption (twin B has 9)", maxUsers)
	}
	if used != 1 {
		t.Errorf("used = %d, want 1 (only school A's guru; twin B's two gurus must not count)", used)
	}
}

// TestDeleteTwinSchoolOperatorIsTenantScoped pins the id-first cascade in
// models.DeleteUser: two schools share a name but have different instansi_ids,
// so deleting the operator of one must leave the other school's accounts.
func TestDeleteTwinSchoolOperatorIsTenantScoped(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	var instA, instB int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Cascade', 'CASCADEA') RETURNING id`).Scan(&instA); err != nil {
		t.Fatalf("seed instansi A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar Cascade', 'CASCADEB') RETURNING id`).Scan(&instB); err != nil {
		t.Fatalf("seed instansi B: %v", err)
	}

	opA := createCascadeUser(t, pool, "cascade-tw-op-a", "SMA Kembar Cascade", &instA, nil, models.RoleOperator)
	subA := createCascadeUser(t, pool, "cascade-tw-sub-a", "SMA Kembar Cascade", &instA, nil, models.RoleGuru)
	guruB := createCascadeUser(t, pool, "cascade-tw-guru-b", "SMA Kembar Cascade", &instB, nil, models.RoleGuru)
	root := createCascadeSuperadmin(t, pool, "cascade-tw-root")

	rt := newVoucherTestClient(t, pool)
	rt.login(t, root.ID)
	status, resp := postJSON(t, rt.client, rt.srv, "/api/users/"+strconv.Itoa(opA.ID)+"/delete", nil)
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("delete twin operator: status=%d resp=%+v", status, resp)
	}

	if _, err := models.GetUserByID(ctx, pool, opA.ID); err == nil {
		t.Error("operator A must be deleted")
	}
	if _, err := models.GetUserByID(ctx, pool, subA.ID); err == nil {
		t.Error("sub-account A must be deleted with its operator")
	}
	if _, err := models.GetUserByID(ctx, pool, guruB.ID); err != nil {
		t.Errorf("twin school B account must survive deleting operator A: %v", err)
	}
}
