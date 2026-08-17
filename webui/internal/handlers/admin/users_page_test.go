package admin

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Kelola User section rendering test (on the merged /admin/settings page): the
// Operator role chip (id="roleOperator") and the package selector
// (id="packageSelect") must NOT be rendered for an operator whose session role
// is the raw role JSON (e.g. '["guru","operator"]' — exactly what vouchers.go
// writes into the session after a redeem/activate without re-login). The
// template guards compare by role MEMBERSHIP (hasRole / __adminHasRole), not
// by exact string equality, so every operator is covered regardless of
// session-role format. A superadmin still sees both controls (it may create
// operator accounts).
// ---------------------------------------------------------------------------

// usersPageTemplates are exactly the templates the merged settings page pulls
// in (the Kelola User section lives in settings.html now). Loading precisely
// this set keeps the test focused on the real page without parsing every
// template in the repo.
var usersPageTemplates = []string{
	"admin/settings.html",
	"admin/partials/head.html",
	"admin/partials/nav.html",
	"admin/partials/settings-tabs.html",
	"admin/partials/svg-symbols.html",
}

// loadUsersPageTemplatesForTest registers the real settings templates on the
// gin engine with the funcMap subset settings.html + its partials actually use
// (dict, default, displayRole, contains — from head/nav — plus substr and
// hasRole from the page body), mirroring cmd/server/main.go.
func loadUsersPageTemplatesForTest(t *testing.T, r *gin.Engine) {
	t.Helper()
	templatesDir := "templates"
	if _, err := os.Stat(templatesDir); err != nil {
		templatesDir = filepath.Join("..", "..", "..", "templates")
	}
	if _, err := os.Stat(templatesDir); err != nil {
		t.Fatalf("resolve templates dir: %v", err)
	}

	tmpl := template.New("").Funcs(template.FuncMap{
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
	})
	for _, name := range usersPageTemplates {
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

// newUsersPageTestRouter mirrors the production wiring for the settings page
// (AuthRequired → SettingsPage; the Kelola User section is role-gated inside
// the merged page, and feature-locked accounts get only the billing section).
// Unlike the dashboard seam, the session role is stored RAW (u.Role) — exactly
// like vouchers.go does after a redeem/activate — so the test exercises the
// multi-role / raw-JSON session-role format that used to leak the Operator chip.
func newUsersPageTestRouter(t *testing.T, pool *pgxpool.Pool, storagePath string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-users-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{StoragePath: storagePath})
	})

	r.POST("/test/login/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		u, err := models.GetUserByID(c.Request.Context(), pool, id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false})
			return
		}
		s := sessions.Default(c)
		s.Set(middleware.SessionKeyAdminID, u.ID)
		s.Set(middleware.SessionKeyUsername, u.Username)
		s.Set(middleware.SessionKeyName, u.Name)
		// RAW role JSON — reproduces the session state after a voucher
		// redeem/activate (vouchers.go writes COALESCE(role,'') verbatim).
		s.Set(middleware.SessionKeyRole, u.Role)
		s.Set(middleware.SessionKeyIsSuper, u.IsSuperAdmin())
		s.Set(middleware.SessionKeyInstansi, u.Instansi)
		_ = s.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	adminPages := r.Group("/admin", middleware.AuthRequired())
	adminPages.GET("/settings", SettingsPage())

	loadUsersPageTemplatesForTest(t, r)
	return r
}

// fetchUsersPage logs in as the given user and fetches /admin/settings,
// returning the HTTP status and the rendered HTML.
func fetchUsersPage(t *testing.T, pool *pgxpool.Pool, srv *httptest.Server, userID int) (int, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(userID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}
	resp, err := client.Get(srv.URL + "/admin/settings")
	if err != nil {
		t.Fatalf("GET /admin/settings: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// TestUsersPageHidesOperatorRoleForRawRoleJSON locks in the UI fix: an
// operator whose session role is the RAW role JSON ("[\"guru\",\"operator\"]",
// what a voucher redeem writes without re-login) must NOT see the Operator
// role chip (id="roleOperator") nor the package selector (id="packageSelect")
// on the Kelola User page — the exact scenario that previously leaked both
// controls because the template compared .admin_role against the exact string
// "operator". A superadmin must still see both (it may create operator
// accounts). The page renders through the REAL UsersPage handler + REAL
// templates, so the test breaks the moment the template guard regresses.
func TestUsersPageHidesOperatorRoleForRawRoleJSON(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	// A multi-role operator (guru + operator) whose role JSON is exactly what
	// applyRedemptionEntitlement writes after a school voucher redeem.
	op, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_users_op", Name: "IT Users Op",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi: "SMK Alpha",
		Role:     models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}),
		Package:  "sekolah-test",
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("create operator: %v", err)
	}

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_users_su", Name: "IT Users SU",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-users-ui")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	srv := httptest.NewServer(newUsersPageTestRouter(t, pool, storageDir))
	defer srv.Close()

	// 1) Multi-role operator with raw role JSON session: no Operator chip, no
	// package selector — but the Guru/Pengawas chips still render.
	status, body := fetchUsersPage(t, pool, srv, op.ID)
	if status != http.StatusOK {
		t.Fatalf("users page (operator): status=%d, want 200", status)
	}
	if strings.Contains(body, `id="roleOperator"`) {
		t.Error("operator users page must NOT render id=\"roleOperator\" (Operator chip) for a raw-role-JSON session")
	}
	if strings.Contains(body, `id="packageSelect"`) {
		t.Error("operator users page must NOT render id=\"packageSelect\" (sub-accounts have no own package)")
	}
	if !strings.Contains(body, `id="roleGuru"`) || !strings.Contains(body, `id="rolePengawas"`) {
		t.Error("operator users page must still render the Guru and Pengawas role chips")
	}
	if !strings.Contains(body, "Tambah User Manual") {
		t.Error("operator users page did not render fully (Tambah User Manual form missing)")
	}

	// 2) Superadmin (raw role JSON session too — the seam stores u.Role
	// verbatim): sees both the Operator chip and the package selector.
	status, body = fetchUsersPage(t, pool, srv, su.ID)
	if status != http.StatusOK {
		t.Fatalf("users page (superadmin): status=%d, want 200", status)
	}
	if !strings.Contains(body, `id="roleOperator"`) {
		t.Error("superadmin users page must render id=\"roleOperator\" (superadmin may create operator accounts)")
	}
	if !strings.Contains(body, `id="packageSelect"`) {
		t.Error("superadmin users page must render id=\"packageSelect\"")
	}
}
