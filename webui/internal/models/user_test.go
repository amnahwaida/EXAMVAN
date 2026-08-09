package models

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// fakeRow implements pgx.Row with a canned value or error.
type fakeRow struct {
	value int
	err   error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > 0 {
		if p, ok := dest[0].(*int); ok {
			*p = r.value
		}
	}
	return nil
}

// fakeQuerier records the query it received and returns a canned row.
type fakeQuerier struct {
	row  fakeRow
	sql  string
	args []any
}

func (f *fakeQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.sql = sql
	f.args = args
	return f.row
}

func TestCountRecentRegistrationsByIP(t *testing.T) {
	ctx := context.Background()

	t.Run("returns count and binds the IP argument", func(t *testing.T) {
		q := &fakeQuerier{row: fakeRow{value: 3}}
		got, err := CountRecentRegistrationsByIP(ctx, q, "203.0.113.7")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 3 {
			t.Fatalf("got %d, want 3", got)
		}
		if len(q.args) != 1 || q.args[0] != "203.0.113.7" {
			t.Errorf("args = %v, want [203.0.113.7]", q.args)
		}
		// Regression guard on the SQL shape: must target the registered_ip
		// column with a $1 placeholder and a 24-hour window so the cap cannot
		// silently drift to an unbounded or wrong-window count.
		for _, frag := range []string{"admin_users", "registered_ip = $1", "interval '24 hours'", "COUNT(*)"} {
			if !strings.Contains(q.sql, frag) {
				t.Errorf("query missing %q:\n%s", frag, q.sql)
			}
		}
	})

	t.Run("zero accounts returns zero", func(t *testing.T) {
		q := &fakeQuerier{row: fakeRow{value: 0}}
		got, err := CountRecentRegistrationsByIP(ctx, q, "198.51.100.9")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 0 {
			t.Fatalf("got %d, want 0", got)
		}
	})

	t.Run("propagates scan error with context", func(t *testing.T) {
		q := &fakeQuerier{row: fakeRow{err: errors.New("boom")}}
		_, err := CountRecentRegistrationsByIP(ctx, q, "10.0.0.1")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "count recent registrations by IP") {
			t.Errorf("error = %v, want wrapped context", err)
		}
	})
}

func TestRegistrationAllowedByPerIPLimit(t *testing.T) {
	cases := []struct {
		name     string
		recent   int
		maxPerIP int
		want     bool
	}{
		{"zero cap means unlimited", 999, 0, true},
		{"negative cap treated as unlimited", 5, -1, true},
		{"below cap allowed", 2, 3, true},
		{"exactly at cap blocked", 3, 3, false},
		{"above cap blocked", 4, 3, false},
		{"cap one allows the first account only", 0, 1, true},
		{"cap one blocks the second account", 1, 1, false},
	}
	for _, c := range cases {
		if got := RegistrationAllowedByPerIPLimit(c.recent, c.maxPerIP); got != c.want {
			t.Errorf("%s: RegistrationAllowedByPerIPLimit(%d, %d) = %v, want %v",
				c.name, c.recent, c.maxPerIP, got, c.want)
		}
	}
}

