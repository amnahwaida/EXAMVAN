package models

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
)

// ---------------------------------------------------------------------------
// DB-backed tests for AuthenticateUser (skipped when TEST_DATABASE_URL is
// unset, so plain `go test ./...` in CI keeps passing). Expiry no longer
// blocks login: an expired account is admitted so its owner can renew on the
// billing page — feature access is gated downstream by IsFeatureLocked and
// the FeatureLockRequired middleware, not by AuthenticateUser.
// ---------------------------------------------------------------------------

// setupAuthTestDB returns a pool for this package's DB-backed tests, scoped to
// the package's own PostgreSQL schema ("it_models" — derived from the package
// name by database.NewPackageTestPool), so `go test ./...` can run this
// package and internal/handlers/admin in parallel: each package TRUNCATEs
// only the tables in its own schema, so no AccessExclusiveLock is ever
// shared. Skips (not fails) when TEST_DATABASE_URL is unset.
func setupAuthTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return database.NewPackageTestPool(t, "models")
}

// createAuthTestUser inserts an active guru account with the given expiry
// (past = expired, future = valid) and a unique username.
func createAuthTestUser(t *testing.T, pool *pgxpool.Pool, username string, expiresAt time.Time) AdminUser {
	t.Helper()
	u, err := CreateUser(context.Background(), pool, &AdminUser{
		Username: username, Name: username,
		PasswordHash: "pass-" + username,
		Status:       UserStatusActive,
		Instansi:     "personal",
		Role:         SerializeRoles([]string{RoleGuru}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return *u
}

// claimUnactivatedVoucher inserts a claimed-but-NOT-active voucher redemption
// for the user with the given remaining lifetime, exactly the row shape
// AuthenticateUser's usable-voucher query counts (`NOT is_active AND
// remaining_seconds > 0` — note active redemptions are deliberately excluded
// from the count).
func claimUnactivatedVoucher(t *testing.T, pool *pgxpool.Pool, userID int, remainingSeconds int64) {
	t.Helper()
	ctx := context.Background()
	v, err := CreateVoucher(ctx, pool, &Voucher{
		Code: fmt.Sprintf("T-%d", userID), Package: "guru",
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
		t.Fatalf("insert redemption: %v", err)
	}
}

// TestAuthenticateUserExpiredWithoutVoucher locks in the new rule: an account
// whose expiry has passed — holding NO usable claimed voucher — can still log
// in. It is admitted so its owner can reach the billing page and renew; the
// feature lock (IsFeatureLocked) is enforced downstream, not here.
func TestAuthenticateUserExpiredWithoutVoucher(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	user := createAuthTestUser(t, pool, "expired-novc", time.Now().UTC().Add(-24*time.Hour))
	if !user.IsExpired() {
		t.Fatalf("test fixture: user must be expired")
	}

	got, msg := AuthenticateUser(ctx, pool, "expired-novc", "pass-expired-novc")
	if msg != "" {
		t.Errorf("login expired-without-voucher: msg=%q, want success", msg)
	}
	if got == nil || got.ID != user.ID {
		t.Errorf("login expired-without-voucher: got user=%+v, want user %d", got, user.ID)
	}
	if !got.IsFeatureLocked() {
		t.Errorf("login expired-without-voucher: want IsFeatureLocked()=true, got false")
	}
}

// TestAuthenticateUserExpiredWithUsableVoucher locks in the retained behavior:
// an expired account that holds a claimed-but-unactivated voucher with
// remaining lifetime can still log in (so the user can activate the package on
// the billing page) — now just one case of the general rule that expired
// accounts may log in.
func TestAuthenticateUserExpiredWithUsableVoucher(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	user := createAuthTestUser(t, pool, "expired-vc", time.Now().UTC().Add(-24*time.Hour))
	if !user.IsExpired() {
		t.Fatalf("test fixture: user must be expired")
	}
	claimUnactivatedVoucher(t, pool, user.ID, 30*86400) // ~30 days left

	got, msg := AuthenticateUser(ctx, pool, "expired-vc", "pass-expired-vc")
	if msg != "" {
		t.Errorf("login expired-with-usable-voucher: msg=%q, want success", msg)
	}
	if got == nil || got.ID != user.ID {
		t.Errorf("login expired-with-usable-voucher: got user=%+v, want user %d", got, user.ID)
	}
	if !got.IsFeatureLocked() {
		t.Errorf("login expired-with-usable-voucher: want IsFeatureLocked()=true, got false")
	}
}

// TestAuthenticateUserExpiredWithExhaustedVoucher guards that an expired
// account whose claimed voucher lifetime is fully spent is still admitted
// (same rule as above — expiry no longer gates login).
func TestAuthenticateUserExpiredWithExhaustedVoucher(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	user := createAuthTestUser(t, pool, "expired-vc0", time.Now().UTC().Add(-24*time.Hour))
	claimUnactivatedVoucher(t, pool, user.ID, 0) // no lifetime left

	got, msg := AuthenticateUser(ctx, pool, "expired-vc0", "pass-expired-vc0")
	if msg != "" {
		t.Errorf("login expired-with-exhausted-voucher: msg=%q, want success", msg)
	}
	if got == nil || got.ID != user.ID {
		t.Errorf("login expired-with-exhausted-voucher: got user=%+v, want user %d", got, user.ID)
	}
	if !got.IsFeatureLocked() {
		t.Errorf("login expired-with-exhausted-voucher: want IsFeatureLocked()=true, got false")
	}
}

// TestAuthenticateUserValidAccountBaseline guards the control case: a
// non-expired account with a correct password logs in regardless of vouchers.
func TestAuthenticateUserValidAccountBaseline(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	user := createAuthTestUser(t, pool, "valid-baseline", time.Now().UTC().Add(7*24*time.Hour))
	if user.IsExpired() {
		t.Fatalf("test fixture: user must not be expired")
	}

	got, msg := AuthenticateUser(ctx, pool, "valid-baseline", "pass-valid-baseline")
	if msg != "" {
		t.Errorf("login valid account: msg=%q, want success", msg)
	}
	if got == nil || got.ID != user.ID {
		t.Errorf("login valid account: got user=%+v, want user %d", got, user.ID)
	}
}

// TestAuthenticateUserUnknownUsernameSameMessage locks in the anti-enumeration
// behavior: an UNKNOWN username and a WRONG password must produce the same
// user-facing message (nothing distinguishes "exists" from "missing"), and —
// via the dummy compare hash — the same bcrypt cost profile.
func TestAuthenticateUserUnknownUsernameSameMessage(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	createAuthTestUser(t, pool, "enum-target", time.Now().UTC().Add(7*24*time.Hour))

	got, msg := AuthenticateUser(ctx, pool, "no-such-username-here", "pass-enum-target")
	if got != nil {
		t.Errorf("unknown username: got user %+v, want nil", got)
	}
	if msg != "Username atau password salah" {
		t.Errorf("unknown username: msg=%q, want the generic rejection", msg)
	}

	// The known account with a wrong password must be indistinguishable.
	got2, msg2 := AuthenticateUser(ctx, pool, "enum-target", "wrong-password")
	if got2 != nil {
		t.Errorf("wrong password: got user %+v, want nil", got2)
	}
	if msg2 != msg {
		t.Errorf("wrong password: msg=%q, want identical to unknown-username msg %q", msg2, msg)
	}
}

// TestDummyCompareHashValid guards the timing equalizer itself: the
// precomputed dummy hash must be a usable bcrypt hash of the known dummy
// password, and must never authenticate a real user by accident (it only ever
// runs an unread compare, whose result is discarded).
func TestDummyCompareHashValid(t *testing.T) {
	if dummyCompareHash == "" {
		t.Fatal("dummyCompareHash is empty — bcrypt precompute failed")
	}
	if !CheckPassword(dummyComparePassword, dummyCompareHash) {
		t.Error("dummy hash does not verify the dummy password — the timing equalizer is broken")
	}
	if CheckPassword("any-other-password", dummyCompareHash) {
		t.Error("dummy hash accepted an arbitrary password")
	}
}
