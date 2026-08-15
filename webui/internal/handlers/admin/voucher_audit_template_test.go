package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVoucherAuditUIMarkupPresent is a textual guard over the SuperAdmin
// voucher-audit page (same style as TestAutoApproveUIMarkupPresent): it pins
// the UI half of the global voucher claim/activation audit trail so a later
// edit cannot silently drop the page, its data table, or the nav entry. The
// rendered-behaviour side is covered by the DB-backed integration test
// (TestVoucherAuditLogsEndpoint); this guard catches the markup disappearing
// entirely.
func TestVoucherAuditUIMarkupPresent(t *testing.T) {
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

	// The page itself: title, the audit table, the API fetch with search, and
	// the pagination renderer.
	page := read("admin/voucher_audit.html")
	for _, frag := range []string{
		"Riwayat Klaim &amp; Aktivasi Voucher",
		`/admin/api/vouchers/audit-logs?page=`,
		"loadAuditLogs(",
		"renderAuditLogsTable",
		"renderAuditPagination",
		"voucher_redeemed",
		"voucher_activated",
		`id="auditLogsBody"`,
		`id="auditSearchInput"`,
		"localizeUTC(l.created_at)",
	} {
		if !strings.Contains(page, frag) {
			t.Errorf("voucher_audit.html must contain %q (audit page markup)", frag)
		}
	}

	// The nav (desktop + mobile Pengaturan dropdowns): the SuperAdmin-only
	// entry pointing at the new page.
	nav := read("admin/partials/nav.html")
	if strings.Count(nav, "/admin/vouchers/audit") != 2 {
		t.Errorf("nav.html must link /admin/vouchers/audit in both desktop and mobile dropdowns, got %d", strings.Count(nav, "/admin/vouchers/audit"))
	}
	if !strings.Contains(nav, "Riwayat Klaim Voucher") {
		t.Errorf("nav.html must contain the %q label", "Riwayat Klaim Voucher")
	}
}
