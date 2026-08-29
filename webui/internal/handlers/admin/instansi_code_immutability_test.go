package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/examvan/webui/internal/models"
)

func TestInstansiCodeImmutableOnRename(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)

	op, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "op-code-immutable", Name: "Op Code Immutable",
		PasswordHash: "pass-op-code-immutable", Status: models.UserStatusActive,
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
	opAfter := mustGetUser(t, pool, "op-code-immutable")
	if !opAfter.IsOperator() {
		t.Fatalf("op must be operator after redeem")
	}

	updateInstansi := func(name string) (code string) {
		t.Helper()
		status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update", map[string]interface{}{"instansi": name})
		if status != http.StatusOK || !resp.Success {
			t.Fatalf("update instansi %q: status=%d resp=%+v", name, status, resp)
		}
		var out struct {
			Success      bool   `json:"success"`
			InstansiCode string `json:"instansi_code"`
		}
		if err := json.Unmarshal([]byte(resp.Body), &out); err != nil {
			body, _ := io.ReadAll(strings.NewReader(resp.Body))
			t.Fatalf("unmarshal instansi_code: %v body=%s", err, string(body))
		}
		if out.InstansiCode == "" {
			t.Fatalf("instansi_code empty after setting %q", name)
		}
		return out.InstansiCode
	}

	code1 := updateInstansi("SMA Negeri 1 Jakarta")
	opAfter = mustGetUser(t, pool, "op-code-immutable")
	if opAfter.Instansi != "SMA Negeri 1 Jakarta" {
		t.Fatalf("instansi after first set = %q, want SMA Negeri 1 Jakarta", opAfter.Instansi)
	}
	id1, dbCode1 := instansiLink(t, pool, opAfter.ID)
	if dbCode1 != code1 {
		t.Fatalf("db code %q != response code %q after first set", dbCode1, code1)
	}
	var tableCode string
	if err := pool.QueryRow(ctx, `SELECT code FROM instansi WHERE id=$1`, id1).Scan(&tableCode); err != nil {
		t.Fatalf("load instansi table code: %v", err)
	}
	if tableCode != code1 {
		t.Fatalf("instansi table code %q != %q", tableCode, code1)
	}

	tc.createUser(t, "sub-code-a")
	subA := mustGetUser(t, pool, "sub-code-a")
	_, subCodeA := instansiLink(t, pool, subA.ID)
	if subCodeA != code1 {
		t.Fatalf("sub code %q != operator code %q", subCodeA, code1)
	}

	code2 := updateInstansi("SMA Negeri 1 Bandung")
	if code2 != code1 {
		t.Fatalf("code after rename = %q, want immutable %q", code2, code1)
	}
	opAfter = mustGetUser(t, pool, "op-code-immutable")
	if opAfter.Instansi != "SMA Negeri 1 Bandung" {
		t.Fatalf("instansi after rename = %q, want SMA Negeri 1 Bandung", opAfter.Instansi)
	}
	_, dbCode2 := instansiLink(t, pool, opAfter.ID)
	if dbCode2 != code1 {
		t.Fatalf("db code after rename = %q, want %q", dbCode2, code1)
	}
	if err := pool.QueryRow(ctx, `SELECT code FROM instansi WHERE id=$1`, id1).Scan(&tableCode); err != nil {
		t.Fatalf("load table code after rename: %v", err)
	}
	if tableCode != code1 {
		t.Fatalf("table code after rename = %q, want %q", tableCode, code1)
	}
	subA = mustGetUser(t, pool, "sub-code-a")
	_, subCodeA = instansiLink(t, pool, subA.ID)
	if subCodeA != code1 {
		t.Fatalf("sub code after rename = %q, want %q", subCodeA, code1)
	}
	if subA.Instansi != "SMA Negeri 1 Bandung" {
		t.Fatalf("sub instansi after rename = %q, want SMA Negeri 1 Bandung", subA.Instansi)
	}

	code3 := updateInstansi("SMK Baru Harapan")
	if code3 != code1 {
		t.Fatalf("code after second rename = %q, want immutable %q", code3, code1)
	}
	opAfter = mustGetUser(t, pool, "op-code-immutable")
	_, dbCode3 := instansiLink(t, pool, opAfter.ID)
	if dbCode3 != code1 {
		t.Fatalf("db code after second rename = %q, want %q", dbCode3, code1)
	}
}

func TestInstansiCodeImmutableOnRepeatedRenameWithoutSubAccounts(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()
	createSchoolVoucher(t, pool)

	op, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "op-code-repeat", Name: "Op Code Repeat",
		PasswordHash: "pass-op-code-repeat", Status: models.UserStatusActive,
		Instansi:     "SMA Awal",
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create operator: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")

	var firstCode string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(instansi_code,'') FROM admin_users WHERE id=$1`, op.ID).Scan(&firstCode); err != nil {
		t.Fatalf("load first code: %v", err)
	}
	if firstCode == "" {
		status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update", map[string]interface{}{"instansi": "SMA Awal"})
		if status != http.StatusOK || !resp.Success {
			t.Fatalf("initial claim: %d %+v", status, resp)
		}
		var out struct{ InstansiCode string `json:"instansi_code"` }
		_ = json.Unmarshal([]byte(resp.Body), &out)
		firstCode = out.InstansiCode
	}

	renames := []string{"SMA Negeri 2 Jakarta", "SMA Negeri 3 Surabaya", "SMA Negeri 4 Bandung"}
	for _, name := range renames {
		status, resp := postJSON(t, tc.client, tc.srv, "/api/instansi/update", map[string]interface{}{"instansi": name})
		if status != http.StatusOK || !resp.Success {
			t.Fatalf("rename to %q: %d %+v", name, status, resp)
		}
		var out struct{ InstansiCode string `json:"instansi_code"` }
		if err := json.Unmarshal([]byte(resp.Body), &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if out.InstansiCode != firstCode {
			t.Fatalf("rename to %q: code %q != first %q", name, out.InstansiCode, firstCode)
		}
		var dbCode string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(instansi_code,'') FROM admin_users WHERE id=$1`, op.ID).Scan(&dbCode); err != nil {
			t.Fatalf("load db code: %v", err)
		}
		if dbCode != firstCode {
			t.Fatalf("db code after %q = %q, want %q", name, dbCode, firstCode)
		}
	}
}
