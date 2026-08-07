package admin

import (
	"strings"
	"testing"
)

// The role-follows-package semantics of applyRedemptionEntitlement live in the
// pure function nextRolesState (the SQL wrapper just reads the three current
// columns, calls it, and writes the three new columns back). These tests
// exercise that pure logic directly, so no database is needed.

// TestNextRolesStateAdminGrantedOperatorSurvivesSwitch is the headline
// scenario: an operator granted by the ADMIN (not by a package) must survive a
// full sekolah → guru → sekolah package switch, while package_role toggles to
// reflect only the ACTIVE package. Before base_role tracking this operator
// would be stripped when the guru voucher was activated.
func TestNextRolesStateAdminGrantedOperatorSurvivesSwitch(t *testing.T) {
	// Fresh account created by the admin with roles [guru, operator]; neither
	// base_role nor package_role is tracked yet (CreateUser does not set them).
	// base_role is derived lazily as current roles minus the package grant, so
	// the admin-granted operator is captured and survives.
	role, pkgRole, baseRole := nextRolesState(`["guru","operator"]`, "", "", `["operator"]`)
	if want := `["guru","operator"]`; role != want {
		t.Fatalf("activate sekolah: role = %s, want %s", role, want)
	}
	if pkgRole != `["operator"]` {
		t.Errorf("activate sekolah: package_role = %s, want %s", pkgRole, `["operator"]`)
	}
	if want := `["guru","operator"]`; baseRole != want {
		t.Errorf("activate sekolah: base_role = %s, want %s (admin operator must be kept)", baseRole, want)
	}

	// Switch to a guru voucher: the operator granted by the package goes away,
	// but the admin-granted operator (now tracked in base_role) stays.
	role, pkgRole, baseRole = nextRolesState(role, pkgRole, baseRole, "")
	if want := `["guru","operator"]`; role != want {
		t.Fatalf("activate guru: role = %s, want %s", role, want)
	}
	if pkgRole != "" {
		t.Errorf("activate guru: package_role = %s, want empty", pkgRole)
	}
	if want := `["guru","operator"]`; baseRole != want {
		t.Errorf("activate guru: base_role = %s, want %s", baseRole, want)
	}

	// Back to sekolah: the package re-grants operator.
	role, pkgRole, baseRole = nextRolesState(role, pkgRole, baseRole, `["operator"]`)
	if want := `["guru","operator"]`; role != want {
		t.Fatalf("reactivate sekolah: role = %s, want %s", role, want)
	}
	if pkgRole != `["operator"]` {
		t.Errorf("reactivate sekolah: package_role = %s, want %s", pkgRole, `["operator"]`)
	}
	if want := `["guru","operator"]`; baseRole != want {
		t.Errorf("reactivate sekolah: base_role = %s, want %s", baseRole, want)
	}
}

// TestNextRolesStatePackageOperatorIsTransient is the contrast case: an
// operator that comes ONLY from the package must disappear when a guru
// voucher is activated — role follows the active package, not history.
func TestNextRolesStatePackageOperatorIsTransient(t *testing.T) {
	// Plain guru account — operator arrives solely via the sekolah package and
	// must NOT leak into base_role.
	role, pkgRole, baseRole := nextRolesState(`["guru"]`, "", "", `["operator"]`)
	if want := `["guru","operator"]`; role != want {
		t.Fatalf("activate sekolah: role = %s, want %s", role, want)
	}
	if want := `["guru"]`; baseRole != want {
		t.Errorf("activate sekolah: base_role = %s, want %s (operator must NOT leak into base)", baseRole, want)
	}

	// Switch to guru: the package-granted operator is dropped.
	role, pkgRole, baseRole = nextRolesState(role, pkgRole, baseRole, "")
	if want := `["guru"]`; role != want {
		t.Fatalf("activate guru: role = %s, want %s", role, want)
	}
	if pkgRole != "" {
		t.Errorf("activate guru: package_role = %s, want empty", pkgRole)
	}
	if want := `["guru"]`; baseRole != want {
		t.Errorf("activate guru: base_role = %s, want %s", baseRole, want)
	}
}

