package admin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Per-request expiry enforcement in middleware.AuthRequired (closes the
// 1-day-session gap: an account whose active period expired while logged in
// loses access immediately, not at the next login). Requires PostgreSQL
// (TEST_DATABASE_URL), same infra as the voucher lifecycle tests.
// ---------------------------------------------------------------------------

// claimPausedVoucher inserts a claimed-but-NOT-active voucher redemption with
// the given remaining lifetime — the exact row shape the middleware (and
// AuthenticateUser) counts as a usable voucher.
func claimPausedVoucher(t *testing.T, pool *pgxpool.Pool, userID int, remainingSeconds int64) {
	t.Helper()
	ctx := context.Background()
	v, err := models.CreateVoucher(ctx, pool, &models.Voucher{
		Code: fmt.Sprintf("AUTHEXP-%d", userID), Package: "guru",
		DurationType: "bulanan", MaxUsage: 1, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create voucher: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO voucher_redemptions
			(voucher_id, user_id, is_active, remaining_seconds, package,
			 max_exams, max_pdf_size, max_concurrent_exams, max_storage_size, role)
		VALUES ($1, $2, false, $3, 'guru', 1, 1048576, 1, 52428800, '')`,
		v.ID, userID, remainingSeconds); err != nil {
		t.Fatalf("insert paused redemption: %v", err)
	}
}

// getAuthPing hits the protected /api/auth-ping probe with an Accept:
// application/json header (so AuthRequired answers with JSON instead of an
// HTML redirect, exactly like the real admin frontend's AJAX calls) and
// returns the response status.
func getAuthPing(t *testing.T, tc *voucherTestClient) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, tc.srv.URL+"/api/auth-ping", nil)
	if err != nil {
		t.Fatalf("build auth-ping request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := tc.client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/auth-ping: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestAuthRequiredExpiredSessionKicksUserOut locks in the core of the
// feature: a logged-in account whose expiry passes (no usable voucher on
// hand) is rejected on the very next request — previously the session kept
// working until its 1-day cookie expired. The session is dropped, so a
// follow-up request is unauthenticated.
func TestAuthRequiredExpiredSessionKicksUserOut(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	user := createOperatorUser(t, pool, "expired-session", "personal", "pass-expired-session")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)

	// Healthy account: the protected route answers.
	if status := getAuthPing(t, tc); status != http.StatusOK {
		t.Fatalf("auth-ping before expiry: status=%d, want 200", status)
	}

	// Expire the account (no voucher on hand).
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	// The very next request is rejected (403 for API calls).
	if status := getAuthPing(t, tc); status != http.StatusForbidden {
		t.Errorf("auth-ping after expiry: status=%d, want 403", status)
	}
	// The session was dropped: a follow-up request is unauthenticated (401).
	if status := getAuthPing(t, tc); status != http.StatusUnauthorized {
		t.Errorf("auth-ping after session drop: status=%d, want 401", status)
	}
}

// TestAuthRequiredExpiredWithUsableVoucherKeepsAccess locks in the exception,
// mirroring AuthenticateUser's login gate: an expired account still holding a
// claimed-but-unactivated voucher with remaining lifetime keeps its access
// (it may reach the billing page to activate the package).
func TestAuthRequiredExpiredWithUsableVoucherKeepsAccess(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	user := createOperatorUser(t, pool, "expired-vc-session", "personal", "pass-expired-vc-session")
	claimPausedVoucher(t, pool, user.ID, 30*86400) // ~30 days left

	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)
	if status := getAuthPing(t, tc); status != http.StatusOK {
		t.Errorf("auth-ping expired-with-usable-voucher: status=%d, want 200", status)
	}
}

// TestAuthRequiredExpiredHTMLRedirectShowsFlash covers the HTML (non-API)
// branch: an expired account navigating the admin UI is redirected to /login
// with a flash explaining why — the login page renders it, so the forced
// logout is not silent.
func TestAuthRequiredExpiredHTMLRedirectShowsFlash(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	user := createOperatorUser(t, pool, "expired-html", "personal", "pass-expired-html")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)

	// Plain navigation (no Accept: application/json): the middleware answers
	// with a redirect to /login, which the client follows; the login page
	// consumes the flash and shows the expiry reason.
	resp, err := tc.client.Get(tc.srv.URL + "/api/auth-ping")
	if err != nil {
		t.Fatalf("GET /api/auth-ping (HTML): %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login page after redirect: status=%d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Masa aktif akun Anda telah habis") {
		t.Errorf("login page missing expiry flash, body=%s", string(body))
	}
}
