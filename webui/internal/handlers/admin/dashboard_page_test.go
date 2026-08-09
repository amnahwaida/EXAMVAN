package admin

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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
// Dashboard page rendering test: the "Sisa Disk Server" stat card must be
// rendered with a real free-disk value (server_disk_free), never the "—"
// fallback, and the value must agree with the getFreeDiskSpace helper on the
// same storage partition.
// ---------------------------------------------------------------------------

// dashboardTemplates are exactly the templates dashboard.html pulls in (its
// own body plus the head/nav/svg partials). Loading precisely this set keeps
// the test focused on the real page without parsing every template in the
// repo (which would require the server's full funcMap).
var dashboardTemplates = []string{
	"admin/dashboard.html",
	"admin/partials/head.html",
	"admin/partials/nav.html",
	"admin/partials/svg-symbols.html",
}

// loadDashboardTemplatesForTest registers the real dashboard templates on the
// gin engine with the minimal funcMap subset those templates actually use
// (`dict`, `default`, `displayRole`, `contains` — from head/nav partials —
// plus `sub`, `add`, `seq` for the pagination block), mirroring the server's
// own loading (cmd/server/main.go: template.New("").Funcs(funcMap) +
// per-file Parse). The templates dir is resolved relative to the test package
// dir (webui/internal/handlers/admin → ../../../templates), which is where
// `go test` runs from.
func loadDashboardTemplatesForTest(t *testing.T, r *gin.Engine) {
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
		"sub":         func(a, b int) int { return a - b },
		"add":         func(a, b int) int { return a + b },
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
	})
	for _, name := range dashboardTemplates {
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

// newDashboardPageTestRouter mirrors the production wiring for the dashboard
// page (AuthRequired → FeatureLockRequired → Dashboard), plus the
// /test/login/:id session seam shared by the other integration tests. The
// storage path is pointed at a writable temp dir so getStoragePath →
// getFreeDiskSpace measures real (positive) free space — exactly like the
// disk-cap and stats tests. Unlike the JSON-only routers, this one serves the
// REAL page with the REAL templates, so the rendered HTML can be asserted.
func newDashboardPageTestRouter(t *testing.T, pool *pgxpool.Pool, storagePath string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{StoragePath: storagePath})
	})

	// loginSeam is a helper that writes the session for a user. When rawRole is
	// true the session role is stored VERBATIM from the DB (u.Role) — the exact
	// state vouchers.go writes after a redeem/activate without re-login — which
	// is how the role JSON can be '["guru","operator"]' instead of the
	// normalized "operator". The template guards must handle both formats (they
	// compare by role membership via hasRole, not exact equality).
	loginSeam := func(rawRole bool) gin.HandlerFunc {
		return func(c *gin.Context) {
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
			var adminRole string
			if rawRole {
				// Raw role JSON — reproduces the session after a voucher
				// redeem/activate (vouchers.go writes COALESCE(role,'')).
				adminRole = u.Role
			} else {
				// Mirror the production login handler's session-role
				// normalization (cmd/server/main.go loginHandler): superadmin →
				// "superadmin", operator → "operator", everyone else keeps the
				// raw role JSON. The template role guards compare against these
				// exact values, so the seam must reproduce them for the
				// rendered pages to be asserted faithfully.
				adminRole = u.Role
				if u.IsSuperAdmin() {
					adminRole = "superadmin"
				} else if models.HasRole(u.Role, models.RoleOperator) {
					adminRole = "operator"
				}
			}
			s.Set(middleware.SessionKeyRole, adminRole)
			s.Set(middleware.SessionKeyIsSuper, u.IsSuperAdmin())
			s.Set(middleware.SessionKeyInstansi, u.Instansi)
			_ = s.Save()
			c.JSON(http.StatusOK, gin.H{"success": true})
		}
	}
	r.POST("/test/login/:id", loginSeam(false))
	r.POST("/test/login-raw/:id", loginSeam(true))

	// Real dashboard behind AuthRequired → FeatureLockRequired, exactly like
	// production (cmd/server/main.go: lockedPages.GET("/dashboard", ...)).
	adminPages := r.Group("/admin", middleware.AuthRequired())
	adminPages.GET("/dashboard", middleware.FeatureLockRequired(), Dashboard())

	loadDashboardTemplatesForTest(t, r)
	return r
}

