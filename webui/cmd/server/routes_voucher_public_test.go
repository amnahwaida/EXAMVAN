package main

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/config"
)

// TestNoPublicVoucherRoutes locks in the routing contract behind the
// sub-account voucher policy: voucher claim/activate endpoints are
// session-based ADMIN API endpoints ONLY (/admin/api/*). No voucher path may
// be registered on a public (non-/admin) route — the Android/desktop exam
// clients expose no voucher UI and never call these endpoints (see the
// client-surface audit in README.md), and a token-less caller must never be
// able to reach a redeem/activate endpoint.
//
// The test drives the real registerRoutes — the exact function main() calls —
// and inspects the resulting route table, so it fails at registration time if
// a voucher route is ever moved outside /admin (e.g. into the public /api
// group or a top-level path). It runs without a database: registerRoutes only
// builds handler closures; the pool is read from the request context at call
// time, so passing nil is safe for route-table inspection.
func TestNoPublicVoucherRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerRoutes(r, &config.Config{Version: "test", AdminUser: "superadmin"}, nil)

	// 1. Negative: no registered route that claims or activates vouchers may
	//    live outside the /admin prefix. Paths are matched on "voucher" as
	//    well as "redeem"/"activate" so a public claim endpoint named
	//    differently (e.g. /api/redeem) is still caught. Legitimate homes: the
	//    super-admin page /admin/vouchers and the admin API group
	//    /admin/api/vouchers/*.
	for _, route := range r.Routes() {
		p := strings.ToLower(route.Path)
		if (strings.Contains(p, "voucher") || strings.Contains(p, "redeem") || strings.Contains(p, "activate")) &&
			!strings.HasPrefix(route.Path, "/admin") {
			t.Errorf("voucher claim/activate route registered outside /admin: %s %s", route.Method, route.Path)
		}
	}

	// 2. Positive: the claim/activate/display endpoints the billing UI depends
	//    on still exist under /admin/api — so the test fails if they are
	//    REMOVED from the admin group rather than merely moved somewhere else.
	registered := map[string]bool{}
	for _, route := range r.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, key := range []string{
		"POST /admin/api/vouchers/redeem",
		"POST /admin/api/vouchers/activate",
		"GET /admin/api/vouchers/mine",
	} {
		if !registered[key] {
			t.Errorf("voucher endpoint not registered under /admin/api: %s", key)
		}
	}
}
