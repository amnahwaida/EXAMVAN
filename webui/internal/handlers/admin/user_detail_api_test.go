package admin

// ---------------------------------------------------------------------------
// Tests for GET /admin/api/users/:user_id — the single-account detail
// endpoint backing the Atur User modal (which replaced the "fetch the whole
// list with per_page=1000 to find one id" pattern in the Kelola User tab).
//
//  1. A superadmin can read any account's detail.
//  2. An operator can read only accounts in its own instansi.
//  3. An operator cannot read a peer operator account, a superadmin account,
//     or an account of another school (mirrors EditUser's scope rules).
//  4. Unknown ids answer 404, malformed ids 400.
//
// The endpoint must mirror the ListUsers row shape (username, instansi, ...)
// so the modal renders identical data in both contexts.
// ---------------------------------------------------------------------------

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/examvan/webui/internal/models"
)

func TestGetUserDetailSuperAdminReadsAnyAccount(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_detail_su", Name: "IT Detail SU",
		PasswordHash: "pass-it-detail-su", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	sub, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_detail_guru", Name: "IT Detail Guru",
		PasswordHash: "pass-it-detail-guru", Status: models.UserStatusActive,
		Instansi:           "SMK Detail",
		Role:               models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:           3, MaxPDFSize: 1048576,
		MaxConcurrentExams: 2, MaxStorageSize: 50 * 1024 * 1024,
		Package: "free",
	})
	if err != nil {
		t.Fatalf("create sub guru: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, su.ID)

	status, resp := getAPIJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(sub.ID))
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("superadmin detail: status=%d resp=%+v", status, resp)
	}
	var body struct {
		User map[string]interface{} `json:"user"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("decode detail: %v (body=%s)", err, resp.Body)
	}
	if body.User == nil {
		t.Fatalf("detail body missing user object: %s", resp.Body)
	}
	if body.User["username"] != "it_detail_guru" {
		t.Errorf("detail username = %v, want it_detail_guru", body.User["username"])
	}
	if body.User["instansi"] != "SMK Detail" {
		t.Errorf("detail instansi = %v, want SMK Detail", body.User["instansi"])
	}
	if body.User["status"] != "active" {
		t.Errorf("detail status = %v, want active", body.User["status"])
	}
}

func TestGetUserDetailOperatorScope(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createOperatorUser(t, pool, "it_detail_op", "SMK Alpha", "pass-it-detail-op")
	// Grant the operator role directly (like a redeemed school voucher would).
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET role = '["guru","operator"]' WHERE id = $1`, op.ID); err != nil {
		t.Fatalf("grant operator role: %v", err)
	}
	ownSub, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_detail_sub", Name: "IT Detail Sub",
		PasswordHash: "pass-it-detail-sub", Status: models.UserStatusActive,
		Instansi:           "SMK Alpha",
		Role:               models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:           3, MaxPDFSize: 1048576,
		MaxConcurrentExams: 2, MaxStorageSize: 50 * 1024 * 1024,
		Package: "free",
	})
	if err != nil {
		t.Fatalf("create own sub: %v", err)
	}
	otherSub, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_detail_other", Name: "IT Detail Other",
		PasswordHash: "pass-it-detail-other", Status: models.UserStatusActive,
		Instansi:           "SMK Beta",
		Role:               models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:           3, MaxPDFSize: 1048576,
		MaxConcurrentExams: 2, MaxStorageSize: 50 * 1024 * 1024,
		Package: "free",
	})
	if err != nil {
		t.Fatalf("create other school sub: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)

	// 1) Own instansi account: 200 with the sub-account's data.
	status, resp := getAPIJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(ownSub.ID))
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("operator own-instansi detail: status=%d resp=%+v", status, resp)
	}
	var ownBody struct {
		User map[string]interface{} `json:"user"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &ownBody); err != nil {
		t.Fatalf("decode own detail: %v (body=%s)", err, resp.Body)
	}
	if ownBody.User["username"] != "it_detail_sub" {
		t.Errorf("detail username = %v, want it_detail_sub", ownBody.User["username"])
	}

	// 2) Another school's account: rejected.
	status, resp = getAPIJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(otherSub.ID))
	if status != http.StatusBadRequest {
		t.Fatalf("operator cross-school detail: status=%d, want 400 (resp=%+v)", status, resp)
	}

	// 3) A peer operator account (same school): rejected.
	peer := createOperatorUser(t, pool, "it_detail_peer", "SMK Alpha", "pass-it-detail-peer")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET role = '["guru","operator"]' WHERE id = $1`, peer.ID); err != nil {
		t.Fatalf("grant peer operator role: %v", err)
	}
	status, resp = getAPIJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(peer.ID))
	if status != http.StatusBadRequest {
		t.Fatalf("operator peer-operator detail: status=%d, want 400 (resp=%+v)", status, resp)
	}

	// 4) The superadmin account: rejected for operators.
	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_detail_su2", Name: "IT Detail SU2",
		PasswordHash: "pass-it-detail-su2", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	status, resp = getAPIJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(su.ID))
	if status != http.StatusBadRequest {
		t.Fatalf("operator superadmin detail: status=%d, want 400 (resp=%+v)", status, resp)
	}
}

func TestGetUserDetailNotFound(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()
	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_detail_su3", Name: "IT Detail SU3",
		PasswordHash: "pass-it-detail-su3", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, su.ID)

	status, resp := getAPIJSON(t, tc.client, tc.srv, "/api/users/999999")
	if status != http.StatusNotFound {
		t.Fatalf("unknown id detail: status=%d, want 404 (resp=%+v)", status, resp)
	}

	status, resp = getAPIJSON(t, tc.client, tc.srv, "/api/users/abc")
	if status != http.StatusBadRequest {
		t.Fatalf("invalid id detail: status=%d, want 400 (resp=%+v)", status, resp)
	}
}