// getDashboardPage fetches /admin/dashboard with the client's session and
// returns the HTTP status and the rendered HTML body.
func getDashboardPage(t *testing.T, client *http.Client, srv *httptest.Server) (int, string) {
	t.Helper()
	resp, err := client.Get(srv.URL + "/admin/dashboard")
	if err != nil {
		t.Fatalf("GET /admin/dashboard: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /admin/dashboard body: %v", err)
	}
	return resp.StatusCode, string(body)
}

// TestDashboardRendersServerDiskFree locks in the UI half of the storage-quota
// feature: the rendered /admin/dashboard page must show the "Sisa Disk Server"
// stat card (stat-card stat-disk) with a REAL free-disk value — never the "—"
// fallback — and that value must agree with the getFreeDiskSpace helper on the
// same storage partition (within a small tolerance for background writes on
// the shared filesystem between the handler's statfs and this check, the same
// ±1 MB convention the stats/disk-cap tests use). The page is rendered through
// the REAL Dashboard handler with the REAL templates, so the test breaks the
// moment the template conditional or the handler's formatting regresses.
func TestDashboardRendersServerDiskFree(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_dash_super", Name: "IT Dash Super",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role:               models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams:           3,
		MaxPDFSize:         1048576,
		MaxConcurrentExams: 2,
		MaxStorageSize:     50 * 1024 * 1024,
		Package:            "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	// Seed one active exam so the stats cards (Total / Aktif / Storage) and
	// the exam table have real data to render — the storage card shares the
	// `.stat-value` markup with the disk card, so its presence is also a good
	// sanity signal that the page rendered fully.
	if _, err := pool.Exec(ctx, `INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, created_by)
		VALUES ('IT Dashboard Exam', '/tmp/it-dashboard.pdf', 5*1024*1024, 'ITDASH1', 'ITDASH1', 'active', $1)`, su.ID); err != nil {
		t.Fatalf("seed exam: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-dash-it")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	srv := httptest.NewServer(newDashboardPageTestRouter(t, pool, storageDir))
	defer srv.Close()

	// 1) Without a session the page redirects to login (HTML page, so
	// AuthRequired redirects instead of returning the API 401). The default
	// http.Client follows redirects, which would land on the (unregistered)
	// /login route and report its 404 — so use a client that stops at the
	// first response and assert the redirect itself.
	anonClient := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	anonResp, err := anonClient.Get(srv.URL + "/admin/dashboard")
	if err != nil {
		t.Fatalf("GET dashboard unauthenticated: %v", err)
	}
	anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusFound {
		t.Fatalf("dashboard without session: status=%d, want 302 redirect to login", anonResp.StatusCode)
	}
	if loc := anonResp.Header.Get("Location"); !strings.Contains(loc, "/login") {
		t.Fatalf("dashboard without session: redirect Location=%q, want a /login target", loc)
	}

	// 2) As superadmin: 200 with the disk card rendered and a real value.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	// Snapshot the free space BEFORE the request so the rendered value can be
	// bounded by [before, after] — the handler's statfs happens somewhere in
	// between, and background writes on the shared filesystem can shift the
	// free bytes between any two statfs calls.
	beforeMB := getFreeDiskSpace(storageDir) / (1024 * 1024)
	status, body := getDashboardPage(t, client, srv)
	if status != http.StatusOK {
		t.Fatalf("dashboard page: status=%d, want 200", status)
	}

	// The card itself: class marker + label.
	const cardMarker = `class="stat-card stat-disk"`
	idx := strings.Index(body, cardMarker)
	if idx < 0 {
		t.Fatalf("dashboard must render the stat-disk card (%q), but it is missing", cardMarker)
	}
	if !strings.Contains(body, "Sisa Disk Server") {
		t.Error("dashboard must render the 'Sisa Disk Server' label")
	}

	// Extract the rendered value from inside the disk card's stat-value span.
	// The chunk runs from the card marker to the "Sisa Disk Server" label, so
	// it is self-documenting and does not depend on the title/icon markup
	// length staying under a magic byte budget.
	labelOff := strings.Index(body[idx:], "Sisa Disk Server")
	if labelOff < 0 {
		t.Fatalf("cannot find the 'Sisa Disk Server' label inside the stat-disk card")
	}
	chunk := body[idx : idx+labelOff]
	m := regexp.MustCompile(`stat-value">([^<]+)</span>`).FindStringSubmatch(chunk)
	if len(m) != 2 {
		t.Fatalf("cannot find the stat-value span inside the stat-disk card (chunk=%q)", chunk)
	}
	rendered := strings.TrimSpace(m[1])
	if rendered == "—" {
		t.Fatalf("server_disk_free rendered as %q (free space could not be determined), want a real value", rendered)
	}
	parts := strings.Fields(rendered)
	if len(parts) != 2 {
		t.Fatalf("unexpected server_disk_free format %q, want e.g. '123.45 GB' or '456.7 MB'", rendered)
	}
	val, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		t.Fatalf("parse server_disk_free number %q: %v", parts[0], err)
	}
	renderedMB := val
	switch parts[1] {
	case "GB":
		renderedMB = val * 1024
	case "MB":
		// already MB
	default:
		t.Fatalf("unexpected server_disk_free unit %q, want GB or MB", parts[1])
	}
	if renderedMB <= 0 {
		t.Fatalf("server_disk_free = %q (%.2f MB), want > 0 (temp storage dir on a real disk)", rendered, renderedMB)
	}

	// 3) The rendered value agrees with the free-space helper used by the
	// storage quota editors — same statfs, same partition. Because background
	// writes can shift the free bytes between statfs calls, the value is
	// bounded by the before/after snapshots around the request (the handler's
	// statfs falls strictly between them); the margin additionally covers the
	// display granularity — the card renders in GB (%.2f GB ≈ 10.24 MB per
	// tick, half-step 5.12 MB) when the disk is large, so ±8 MB absorbs both
	// the rounding and a small write during the request without weakening the
	// "rendered from the real partition, not hardcoded/—" guarantee.
	afterMB := getFreeDiskSpace(storageDir) / (1024 * 1024)
	const displayMarginMB = 8
	lo := math.Min(beforeMB, afterMB) - displayMarginMB
	hi := math.Max(beforeMB, afterMB) + displayMarginMB
	if renderedMB < lo || renderedMB > hi {
		t.Fatalf("server_disk_free = %q (%.2f MB), want within [%.2f, %.2f] MB (getFreeDiskSpace snapshots around the request ±%d MB display margin)",
			rendered, renderedMB, lo, hi, displayMarginMB)
	}
}

