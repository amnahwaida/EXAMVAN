package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"html/template"
	"time"

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
// Access-scoping tests for the submissions views (SubmissionsPage)
// ---------------------------------------------------------------------------
//
// H2 + review B (13 Sep 2026): the JSON list endpoint that used to carry this
// contract (GET /admin/api/submissions — ListSubmissions) was removed as a
// dead endpoint; the contract now lives on the server-rendered submissions
// page (GET /admin/submissions?exam_id=...). Besides the LIST scoping (an
// operator from instansi B must not see instansi A submissions), the page's
// exam-info card (name, exam token, creator/delegate/pengawas usernames,
// submission count, schedule) must also be tenant-gated — enumerating
// ?exam_id=N must answer 404 for exams the caller cannot access.
//
// The router mirrors production wiring: sessions → AuthRequired →
// FeatureLockRequired → GET /submissions (the in-handler ownership check is
// the only guard, which is exactly what these tests exercise).

// newSubmissionsScopeRouter mirrors the production route stack for
// GET /admin/submissions, including the FeatureLockRequired layer the real
// server nests under the admin group. The real submissions.html template is
// registered so the page actually renders on the happy paths.
func newSubmissionsScopeRouter(t *testing.T, pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-subm-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{Version: "test", StoragePath: t.TempDir()})
	})

	// Test-only login seam: builds a session exactly like a real login would,
	// from the DB row (AuthRequired re-derives is_operator from the stored
	// Role on every request).
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

	adminAPI := r.Group("/admin", middleware.AuthRequired())
	lockedAPI := adminAPI.Group("", middleware.FeatureLockRequired())
	lockedAPI.GET("/submissions", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				t.Logf("PANIC in SubmissionsPage: %v", rec)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		SubmissionsPage()(c)
		if len(c.Errors) > 0 {
			t.Logf("GIN ERRORS: %s", c.Errors.String())
		}
		t.Logf("POST-HANDLER: status=%d written=%v", c.Writer.Status(), c.Writer.Written())
	})
	loadSubmissionsPageTemplatesForTest(t, r)
	return r
}

// submissionsPageTemplates are exactly the templates submissions.html pulls
// in (its own body plus the head/nav partials) — the same focused set the
// dashboard page test uses.
var submissionsPageTemplates = []string{
	"admin/submissions.html",
	"admin/partials/head.html",
	"admin/partials/nav.html",
	"admin/partials/svg-symbols.html",
}

