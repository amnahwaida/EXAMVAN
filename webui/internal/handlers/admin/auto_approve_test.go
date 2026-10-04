package admin

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/examvan/webui/internal/database"
	api "github.com/examvan/webui/internal/handlers/api"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Integration tests: GET/POST /admin/api/pengawas/exams/:exam_id/auto-approve
// ---------------------------------------------------------------------------
//
// The auto-approve flag is stored on the exam row (server-side), so it keeps
// working when no pengawas monitoring page is open. These tests lock in the
// get/set endpoints and their exam-scoped authorization.

// newAutoApproveTestRouter mirrors production wiring: AuthRequired, the
// /test/login/:id session seam, and the two auto-approve endpoints.
func newAutoApproveTestRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("db", pool) })

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

	// NOTE: the group is named adminAPI, NOT api — the package import for the
	// public request-approval handler below is aliased `api`, and a local
	// variable named api would shadow it.
	adminAPI := r.Group("/admin/api", middleware.AuthRequired())
	adminAPI.GET("/pengawas/exams", PengawasExams())
	adminAPI.GET("/pengawas/exams/:exam_id/auto-approve", GetAutoApprove())
	adminAPI.POST("/pengawas/exams/:exam_id/auto-approve", SetAutoApprove())
	adminAPI.GET("/pengawas/exams/:exam_id/approvals", GetPendingApprovals())
	adminAPI.POST("/pengawas/exams/:exam_id/approvals/:mac_address", SetApprovalStatus())
	adminAPI.GET("/pengawas/exams/:exam_id/audit-logs", GetExamAuditLogs())

	// The public request-approval endpoint, so a test can verify that a
	// revoke done through the admin endpoint survives the device's next poll.
	r.POST("/api/exams/request-approval", api.RequestApproval())
	return r
}

// autoApproveFixture holds the users/exam for the authorization matrix:
// GuruID owns the exam, PwID is an assigned pengawas (may access), OtherID is
// a pengawas from another instansi with no assignment (must 403). Token is the
// exam's token, needed by the public request-approval endpoint (anti-spam).
type autoApproveFixture struct {
	ExamID  int
	GuruID  int
	PwID    int
	OtherID int
	Token   string
}

