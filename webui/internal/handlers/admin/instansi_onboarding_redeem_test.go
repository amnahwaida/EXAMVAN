package admin

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestRedeemSchoolVoucherAnnouncesInstansiOnboarding guards the immediate UX:
// the success toast itself must tell a freshly-minted school operator to set
// the school name, not rely only on the modal after the reload. The JSON flag
// lets the client keep the toast on screen long enough to read.
func TestRedeemSchoolVoucherAnnouncesInstansiOnboarding(t *testing.T) {
	pool := setupVoucherITDB(t)
	createSchoolVoucherCode(t, pool, "IT-ONBOARD")
	user := createOperatorUser(t, pool, "onboard-redeem", "personal", "pass-onboard-redeem")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)

	status, resp := postForm(t, tc.client, tc.srv, "/api/vouchers/redeem",
		url.Values{"code": {"IT-ONBOARD"}})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("redeem: status=%d resp=%+v", status, resp)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "atur nama instansi") {
		t.Errorf("redeem message must announce the instansi step, got %q", resp.Message)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(resp.Body), &payload); err != nil {
		t.Fatalf("decode redeem response: %v", err)
	}
	if got, _ := payload["needs_instansi"].(bool); !got {
		t.Errorf("needs_instansi = %v, want true", payload["needs_instansi"])
	}
}
