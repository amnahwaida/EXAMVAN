package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAutoApproveUIMarkupPresent is a textual guard over the admin templates
// (same style as TestAllNavIncludesForwardNeedsInstansi): it pins the UI half
// of the auto-approve feature so a later edit cannot silently drop the
// indicator badges or the revoke action. The rendered-behaviour sides are
// covered by the integration tests (TestDashboardShowsAutoApproveIndicator,
// TestPengawasExamsListSurfacesAutoApprove, TestAutoApproveRevokeApprovedDevice
// PersistsOnPoll); this guard catches the markup disappearing entirely.
func TestAutoApproveUIMarkupPresent(t *testing.T) {
	templatesDir := "templates"
	if _, err := os.Stat(templatesDir); err != nil {
		templatesDir = filepath.Join("..", "..", "..", "templates")
	}
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(templatesDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(data)
	}

	// Dashboard: the badge is server-rendered per row inside the status cell.
	dash := read("admin/dashboard.html")
	for _, frag := range []string{
		`{{if $exam.AutoApprove}}`,
		`id="auto-approve-{{$exam.ID}}"`,
		"Auto-approve aktif",
	} {
		if !strings.Contains(dash, frag) {
			t.Errorf("dashboard.html must contain %q (auto-approve badge markup)", frag)
		}
	}

	// Pengawas list: the badge is JS-rendered from the API's auto_approve field.
	pengawas := read("admin/pengawas.html")
	for _, frag := range []string{
		"ex.auto_approve",
		"autoBadge",
		"Auto-approve aktif",
	} {
		if !strings.Contains(pengawas, frag) {
			t.Errorf("pengawas.html must contain %q (auto-approve badge markup)", frag)
		}
	}

	// Pengawas detail: the revoke ("Tolak") action on the monitoring table and
	// the immediate monitoring-table refresh after an approval decision.
	detail := read("admin/pengawas_detail.html")
	for _, frag := range []string{
		`Cabut izin perangkat`,
		"loadDetail(undefined, true)",
	} {
		if !strings.Contains(detail, frag) {
			t.Errorf("pengawas_detail.html must contain %q (revoke action markup)", frag)
		}
	}
}