// TestPlanToggleUserStatus covers the pure decision behind ToggleUserStatus.
// The regression focus: a legacy account whose expires_at is NULL (unlimited)
// that was suspended and is now being re-activated must come back WITHOUT a
// +1-day renewal — NULL stays NULL. Previously every non-active user was
// renewed with +1 day on activation, silently converting unlimited legacy
// accounts into expiring ones (and tombstoning their exams a day later).
func TestPlanToggleUserStatus(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)
	future := now.Add(48 * time.Hour)

	t.Run("pending_otp account is blocked from toggling", func(t *testing.T) {
		u := AdminUser{Username: "pending_guru", Status: UserStatusPendingOTP, ExpiresAt: nil}
		out := planToggleUserStatus(u, now)
		if !out.PendingOTPBlocked {
			t.Error("PendingOTPBlocked = false, want true (email verification incomplete)")
		}
		if out.NewStatus != "" {
			t.Errorf("NewStatus = %q, want \"\" (no status change — toggle refused)", out.NewStatus)
		}
		if out.RenewExpiry != nil || out.ReactivateLegacyNull || out.FreezeClock {
			t.Errorf("pending_otp must not carry activation flags: %+v", out)
		}
	})

	t.Run("suspending an active user has no renewal side effects", func(t *testing.T) {
		u := AdminUser{Username: "guru1", Status: UserStatusActive, ExpiresAt: &future}
		out := planToggleUserStatus(u, now)
		if out.NewStatus != UserStatusSuspended {
			t.Errorf("NewStatus = %q, want %q", out.NewStatus, UserStatusSuspended)
		}
		if out.ReactivateLegacyNull || out.FreezeClock || out.RenewExpiry != nil {
			t.Errorf("suspension must not carry reactivation side effects: %+v", out)
		}
		if !strings.Contains(out.Message, "dinonaktifkan") {
			t.Errorf("Message = %q, want suspension message", out.Message)
		}
	})

	t.Run("reactivating legacy NULL-expiry keeps NULL (no +1 day)", func(t *testing.T) {
		u := AdminUser{Username: "legacy_guru", Status: UserStatusSuspended, ExpiresAt: nil}
		out := planToggleUserStatus(u, now)
		if out.NewStatus != UserStatusActive {
			t.Errorf("NewStatus = %q, want %q", out.NewStatus, UserStatusActive)
		}
		if !out.ReactivateLegacyNull {
			t.Error("ReactivateLegacyNull = false, want true (NULL-expiry legacy account)")
		}
		if out.RenewExpiry != nil {
			t.Errorf("RenewExpiry = %v, want nil — legacy NULL expiry must NOT be granted +1 day", out.RenewExpiry)
		}
		if out.FreezeClock {
			t.Error("FreezeClock = true, want false — no expiry to freeze")
		}
		if !strings.Contains(out.Message, "tanpa batas") {
			t.Errorf("Message = %q, want 'tanpa batas' indicator", out.Message)
		}
	})

	t.Run("reactivating expired account grants exactly +1 day", func(t *testing.T) {
		u := AdminUser{Username: "expired_guru", Status: UserStatusSuspended, ExpiresAt: &past}
		out := planToggleUserStatus(u, now)
		if out.NewStatus != UserStatusActive {
			t.Errorf("NewStatus = %q, want %q", out.NewStatus, UserStatusActive)
		}
		if out.RenewExpiry == nil {
			t.Fatal("RenewExpiry = nil, want +1 day renewal for an expired account")
		}
		want := now.Add(24 * time.Hour)
		if !out.RenewExpiry.Equal(want) {
			t.Errorf("RenewExpiry = %v, want %v", out.RenewExpiry, want)
		}
		if out.ReactivateLegacyNull {
			t.Error("ReactivateLegacyNull = true, want false")
		}
		if !strings.Contains(out.Message, "+1 hari") {
			t.Errorf("Message = %q, want '+1 hari' indicator", out.Message)
		}
	})

	t.Run("reactivating account with valid future expiry freezes clock", func(t *testing.T) {
		u := AdminUser{Username: "future_guru", Status: UserStatusSuspended, ExpiresAt: &future}
		out := planToggleUserStatus(u, now)
		if out.NewStatus != UserStatusActive {
			t.Errorf("NewStatus = %q, want %q", out.NewStatus, UserStatusActive)
		}
		if !out.FreezeClock {
			t.Error("FreezeClock = false, want true (suspension clock must be frozen)")
		}
		if out.RenewExpiry != nil {
			t.Errorf("RenewExpiry = %v, want nil for a still-valid expiry", out.RenewExpiry)
		}
		if out.ReactivateLegacyNull {
			t.Error("ReactivateLegacyNull = true, want false")
		}
	})
}

