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
	adminAPI.POST("/pengawas/exams/:exam_id/approvals/:mac_address", SetApprovalStatus())

	// The public request-approval endpoint, so a test can verify that a
	// revoke done through the admin endpoint survives the device's next poll.
	r.POST("/api/exams/request-approval", api.RequestApproval())
	return r
}

// autoApproveFixture holds the users/exam for the authorization matrix:
// GuruID owns the exam, PwID is an assigned pengawas (may access), OtherID is
// a pengawas from another instansi with no assignment (must 403).
type autoApproveFixture struct {
	ExamID  int
	GuruID  int
	PwID    int
	OtherID int
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

	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Ujian AA', 'aa.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, fmt.Sprintf("AA%06d", time.Now().UnixNano()%1000000), guruID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2)`, examID, pwID); err != nil {
		t.Fatalf("assign pengawas: %v", err)
	}

	return autoApproveFixture{ExamID: examID, GuruID: guruID, PwID: pwID, OtherID: otherID}
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
		"identity_data": map[string]interface{}{}, "reset": false,
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
