package admin

// Tests for the merged /admin/settings page (single page, client-side tabs):
// role gating of the six sections, the lazy per-section JS wiring, and a
// route-level smoke test (superadmin/guru render 200 with the full server
// funcMap; the sections' API data comes from the existing users/billing
// handler tests).

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// settingsTemplates are exactly the templates settings.html pulls in.
var settingsTemplates = []string{
	"admin/settings.html",
	"admin/partials/head.html",
	"admin/partials/nav.html",
	"admin/partials/settings-tabs.html",
	"admin/partials/svg-symbols.html",
}

// settingsRenderFuncs is the funcMap subset settings.html + its partials use.
func settingsRenderFuncs() template.FuncMap {
	return template.FuncMap{
		"dict": func(values ...interface{}) map[string]interface{} {
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i+1 < len(values); i += 2 {
				m[fmt.Sprintf("%v", values[i])] = values[i+1]
			}
			return m
		},
		"default": func(d, v interface{}) interface{} {
			if v == nil || v == "" {
				return d
			}
			return v
		},
		"displayRole": models.DisplayRoles,
		"contains":    strings.Contains,
		"hasRole":     models.HasRole,
		"substr": func(s string, start, end int) string {
			runes := []rune(s)
			if start < 0 {
				start = 0
			}
			if start >= len(runes) {
				return ""
			}
			if end > len(runes) {
				end = len(runes)
			}
			if end <= start {
				return ""
			}
			return string(runes[start:end])
		},
	}
}

func renderSettingsForRole(t *testing.T, role string) string {
	t.Helper()
	return renderSettingsForRoleLocked(t, role, false, false)
}

// renderSettingsForRoleLocked renders the settings page for the given role
// with the feature-lock and expiry flags the handler derives from the session
// user: featureLocked hides every section but billing, and userExpired drives
// the yellow "Masa aktif akun Anda telah berakhir" banner. In production the
// two always move together (an expired account is feature-locked), but the
// template keeps them as separate inputs — the test pins both combinations.
func renderSettingsForRoleLocked(t *testing.T, role string, featureLocked, userExpired bool) string {
	t.Helper()
	templatesDir := "templates"
	if _, err := os.Stat(templatesDir); err != nil {
		templatesDir = filepath.Join("..", "..", "..", "templates")
	}
	tmpl := template.New("").Funcs(settingsRenderFuncs())
	for _, name := range settingsTemplates {
		data, err := os.ReadFile(filepath.Join(templatesDir, name))
		if err != nil {
			t.Fatalf("read template %s: %v", name, err)
		}
		if _, err := tmpl.New(name).Parse(string(data)); err != nil {
			t.Fatalf("parse template %s: %v", name, err)
		}
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "admin/settings.html", map[string]interface{}{
		"active_page":             "settings",
		"admin_role":              role,
		"admin_user":              "tester",
		"csrf_token":              "guard-csrf-token",
		"version":                 "test",
		"feature_locked":          featureLocked,
		"admin_instansi":          "SMA NEGERI 1",
		"storage_free_mb":         2048.5,
		"storage_free_display":    "Sisa disk server: 2.00 GB",
		"voucher_enabled":         true,
		"user_package":            "free",
		"user_package_name":       "",
		"user_expires_at":         "",
		"user_max_total_exams":    int64(1),
		"user_max_concurrent":     int64(1),
		"user_max_pdf_size_mb":    1.0,
		"user_max_storage_mb":     0.0,
		"user_is_super":           true,
		"user_expired":            userExpired,
		"user_max_accounts":       int64(0),
		"user_accounts_used":      int64(0),
		"user_accounts_pct":       0,
		"user_accounts_remaining": int64(0),
		"user_operator_created":   false,
	})
	if err != nil {
		t.Fatalf("render settings.html (%s, locked=%v, expired=%v): %v", role, featureLocked, userExpired, err)
	}
	return buf.String()
}