// TestDashboardHidesServerDiskForNonSuper locks in the "Sisa Disk Server"
// visibility policy: the stat-disk card (and its label) must be rendered ONLY
// for the superadmin. Every other role (operator, guru, pengawas) must not see
// it — the dashboard still renders normally (sanity-checked via the storage
// card) so the absence is a real conditional, not a broken page. The
// superadmin side is covered by TestDashboardRendersServerDiskFree above, so
// this test together with it pins both branches of the {{if .is_super}} guard.
func TestDashboardHidesServerDiskForNonSuper(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	guru, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_dash_guru", Name: "IT Dash Guru",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi:     "SMA Test",
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create guru: %v", err)
	}

	// Seed one exam owned by the guru so the page renders with real data and
	// the storage card (the sanity marker below) is definitely present.
	if _, err := pool.Exec(ctx, `INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, created_by)
		VALUES ('IT Guru Dashboard Exam', '/tmp/it-guru-dashboard.pdf', 2*1024*1024, 'ITGDASH1', 'ITGDASH1', 'active', $1)`, guru.ID); err != nil {
		t.Fatalf("seed exam: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-dash-guru-it")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	srv := httptest.NewServer(newDashboardPageTestRouter(t, pool, storageDir))
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(guru.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	status, body := getDashboardPage(t, client, srv)
	if status != http.StatusOK {
		t.Fatalf("dashboard page (guru): status=%d, want 200", status)
	}

	// The disk card + label must be hidden for a non-superadmin.
	if strings.Contains(body, `class="stat-card stat-disk"`) {
		t.Error("non-superadmin dashboard must NOT render the stat-disk card")
	}
	if strings.Contains(body, "Sisa Disk Server") {
		t.Error("non-superadmin dashboard must NOT render the 'Sisa Disk Server' label")
	}

	// Sanity: the page rendered fully (storage card present) — otherwise the
	// assertions above would pass vacuously on a broken/empty page.
	if !strings.Contains(body, `class="stat-card stat-storage"`) {
		t.Error("dashboard must still render the storage card for a non-superadmin (page rendered fully)")
	}
}

// ---------------------------------------------------------------------------
// Instansi UI visibility tests: the dashboard's "Instansi (Klik untuk Ubah)"
// card and the mandatory-instansi onboarding modal in nav.html. Both drive
// POST /admin/api/instansi/update, which is registered under
// AdminManagementRequired — so the card must be rendered ONLY for SuperAdmin
// & Operator (a guru/pengawas must never see a control that would 403), and
// the onboarding modal only for a school-package account whose instansi is
// still the unset "personal" sentinel (a SuperAdmin or an operator that
// already claimed its school name must not be nagged). These lock in the
// server-side half of the UI flow: the seam login reproduces the production
// session-role normalization ("superadmin"/"operator"), and the templates
// are rendered through the REAL Dashboard handler + REAL nav partial.
// ---------------------------------------------------------------------------

// fetchInstansiUIPage logs in as the given user (via the normalized seam when
// rawRole is false, or the RAW-role seam — u.Role verbatim, the session state
// vouchers.go writes after a redeem/activate without re-login — when true) and
// fetches /admin/dashboard. Returns the HTTP status and the rendered HTML (plus
// the redirect Location when the response is a redirect — a pengawas-only
// account is sent away from the dashboard by Dashboard()'s guru-only gate, so
// the caller asserts on the redirect rather than a 200 render).
func fetchInstansiUIPage(t *testing.T, pool *pgxpool.Pool, srv *httptest.Server, userID int, rawRole bool) (int, string, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		// Do not follow redirects: following would land on the unregistered
		// /admin/pengawas route's 404 and hide the 302 we want to assert.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	loginPath := "/test/login/" + strconv.Itoa(userID)
	if rawRole {
		loginPath = "/test/login-raw/" + strconv.Itoa(userID)
	}
	if resp, err := client.Post(srv.URL+loginPath, "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}
	resp, err := client.Get(srv.URL + "/admin/dashboard")
	if err != nil {
		t.Fatalf("GET /admin/dashboard: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header.Get("Location")
}

// TestDashboardInstansiCardOnlyForManagementRoles locks in the visibility
// policy of the "Instansi (Klik untuk Ubah)" stat card: SuperAdmin and
// Operator see it (they may call the AdminManagementRequired endpoint it
// drives), while guru and pengawas must not — the card would only invite a
// 403. The page still renders fully for every role (storage card sanity
// marker), so the absence is a real conditional, not a broken render.
func TestDashboardInstansiCardOnlyForManagementRoles(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_inst_su", Name: "IT Inst SU",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	op, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_inst_op", Name: "IT Inst Op",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi:     "SMK Alpha",
		Role:         models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}),
		Package:      "sekolah-test",
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("create operator: %v", err)
	}
	guru, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_inst_guru", Name: "IT Inst Guru",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi:     "SMK Alpha",
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create guru: %v", err)
	}
	pw, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_inst_pw", Name: "IT Inst PW",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi:     "SMK Alpha",
		Role:         models.SerializeRoles([]string{models.RolePengawas}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create pengawas: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-inst-ui")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	srv := httptest.NewServer(newDashboardPageTestRouter(t, pool, storageDir))
	defer srv.Close()

	const cardMarker = `class="stat-card stat-instansi"`
	const cardLabel = "Instansi (Klik untuk Ubah)"
	cases := []struct {
		name     string
		userID   int
		want     bool
		redirect bool // the dashboard sends this role away (Dashboard()'s guru-only gate)
	}{
		{"superadmin", su.ID, true, false},
		{"operator", op.ID, true, false},
		{"guru", guru.ID, false, false},
		{"pengawas", pw.ID, false, true},
	}
	for _, tc := range cases {
		status, body, location := fetchInstansiUIPage(t, pool, srv, tc.userID, false)
		// A pengawas-only account is sent to /admin/pengawas by Dashboard()'s
		// guru-only gate — it never renders the dashboard, so it cannot see
		// the card either (and its 302 is itself the assertion that the card
		// is out of reach).
		if tc.redirect {
			if status != http.StatusFound || !strings.Contains(location, "/admin/pengawas") {
				t.Errorf("%s: dashboard status=%d location=%q, want 302 to /admin/pengawas", tc.name, status, location)
			}
			continue
		}
		if status != http.StatusOK {
			t.Fatalf("%s: dashboard status=%d, want 200", tc.name, status)
		}
		gotCard := strings.Contains(body, cardMarker)
		if gotCard != tc.want {
			t.Errorf("%s: instansi card present=%v, want %v", tc.name, gotCard, tc.want)
		}
		gotLabel := strings.Contains(body, cardLabel)
		if gotLabel != tc.want {
			t.Errorf("%s: instansi card label present=%v, want %v", tc.name, gotLabel, tc.want)
		}
		// Sanity: the page rendered fully for every role.
		if !strings.Contains(body, `class="stat-card stat-storage"`) {
			t.Errorf("%s: storage card missing — page did not render fully", tc.name)
		}
	}
}

// TestDashboardOperatorFeaturesForRawRoleJSON locks in the dashboard half of the
// role-membership fix: a multi-role operator (guru + operator) whose session
// role is the RAW role JSON — exactly what a voucher redeem/activate writes
// without re-login — must still see every operator feature on the dashboard:
// the instansi card, the exam-table Pembuat/Guru columns, the Delegasi Ujian
// action, the Pengawas Config section, and IS_PRIVILEGED=true. Previously the
// template compared .admin_role against the exact string "operator", so a raw
// JSON session hid these features until the operator re-logged-in.
func TestDashboardOperatorFeaturesForRawRoleJSON(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	// Multi-role operator with the raw role JSON format applyRedemptionEntitlement
	// writes after a school voucher redeem.
	op, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_dash_rawop", Name: "IT Dash Raw Op",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi:     "SMK Raw",
		Role:         models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}),
		Package:      "sekolah-test",
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("create operator: %v", err)
	}
	// A plain guru must NOT gain operator features from the same raw-JSON seam.
	guru, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_dash_rawguru", Name: "IT Dash Raw Guru",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi:     "SMK Raw",
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create guru: %v", err)
	}
	// One exam so the table + operator-only columns render with real data.
	if _, err := pool.Exec(ctx, `INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, created_by)
		VALUES ('IT Raw Op Exam', '/tmp/it-raw-op.pdf', 3*1024*1024, 'ITRAWOP1', 'ITRAWOP1', 'active', $1)`, op.ID); err != nil {
		t.Fatalf("seed exam: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-dash-rawop")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	srv := httptest.NewServer(newDashboardPageTestRouter(t, pool, storageDir))
	defer srv.Close()

	// Operator with raw JSON session role: all operator features present.
	status, body, _ := fetchInstansiUIPage(t, pool, srv, op.ID, true)
	if status != http.StatusOK {
		t.Fatalf("dashboard (raw operator): status=%d, want 200", status)
	}
	if !strings.Contains(body, `class="stat-card stat-instansi"`) {
		t.Error("raw-JSON operator dashboard must render the instansi card")
	}
	// The delegation BUTTON (inside the exam-action dropdown) is guarded; the
	// modal itself is always rendered (hidden) so it cannot serve as the marker.
	if !strings.Contains(body, "openDelegateExamModal(") {
		t.Error("raw-JSON operator dashboard must render the Delegasi Ujian action button")
	}
	if !strings.Contains(body, "Pengawas Ujian") {
		t.Error("raw-JSON operator dashboard must render the Pengawas Config section")
	}
	if !strings.Contains(body, "IS_PRIVILEGED = true") {
		t.Error("raw-JSON operator dashboard must set IS_PRIVILEGED = true")
	}
	// Pembuat/Guru table columns are rendered per-row; the header guard is the
	// same hasRole condition, so assert the table header cell.
	if !strings.Contains(body, "<th scope=\"col\">Pembuat</th>") {
		t.Error("raw-JSON operator dashboard must render the Pembuat column header")
	}
	if !strings.Contains(body, `class="stat-card stat-storage"`) {
		t.Error("raw-JSON operator dashboard storage card missing — page did not render fully")
	}

	// Plain guru with the same raw-JSON seam: no operator features.
	status, body, _ = fetchInstansiUIPage(t, pool, srv, guru.ID, true)
	if status != http.StatusOK {
		t.Fatalf("dashboard (raw guru): status=%d, want 200", status)
	}
	if strings.Contains(body, `class="stat-card stat-instansi"`) {
		t.Error("guru dashboard must NOT render the instansi card (management-only)")
	}
	if strings.Contains(body, "openDelegateExamModal(") {
		t.Error("guru dashboard must NOT render the Delegasi Ujian action button")
	}
	if strings.Contains(body, "<th scope=\"col\">Pembuat</th>") {
		t.Error("guru dashboard must NOT render the Pembuat column header (management-only)")
	}
	if strings.Contains(body, "IS_PRIVILEGED = true") {
		t.Error("guru dashboard must set IS_PRIVILEGED = false")
	}
}