func createAutoApproveFixture(t *testing.T, pool *pgxpool.Pool) autoApproveFixture {
	t.Helper()
	ctx := context.Background()

	mk := func(username, name, instansi string, roles ...string) int {
		t.Helper()
		u, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: username, Name: name, Instansi: instansi,
			PasswordHash: "pass", Status: models.UserStatusActive,
			Role: models.SerializeRoles(roles),
		})
		if err != nil {
			t.Fatalf("create user %s: %v", username, err)
		}
		return u.ID
	}

	guruID := mk("aa-guru", "Guru AA", "SMA Alpha", models.RoleGuru)
	pwID := mk("aa-pengawas", "Pengawas AA", "SMA Alpha", models.RolePengawas)
	otherID := mk("aa-other", "Pengawas Lain", "SMK Beta", models.RolePengawas)

	token := fmt.Sprintf("AA%06d", time.Now().UnixNano()%1000000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Ujian AA', 'aa.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, token, guruID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2)`, examID, pwID); err != nil {
		t.Fatalf("assign pengawas: %v", err)
	}

	return autoApproveFixture{ExamID: examID, GuruID: guruID, PwID: pwID, OtherID: otherID, Token: token}
}

// autoApproveClient keeps the session cookie across login and API calls.
type autoApproveClient struct {
	t    *testing.T
	base string
	hc   *http.Client
}

func newAutoApproveClient(t *testing.T, srv *httptest.Server) *autoApproveClient {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &autoApproveClient{t: t, base: srv.URL, hc: &http.Client{Jar: jar}}
}

func (c *autoApproveClient) login(userID int) {
	c.t.Helper()
	resp, err := c.hc.Post(c.base+fmt.Sprintf("/test/login/%d", userID), "application/json", nil)
	if err != nil {
		c.t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
}

func (c *autoApproveClient) do(method, path string, body interface{}) (int, map[string]interface{}) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		c.t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, out
}

func examAutoApprove(t *testing.T, pool *pgxpool.Pool, examID int) bool {
	t.Helper()
	var enabled bool
	if err := pool.QueryRow(context.Background(),
		`SELECT auto_approve FROM exams WHERE id = $1`, examID).Scan(&enabled); err != nil {
		t.Fatalf("read auto_approve: %v", err)
	}
	return enabled
}

// An assigned pengawas may read and toggle the flag; the change must land in
// the DB (the whole point: it works with the page closed) and be readable back.
func TestAutoApproveGetSetByAssignedPengawas(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	path := fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID)

	code, out := client.do(http.MethodGet, path, nil)
	if code != http.StatusOK || out["enabled"] != false {
		t.Fatalf("GET status=%d out=%v, want 200 + enabled=false initially", code, out)
	}

	code, out = client.do(http.MethodPost, path, map[string]interface{}{"enabled": true})
	if code != http.StatusOK || out["enabled"] != true {
		t.Fatalf("POST status=%d out=%v, want 200 + enabled=true", code, out)
	}
	if !examAutoApprove(t, pool, fx.ExamID) {
		t.Error("auto_approve not persisted in DB after toggle")
	}

	code, out = client.do(http.MethodGet, path, nil)
	if code != http.StatusOK || out["enabled"] != true {
		t.Fatalf("GET after toggle status=%d out=%v, want 200 + enabled=true", code, out)
	}

	// Toggle back off.
	code, out = client.do(http.MethodPost, path, map[string]interface{}{"enabled": false})
	if code != http.StatusOK || out["enabled"] != false {
		t.Fatalf("POST off status=%d out=%v, want 200 + enabled=false", code, out)
	}
	if examAutoApprove(t, pool, fx.ExamID) {
		t.Error("auto_approve still on in DB after toggling off")
	}
}

// The owner (creator) may also toggle the flag.
func TestAutoApproveOwnerCanToggle(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.GuruID)

	path := fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID)
	code, out := client.do(http.MethodPost, path, map[string]interface{}{"enabled": true})
	if code != http.StatusOK || out["enabled"] != true {
		t.Fatalf("POST status=%d out=%v, want 200 + enabled=true", code, out)
	}
}

// A pengawas with no assignment to this exam (and from another instansi) must
// be denied on both read and write — same exam-scoped auth as the approvals.
func TestAutoApproveUnauthorizedPengawasDenied(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.OtherID)

	path := fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID)
	if code, _ := client.do(http.MethodGet, path, nil); code != http.StatusForbidden {
		t.Fatalf("GET status=%d, want 403 for unassigned pengawas", code)
	}
	if code, _ := client.do(http.MethodPost, path, map[string]interface{}{"enabled": true}); code != http.StatusForbidden {
		t.Fatalf("POST status=%d, want 403 for unassigned pengawas", code)
	}
	if examAutoApprove(t, pool, fx.ExamID) {
		t.Error("auto_approve changed to true despite 403 — handler must not run")
	}
}

// A pengawas may revoke an already-approved device via the approvals endpoint
// (the "cabut izin" path for devices auto-approve let in), and the rejection
// survives the device's next approval poll — it is not silently resurrected.
func TestAutoApproveRevokeApprovedDevicePersistsOnPoll(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)

	mac := "AA:BB:CC:DD:EE:0A"
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Siswa Revoke', '01', 'XII A', 'approved')`,
		fx.ExamID, mac); err != nil {
		t.Fatalf("insert approved device: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	// Revoke via the admin endpoint.
	code, out := client.do(http.MethodPost,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals/%s", fx.ExamID, mac),
		map[string]interface{}{"status": "rejected"})
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("revoke status=%d out=%v, want 200 success", code, out)
	}

	// The device's next poll (reset=false) must NOT resurrect the approval.
	code, out = client.do(http.MethodPost, "/api/exams/request-approval", map[string]interface{}{
		"exam_id": fx.ExamID, "mac_address": mac,
		"student_name": "Siswa Revoke", "exam_number": "01", "student_class": "XII A",
		"identity_data": map[string]interface{}{}, "reset": false, "token": fx.Token,
	})
	if code != http.StatusOK || out["status"] != "rejected" {
		t.Fatalf("poll status=%d out=%v, want rejected preserved after revoke", code, out)
	}
}