// loadSubmissionsPageTemplatesForTest registers the real submissions page
// templates on the gin engine with the minimal funcMap subset the page uses,
// mirroring the server's own loading (cmd/server/main.go:
// template.New("").Funcs(funcMap) + per-file Parse). The templates dir is
// resolved relative to the test package dir, where `go test` runs from.
func loadSubmissionsPageTemplatesForTest(t *testing.T, r *gin.Engine) {
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
		"contains":    strings.Contains,
		"displayRole": models.DisplayRoles,
		"hasRole":     models.HasRole,
		"ge":          func(a, b int) bool { return a >= b },
		"gt":          func(a, b int) bool { return a > b },
		"lt":          func(a, b int) bool { return a < b },
		"le":          func(a, b int) bool { return a <= b },
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
		"sub": func(a, b int) int { return a - b },
		"add": func(a, b int) int { return a + b },
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i + 1
			}
			return s
		},
		"json": func(v interface{}) string { b, _ := json.Marshal(v); return string(b) },
		"formatExamTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.UTC().Format("2006-01-02 15:04")
		},
	})
	for _, name := range submissionsPageTemplates {
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

// scopeFixture carries the actors and the exam under test.
type scopeFixture struct {
	ExamID          int
	GuruID          int
	PengawasID      int
	OperatorSameID  int
	OperatorOtherID int
}

// createScopeFixture builds: a guru (SMA Alpha) who owns an exam, a pengawas
// assigned to it, an operator from the SAME instansi as the owner, and an
// operator from a DIFFERENT instansi (SMK Beta). One submission row is seeded
// so a 200 response genuinely carries data.
func createScopeFixture(t *testing.T, pool *pgxpool.Pool) scopeFixture {
	t.Helper()
	ctx := context.Background()

	mk := func(username, name, instansi string, roles ...string) int {
		u, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username:     username,
			Name:         name,
			Instansi:     instansi,
			PasswordHash: "pass-" + username,
			Status:       models.UserStatusActive,
			Role:         models.SerializeRoles(roles),
		})
		if err != nil {
			t.Fatalf("create user %s: %v", username, err)
		}
		return u.ID
	}

	guruID := mk("ss-guru", "Guru Alpha", "SMA Alpha", models.RoleGuru)
	pengawasID := mk("ss-pengawas", "Pengawas Alpha", "SMA Alpha", models.RolePengawas)
	operatorSameID := mk("ss-op-same", "Operator Alpha", "SMA Alpha", models.RoleOperator)
	operatorOtherID := mk("ss-op-other", "Operator Beta", "SMK Beta", models.RoleOperator)

	token := fmt.Sprintf("SS%06d", time.Now().UnixNano()%1000000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Ujian SS', 'ss.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, token, guruID).Scan(&examID); err != nil {
		t.Fatalf("seed exam: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2)`, examID, pengawasID); err != nil {
		t.Fatalf("assign pengawas: %v", err)
	}

	answers := `{"1":"A","2":"B"}`
	// start_time is a TEXT column in schema.sql (the desktop client sends a
	// local string), so seed it as a formatted string, not a time.Time.
	startedAt := time.Now().Format(time.RFC3339)
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, answers_json, score, start_time, created_at)
		VALUES ($1, 'AA:BB:CC:DD:EE:FF', 'Siswa E2E', '01', 'XII A', $2, 80, $3, $4)`,
		examID, answers, startedAt, time.Now()); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	return scopeFixture{
		ExamID:          examID,
		GuruID:          guruID,
		PengawasID:      pengawasID,
		OperatorSameID:  operatorSameID,
		OperatorOtherID: operatorOtherID,
	}
}

// scopeStatus captures the HTTP status of a page fetch without the body —
// used by the contract assertions that only care about the code.
func (sc *scopeClient) fetchStatus(examID int) (int, string) {
	return sc.list(examID)
}

// scopeClient is one logged-in browser hitting the submissions list.
type scopeClient struct {
	t     *testing.T
	base  string
	client *http.Client
}

func newScopeClient(t *testing.T, base string) *scopeClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &scopeClient{t: t, base: base, client: &http.Client{Jar: jar}}
}

func (sc *scopeClient) login(userID int) {
	sc.t.Helper()
	resp, err := sc.client.Post(sc.base+"/test/login/"+strconv.Itoa(userID), "", nil)
	if err != nil {
		sc.t.Fatalf("login %d: %v", userID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		sc.t.Fatalf("login %d: status=%d", userID, resp.StatusCode)
	}
}

// list fetches GET /admin/submissions?exam_id=<id> and returns the HTTP
// status plus the rendered body (for page-content assertions).
func (sc *scopeClient) list(examID int) (int, string) {
	sc.t.Helper()
	return sc.rawList("/admin/submissions?" + url.Values{"exam_id": {strconv.Itoa(examID)}}.Encode())
}

// listAny fetches GET /admin/submissions with the given raw query and returns
// the HTTP status plus the rendered body.
func (sc *scopeClient) listAny(rawQuery string) (int, string) {
	sc.t.Helper()
	return sc.rawList("/admin/submissions" + rawQuery)
}

// rawList performs the actual GET against the given path and returns status
// and body. A panic in the page handler surfaces here as an http.Client
// error — which fatals loudly instead of silently reporting a status.
func (sc *scopeClient) rawList(path string) (int, string) {
	sc.t.Helper()
	req, err := http.NewRequest(http.MethodGet, sc.base+path, nil)
	if err != nil {
		sc.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := sc.client.Do(req)
	if err != nil {
		sc.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		sc.t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

// TestSubmissionsPageOperatorBypass pins the H2 contract on the submissions
// page: operators are scoped by instansi (same-instansi operator may read;
// cross-instansi/personal operator must get 403), while the legacy
// owner/pengawas paths keep working.
func TestSubmissionsPageOperatorBypass(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createScopeFixture(t, pool)

	r := newSubmissionsScopeRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()
	base := srv.URL

	// Guru (owner): allowed.
	gc := newScopeClient(t, base)
	gc.login(fix.GuruID)
	if status, _ := gc.list(fix.ExamID); status != http.StatusOK {
		t.Errorf("guru (owner) list: status=%d, want 200", status)
	}

	// Pengawas assigned to the exam: allowed (UserCanAccessExam covers
	// assignments).
	pc := newScopeClient(t, base)
	pc.login(fix.PengawasID)
	if status, _ := pc.list(fix.ExamID); status != http.StatusOK {
		t.Errorf("pengawas (assigned) list: status=%d, want 200", status)
	}

	// Operator from the SAME instansi as the exam owner: allowed.
	oc := newScopeClient(t, base)
	oc.login(fix.OperatorSameID)
	if status, _ := oc.list(fix.ExamID); status != http.StatusOK {
		t.Errorf("operator same-instansi list: status=%d, want 200", status)
	}

	// Operator from a DIFFERENT instansi: 403.
	xo := newScopeClient(t, base)
	xo.login(fix.OperatorOtherID)
	if status, _ := xo.list(fix.ExamID); status != http.StatusForbidden {
		t.Errorf("operator cross-instansi list: status=%d, want 403", status)
	}
}

// TestSubmissionsPageExamInfoCardGated pins review finding B (13 Sep 2026):
// the exam-info card (exam name/token, creator & pengawas usernames, submission
// count, schedule) renders ONLY for callers who can access the exam. A
// same-instansi operator sees the real exam name in the card; a
// cross-instansi operator is rejected outright (403) instead of receiving a
// fully-populated info card for a tenant they cannot access.
func TestSubmissionsPageExamInfoCardGated(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createScopeFixture(t, pool)

	r := newSubmissionsScopeRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()
	base := srv.URL

	// Same-instansi operator: the info card renders with the exam name.
	oc := newScopeClient(t, base)
	oc.login(fix.OperatorSameID)
	status, body := oc.list(fix.ExamID)
	if status != http.StatusOK {
		t.Fatalf("same-instansi operator page: status=%d, want 200", status)
	}
	if !strings.Contains(body, "Ujian SS") {
		t.Errorf("same-instansi operator must see the exam-info card with the exam name (status=%d, body len=%d)", status, len(body))
		t.Logf("BODY: %.500s", body)
	}

	// Cross-instansi operator: 403 (never a populated card). The exam name
	// must not appear in the response at all.
	xo := newScopeClient(t, base)
	xo.login(fix.OperatorOtherID)
	status, body = xo.list(fix.ExamID)
	if status != http.StatusForbidden {
		t.Errorf("cross-instansi operator page: status=%d, want 403", status)
	}
	if strings.Contains(body, "Ujian SS") {
		t.Error("cross-instansi operator must not see any exam info in the response")
	}

	// A garbage exam_id (non-numeric) must not panic the page handler —
	// the handler parses exam_id with strconv.Atoi and must 200/404 sanely.
	gc := newScopeClient(t, base)
	gc.login(fix.GuruID)
	status, _ = gc.listAny("?exam_id=abc")
	if status != http.StatusOK {
		t.Errorf("garbage exam_id page: status=%d, want 200 (filter ignored)", status)
	}
}

// TestSubmissionsPageExcludesHeartbeatPlaceholders pins the heartbeat-leak
// fix: placeholder rows written by the heartbeat flusher (NULL/empty
// answers_json, start_time set) are presence tracking for Monitoring
// Perangkat — they must not appear on the Hasil Ujian page, neither in the
// table nor in the "Peserta" count card.
func TestSubmissionsPageExcludesHeartbeatPlaceholders(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createScopeFixture(t, pool)
	ctx := context.Background()

	// A heartbeat placeholder exactly like flushHeartbeatBatch writes it:
	// answers_json NULL, score NULL, start_time set.
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, start_time, created_at, identity_data)
		VALUES ($1, 'AA:BB:CC:DD:EE:00', 'Siswa Heartbeat', '02', 'XII A', $2, $3, '{}')`,
		fix.ExamID, time.Now().Format(time.RFC3339), time.Now()); err != nil {
		t.Fatalf("seed heartbeat placeholder: %v", err)
	}

	r := newSubmissionsScopeRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	gc := newScopeClient(t, srv.URL)
	gc.login(fix.GuruID)
	status, body := gc.list(fix.ExamID)
	if status != http.StatusOK {
		t.Fatalf("guru list: status=%d, want 200", status)
	}
	if !strings.Contains(body, "Siswa E2E") {
		t.Error("submitted student must still be listed on Hasil Ujian")
	}
	if strings.Contains(body, "Siswa Heartbeat") {
		t.Error("heartbeat placeholder must NOT be listed on Hasil Ujian")
	}
	if !strings.Contains(body, `Peserta: <strong>1</strong>`) {
		t.Error("Peserta count must be 1 (heartbeat placeholder excluded)")
	}
}

