package admin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Tenant-scoping tests for GET /admin/api/submissions/export (M6)
// ---------------------------------------------------------------------------
//
// M6: the all-exams export (exportAllXLSX) resolves the operator's instansi
// with a raw QueryRow whose error is swallowed, then only adds a tenant
// condition `if instansi != ""`. An operator whose instansi is empty (or
// cannot be resolved) therefore exports EVERY tenant's submissions — student
// PII from other schools. The intended contract:
//
//	operator with a real instansi → exports that instansi's submissions
//	operator with ""/"personal"   → exports only own-created exams' submissions
//	operator row deleted          → 401 (AuthRequired re-validation)
//
// The XLSX content is the assertion surface: exportAllXLSX writes student
// names on the "Semua Hasil" sheet, so cross-tenant leakage is observable by
// scanning the exported rows for the other tenant's student name.

// newSubmissionsExportRouter mirrors the production route stack for
// GET /admin/api/submissions/export (sessions → AuthRequired →
// FeatureLockRequired). The production RateLimit middleware is omitted: with
// a nil Redis client it is open-access, so it adds no test signal.
func newSubmissionsExportRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
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

	adminAPI := r.Group("/admin/api", middleware.AuthRequired())
	lockedAPI := adminAPI.Group("", middleware.FeatureLockRequired())
	lockedAPI.GET("/submissions/export", ExportSubmissions())
	return r
}

// exportScopeFixture carries the two tenants under test.
type exportScopeFixture struct {
	OperatorID  int // operator in "SMA Alpha", owns an exam with a submission
	OtherGuruID int // guru in "SMK Beta", owns an exam with a submission
}

// createExportScopeFixture builds two tenants: an operator ("SMA Alpha") who
// OWNS an exam with one submission, and a guru ("SMK Beta") who owns another
// exam with one submission. The operator must own an exam so the intended
// own-created fallback still returns rows once instansi scoping is narrowed.
func createExportScopeFixture(t *testing.T, pool *pgxpool.Pool) exportScopeFixture {
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

	operatorID := mk("xs-op", "Operator Alpha", "SMA Alpha", models.RoleOperator)
	otherGuruID := mk("xs-guru", "Guru Beta", "SMK Beta", models.RoleGuru)

	seedTenant := func(ownerID int, examName, tokenPrefix, studentName string) {
		// Distinct prefixes keep the two tokens unique even if the two
		// UnixNano reads land on the same remainder.
		token := fmt.Sprintf("%s%06d", tokenPrefix, time.Now().UnixNano()%1000000)
		var examID int
		if err := pool.QueryRow(ctx, `
			INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
			VALUES ($1, 'xs.pdf', 1024, $2, $2, 'active', 'medium', $3)
			RETURNING id`, examName, token, ownerID).Scan(&examID); err != nil {
			t.Fatalf("seed exam %s: %v", examName, err)
		}
		// start_time is a TEXT column (the desktop client sends a local
		// string), so seed it as a formatted string, not a time.Time.
		startedAt := time.Now().Format(time.RFC3339)
		if _, err := pool.Exec(ctx, `
			INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, answers_json, score, start_time, created_at)
			VALUES ($1, 'AA:BB:CC:DD:EE:FF', $2, '01', 'XII A', $3, 80, $4, $5)`,
			examID, studentName, `{"1":"A","2":"B"}`, startedAt, time.Now()); err != nil {
			t.Fatalf("seed submission for %s: %v", examName, err)
		}
	}

	seedTenant(operatorID, "Ujian Operator Alpha", "XA", "Siswa Own Tenant")
	seedTenant(otherGuruID, "Ujian Guru Beta", "XB", "Siswa Beta Tenant")

	return exportScopeFixture{
		OperatorID:  operatorID,
		OtherGuruID: otherGuruID,
	}
}

// exportScopeClient is one logged-in browser downloading the export.
type exportScopeClient struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newExportScopeClient(t *testing.T, base string) *exportScopeClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &exportScopeClient{t: t, base: base, client: &http.Client{Jar: jar}}
}