// The pengawas exam list API must surface the exam's auto_approve flag so the
// pengawas page can badge exams whose server-side auto-approve is still on
// (visible before the exam is reused for the next session).
func TestPengawasExamsListSurfacesAutoApprove(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	if err := models.SetExamAutoApprove(context.Background(), pool, fx.ExamID, true); err != nil {
		t.Fatalf("enable auto-approve: %v", err)
	}

	// A second exam (flag OFF, also assigned to the pengawas) pins the
	// negative side of the JSON contract — the list must surface false, not
	// omit or default the field to true.
	var offID int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, created_by)
		VALUES ('Ujian AA Off', 'aa-off.pdf', 512, $1, $1, 'active', $2)
		RETURNING id`, fmt.Sprintf("AAOFF%03d", time.Now().UnixNano()%1000), fx.GuruID).Scan(&offID); err != nil {
		t.Fatalf("insert off exam: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2)`, offID, fx.PwID); err != nil {
		t.Fatalf("assign pengawas to off exam: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID) // assigned pengawas — both exams appear in their list

	code, out := client.do(http.MethodGet, "/admin/api/pengawas/exams?per_page=50", nil)
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("list status=%d out=%v, want 200 success", code, out)
	}
	exams, ok := out["exams"].([]interface{})
	if !ok {
		t.Fatalf("exams is not an array: %T", out["exams"])
	}
	onFound, offFound := false, false
	for _, raw := range exams {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id, ok := m["id"].(float64)
		if !ok {
			continue
		}
		switch int(id) {
		case fx.ExamID:
			onFound = true
			if m["auto_approve"] != true {
				t.Errorf("exam %d auto_approve = %v, want true", fx.ExamID, m["auto_approve"])
			}
		case offID:
			offFound = true
			if m["auto_approve"] != false {
				t.Errorf("exam %d auto_approve = %v, want false", offID, m["auto_approve"])
			}
		}
	}
	if !onFound {
		t.Errorf("exam %d not found in pengawas list", fx.ExamID)
	}
	if !offFound {
		t.Errorf("exam %d not found in pengawas list", offID)
	}
}

// The pending-approvals list is paginated so a spam-flooded queue cannot dump
// unbounded rows onto the monitoring page.
func TestGetPendingApprovalsPaginated(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)

	for i := 0; i < 5; i++ {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
			VALUES ($1, $2, 'Siswa Pending', $3, 'XII A', 'pending')`,
			fx.ExamID, fmt.Sprintf("EE:00:00:00:00:0%d", i), fmt.Sprintf("%02d", i)); err != nil {
			t.Fatalf("insert pending %d: %v", i, err)
		}
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	path := fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals?limit=2&page=1", fx.ExamID)
	code, out := client.do(http.MethodGet, path, nil)
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("page 1 status=%d out=%v, want 200 success", code, out)
	}
	data, _ := out["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("page 1 returned %d rows, want 2", len(data))
	}
	if out["total"] != float64(5) {
		t.Fatalf("total = %v, want 5", out["total"])
	}
	if out["limit"] != float64(2) || out["page"] != float64(1) {
		t.Fatalf("pagination meta = page %v limit %v, want 1/2", out["page"], out["limit"])
	}

	// Last page returns the remainder.
	path = fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals?limit=2&page=3", fx.ExamID)
	code, out = client.do(http.MethodGet, path, nil)
	if code != http.StatusOK {
		t.Fatalf("page 3 status=%d, want 200", code)
	}
	data, _ = out["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("page 3 returned %d rows, want 1 (remainder)", len(data))
	}
}

// ---------------------------------------------------------------------------
// Audit trail: who toggled auto-approve, and when
// ---------------------------------------------------------------------------

// Toggling the flag must leave an append-only audit trail — one row per
// change, in order, attributed to the acting user (username snapshot).
func TestAutoApproveToggleWritesAuditLog(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID) // username "aa-pengawas"

	path := fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID)

	// Enable, then disable — two separate actions, both attributable.
	if code, out := client.do(http.MethodPost, path, map[string]interface{}{"enabled": true}); code != http.StatusOK || out["enabled"] != true {
		t.Fatalf("POST enable status=%d out=%v, want 200 + enabled=true", code, out)
	}
	if code, out := client.do(http.MethodPost, path, map[string]interface{}{"enabled": false}); code != http.StatusOK || out["enabled"] != false {
		t.Fatalf("POST disable status=%d out=%v, want 200 + enabled=false", code, out)
	}

	rows, err := pool.Query(context.Background(),
		`SELECT username, action, exam_id, detail FROM admin_audit_logs WHERE exam_id = $1 ORDER BY id`, fx.ExamID)
	if err != nil {
		t.Fatalf("query audit logs: %v", err)
	}
	defer rows.Close()

	type auditRow struct {
		username string
		action   string
		examID   int
		detail   string
	}
	var got []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.username, &r.action, &r.examID, &r.detail); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		got = append(got, r)
	}

	if len(got) != 2 {
		t.Fatalf("audit rows = %d, want 2 (enable + disable): %+v", len(got), got)
	}
	if got[0].username != "aa-pengawas" || got[0].action != models.ActionAutoApproveEnable {
		t.Errorf("first audit row = %+v, want username aa-pengawas + action %s", got[0], models.ActionAutoApproveEnable)
	}
	if got[1].username != "aa-pengawas" || got[1].action != models.ActionAutoApproveDisable {
		t.Errorf("second audit row = %+v, want username aa-pengawas + action %s", got[1], models.ActionAutoApproveDisable)
	}
	for _, r := range got {
		if r.examID != fx.ExamID {
			t.Errorf("audit row exam_id = %d, want %d", r.examID, fx.ExamID)
		}
		if r.detail == "" {
			t.Errorf("audit row %s has empty detail — UI hint relies on the snapshot", r.action)
		}
	}
}

// The GET endpoint must surface the audit hint (who/when/action) so the
// monitoring page can show "Diaktifkan oleh <user> pada <time>" next to the
// toggle — and omit the fields when the exam has no trail yet.
func TestAutoApproveGetReturnsLastChanged(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	path := fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID)

	// No trail yet → no last_changed fields.
	code, out := client.do(http.MethodGet, path, nil)
	if code != http.StatusOK {
		t.Fatalf("GET status=%d, want 200", code)
	}
	if _, ok := out["last_changed_by"]; ok {
		t.Errorf("GET before any toggle returned last_changed_by=%v, want omitted", out["last_changed_by"])
	}

	// Enable → the hint points at the acting pengawas with the right action.
	if code, _ := client.do(http.MethodPost, path, map[string]interface{}{"enabled": true}); code != http.StatusOK {
		t.Fatalf("POST enable status=%d, want 200", code)
	}
	code, out = client.do(http.MethodGet, path, nil)
	if code != http.StatusOK {
		t.Fatalf("GET status=%d, want 200", code)
	}
	if got, want := out["last_changed_by"], "aa-pengawas"; got != want {
		t.Errorf("last_changed_by = %v, want %s", got, want)
	}
	if got, want := out["last_action"], models.ActionAutoApproveEnable; got != want {
		t.Errorf("last_action = %v, want %s", got, want)
	}
	if at, ok := out["last_changed_at"].(string); !ok || at == "" {
		t.Errorf("last_changed_at = %v, want non-empty RFC3339 string", out["last_changed_at"])
	} else if _, err := time.Parse(time.RFC3339, at); err != nil {
		t.Errorf("last_changed_at %v is not RFC3339: %v", at, err)
	}
}

// A 403 must leave NO audit trail: denied callers must not be able to forge
// an "enabled" entry, and their denied attempt is not an admin action.
func TestAutoApproveUnauthorizedWritesNoAuditLog(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.OtherID) // unassigned pengawas from another instansi

	path := fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID)
	if code, _ := client.do(http.MethodPost, path, map[string]interface{}{"enabled": true}); code != http.StatusForbidden {
		t.Fatalf("POST status=%d, want 403 for unassigned pengawas", code)
	}

	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_audit_logs`).Scan(&n); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if n != 0 {
		t.Errorf("audit rows = %d after denied toggle, want 0 — 403 must not write the trail", n)
	}
}