// TestMandatoryInstansiModalOnlyForUnclaimedSchoolOperator locks in the
// onboarding modal in nav.html (shared by every admin page): it must appear
// ONLY for a school-package ("sekolah…") account whose instansi is still the
// unset "personal" sentinel — i.e. a school operator who redeemed its voucher
// but has not claimed a school name yet (helpers.go needs_instansi). It must
// NOT appear for an operator that already set its school name, for the
// SuperAdmin, or for a plain guru. This also pins the nav partial receiving
// the flag: every page passes needs_instansi into the nav dict, so the modal
// actually renders (previously the flag was computed server-side but never
// forwarded to the partial, silently dropping the modal).
func TestMandatoryInstansiModalOnlyForUnclaimedSchoolOperator(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	unclaimed, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_inst_uncl", Name: "IT Inst Unclaimed",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi:     "personal", // redeemed the school voucher, school name not yet set
		Role:         models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}),
		Package:      "sekolah-test",
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("create unclaimed operator: %v", err)
	}
	claimed, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_inst_claim", Name: "IT Inst Claimed",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi:     "SMK Beta", // already claimed a school name
		Role:         models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}),
		Package:      "sekolah-test",
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("create claimed operator: %v", err)
	}
	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_inst_su2", Name: "IT Inst SU2",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	guru, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_inst_guru2", Name: "IT Inst Guru2",
		PasswordHash: "x", Status: models.UserStatusActive,
		Instansi:     "personal",
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams:     3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create guru: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-inst-modal")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	srv := httptest.NewServer(newDashboardPageTestRouter(t, pool, storageDir))
	defer srv.Close()

	const modalMarker = `id="instansiOnboardingModal"`
	const modalTitle = "Wajib Atur Nama Instansi / Sekolah"
	cases := []struct {
		name   string
		userID int
		want   bool
	}{
		{"unclaimed school operator", unclaimed.ID, true},
		{"claimed school operator", claimed.ID, false},
		{"superadmin", su.ID, false},
		{"plain guru", guru.ID, false},
	}
	for _, tc := range cases {
		status, body, _ := fetchInstansiUIPage(t, pool, srv, tc.userID, false)
		if status != http.StatusOK {
			t.Fatalf("%s: dashboard status=%d, want 200", tc.name, status)
		}
		gotModal := strings.Contains(body, modalMarker)
		if gotModal != tc.want {
			t.Errorf("%s: onboarding modal present=%v, want %v", tc.name, gotModal, tc.want)
		}
		gotTitle := strings.Contains(body, modalTitle)
		if gotTitle != tc.want {
			t.Errorf("%s: onboarding modal title present=%v, want %v", tc.name, gotTitle, tc.want)
		}
		if !strings.Contains(body, `class="stat-card stat-storage"`) {
			t.Errorf("%s: storage card missing — page did not render fully", tc.name)
		}
	}
}

