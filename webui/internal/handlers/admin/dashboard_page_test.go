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
		s.Set(middleware.SessionKeyRole, u.Role)
		s.Set(middleware.SessionKeyIsSuper, u.IsSuperAdmin())
		s.Set(middleware.SessionKeyInstansi, u.Instansi)
		_ = s.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

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