// Unknown exams 404 instead of toggling something the caller cannot see.
func TestAutoApproveUnknownExam404(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	path := "/admin/api/pengawas/exams/999999/auto-approve"
	if code, _ := client.do(http.MethodGet, path, nil); code != http.StatusNotFound {
		t.Fatalf("GET status=%d, want 404 for unknown exam", code)
	}
	if code, _ := client.do(http.MethodPost, path, map[string]interface{}{"enabled": true}); code != http.StatusNotFound {
		t.Fatalf("POST status=%d, want 404 for unknown exam", code)
	}
}

// ---------------------------------------------------------------------------
// Audit history panel: full admin_audit_logs trail per exam
// ---------------------------------------------------------------------------

// The audit-logs endpoint returns the complete history (not just the last
// toggle), newest first, each row attributed to its actor — the payload the
// monitoring page's "Riwayat Audit" panel renders.
func TestExamAuditLogsFullHistory(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	path := fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID)

	// Two different actors, so the trail must preserve per-row attribution.
	pw := newAutoApproveClient(t, srv)
	pw.login(fx.PwID)
	if code, out := pw.do(http.MethodPost, path, map[string]interface{}{"enabled": true}); code != http.StatusOK || out["enabled"] != true {
		t.Fatalf("enable by pengawas status=%d out=%v, want 200", code, out)
	}

	guru := newAutoApproveClient(t, srv)
	guru.login(fx.GuruID)
	if code, out := guru.do(http.MethodPost, path, map[string]interface{}{"enabled": false}); code != http.StatusOK || out["enabled"] != false {
		t.Fatalf("disable by guru status=%d out=%v, want 200", code, out)
	}

	// Both rows must surface, newest first (the guru's disable).
	logsPath := fmt.Sprintf("/admin/api/pengawas/exams/%d/audit-logs", fx.ExamID)
	code, out := guru.do(http.MethodGet, logsPath, nil)
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("GET audit-logs status=%d out=%v, want 200 success", code, out)
	}
	logs, ok := out["logs"].([]interface{})
	if !ok {
		t.Fatalf("logs is not an array: %T", out["logs"])
	}
	if len(logs) != 2 {
		t.Fatalf("audit logs = %d, want 2 (enable + disable)", len(logs))
	}

	first := logs[0].(map[string]interface{})
	if first["username"] != "aa-guru" || first["action"] != models.ActionAutoApproveDisable {
		t.Errorf("newest log = %v, want aa-guru + %s", first, models.ActionAutoApproveDisable)
	}
	second := logs[1].(map[string]interface{})
	if second["username"] != "aa-pengawas" || second["action"] != models.ActionAutoApproveEnable {
		t.Errorf("older log = %v, want aa-pengawas + %s", second, models.ActionAutoApproveEnable)
	}
	for i, raw := range logs {
		m := raw.(map[string]interface{})
		if at, ok := m["created_at"].(string); !ok || at == "" {
			t.Errorf("log %d created_at = %v, want non-empty RFC3339", i, m["created_at"])
		} else if _, err := time.Parse(time.RFC3339, at); err != nil {
			t.Errorf("log %d created_at %v is not RFC3339: %v", i, at, err)
		}
		if d, _ := m["detail"].(string); d == "" {
			t.Errorf("log %d has empty detail snapshot — UI panel relies on it", i)
		}
	}
}

