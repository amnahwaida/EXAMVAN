package admin

import (
	"testing"

	"github.com/examvan/webui/internal/models"
)

// TestPackageEntitlementConcurrentQuota guards the 6-value signature of
// packageEntitlement and the max_concurrent_exams mapping per package
// (concurrent must never exceed the total exam quota, and school packages
// still grant the operator role).
func TestPackageEntitlementConcurrentQuota(t *testing.T) {
	cases := []struct {
		pkg            string
		wantExams      int64
		wantConcurrent int64
	}{
		{"", 1, 1}, // free / unknown
		{"guru", 1, 1},
		{"individu", 2, 2},
		{"sekolah_kecil", 3, 3},
		{"sekolah_menengah", 5, 5},
		{"sekolah_besar", 10, 10},
		{"sekolah_unggulan", 99999, 99999},
	}
	for _, c := range cases {
		exams, _, concurrent, _, _, _ := packageEntitlement(c.pkg)
		if exams != c.wantExams || concurrent != c.wantConcurrent {
			t.Errorf("packageEntitlement(%q) exams=%d concurrent=%d, want exams=%d concurrent=%d",
				c.pkg, exams, concurrent, c.wantExams, c.wantConcurrent)
		}
		if concurrent > exams {
			t.Errorf("packageEntitlement(%q): concurrent=%d exceeds total exams=%d", c.pkg, concurrent, exams)
		}
	}

	// School packages must still grant the operator role.
	if _, _, _, _, _, role := packageEntitlement("sekolah_kecil"); role != models.SerializeRoles([]string{models.RoleOperator}) {
		t.Errorf("sekolah_kecil should grant operator role, got %q", role)
	}
	// Non-school packages must not grant a role.
	if _, _, _, _, _, role := packageEntitlement("guru"); role != "" {
		t.Errorf("guru should not grant a role, got %q", role)
	}

	// Sub-account quota defaults: school packages cap how many accounts the
	// operator may create (0 = unlimited for non-school / unggulan).
	wantUsers := map[string]int64{
		"": 0, "guru": 0, "individu": 0,
		"sekolah_kecil": 10, "sekolah_menengah": 25, "sekolah_besar": 50, "sekolah_unggulan": 0,
	}
	for pkg, want := range wantUsers {
		if _, _, _, _, maxUsers, _ := packageEntitlement(pkg); maxUsers != want {
			t.Errorf("packageEntitlement(%q) max_users=%d, want %d", pkg, maxUsers, want)
		}
	}
}