// TestComputeFrozenExpiry covers the pure freeze-clock arithmetic behind
// ResumeSuspendedAccountClock: after a suspension, expires_at is extended by
// exactly the suspension duration so the package lifetime did not burn while
// the account was locked out. Nothing to freeze — never suspended, no expiry
// (NULL = unlimited), or suspended_at in the future/now (clock skew) — yields
// nil.
func TestComputeFrozenExpiry(t *testing.T) {
	now := time.Now().UTC()
	past := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	future := func(d time.Duration) *time.Time { t := now.Add(d); return &t }

	cases := []struct {
		name        string
		suspendedAt *time.Time
		expiresAt   *time.Time
		want        *time.Time
	}{
		{
			name:        "extends expiry by exactly the suspension duration",
			suspendedAt: past(2 * time.Hour),
			expiresAt:   future(24 * time.Hour),
			want:        future(26 * time.Hour),
		},
		{
			name:        "long suspension extends proportionally",
			suspendedAt: past(48 * time.Hour),
			expiresAt:   future(12 * time.Hour),
			want:        future(60 * time.Hour),
		},
		{
			name:        "already-expired expiry is pushed past the suspension",
			suspendedAt: past(72 * time.Hour),
			expiresAt:   past(24 * time.Hour),
			want:        future(48 * time.Hour),
		},
		{
			name:        "never suspended yields no freeze",
			suspendedAt: nil,
			expiresAt:   future(24 * time.Hour),
			want:        nil,
		},
		{
			name:        "no expiry (NULL = unlimited) yields no freeze",
			suspendedAt: past(2 * time.Hour),
			expiresAt:   nil,
			want:        nil,
		},
		{
			name:        "clock skew (suspended_at in the future) yields no freeze",
			suspendedAt: future(time.Hour),
			expiresAt:   future(24 * time.Hour),
			want:        nil,
		},
		{
			name:        "suspended_at exactly at now is treated as no freeze",
			suspendedAt: &now,
			expiresAt:   future(24 * time.Hour),
			want:        nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := computeFrozenExpiry(c.suspendedAt, c.expiresAt, now)
			if c.want == nil {
				if got != nil {
					t.Errorf("computeFrozenExpiry() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("computeFrozenExpiry() = nil, want non-nil frozen expiry")
			}
			if !got.Equal(*c.want) {
				t.Errorf("computeFrozenExpiry() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestIsFeatureLocked covers the pure helper behind the feature-lock: an
// account whose active period has run out (expires_at in the past) is locked —
// except SuperAdmin, whose expiry may be absent or stale and must never gate
// the platform owner. Suspended status is deliberately out of scope here:
// callers (AuthenticateUser / middleware.AuthRequired) route suspended
// accounts through the suspension branch first, so IsFeatureLocked only ever
// decides between "valid, use normally" and "expired, billing-only".
func TestIsFeatureLocked(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name string
		u    AdminUser
		want bool
	}{
		{
			name: "expired guru is feature-locked",
			u:    AdminUser{Role: SerializeRoles([]string{RoleGuru}), Status: UserStatusActive, ExpiresAt: &past},
			want: true,
		},
		{
			name: "future expiry is not locked",
			u:    AdminUser{Role: SerializeRoles([]string{RoleGuru}), Status: UserStatusActive, ExpiresAt: &future},
			want: false,
		},
		{
			name: "nil expiry (lifetime account) is not locked",
			u:    AdminUser{Role: SerializeRoles([]string{RoleGuru}), Status: UserStatusActive, ExpiresAt: nil},
			want: false,
		},
		{
			name: "bare 'superadmin' role with past expiry is never locked",
			u:    AdminUser{Role: RoleSuperAdmin, Status: UserStatusActive, ExpiresAt: &past},
			want: false,
		},
		{
			name: "JSON superadmin role with past expiry is never locked",
			u:    AdminUser{Role: SerializeRoles([]string{RoleSuperAdmin, RoleGuru}), Status: UserStatusActive, ExpiresAt: &past},
			want: false,
		},
		{
			name: "suspended guru with past expiry still reports locked",
			u:    AdminUser{Role: SerializeRoles([]string{RoleGuru}), Status: UserStatusSuspended, ExpiresAt: &past},
			want: true, // callers short-circuit on status before consulting this
		},
	}
	for _, c := range cases {
		if got := c.u.IsFeatureLocked(); got != c.want {
			t.Errorf("%s: IsFeatureLocked() = %v, want %v", c.name, got, c.want)
		}
	}
}
