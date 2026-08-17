package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Full-stack integration tests for the feature lock: an account whose active
// period has run out can still log in, but every admin feature except the
// billing endpoints is blocked until the account is renewed (voucher
// activation or an admin expiry extension). Same PostgreSQL-backed infra and
// router as the voucher lifecycle tests (skips when TEST_DATABASE_URL is
// unset).
// ---------------------------------------------------------------------------

// billingPing hits the billing-exempt probe (the test analogue of GET
// /admin/api/vouchers/mine) and returns the status plus the JSON body.
func billingPing(t *testing.T, tc *voucherTestClient) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, tc.srv.URL+"/api/billing-ping", nil)
	if err != nil {
		t.Fatalf("build billing-ping request: %v", err)
	}
	resp, err := tc.client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/billing-ping: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// featurePing hits the feature-gated probe (/api/auth-ping) with an Accept:
// application/json header — the way the admin frontend's AJAX calls arrive —
// and returns the status plus the JSON body.
func featurePing(t *testing.T, tc *voucherTestClient) (int, string) {
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
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// firstRedemptionID returns the ID of the user's most recent (highest)
// voucher redemption row.
func firstRedemptionID(t *testing.T, pool *pgxpool.Pool, userID int) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM voucher_redemptions WHERE user_id=$1 ORDER BY id DESC LIMIT 1`,
		userID).Scan(&id); err != nil {
		t.Fatalf("find latest redemption for user %d: %v", userID, err)
	}
	return id
}

// TestFeatureLockRenewalClosesAccess locks in the complete self-service
// recovery loop over real HTTP:
//
//	expired account (no usable voucher) → every feature endpoint 403s while
//	billing keeps working → it redeems a guru voucher (billing-exempt; redeeming
//	with no active package auto-activates it) → expires_at moves to the future
//	→ the very same session is un-locked and every feature endpoint answers
//	again.
//
// It also guards that the lock is genuinely feature-wide (a feature endpoint
// must be blocked, not just one route) and that the lock message tells the
// owner where to renew.
func TestFeatureLockRenewalClosesAccess(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createGuruVoucher(t, pool)
	user := createOperatorUser(t, pool, "flock-renew", "personal", "pass-flock-renew")
	// Expire the account before the session is opened (login already happened
	// elsewhere in the app; here we build the locked state directly).
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)

	// --- Locked: features blocked, billing open, message points to renewal ---
	if status, body := featurePing(t, tc); status != http.StatusForbidden {
		t.Fatalf("feature ping while locked: status=%d, want 403", status)
	} else if !strings.Contains(body, "Masa aktif akun Anda telah habis") {
		t.Errorf("feature 403 body=%s, want the lock message", body)
	}
	// A real feature endpoint (not just the probe) is equally blocked — and the
	// 403 carries the LOCK message (not the role-denied one), pinning the lock
	// as the blocker for a guru account that would pass the role gate's
	// admin-management check were it not locked.
	if status, resp := tc.createUser(t, "flock-nope"); status != http.StatusForbidden {
		t.Errorf("create-user while locked: status=%d, want 403", status)
	} else if !strings.Contains(resp.Body, "Masa aktif akun Anda telah habis") {
		t.Errorf("create-user while locked body=%s, want the lock message", resp.Body)
	}
	// Billing stays reachable so the owner can renew.
	if status, body := billingPing(t, tc); status != http.StatusOK {
		t.Fatalf("billing ping while locked: status=%d, want 200 (body=%s)", status, body)
	}

	// --- Renewal: redeeming a voucher (billing-exempt) closes the loop. With
	// no active package on hand the redeem auto-activates the new package and
	// applies its entitlement, moving expires_at to the future.
	tc.redeem(t, "IT-GURU")

	// The account expiry moved to the future — the authoritative lock clock.
	renewed := mustGetUser(t, pool, "flock-renew")
	if renewed.ExpiresAt == nil || !renewed.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("after redeem: expires_at=%v, want future", renewed.ExpiresAt)
	}
	if renewed.IsFeatureLocked() {
		t.Fatalf("after redeem: IsFeatureLocked()=true, want false")
	}

	// --- Unlocked again on the SAME session: features restored ----------------
	if status, body := featurePing(t, tc); status != http.StatusOK {
		t.Fatalf("feature ping after renewal: status=%d, want 200 (body=%s)", status, body)
	}
	// The feature-lock is no longer the blocker: the endpoint now fails only on
	// the role gate (this account is a guru, not an operator), whose message
	// differs from the lock notice — proving the lock lifted.
	if status, resp := tc.createUser(t, "flock-ok"); status != http.StatusForbidden {
		t.Errorf("create-user after renewal: status=%d, want 403 from the role gate", status)
	} else if strings.Contains(resp.Body, "Masa aktif akun Anda telah habis") {
		t.Errorf("create-user after renewal body=%s, lock message must be gone (lock lifted)", resp.Body)
	}
}

// TestFeatureLockRenewalByActivatingPausedVoucher covers the second renewal
// path: an account that holds a claimed-but-paused (is_active=false) voucher
// stays feature-locked until it ACTIVATES that package on the billing page —
// the exact "expired user with a usable voucher" flow the design kept.
// Activation is billing-exempt and moves expires_at to the future, un-locking
// the same session.
func TestFeatureLockRenewalByActivatingPausedVoucher(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	user := createOperatorUser(t, pool, "flock-paused", "personal", "pass-flock-paused")
	claimPausedVoucher(t, pool, user.ID, 30*86400) // ~30 days of lifetime left, not yet active
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)

	// Locked despite holding the voucher — only the billing page is open.
	if status, _ := featurePing(t, tc); status != http.StatusForbidden {
		t.Fatalf("feature ping with paused voucher: status=%d, want 403", status)
	}
	if status, _ := billingPing(t, tc); status != http.StatusOK {
		t.Fatalf("billing ping with paused voucher: status=%d, want 200", status)
	}

	// Activate the paused package (billing-exempt) → expires_at to the future.
	rid := firstRedemptionID(t, pool, user.ID)
	tc.activate(t, rid)

	renewed := mustGetUser(t, pool, "flock-paused")
	if renewed.ExpiresAt == nil || !renewed.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("after activate: expires_at=%v, want future", renewed.ExpiresAt)
	}
	if status, _ := featurePing(t, tc); status != http.StatusOK {
		t.Errorf("feature ping after activation: status=%d, want 200", status)
	}
}

// TestFeatureLockAdminRenewalUnlocksSameSession covers the other renewal
// route — an administrator manually extending expires_at (the renewal admin
// action, no voucher involved): the lock lifts on the next request, same
// session, with no re-login.
func TestFeatureLockAdminRenewalUnlocksSameSession(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	user := createOperatorUser(t, pool, "flock-admin-renew", "personal", "pass-flock-admin-renew")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)

	if status, _ := featurePing(t, tc); status != http.StatusForbidden {
		t.Fatalf("feature ping while locked: status=%d, want 403", status)
	}

	// Admin renewal: extend the expiry (real DB write).
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() + interval '7 days' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("renew account: %v", err)
	}
	if status, body := featurePing(t, tc); status != http.StatusOK {
		t.Fatalf("feature ping after admin renewal: status=%d, want 200 (body=%s)", status, body)
	}
}

// TestFeatureLockSurvivesLogoutRestart guards that the lock is not a
// one-request fluke: even after a fresh login (new session), the account is
// still feature-locked until its expiry moves — and billing remains the one
// open door. (The middleware re-derives the lock from the DB per request, so
// a re-login cannot dodge it.)
func TestFeatureLockSurvivesLogoutRestart(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	user := createOperatorUser(t, pool, "flock-restart", "personal", "pass-flock-restart")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	// First session: locked.
	tc1 := newVoucherTestClient(t, pool)
	tc1.login(t, user.ID)
	if status, _ := featurePing(t, tc1); status != http.StatusForbidden {
		t.Fatalf("session 1 feature ping: status=%d, want 403", status)
	}

	// Brand-new session (fresh cookie jar — a logout+login round trip): still
	// locked, still billing-only.
	tc2 := newVoucherTestClient(t, pool)
	tc2.login(t, user.ID)
	if status, _ := featurePing(t, tc2); status != http.StatusForbidden {
		t.Errorf("session 2 feature ping: status=%d, want 403 (lock follows the account)", status)
	}
	if status, _ := billingPing(t, tc2); status != http.StatusOK {
		t.Errorf("session 2 billing ping: status=%d, want 200", status)
	}
}

// TestFeatureLockAPIBranchDoesNotRedirect guards the API branch of the
// middleware: an AJAX-style request from a locked account must get a clean 403
// JSON — never an HTML 302 — so the frontend can show the notice instead of
// silently being bounced to a page it did not ask for.
func TestFeatureLockAPIBranchDoesNotRedirect(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	user := createOperatorUser(t, pool, "flock-api", "personal", "pass-flock-api")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)

	req, err := http.NewRequest(http.MethodGet, tc.srv.URL+"/api/auth-ping", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json")
	resp, err := tc.client.Do(req)
	if err != nil {
		t.Fatalf("GET auth-ping: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403 (no redirect for API)", resp.StatusCode)
	}
	if loc := resp.Request.URL.String(); strings.Contains(loc, "/admin/settings") {
		t.Errorf("request was redirected to %s, want the 403 kept in place", loc)
	}
	var out struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, body)
	}
	if out.Success {
		t.Error("success = true, want false (locked request must not reach the handler)")
	}
	if !strings.Contains(out.Message, "Masa aktif akun Anda telah habis") {
		t.Errorf("message = %q, want the feature-lock notice", out.Message)
	}
}

// TestFeatureLockHTMLBranchRedirectsToBilling covers the browser branch: a
// locked account navigating the admin UI (plain HTML accept) is redirected to
// /admin/settings#billing — the settings hub that replaced the old
// /admin/billing renewal page — and the settings mirror renders the lock
// flash, so the owner lands exactly where they can renew and knows why.
func TestFeatureLockHTMLBranchRedirectsToBilling(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	user := createOperatorUser(t, pool, "flock-html", "personal", "pass-flock-html")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)

	// Plain GET (no API headers): FeatureLockRequired redirects to
	// /admin/settings#billing, the client follows, and the settings mirror
	// consumes the flash with the expiry reason.
	resp, err := tc.client.Get(tc.srv.URL + "/api/auth-ping")
	if err != nil {
		t.Fatalf("GET auth-ping (HTML): %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings page after redirect: status=%d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Masa aktif akun Anda telah habis") {
		t.Errorf("settings page missing lock flash, body=%s", body)
	}
}

// TestFeatureLockSuperAdminNeverLocked guards the platform-owner exemption:
// a SuperAdmin account whose expires_at is (stale) in the past must pass
// every feature gate — the lock must never cut off the person who runs the
// platform.
func TestFeatureLockSuperAdminNeverLocked(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "flock-root", Name: "Flock Root",
		PasswordHash: "pass-flock-root", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	// Stale expiry in the past — must NOT lock the platform owner.
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '30 days' WHERE id = $1`, root.ID); err != nil {
		t.Fatalf("stale-expire superadmin: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	// SuperAdmin bypasses the feature lock entirely.
	if status, body := featurePing(t, tc); status != http.StatusOK {
		t.Fatalf("superadmin feature ping: status=%d, want 200 (body=%s)", status, body)
	}
	if got, _ := models.AuthenticateUser(ctx, pool, "flock-root", "pass-flock-root"); got == nil || got.IsFeatureLocked() {
		t.Fatalf("superadmin IsFeatureLocked()=true, want never locked")
	}
}

// TestFeatureLockSettingsReachableAfterLock completes the loop for the
// plain-HTML side of the settings hub itself: it must be reachable (200, not
// redirected) while the account is locked, because its Paket & Voucher
// section IS the renewal page (the old /admin/billing redirects here).
func TestFeatureLockSettingsReachableAfterLock(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	user := createOperatorUser(t, pool, "flock-bill", "personal", "pass-flock-bill")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("expire account: %v", err)
	}

	tc := newVoucherTestClient(t, pool)
	tc.login(t, user.ID)

	req, err := http.NewRequest(http.MethodGet, tc.srv.URL+"/admin/settings", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := tc.client.Do(req)
	if err != nil {
		t.Fatalf("GET /admin/settings: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// The settings mirror answers 200 JSON for any valid session.
	if resp.StatusCode != http.StatusOK {
		t.Errorf("settings page while locked: status=%d, want 200 (body=%s)", resp.StatusCode, body)
	}
}
