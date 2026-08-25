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

	// The merged settings page: the audit section's title and table markup, and
	// the pagination/search wiring. (The standalone voucher_audit.html was
	// removed; its URL now 302-redirects to /admin/settings#voucher-audit.)
	//
	// Diagnosis (Batch 15 / ronde 9): test ini GAGAL di HEAD karena fragmen
	// "loadAuditLogs(" tidak lagi ada di settings.html. Sejak modul audit
	// diekstrak ke static/js/settings-voucher-audit.js (lazy-loaded saat tab
	// dibuka), template TIDAK lagi memanggil loadAuditLogs( langsung; ia hanya
	// me-wire-nya secara lazy: wire('auditSearchInput','keyup', …
	// lazy('loadAuditLogs')(1)). String literal lama menjadi
	// 'loadAuditLogs')(1) sehingga Contains gagal padahal arsitekturnya benar.
	// Perbaikan mengunci STRUKTUR kini: wiring lazy di template + definisi
	// fungsi di modul JS — bukan menambal template demi string lama.
	page := read("admin/settings.html")
	for _, frag := range []string{
		"Riwayat Klaim &amp; Aktivasi Voucher",
		"lazy('loadAuditLogs')",
		`id="auditLogsBody"`,
		`id="auditSearchInput"`,
	} {
		if !strings.Contains(page, frag) {
			t.Errorf("settings.html must contain %q (audit section markup)", frag)
		}
	}

	auditJS := readRoot("static/js/settings-voucher-audit.js")
	for _, frag := range []string{
		"function loadAuditLogs(",
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
	// 5-tab redesign: the audit trail is a sub-tab of the Voucher tab, so the
	// tab bar offers the merged #vouchers (desktop href + mobile dropdown
	// option) and must NOT offer a standalone #voucher-audit link.
	if strings.Contains(tabs, "/admin/settings#voucher-audit") {
		t.Errorf("settings-tabs.html must NOT link /admin/settings#voucher-audit anymore (moved into Voucher sub-tab), got %d", strings.Count(tabs, "/admin/settings#voucher-audit"))
	}
	if strings.Count(tabs, "#vouchers") != 1 {
		t.Errorf("settings-tabs.html must link #vouchers once in the desktop tab bar, got %d", strings.Count(tabs, "#vouchers"))
	}
	if strings.Count(tabs, `value="vouchers"`) != 1 {
		t.Errorf("settings-tabs.html must offer the vouchers option once in the mobile dropdown, got %d", strings.Count(tabs, `value="vouchers"`))
	}
	if !strings.Contains(tabs, "Voucher") {
		t.Errorf("settings-tabs.html must contain the %q tab label", "Voucher")
	}
	// The sub-tab bar lives in the section markup (settings.html), not the tab bar.
	if !strings.Contains(page, "switchVoucherSubtab") {
		t.Errorf("settings.html must contain switchVoucherSubtab (Daftar/Riwayat sub-tab wiring)")
	}
}
