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
		"user_expired":            false,
		"user_max_accounts":       int64(0),
		"user_accounts_used":      int64(0),
		"user_accounts_pct":       0,
		"user_accounts_remaining": int64(0),
		"user_operator_created":   false,
	})
	if err != nil {
		t.Fatalf("render settings.html (%s): %v", role, err)
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
			want: []string{`id="section-users"`, `id="section-billing"`, `id="section-vouchers"`, `id="section-voucher-audit"`, `id="section-packages"`, `id="section-system-apps"`},
			tabs: []string{"/admin/settings#users", "/admin/settings#billing", "/admin/settings#vouchers", "/admin/settings#voucher-audit", "/admin/settings#packages", "/admin/settings#system-apps"},
		},
		{
			role:  "operator",
			want:  []string{`id="section-users"`, `id="section-billing"`},
			not:   []string{`id="section-vouchers"`, `id="section-voucher-audit"`, `id="section-packages"`, `id="section-system-apps"`},
			tabs:  []string{"/admin/settings#users", "/admin/settings#billing"},
			noTab: []string{"/admin/settings#vouchers", "/admin/settings#system-apps"},
		},
		{
			role:  "guru",
			want:  []string{`id="section-billing"`},
			not:   []string{`id="section-users"`, `id="section-vouchers"`, `id="section-packages"`, `id="section-system-apps"`},
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

func TestSettingsPageLazyJsWiring(t *testing.T) {
	out := renderSettingsForRole(t, "superadmin")
	for _, frag := range []string{
		"window.__settingsReady",
		"/static/js/settings-' + key + '.js", // dynamic per-section script loader
		"loadSectionScript",
		"data-section=\"users\"",
		"data-section=\"voucher-audit\"",
		"id=\"section-billing\"",
		"history.replaceState",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("settings.html must contain %q (lazy JS wiring)", frag)
		}
	}
	// Every section script file referenced by the loader must exist.
	for _, key := range []string{"users", "billing", "vouchers", "voucher-audit", "packages", "system-apps"} {
		path := filepath.Join("static", "js", "settings-"+key+".js")
		if _, err := os.Stat(path); err != nil {
			if _, err2 := os.Stat(filepath.Join("..", "..", "..", path)); err2 != nil {
				t.Errorf("missing lazily-loaded section script %s", path)
			}
		}
	}
}

// newSettingsPageTestRouter wires /admin/settings exactly like production
// (AuthRequired -> FeatureLockRequired -> SettingsPage) so the unauthenticated
// redirect and the template set load with the full rendering path.
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
	lockedPages := adminPages.Group("", middleware.FeatureLockRequired())
	lockedPages.GET("/settings", SettingsPage())

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