// An exam with no trail yet returns an empty list (not an error), so the
// panel renders the empty state instead of a broken modal.
func TestExamAuditLogsEmptyTrail(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodGet, fmt.Sprintf("/admin/api/pengawas/exams/%d/audit-logs", fx.ExamID), nil)
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("GET status=%d out=%v, want 200 success", code, out)
	}
	logs, ok := out["logs"].([]interface{})
	if !ok || len(logs) != 0 {
		t.Fatalf("logs = %v, want empty array", out["logs"])
	}
}

// An unassigned pengawas must not read another exam's audit trail (403) —
// the trail is exam-scoped like the approvals themselves.
func TestExamAuditLogsUnauthorized(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.OtherID) // unassigned pengawas from another instansi

	code, _ := client.do(http.MethodGet, fmt.Sprintf("/admin/api/pengawas/exams/%d/audit-logs", fx.ExamID), nil)
	if code != http.StatusForbidden {
		t.Fatalf("GET status=%d, want 403 for unassigned pengawas", code)
	}
}

// ---------------------------------------------------------------------------
// Audit trail for per-device approval decisions (SetApprovalStatus)
// ---------------------------------------------------------------------------

// Approving then rejecting a device must leave two attributable audit rows
// (who decided, which device, when) — the same append-only trail the
// auto-approve toggle uses, so the "Riwayat Audit" panel shows every action.
func TestApprovalDecisionWritesAuditLog(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	mac := "AA:BB:CC:DD:EE:0F"
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Siswa Audit Dec', '09', 'XII A', 'pending')`,
		fx.ExamID, mac); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	path := fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals/%s", fx.ExamID, mac)
	if code, out := client.do(http.MethodPost, path, map[string]interface{}{"status": "approved"}); code != http.StatusOK || out["success"] != true {
		t.Fatalf("approve status=%d out=%v, want 200 success", code, out)
	}
	if code, out := client.do(http.MethodPost, path, map[string]interface{}{"status": "rejected"}); code != http.StatusOK || out["success"] != true {
		t.Fatalf("reject status=%d out=%v, want 200 success", code, out)
	}

	// The audit-logs panel must surface both decisions, newest first.
	code, out := client.do(http.MethodGet, fmt.Sprintf("/admin/api/pengawas/exams/%d/audit-logs", fx.ExamID), nil)
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("GET audit-logs status=%d out=%v, want 200 success", code, out)
	}
	logs, _ := out["logs"].([]interface{})
	if len(logs) != 2 {
		t.Fatalf("audit logs = %d, want 2 (approve + reject)", len(logs))
	}

	first := logs[0].(map[string]interface{}) // newest = the rejection
	if first["username"] != "aa-pengawas" || first["action"] != models.ActionApprovalRejected {
		t.Errorf("newest log = %v, want aa-pengawas + %s", first, models.ActionApprovalRejected)
	}
	second := logs[1].(map[string]interface{})
	if second["username"] != "aa-pengawas" || second["action"] != models.ActionApprovalApproved {
		t.Errorf("older log = %v, want aa-pengawas + %s", second, models.ActionApprovalApproved)
	}
	// detail must carry the device identity (MAC + student snapshot).
	for i, raw := range logs {
		m := raw.(map[string]interface{})
		d, _ := m["detail"].(string)
		if d == "" || !strings.Contains(d, mac) || !strings.Contains(d, "Siswa Audit Dec") {
			t.Errorf("log %d detail = %q, want MAC %s + student snapshot", i, d, mac)
		}
	}
}

