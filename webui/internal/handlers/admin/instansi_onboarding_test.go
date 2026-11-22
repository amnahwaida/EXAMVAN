package admin

import (
	"testing"

	"github.com/examvan/webui/internal/models"
)

// TestNeedsInstansiOnboarding pins the single predicate shared by the
// server-rendered modal and the redeem/activate success toast. Any operator
// holding the unset school sentinel needs onboarding, regardless of package
// (school or custom operator).
func TestNeedsInstansiOnboarding(t *testing.T) {
	operator := models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator})
	cases := []struct {
		name     string
		role     string
		instansi string
		want     bool
	}{
		{"operator, personal", operator, "personal", true},
		{"operator, empty", operator, "", true},
		{"operator, placeholder", operator, "Belum Ditetapkan", true},
		{"operator, Personal casing", operator, "Personal", true},
		{"operator, named school", operator, "SMA Negeri 1", false},
		{"guru only", models.SerializeRoles([]string{models.RoleGuru}), "personal", false},
		{"superadmin only", models.SerializeRoles([]string{models.RoleSuperAdmin}), "personal", false},
		{"empty role", "", "personal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsInstansiOnboarding(tc.role, tc.instansi); got != tc.want {
				t.Errorf("needsInstansiOnboarding(%q, %q) = %v, want %v", tc.role, tc.instansi, got, tc.want)
			}
		})
	}
}

func TestShouldShowInstansiOnboardingHonorsPageExclusions(t *testing.T) {
	operator := models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator})
	if !shouldShowInstansiOnboarding(false, false, operator, "personal") {
		t.Error("unlocked non-super operator with personal instansi must see onboarding")
	}
	if shouldShowInstansiOnboarding(true, false, operator, "personal") {
		t.Error("SuperAdmin must never see onboarding")
	}
	if shouldShowInstansiOnboarding(false, true, operator, "personal") {
		t.Error("feature-locked account must not see onboarding (update endpoint is gated)")
	}
}