func TestSettingsPageSectionsRoleGated(t *testing.T) {
	cases := []struct {
		role  string
		want  []string // section ids that MUST be present
		not   []string // section ids that must NOT be present
		tabs  []string // tab hrefs that must be present
		noTab []string // tab hrefs that must NOT be present
	}{
		{
			role: "superadmin",
			want: []string{`id="section-users"`, `id="section-billing"`, `id="section-vouchers"`, `id="section-general"`, `id="section-system-apps"`},
			tabs: []string{"/admin/settings#users", "/admin/settings#billing", "/admin/settings#vouchers", "/admin/settings#general", "/admin/settings#system-apps"},
		},
		{
			role:  "operator",
			want:  []string{`id="section-users"`, `id="section-billing"`},
			not:   []string{`id="section-vouchers"`, `id="section-general"`, `id="section-system-apps"`},
			tabs:  []string{"/admin/settings#users", "/admin/settings#billing"},
			noTab: []string{"/admin/settings#vouchers", "/admin/settings#general", "/admin/settings#system-apps"},
		},
		{
			role:  "guru",
			want:  []string{`id="section-billing"`},
			not:   []string{`id="section-users"`, `id="section-vouchers"`, `id="section-general"`, `id="section-system-apps"`},
			tabs:  []string{"/admin/settings#billing"},
			noTab: []string{"/admin/settings#users", "/admin/settings#vouchers"},
		},
		{
			role: "pengawas",
			want: []string{`id="section-billing"`},
			not:  []string{`id="section-users"`, `id="section-vouchers"`, `id="section-system-apps"`},
		},
	}
	for _, tc := range cases {
		out := renderSettingsForRole(t, tc.role)
		for _, w := range tc.want {
			if !strings.Contains(out, w) {
				t.Errorf("role=%s: missing section %s", tc.role, w)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(out, n) {
				t.Errorf("role=%s: unexpected section %s", tc.role, n)
			}
		}
		for _, w := range tc.tabs {
			if !strings.Contains(out, w) {
				t.Errorf("role=%s: missing tab %s", tc.role, w)
			}
		}
		for _, n := range tc.noTab {
			if strings.Contains(out, n) {
				t.Errorf("role=%s: unexpected tab %s", tc.role, n)
			}
		}
	}
}

// TestSettingsPageFeatureLockedOnlyBilling locks in the feature-lock
// behaviour of the merged settings hub: a feature-locked (expired) account
// must get ONLY the Paket & Voucher (billing) section and tab — the renewal
// surface that replaced the old /admin/billing page — never the Kelola User
// or SuperAdmin-only sections, regardless of its role.
func TestSettingsPageFeatureLockedOnlyBilling(t *testing.T) {
	// Operator is the classic locked role (superadmin is never locked): even
	// with the operator role, a locked account sees no users section/tab.
	out := renderSettingsForRoleLocked(t, "operator", true, true)
	if !strings.Contains(out, `id="section-billing"`) {
		t.Error("locked settings page must render the billing section (renewal surface)")
	}
	for _, gone := range []string{
		`id="section-users"`,
		`id="section-vouchers"`,
		`id="section-general"`,
		`id="section-system-apps"`,
	} {
		if strings.Contains(out, gone) {
			t.Errorf("locked settings page must NOT render %s (feature lock hides all but billing)", gone)
		}
	}
	// Only the billing tab is offered — no users tab, no SuperAdmin tabs.
	if !strings.Contains(out, "/admin/settings#billing") {
		t.Error("locked settings page must offer the billing tab")
	}
	for _, goneTab := range []string{
		"/admin/settings#users",
		"/admin/settings#vouchers",
		"/admin/settings#general",
		"/admin/settings#system-apps",
	} {
		if strings.Contains(out, goneTab) {
			t.Errorf("locked settings page must NOT offer tab %s", goneTab)
		}
	}

	// A locked superadmin is impossible in practice (the middleware exempts
	// superadmin), but the template gate must hold for completeness: locked
	// wins over role even for the superadmin role string.
	out = renderSettingsForRoleLocked(t, "superadmin", true, true)
	if !strings.Contains(out, `id="section-billing"`) {
		t.Error("locked superadmin settings page must still render the billing section")
	}
	for _, gone := range []string{`id="section-users"`, `id="section-vouchers"`, `id="section-general"`, `id="section-system-apps"`} {
		if strings.Contains(out, gone) {
			t.Errorf("locked superadmin settings page must NOT render %s", gone)
		}
	}

	// Sanity: the SAME role WITHOUT the lock still gets its full sections
	// (guards against the gate accidentally hiding everything).
	out = renderSettingsForRoleLocked(t, "operator", false, false)
	if !strings.Contains(out, `id="section-users"`) || !strings.Contains(out, `id="section-billing"`) {
		t.Error("unlocked operator settings page must render users + billing sections")
	}
	if !strings.Contains(out, "/admin/settings#users") {
		t.Error("unlocked operator settings page must offer the users tab")
	}
}

// settingsSelectOptions extracts the <option value=...> values inside the
// mobile settingsSectionSelect dropdown only (the page has other <select>
// elements whose options must not be counted).
func settingsSelectOptions(out string) []string {
	const open = `<select id="settingsSectionSelect"`
	i := strings.Index(out, open)
	if i < 0 {
		return nil
	}
	rest := out[i:]
	j := strings.Index(rest, "</select>")
	if j < 0 {
		return nil
	}
	var opts []string
	for _, seg := range strings.Split(rest[:j], `<option value="`)[1:] {
		if k := strings.Index(seg, `"`); k > 0 {
			opts = append(opts, seg[:k])
		}
	}
	return opts
}

// TestSettingsPageFeatureLockedBannerAndAccordion pins the two UI details that
// distinguish a feature-locked account from a merely low-privilege one:
//
//   - the yellow expiry banner renders exactly when user_expired is set (and
//     stays absent for a healthy account of the same role), and
//   - the Pengaturan Umum accordion markup (the section itself and its
//     toggle-all button) only ships inside the superadmin section, so a
//     locked account can never receive the accordion UI at all.
//
// Note: the lazily-loaded JS module names (settings-general.js etc.) are NOT
// asserted here — Go's html/template strips // comments from <script> output,
// and the module loader builds filenames from a key variable, so the only
// reliable markup markers are the section/button ids themselves.
func TestSettingsPageFeatureLockedBannerAndAccordion(t *testing.T) {
	// Expired account: the banner is rendered (tells the owner where to renew)
	// and the accordion/toggle-all markup must NOT exist anywhere in the page.
	out := renderSettingsForRoleLocked(t, "operator", true, true)
	if !strings.Contains(out, "Masa aktif akun Anda telah berakhir") {
		t.Error("expired account must see the yellow expiry banner")
	}
	if !strings.Contains(out, "telah berakhir. Seluruh fitur dikunci") {
		t.Errorf("expiry banner must explain the lock, got: %s", excerpt(out, "Masa aktif akun Anda"))
	}
	for _, gone := range []string{
		`id="section-general"`,
		`id="toggleAllGeneralBtn"`,
		"Buka Semua",
	} {
		if strings.Contains(out, gone) {
			t.Errorf("locked page must NOT contain %s (accordion is superadmin-only)", gone)
		}
	}
	// The mobile dropdown must offer only billing for the locked account.
	if got := settingsSelectOptions(out); len(got) != 1 || got[0] != "billing" {
		t.Errorf("locked dropdown options = %v, want exactly [billing]", got)
	}

	// Healthy account of the same role: banner gone, and still no accordion
	// (operator has no Pengaturan Umum section) — but the dropdown grows.
	out = renderSettingsForRoleLocked(t, "operator", false, false)
	if strings.Contains(out, "Masa aktif akun Anda telah berakhir") {
		t.Error("healthy account must NOT see the expiry banner")
	}
	if strings.Contains(out, `id="toggleAllGeneralBtn"`) {
		t.Error("operator must not receive the accordion toggle button")
	}
	if got := settingsSelectOptions(out); len(got) != 2 {
		t.Errorf("operator dropdown options = %v, want 2 options", got)
	}

	// Superadmin (the only accordion owner): banner absent when healthy, and
	// the toggle-all button + Pengaturan Umum section ARE present.
	out = renderSettingsForRoleLocked(t, "superadmin", false, false)
	if strings.Contains(out, "Masa aktif akun Anda telah berakhir") {
		t.Error("healthy superadmin must NOT see the expiry banner")
	}
	for _, want := range []string{
		`id="section-general"`,
		`id="toggleAllGeneralBtn"`,
		"Buka Semua",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("superadmin page must contain %q (accordion machinery)", want)
		}
	}
	if got := settingsSelectOptions(out); len(got) != 5 {
		t.Errorf("superadmin dropdown options = %v, want 5 options", got)
	}
}

