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
