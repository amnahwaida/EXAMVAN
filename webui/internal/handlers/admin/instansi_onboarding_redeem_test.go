package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

func createCustomOperatorVoucherCode(t *testing.T, pool *pgxpool.Pool, code, pkg string) {
	t.Helper()
	if _, err := models.CreateVoucher(context.Background(), pool, &models.Voucher{
		Code: code, Package: pkg, DurationType: "bulanan",
		MaxUsage: 1, IsActive: true,
		IsCustom: true, CustomLabel: pkg,
		CustomMaxExams: 3, CustomMaxPDFSize: 50 * 1024 * 1024,
		CustomMaxConcurrentExams: 3, CustomMaxStorageSize: 500 * 1024 * 1024,
		CustomMaxUsers: 2, CustomRole: models.SerializeRoles([]string{models.RoleOperator}),
	}); err != nil {
		t.Fatalf("create custom operator voucher %s: %v", code, err)
	}
}

func requireNeedsInstansi(t *testing.T, resp apiResp, want bool) {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(resp.Body), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := payload["needs_instansi"].(bool); got != want {
		t.Errorf("needs_instansi = %v, want %v", payload["needs_instansi"], want)
	}
}

// TestRedeemOperatorVoucherAnnouncesInstansiOnboarding guards the immediate UX
// for both school and custom operator vouchers: the success toast itself must
// tell a freshly-minted operator to set the instansi, not rely only on the
// modal after the reload. The JSON flag lets the client keep the toast on
// screen long enough to read.
func TestRedeemOperatorVoucherAnnouncesInstansiOnboarding(t *testing.T) {
	pool := setupVoucherITDB(t)
	cases := []struct {
		name string
		code string
		pkg  string
	}{
		{"school package", "IT-ONBOARD-SCHOOL", "sekolah-test"},
		{"custom operator package", "IT-ONBOARD-CUSTOM", "custom-operator"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pkg == "sekolah-test" {
				createSchoolVoucherCode(t, pool, tc.code)
			} else {
				createCustomOperatorVoucherCode(t, pool, tc.code, tc.pkg)
			}
			user := createOperatorUser(t, pool,
				fmt.Sprintf("onboard-redeem-%d", i), "personal", fmt.Sprintf("pass-onboard-redeem-%d", i))
			client := newVoucherTestClient(t, pool)
			client.login(t, user.ID)

			status, resp := postForm(t, client.client, client.srv, "/api/vouchers/redeem",
				url.Values{"code": {tc.code}})
			if status != http.StatusOK || !resp.Success {
				t.Fatalf("redeem: status=%d resp=%+v", status, resp)
			}
			if !strings.Contains(strings.ToLower(resp.Message), "atur nama instansi") {
				t.Errorf("redeem message must announce the instansi step, got %q", resp.Message)
			}
			requireNeedsInstansi(t, resp, true)
		})
	}
}

// TestActivateCustomOperatorVoucherAnnouncesInstansiOnboarding covers the
// activate response path too: switching back to a paused custom operator
// package must repeat the instansi requirement in the toast.
func TestActivateCustomOperatorVoucherAnnouncesInstansiOnboarding(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()
	const pkgA = "custom-operator-a"
	const pkgB = "custom-operator-b"
	createCustomOperatorVoucherCode(t, pool, "IT-ONBOARD-ACT-A", pkgA)
	createCustomOperatorVoucherCode(t, pool, "IT-ONBOARD-ACT-B", pkgB)

	user := createOperatorUser(t, pool, "onboard-activate", "personal", "pass-onboard-activate")
	client := newVoucherTestClient(t, pool)
	client.login(t, user.ID)
	client.redeem(t, "IT-ONBOARD-ACT-A")

	var redemptionID int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM voucher_redemptions WHERE user_id = $1 AND package = $2`,
		user.ID, pkgA).Scan(&redemptionID); err != nil {
		t.Fatalf("find redemption A: %v", err)
	}

	client.redeem(t, "IT-ONBOARD-ACT-B")

	status, resp := postForm(t, client.client, client.srv, "/api/vouchers/activate",
		url.Values{"redemption_id": {fmt.Sprintf("%d", redemptionID)}})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("activate: status=%d resp=%+v", status, resp)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "atur nama instansi") {
		t.Errorf("activate message must announce the instansi step, got %q", resp.Message)
	}
	requireNeedsInstansi(t, resp, true)
}