// A decision on an unknown device must NOT write an audit row: the UPDATE
// matched nothing, so there is no real action to attribute (and a forged row
// would corrupt the trail's trustworthiness).
func TestApprovalDecisionUnknownDeviceWritesNoAuditLog(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodPost,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals/FF:FF:FF:FF:FF:FF", fx.ExamID),
		map[string]interface{}{"status": "approved"})
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("status=%d out=%v, want 200 (historical contract keeps 200)", code, out)
	}

	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_audit_logs`).Scan(&n); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if n != 0 {
		t.Errorf("audit rows = %d after no-op decision, want 0", n)
	}
}

// Pins PostgreSQL's matched-row semantics: re-deciding an already-decided
// device (e.g. approving a device that is already approved) still matches a
// row, so an audit row IS written — an explicit decision is an action. This
// guards against a future DB/trigger change silently dropping the trail.
func TestApprovalDecisionRepeatOnKnownDeviceStillAudited(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	mac := "AA:BB:CC:DD:EE:11"
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Siswa Repeat', '11', 'XII A', 'approved')`,
		fx.ExamID, mac); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodPost,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals/%s", fx.ExamID, mac),
		map[string]interface{}{"status": "approved"})
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("re-approve status=%d out=%v, want 200 success", code, out)
	}

	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE exam_id = $1 AND action = $2`,
		fx.ExamID, models.ActionApprovalApproved).Scan(&n); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if n != 1 {
		t.Errorf("audit rows = %d for repeated approve, want 1 (matched-row semantics)", n)
	}
}

// A denied pengawas must not be able to leave approval-decision rows either:
// the 403 gate fires before any update or audit write.
func TestApprovalDecisionUnauthorizedWritesNoAuditLog(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	mac := "AA:BB:CC:DD:EE:10"
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, status)
		VALUES ($1, $2, 'Siswa Denied', 'pending')`, fx.ExamID, mac); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.OtherID) // unassigned pengawas from another instansi

	code, _ := client.do(http.MethodPost,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals/%s", fx.ExamID, mac),
		map[string]interface{}{"status": "approved"})
	if code != http.StatusForbidden {
		t.Fatalf("POST status=%d, want 403 for unassigned pengawas", code)
	}

	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_audit_logs`).Scan(&n); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if n != 0 {
		t.Errorf("audit rows = %d after denied decision, want 0", n)
	}
}

// A decision against a stopped (inactive) exam must be rejected outright:
// approving or revoking a device there would write an audit row (and possibly
// a monitoring row) that nobody can act on. The gate fires before any update
// or audit write, so the device row stays untouched and the trail stays clean.
func TestApprovalDecisionInactiveExamRejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	mac := "AA:BB:CC:DD:EE:12"
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Siswa Inactive', '12', 'XII A', 'pending')`, fx.ExamID, mac); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	// Stop the exam: its approval queue is dead, so decisions must fail.
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET status = 'inactive', exam_started_at = NULL WHERE id = $1`, fx.ExamID); err != nil {
		t.Fatalf("stop exam: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodPost,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals/%s", fx.ExamID, mac),
		map[string]interface{}{"status": "approved"})
	if code != http.StatusBadRequest {
		t.Fatalf("POST status=%d out=%v, want 400 for inactive exam", code, out)
	}

	// The device row must be untouched and no audit row written.
	var st string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
		fx.ExamID, mac).Scan(&st); err != nil {
		t.Fatalf("read approval row: %v", err)
	}
	if st != "pending" {
		t.Errorf("approval status = %q, want pending (untouched)", st)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_audit_logs`).Scan(&n); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if n != 0 {
		t.Errorf("audit rows = %d after rejected decision on inactive exam, want 0", n)
	}
}