func (ec *exportScopeClient) login(userID int) {
	ec.t.Helper()
	resp, err := ec.client.Post(ec.base+"/test/login/"+strconv.Itoa(userID), "", nil)
	if err != nil {
		ec.t.Fatalf("login %d: %v", userID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		ec.t.Fatalf("login %d: status=%d", userID, resp.StatusCode)
	}
}

// exportAll downloads the all-exams export (no exam_id → exportAllXLSX) and
// returns the HTTP status plus the raw response body.
func (ec *exportScopeClient) exportAll() (int, []byte) {
	ec.t.Helper()
	req, err := http.NewRequest(http.MethodGet, ec.base+"/admin/api/submissions/export", nil)
	if err != nil {
		ec.t.Fatalf("build request: %v", err)
	}
	// JSON Accept makes isAPIRequest true, so middleware failures answer as
	// JSON status codes (e.g. a deleted user → 401) instead of a 302 redirect.
	req.Header.Set("Accept", "application/json")
	resp, err := ec.client.Do(req)
	if err != nil {
		ec.t.Fatalf("GET export: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ec.t.Fatalf("read export body: %v", err)
	}
	return resp.StatusCode, body
}

// exportContainsStudent reports whether the exported workbook's "Semua Hasil"
// sheet contains the given student name in any cell.
func exportContainsStudent(t *testing.T, body []byte, studentName string) bool {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open exported xlsx: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Semua Hasil")
	if err != nil {
		t.Fatalf("read sheet Semua Hasil: %v", err)
	}
	for _, row := range rows {
		for _, cell := range row {
			if strings.Contains(cell, studentName) {
				return true
			}
		}
	}
	return false
}

// TestSubmissionsExportOperatorTenantScoped is the regression guard: an
// operator with a real instansi ("SMA Alpha") exports that tenant's
// submissions only — never the other tenant's ("SMK Beta") student data.
func TestSubmissionsExportOperatorTenantScoped(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createExportScopeFixture(t, pool)

	r := newSubmissionsExportRouter(pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	oc := newExportScopeClient(t, srv.URL)
	oc.login(fix.OperatorID)

	status, body := oc.exportAll()
	if status != http.StatusOK {
		t.Fatalf("operator export: status=%d, want 200", status)
	}
	if !exportContainsStudent(t, body, "Siswa Own Tenant") {
		t.Errorf("own-tenant submission missing from operator export")
	}
	if exportContainsStudent(t, body, "Siswa Beta Tenant") {
		t.Errorf("cross-tenant submission leaked into operator export (M6)")
	}
}

// TestSubmissionsExportOperatorWipedInstansi pins the M6 fix: an operator
// whose instansi has been cleared ("" — the shared bucket, not a real tenant)
// must NOT fall through to an unscoped export of every tenant. The intended
// fallback is own-created exams only.
func TestSubmissionsExportOperatorWipedInstansi(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createExportScopeFixture(t, pool)

	r := newSubmissionsExportRouter(pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	oc := newExportScopeClient(t, srv.URL)
	oc.login(fix.OperatorID)

	// Wipe the instansi AFTER login: the session still carries "SMA Alpha"
	// but AuthRequired re-validates against the row on every request and
	// self-heals the session to "" — exactly the production sequence when an
	// admin clears an operator's instansi.
	if _, err := pool.Exec(context.Background(),
		`UPDATE admin_users SET instansi = '' WHERE id = $1`, fix.OperatorID); err != nil {
		t.Fatalf("wipe operator instansi: %v", err)
	}

	status, body := oc.exportAll()
	if status != http.StatusOK {
		t.Fatalf("operator export after wipe: status=%d, want 200 (own-created fallback, not 500)", status)
	}
	if !exportContainsStudent(t, body, "Siswa Own Tenant") {
		t.Errorf("own-created submission missing from wiped-instansi export (fallback must keep own data)")
	}
	if exportContainsStudent(t, body, "Siswa Beta Tenant") {
		t.Errorf("cross-tenant submission leaked after instansi wipe (M6: empty instansi must not widen the export to every tenant)")
	}
}

// TestSubmissionsExportDeletedOperator pins the session guard: once the
// operator's row is gone, AuthRequired's per-request re-validation must end
// the session with 401 — no export may be served to a deleted account.
func TestSubmissionsExportDeletedOperator(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createExportScopeFixture(t, pool)

	r := newSubmissionsExportRouter(pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	oc := newExportScopeClient(t, srv.URL)
	oc.login(fix.OperatorID)

	ctx := context.Background()
	// exams.created_by has no ON DELETE clause (plain REFERENCES), so a user
	// who still owns exams cannot be deleted — detach the operator's exams
	// first (the same effect DeleteUser relies on for sub-accounts).
	if _, err := pool.Exec(ctx,
		`UPDATE exams SET created_by = NULL WHERE created_by = $1`, fix.OperatorID); err != nil {
		t.Fatalf("detach operator exams: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM admin_users WHERE id = $1`, fix.OperatorID); err != nil {
		t.Fatalf("delete operator: %v", err)
	}

	status, _ := oc.exportAll()
	if status != http.StatusUnauthorized {
		t.Errorf("deleted operator export: status=%d, want 401 (session must die with the row)", status)
	}
}
