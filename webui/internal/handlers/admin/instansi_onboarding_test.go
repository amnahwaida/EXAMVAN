package admin

import (
	"testing"

	"github.com/examvan/webui/internal/models"
)

// TestNeedsInstansiOnboarding pins the single predicate shared by the
// server-rendered modal and the redeem/activate success toast. Only a real
// school package held by an operator with the unset school sentinel needs
// onboarding; custom operator packages may intentionally be personal.
func TestNeedsInstansiOnboarding(t *testing.T) {
	operator := models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator})
	cases := []struct {
		name     string
		pkg      string
		role     string
		instansi string
		want     bool
	}{
		{"school package, operator, personal", "sekolah_kecil", operator, "personal", true},
		{"school package, operator, empty", "sekolah_menengah", operator, "", true},
		{"school package, operator, placeholder", "sekolah_besar", operator, "Belum Ditetapkan", true},
		{"school package, operator, Personal casing", "sekolah_unggulan", operator, "Personal", true},
		{"school package, operator, named school", "sekolah_kecil", operator, "SMA Negeri 1", false},
		{"school package, guru only", "sekolah_kecil", models.SerializeRoles([]string{models.RoleGuru}), "personal", false},
		{"non-school package, operator", "custom-operator", operator, "personal", false},
		{"guru package, operator", "guru", operator, "personal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsInstansiOnboarding(tc.pkg, tc.role, tc.instansi); got != tc.want {
				t.Errorf("needsInstansiOnboarding(%q, %q, %q) = %v, want %v", tc.pkg, tc.role, tc.instansi, got, tc.want)
			}
		})
	}
}