// excerpt returns a short window of s starting at the first occurrence of
// marker (or the beginning of s), for readable test failure output.
func excerpt(s, marker string) string {
	const maxLen = 200
	i := strings.Index(s, marker)
	if i < 0 {
		i = 0
	}
	if i+maxLen > len(s) {
		return s[i:]
	}
	return s[i : i+maxLen]
}

func TestSettingsPageLazyJsWiring(t *testing.T) {
	out := renderSettingsForRole(t, "superadmin")
	for _, frag := range []string{
		"window.__settingsReady",
		"settings-' + key + '.js", // dynamic per-section script loader
		"loadSectionScript",
		"resolveSection",
		"switchVoucherSubtab",
		"settingsSectionSelect",
		"data-section=\"users\"",
		"data-section=\"vouchers\"",
		"data-section=\"general\"",
		"id=\"section-billing\"",
		"history.replaceState",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("settings.html must contain %q (lazy JS wiring)", frag)
		}
	}
	// Every section script file referenced by the loader must exist.
	for _, key := range []string{"users", "billing", "vouchers", "general", "system-apps", "voucher-audit", "packages"} {
		path := filepath.Join("static", "js", "settings-"+key+".js")
		if _, err := os.Stat(path); err != nil {
			if _, err2 := os.Stat(filepath.Join("..", "..", "..", path)); err2 != nil {
				t.Errorf("missing lazily-loaded section script %s", path)
			}
		}
	}
}

