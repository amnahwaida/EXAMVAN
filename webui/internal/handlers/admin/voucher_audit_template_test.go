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
	// readRoot reads from the repo root (static assets live outside templates/).
	readRoot := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(data)
	}

	// The merged settings page: the audit section's title and table markup, the
	// API fetch with search, and the pagination renderer. (The standalone
	// voucher_audit.html was removed; its URL now 302-redirects to
	// /admin/settings#voucher-audit, and the audit JS lives in
	// static/js/settings-voucher-audit.js, loaded lazily when the tab opens.)
	page := read("admin/settings.html")
	for _, frag := range []string{
		"Riwayat Klaim &amp; Aktivasi Voucher",
		"loadAuditLogs(",
		`id="auditLogsBody"`,
		`id="auditSearchInput"`,
	} {
		if !strings.Contains(page, frag) {
			t.Errorf("settings.html must contain %q (audit section markup)", frag)
		}
	}

	auditJS := readRoot("static/js/settings-voucher-audit.js")
	for _, frag := range []string{
		`/admin/api/vouchers/audit-logs?page=`,
		"renderAuditLogsTable",
		"renderAuditPagination",
		"voucher_redeemed",
		"voucher_activated",
		"localizeUTC(l.created_at)",
	} {
		if !strings.Contains(auditJS, frag) {
			t.Errorf("settings-voucher-audit.js must contain %q (audit page script)", frag)
		}
	}

	// The header now has a single "Pengaturan" entry (desktop + mobile) pointing
	// at /admin/settings; the SuperAdmin-only sections live in the settings tab
	// bar (admin/partials/settings-tabs.html) shared by every settings page.
	nav := read("admin/partials/nav.html")
	if strings.Count(nav, "/admin/settings") != 2 {
		t.Errorf("nav.html must link /admin/settings in both desktop and mobile menus, got %d", strings.Count(nav, "/admin/settings"))
	}
	if strings.Contains(nav, "/admin/vouchers/audit") {
		t.Errorf("nav.html must NOT link /admin/vouchers/audit anymore (moved to settings tab bar), got %d", strings.Count(nav, "/admin/vouchers/audit"))
	}

	tabs := read("admin/partials/settings-tabs.html")
	if strings.Count(tabs, "/admin/settings#voucher-audit") != 1 {
		t.Errorf("settings-tabs.html must link /admin/settings#voucher-audit exactly once, got %d", strings.Count(tabs, "/admin/settings#voucher-audit"))
	}
	if !strings.Contains(tabs, "Riwayat Klaim Voucher") {
		t.Errorf("settings-tabs.html must contain the %q label", "Riwayat Klaim Voucher")
	}
}
