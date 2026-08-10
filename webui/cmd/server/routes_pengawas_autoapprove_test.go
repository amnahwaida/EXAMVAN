package main

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/config"
)

// TestPengawasAutoApproveRoutesRegistered locks in the routing contract behind
// the pengawas approval endpoints (server-side auto-approve + per-device
// approve/reject): every one of them is an ADMIN API endpoint under
// /admin/api — read via lockedAPI.GET, writes via the CSRF-protected group —
// and NONE of them may be registered on a public (non-/admin) path.
//
// The test drives the real registerRoutes — the exact function main() calls —
// and inspects the resulting route table, exactly like
// TestNoPublicVoucherRoutes. It runs without a database: registerRoutes only
// builds handler closures; the pool is read from the request context at call
// time, so passing nil is safe for route-table inspection.
func TestPengawasAutoApproveRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerRoutes(r, &config.Config{Version: "test", AdminUser: "superadmin"}, nil)

	// 1. Positive: the endpoints the pengawas UI depends on still exist under
	//    /admin/api — the test fails if any is removed or renamed.
	registered := map[string]bool{}
	for _, route := range r.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, key := range []string{
		// Server-side auto-approve flag (read + toggle).
		"GET /admin/api/pengawas/exams/:exam_id/auto-approve",
		"POST /admin/api/pengawas/exams/:exam_id/auto-approve",
		// Approval queue + per-device decision ("cabut izin" too).
		"GET /admin/api/pengawas/exams/:exam_id/approvals",
		"POST /admin/api/pengawas/exams/:exam_id/approvals/:mac_address",
		// Full audit trail behind the auto-approve toggle (read-only).
		"GET /admin/api/pengawas/exams/:exam_id/audit-logs",
	} {
		if !registered[key] {
			t.Errorf("pengawas endpoint not registered under /admin/api: %s", key)
		}
	}

	// 2. Negative: no approval-management route may live outside /admin. The
	//    student-facing /api/exams/request-approval is deliberately public and
	//    does not match the "approvals"/"auto-approve" markers below, so it is
	//    not caught — only admin approval management must stay behind auth.
	for _, route := range r.Routes() {
		p := strings.ToLower(route.Path)
		if (strings.Contains(p, "auto-approve") || strings.Contains(p, "approvals")) &&
			!strings.HasPrefix(route.Path, "/admin") {
			t.Errorf("pengawas approval route registered outside /admin: %s %s", route.Method, route.Path)
		}
	}
}
