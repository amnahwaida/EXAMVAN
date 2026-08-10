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

	// Pengawas detail: the audit hint (who last toggled auto-approve, when) —
	// the span next to the toggle plus the JS that fills it from the API's
	// last_changed_* fields. Pin them so a later edit cannot silently drop the
	// accountability indicator.
	detail2 := read("admin/pengawas_detail.html")
	for _, frag := range []string{
		`id="aaLastChanged"`,
		"updateAAHint",
		"loadAAStatus",
		"last_changed_by",
		// The hint's verb must stay specific per last_action (res.last_action —
		// distinct from the panel's l.action), not fall back to generic
		// "Diubah" for lifecycle/approval actions.
		`res.last_action === 'exam_created'`,
		`res.last_action === 'exam_pdf_replaced'`,
		`res.last_action === 'exam_deleted'`,
		`res.last_action === 'approval_approved'`,
		`res.last_action === 'approval_rejected'`,
	} {
		if !strings.Contains(detail2, frag) {
			t.Errorf("pengawas_detail.html must contain %q (auto-approve audit hint markup)", frag)
		}
	}

	// Pengawas detail: the full audit-history panel — the "Riwayat Audit"
	// button, the modal, and the JS fetching the complete admin_audit_logs
	// trail (not just the single last hint).
	detail3 := read("admin/pengawas_detail.html")
	for _, frag := range []string{
		"Riwayat Audit",
		`id="auditLogModal"`,
		`id="auditLogBody"`,
		"showAuditLog",
		"closeAuditLogModal",
		"/audit-logs",
		// Per-device decision labels: the panel must keep rendering them so a
		// later edit cannot silently drop approval decisions from the trail.
		"approval_approved",
		"approval_rejected",
		"Perangkat diizinkan",
		"Perangkat ditolak",
		// Lifecycle action labels (created / PDF replaced / exam deleted) so
		// the panel renders proper phrases instead of falling back to the raw
		// detail.
		"exam_created",
		"exam_pdf_replaced",
		"exam_deleted",
		"Ujian dibuat",
		"PDF diganti",
		"Ujian dihapus",
	} {
		if !strings.Contains(detail3, frag) {
			t.Errorf("pengawas_detail.html must contain %q (audit history panel markup)", frag)
		}
	}
}