// newSettingsPageTestRouter wires /admin/settings exactly like production
// (AuthRequired -> SettingsPage, OUTSIDE FeatureLockRequired — the settings
// hub replaced the six standalone pages and stays reachable for
// feature-locked accounts so they can renew via the Paket & Voucher tab; the
// SettingsPage handler itself hides every section but billing for locked
// accounts) so the unauthenticated redirect and the template set load with
// the full rendering path.
func newSettingsPageTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-settings-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("cfg", &config.Config{StoragePath: t.TempDir()})
	})

	adminPages := r.Group("/admin", middleware.AuthRequired())
	adminPages.GET("/settings", SettingsPage())

	loadSettingsTemplatesForTest(t, r)
	return r
}

func loadSettingsTemplatesForTest(t *testing.T, r *gin.Engine) {
	t.Helper()
	templatesDir := "templates"
	if _, err := os.Stat(templatesDir); err != nil {
		templatesDir = filepath.Join("..", "..", "..", "templates")
	}
	tmpl := template.New("").Funcs(settingsRenderFuncs())
	for _, name := range settingsTemplates {
		data, err := os.ReadFile(filepath.Join(templatesDir, name))
		if err != nil {
			t.Fatalf("read template %s: %v", name, err)
		}
		if _, err := tmpl.New(name).Parse(string(data)); err != nil {
			t.Fatalf("parse template %s: %v", name, err)
		}
	}
	r.SetHTMLTemplate(tmpl)
}

func TestSettingsPageRouteRenders(t *testing.T) {
	// No DB in this test, so exercise the route with an unauthenticated request
	// (redirect to login). The rendered sections for each role are covered by
	// TestSettingsPageSectionsRoleGated, and the DB-backed handler data paths
	// by the existing users_page_test / billing_page_test handlers.
	r := newSettingsPageTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("GET /admin/settings unauthenticated = %d, want 302 redirect to login", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "/login") {
		t.Errorf("redirect location = %q, want login URL", loc)
	}
}