// TestNextRolesStateSuperAdminNeverTouched guards that a SuperAdmin's role is
// never modified and package roles are never tracked on the account.
func TestNextRolesStateSuperAdminNeverTouched(t *testing.T) {
	// Modern storage: JSON array form.
	role, pkgRole, baseRole := nextRolesState(`["superadmin"]`, `["operator"]`, `["guru"]`, `["operator"]`)
	if role != `["superadmin"]` {
		t.Errorf("superadmin: role = %s, want %s", role, `["superadmin"]`)
	}
	if pkgRole != "" {
		t.Errorf("superadmin: package_role = %s, want empty", pkgRole)
	}
	if baseRole != `["guru"]` {
		t.Errorf("superadmin: base_role = %s, want %s (unchanged)", baseRole, `["guru"]`)
	}

	// Legacy plain-string form is normalized but otherwise untouched.
	role, pkgRole, _ = nextRolesState("superadmin", "", "", `["operator"]`)
	if role != `["superadmin"]` {
		t.Errorf("superadmin legacy: role = %s, want %s", role, `["superadmin"]`)
	}
	if pkgRole != "" {
		t.Errorf("superadmin legacy: package_role = %s, want empty", pkgRole)
	}
}

// TestNextRolesStateFreshAccounts covers lazy base_role initialization for
// accounts that have never activated a package: base = current roles minus
// whatever the active package grants.
func TestNextRolesStateFreshAccounts(t *testing.T) {
	cases := []struct {
		name         string
		role         string
		newPkgRole   string
		wantRole     string
		wantBaseRole string
		wantPkgRole  string
	}{
		{
			name:         "plain guru activates guru package",
			role:         `["guru"]`,
			newPkgRole:   "",
			wantRole:     `["guru"]`,
			wantBaseRole: `["guru"]`,
			wantPkgRole:  "",
		},
		{
			name:         "guru+pengawas activates guru package",
			role:         `["guru","pengawas"]`,
			newPkgRole:   "",
			wantRole:     `["guru","pengawas"]`,
			wantBaseRole: `["guru","pengawas"]`,
			wantPkgRole:  "",
		},
		{
			name:         "empty role string activates sekolah",
			role:         "",
			newPkgRole:   `["operator"]`,
			wantRole:     `["guru","operator"]`,
			wantBaseRole: `["guru"]`,
			wantPkgRole:  `["operator"]`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			role, pkgRole, baseRole := nextRolesState(c.role, "", "", c.newPkgRole)
			if role != c.wantRole {
				t.Errorf("role = %s, want %s", role, c.wantRole)
			}
			if baseRole != c.wantBaseRole {
				t.Errorf("base_role = %s, want %s", baseRole, c.wantBaseRole)
			}
			if pkgRole != c.wantPkgRole {
				t.Errorf("package_role = %s, want %s", pkgRole, c.wantPkgRole)
			}
		})
	}
}

// TestNextRolesStateLegacyBackfilledPackage guards the pre-migration path
// created by the boot backfill: an active sekolah package with backfilled
// package_role but no base_role yet (old accumulate-forever rows). Switching
// to a guru voucher must attribute the accumulated operator to the package
// (removing it) and derive base = [guru] — the stale operator is not baked in.
func TestNextRolesStateLegacyBackfilledPackage(t *testing.T) {
	role, pkgRole, baseRole := nextRolesState(`["guru","operator"]`, `["operator"]`, "", "")
	if want := `["guru"]`; role != want {
		t.Fatalf("role = %s, want %s", role, want)
	}
	if pkgRole != "" {
		t.Errorf("package_role = %s, want empty", pkgRole)
	}
	if want := `["guru"]`; baseRole != want {
		t.Errorf("base_role = %s, want %s", baseRole, want)
	}
}