// A decision against an exam whose schedule has ended is rejected like an
// inactive one: the public gates already refuse new joins/submits past
// end_time+grace, so a decision here would only write a trail nobody can act
// on. The 400 gate fires before any update or audit write.
func TestApprovalDecisionEndedExamRejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	mac := "AA:BB:CC:DD:EE:13"
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Siswa Ended', '13', 'XII A', 'pending')`, fx.ExamID, mac); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	// Push end_time into the past (beyond the grace window) while the exam
	// stays active — the schedule cutoff must block the decision.
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET end_time = $1 WHERE id = $2`,
		time.Now().UTC().Add(-2*time.Hour), fx.ExamID); err != nil {
		t.Fatalf("set end_time: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodPost,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals/%s", fx.ExamID, mac),
		map[string]interface{}{"status": "approved"})
	if code != http.StatusBadRequest {
		t.Fatalf("POST status=%d out=%v, want 400 for ended exam", code, out)
	}

	// The device row must be untouched and no audit row written.
	var st string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
		fx.ExamID, mac).Scan(&st); err != nil {
		t.Fatalf("read approval row: %v", err)
	}
	if st != "pending" {
		t.Errorf("approval status = %q, want pending (untouched)", st)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_audit_logs`).Scan(&n); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if n != 0 {
		t.Errorf("audit rows = %d after rejected decision on ended exam, want 0", n)
	}
}

// Toggling server-side auto-approve on a stopped exam is rejected: the flag
// does nothing there (RequestApproval refuses to apply it), so writing it —
// and an audit row — would just be dead weight. The 400 fires before any
// change, so the flag stays off and the trail stays clean.
func TestSetAutoApproveInactiveExamRejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET status = 'inactive', exam_started_at = NULL WHERE id = $1`, fx.ExamID); err != nil {
		t.Fatalf("stop exam: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodPost,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID),
		map[string]interface{}{"enabled": true})
	if code != http.StatusBadRequest {
		t.Fatalf("POST status=%d out=%v, want 400 for inactive exam", code, out)
	}
	if examAutoApprove(t, pool, fx.ExamID) {
		t.Error("auto_approve changed to true despite 400 — handler must not run")
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_audit_logs`).Scan(&n); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if n != 0 {
		t.Errorf("audit rows = %d after rejected toggle on inactive exam, want 0", n)
	}
}

// Same for an exam whose schedule has ended (end_time + grace): the toggle is
// rejected without touching the flag or writing an audit row.
func TestSetAutoApproveEndedExamRejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET end_time = $1 WHERE id = $2`,
		time.Now().UTC().Add(-2*time.Hour), fx.ExamID); err != nil {
		t.Fatalf("set end_time: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodPost,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID),
		map[string]interface{}{"enabled": true})
	if code != http.StatusBadRequest {
		t.Fatalf("POST status=%d out=%v, want 400 for ended exam", code, out)
	}
	if examAutoApprove(t, pool, fx.ExamID) {
		t.Error("auto_approve changed to true despite 400 — handler must not run")
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_audit_logs`).Scan(&n); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if n != 0 {
		t.Errorf("audit rows = %d after rejected toggle on ended exam, want 0", n)
	}
}

// The pending-queue read is gated the same way: an inactive exam has no live
// queue (RequestApproval no longer adds rows there), so serving a stale list
// would just mislead the pengawas.
func TestGetPendingApprovalsInactiveExamRejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET status = 'inactive', exam_started_at = NULL WHERE id = $1`, fx.ExamID); err != nil {
		t.Fatalf("stop exam: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodGet,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals", fx.ExamID), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("GET status=%d out=%v, want 400 for inactive exam", code, out)
	}
}

// And for an exam whose schedule has ended.
func TestGetPendingApprovalsEndedExamRejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET end_time = $1 WHERE id = $2`,
		time.Now().UTC().Add(-2*time.Hour), fx.ExamID); err != nil {
		t.Fatalf("set end_time: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodGet,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/approvals", fx.ExamID), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("GET status=%d out=%v, want 400 for ended exam", code, out)
	}
}

