package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Access-scoping tests for GET /admin/api/submissions (ListSubmissions)
// ---------------------------------------------------------------------------
//
// H2: any authenticated operator used to pass the "verify access" block in
// ListSubmissions unconditionally — `!isSuper && !isOp` skipped the ownership
// query entirely, so an operator from instansi B could enumerate the
// submissions of an exam owned by a guru from instansi A by simply guessing
// exam_id. These tests pin the intended contract:
//
//	guru (exam owner)      → 200
//	pengawas (assigned)    → 200
//	operator, same instansi as the exam owner → 200
//	operator, other instansi / personal        → 403
//
// The router mirrors production wiring: sessions → AuthRequired →
// FeatureLockRequired → GET /submissions (no CSRF, no role gate — the
// in-handler ownership check is the only guard, which is exactly what these
// tests exercise).

// newSubmissionsScopeRouter mirrors the production route stack for
// GET /admin/api/submissions, including the FeatureLockRequired layer the real
// server nests under the admin group.
func newSubmissionsScopeRouter(pool *pgxpool.Pool) *gin.Engine {
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
	lockedAPI.GET("/submissions", ListSubmissions())
	return r
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

// list returns the HTTP status of GET /admin/api/submissions?exam_id=<id>.
func (sc *scopeClient) list(examID int) int {
	sc.t.Helper()
	req, err := http.NewRequest(http.MethodGet,
		sc.base+"/admin/api/submissions?"+url.Values{"exam_id": {strconv.Itoa(examID)}}.Encode(), nil)
	if err != nil {
		sc.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := sc.client.Do(req)
	if err != nil {
		sc.t.Fatalf("GET submissions: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestListSubmissionsOperatorBypass pins the H2 contract on the submissions
// list endpoint: operators are scoped by instansi (same-instansi operator may
// read; cross-instansi/personal operator must get 403), while the legacy
// owner/pengawas paths keep working.
func TestListSubmissionsOperatorBypass(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createScopeFixture(t, pool)

	r := newSubmissionsScopeRouter(pool)
	srv := httptest.NewServer(r)
	defer srv.Close()
	base := srv.URL

	// Guru (owner): allowed.
	gc := newScopeClient(t, base)
	gc.login(fix.GuruID)
	if status := gc.list(fix.ExamID); status != http.StatusOK {
		t.Errorf("guru (owner) list: status=%d, want 200", status)
	}

	// Pengawas assigned to the exam: allowed (existing behavior —
	// UserCanAccessExam is a strict superset of the old inline check).
	pc := newScopeClient(t, base)
	pc.login(fix.PengawasID)
	if status := pc.list(fix.ExamID); status != http.StatusOK {
		t.Errorf("pengawas (assigned) list: status=%d, want 200", status)
	}

	// Operator from the SAME instansi as the exam owner: allowed.
	oc := newScopeClient(t, base)
	oc.login(fix.OperatorSameID)
	if status := oc.list(fix.ExamID); status != http.StatusOK {
		t.Errorf("operator same-instansi list: status=%d, want 200", status)
	}

	// Operator from a DIFFERENT instansi: 403 (currently 200 + data = the H2
	// bypass).
	xo := newScopeClient(t, base)
	xo.login(fix.OperatorOtherID)
	if status := xo.list(fix.ExamID); status != http.StatusForbidden {
		t.Errorf("operator cross-instansi list: status=%d, want 403 (H2 bypass)", status)
	}
}