// TestNextRolesStatePackageCannotGrantSuperAdmin guards the defense-in-depth
// rule: even if a package snapshot somehow carried the superadmin role (e.g. a
// direct DB edit of package_settings/custom_role), it must never be granted to
// a non-superadmin account nor tracked in package_role.
func TestNextRolesStatePackageCannotGrantSuperAdmin(t *testing.T) {
	// Single forbidden role: nothing is granted and nothing is tracked.
	role, pkgRole, baseRole := nextRolesState(`["guru"]`, "", "", `["superadmin"]`)
	if strings.Contains(role, "superadmin") {
		t.Errorf("role = %s must never contain superadmin", role)
	}
	if pkgRole != "" {
		t.Errorf("package_role = %s, want empty (forbidden role not tracked)", pkgRole)
	}
	if want := `["guru"]`; baseRole != want {
		t.Errorf("base_role = %s, want %s", baseRole, want)
	}

	// Mixed list: superadmin is stripped, the legitimate role is still granted.
	role, pkgRole, _ = nextRolesState(`["guru"]`, "", "", `["operator","superadmin"]`)
	if strings.Contains(role, "superadmin") {
		t.Errorf("role = %s must never contain superadmin", role)
	}
	if !strings.Contains(role, "operator") {
		t.Errorf("role = %s should still grant operator", role)
	}
	if pkgRole != `["operator"]` {
		t.Errorf("package_role = %s, want %s", pkgRole, `["operator"]`)
	}

	// A real superadmin account is unaffected (existing behavior).
	role, pkgRole, _ = nextRolesState(`["superadmin"]`, "", "", `["operator"]`)
	if role != `["superadmin"]` || pkgRole != "" {
		t.Errorf("superadmin must stay untouched: role=%s pkg=%s", role, pkgRole)
	}
}

// TestOperatorRoleTransition guards the sub-account reconciliation decision
// behind syncInstansiWithOperatorRole: the school → guru → school package
// switch must suspend sub-accounts when the operator role is lost, restore
// them when it is back, and ignore pure non-operator switches.
func TestOperatorRoleTransition(t *testing.T) {
	cases := []struct {
		name string
		prev string
		next string
		want string
	}{
		{
			name: "guru to guru",
			prev: `["guru"]`,
			next: `["guru"]`,
			want: "none",
		},
		{
			name: "sekolah to guru (operator dropped)",
			prev: `["guru","operator"]`,
			next: `["guru"]`,
			want: "suspend",
		},
		{
			name: "guru to sekolah (operator gained)",
			prev: `["guru"]`,
			next: `["guru","operator"]`,
			want: "restore",
		},
		{
			name: "sekolah renewal (operator retained)",
			prev: `["guru","operator"]`,
			next: `["guru","operator"]`,
			want: "restore",
		},
		{
			name: "superadmin never touched",
			prev: `["superadmin"]`,
			next: `["superadmin"]`,
			want: "none",
		},
		{
			name: "admin-granted operator survives guru switch",
			prev: `["guru","operator"]`,
			next: `["guru","operator"]`,
			want: "restore",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := operatorRoleTransition(c.prev, c.next); got != c.want {
				t.Errorf("operatorRoleTransition(%s -> %s) = %q, want %q", c.prev, c.next, got, c.want)
			}
		})
	}
}

// TestNextRolesStateHandEditedBaseRole guards against a hand-edited "[]"
// base_role producing a literal "[]" role via models.ParseRoles' fallback.
// "[]" means "no base roles", so only the package-granted operator remains in
// the role column; the guru fallback returns on the next activation (a
// documented quirk of the value-based matching — see the code comments).
func TestNextRolesStateHandEditedBaseRole(t *testing.T) {
	role, pkgRole, baseRole := nextRolesState(`["guru"]`, "", `[]`, `["operator"]`)
	if role != `["operator"]` {
		t.Errorf("role = %s, want %s", role, `["operator"]`)
	}
	if pkgRole != `["operator"]` {
		t.Errorf("package_role = %s, want %s", pkgRole, `["operator"]`)
	}
	if baseRole != `["guru"]` {
		t.Errorf("base_role = %s, want %s", baseRole, `["guru"]`)
	}
	// No literal "[]" may leak into any of the columns.
	if strings.Contains(role, "[]") || strings.Contains(pkgRole, "[]") || strings.Contains(baseRole, "[]") {
		t.Errorf("hand-edited \"[]\" base_role leaked a literal \"[]\" role: role=%s pkg=%s base=%s", role, pkgRole, baseRole)
	}
}