// TestAllNavIncludesForwardNeedsInstansi locks in the needs_instansi plumbing
// contract across EVERY admin template: each page that includes the shared
// nav.html partial must also forward the needs_instansi flag into the partial's
// dict — otherwise the mandatory-instansi onboarding modal silently disappears
// (the server computes the flag in renderAdminPage, but nav.html only renders
// the modal when the flag reaches it). This is a textual guard over the actual
// templates, so adding a new admin page (or editing an existing nav include)
// without forwarding the flag fails the test. Pages that deliberately do NOT
// include nav (login.html, the base.html reference, the public/ pages) are
// irrelevant here — they have no authenticated session to be a school operator.
func TestAllNavIncludesForwardNeedsInstansi(t *testing.T) {
	templatesDir := "templates"
	if _, err := os.Stat(templatesDir); err != nil {
		templatesDir = filepath.Join("..", "..", "..", "templates")
	}
	matches, err := filepath.Glob(filepath.Join(templatesDir, "admin", "*.html"))
	if err != nil {
		t.Fatalf("glob admin templates: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no admin templates found under %s", templatesDir)
	}
	checked := 0
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatalf("read %s: %v", m, err)
		}
		// Only templates that include the shared nav partial are relevant.
		if !strings.Contains(string(data), `admin/partials/nav.html`) {
			continue
		}
		checked++
		// The nav include must carry the flag in its dict. The opening and
		// closing braces of the same {{template …}} invocation, so a partial
		// string search for "needs_instansi" is safe (it can only appear in
		// the dict on that line).
		if !strings.Contains(string(data), `"needs_instansi" .needs_instansi`) {
			t.Errorf("%s: nav include must forward \"needs_instansi\" .needs_instansi", filepath.Base(m))
		}
	}
	if checked == 0 {
		t.Fatalf("no admin template includes the nav partial — guard is vacuous")
	}
	t.Logf("verified needs_instansi forwarding in %d admin templates", checked)
}