// Reads stay available for review: the auto-approve flag (and its audit hint)
// can still be fetched after the exam stops — only writes/decisions are gated.
func TestGetAutoApproveReadableOnInactiveExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET status = 'inactive', exam_started_at = NULL WHERE id = $1`, fx.ExamID); err != nil {
		t.Fatalf("stop exam: %v", err)
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	code, out := client.do(http.MethodGet,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/auto-approve", fx.ExamID), nil)
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("GET status=%d out=%v, want 200 — flag read stays available on inactive exam", code, out)
	}
}

// ---------------------------------------------------------------------------
// L8 (review_web_flow_dan_dead_code.md 4.1): exam_id yang tidak valid harus
// 400 "ID ujian tidak valid", bukan 404 "Ujian tidak ditemukan" yang
// menyesatkan. Sebelumnya error strconv.Atoi ditelan (examID, _ :=) → 0 →
// GetExamByID(0) gagal → 404 menipu pengawas seolah ujiannya hilang.
// ---------------------------------------------------------------------------

func TestApprovalEndpointsInvalidExamIDReturns400(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID)

	for _, tc := range []struct {
		name string
		call func() (int, map[string]interface{})
	}{
		{
			name: "GET pending approvals",
			call: func() (int, map[string]interface{}) {
				return client.do(http.MethodGet, "/admin/api/pengawas/exams/not-a-number/approvals", nil)
			},
		},
		{
			name: "POST approval status",
			call: func() (int, map[string]interface{}) {
				return client.do(http.MethodPost, "/admin/api/pengawas/exams/not-a-number/approvals/EE:00:00:00:00:01",
					map[string]string{"status": "approved"})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := tc.call()
			if code != http.StatusBadRequest {
				t.Fatalf("status=%d out=%v, want 400 for non-numeric exam_id", code, out)
			}
			if out["message"] != "ID ujian tidak valid" {
				t.Fatalf("message=%v, want \"ID ujian tidak valid\"", out["message"])
			}
		})
	}
}

// TestPengawasExamsStatsCoverEveryAccessibleExam pins the header cards'
// contract on the pengawas LIST page: "Ujian Diawasi" / "Sedang Berlangsung"
// / "Total Siswa" / "Terkumpul" are labelled tenant-wide totals, so they must
// aggregate over EVERY accessible exam — not just the current page slice.
//
// RED before the fix: the handler summed result.Exams (the page), so a
// pengawas with 47 exams always saw "10" in the header while the footer said
// "dari 47 ujian", and page 2 showed a different set of numbers.
func TestPengawasExamsStatsCoverEveryAccessibleExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano()%1000000)

	// Seed submissions on the fixture exam so Total Siswa / Terkumpul are > 0.
	startedAt := time.Now().Format(time.RFC3339)
	for i, mac := range []string{"AA:00:00:00:00:01", "AA:00:00:00:00:02", "AA:00:00:00:00:03"} {
		answers := "NULL"
		if i < 2 {
			answers = `'{"1":"A"}'`
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, answers_json, score, start_time, created_at)
			VALUES ($1, $2, $3, '01', 'XII A', `+answers+`, 80, $4, $5)`,
			fx.ExamID, mac, fmt.Sprintf("Siswa %d", i+1), startedAt, time.Now()); err != nil {
			t.Fatalf("seed submission %d: %v", i, err)
		}
	}

	// Two MORE exams for the same assigned pengawas, so the tenant total is 3
	// while a single page (per_page=5, so all fit) is not the differentiator —
	// use enough exams to force paging instead.
	for i := 0; i < 7; i++ {
		token := fmt.Sprintf("PL%s%02d", uniq, i)
		var id int
		if err := pool.QueryRow(ctx, `
			INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
			VALUES ($1, 'pl.pdf', 1024, $2, $2, 'active', 'medium', $3)
			RETURNING id`, fmt.Sprintf("Ujian Extra %d", i), token, fx.GuruID).Scan(&id); err != nil {
			t.Fatalf("seed extra exam %d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2)`, id, fx.PwID); err != nil {
			t.Fatalf("assign extra exam %d: %v", i, err)
		}
	}

	srv := httptest.NewServer(newAutoApproveTestRouter(pool))
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(fx.PwID), "", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("login pengawas: err=%v", err)
	} else {
		resp.Body.Close()
	}

	get := func(query string) map[string]interface{} {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/api/pengawas/exams"+query, nil)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", query, err)
		}
		defer resp.Body.Close()
		var out map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", query, err)
		}
		return out
	}

	first := get("?page=1&per_page=5")
	stats, _ := first["stats"].(map[string]interface{})
	totalExams, _ := stats["total_exams"].(float64)
	totalStudents, _ := stats["total_students"].(float64)
	totalSubmitted, _ := stats["total_submitted"].(float64)
	listTotal, _ := first["total"].(float64)

	if totalExams != listTotal {
		t.Errorf("stats.total_exams = %v, want the tenant total %v — the header card "+
			"must aggregate every accessible exam, not the current page", totalExams, listTotal)
	}
	if totalStudents != 3 {
		t.Errorf("stats.total_students = %v, want 3 (all devices across the tenant)", totalStudents)
	}
	if totalSubmitted != 2 {
		t.Errorf("stats.total_submitted = %v, want 2", totalSubmitted)
	}

	// Page 2 must report the SAME header numbers (stable overview), only the
	// rows differ.
	second := get("?page=2&per_page=5")
	stats2, _ := second["stats"].(map[string]interface{})
	for _, k := range []string{"total_exams", "total_students", "total_submitted"} {
		if stats2[k] != stats[k] {
			t.Errorf("page 2 stats.%s = %v, want %v (header must be page-independent)", k, stats2[k], stats[k])
		}
	}
}
