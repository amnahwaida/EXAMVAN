package admin

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
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
// Admin ExamPDF R2 guard tests
// ---------------------------------------------------------------------------
//
// ExamPDF serves the exam PDF via a Cloudflare R2 signed URL. Only an ENABLED
// backend may sign: a disabled backend or a malformed (nil-interface) "r2"
// key must be rejected with 500 before any SignedURL call — never a silent
// non-signing pass, and never a panic.

// newExamPDFTestRouter wires the admin ExamPDF endpoint with the production
// auth middleware and a pluggable "r2" key (enabled stub / disabled stub /
// nil interface), mirroring the upload/delete guard test routers.
func newExamPDFTestRouter(pool *pgxpool.Pool, setR2 func(*gin.Context)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{StoragePath: ""})
		setR2(c)
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

	api := r.Group("/admin/api", middleware.AuthRequired())
	api.GET("/exams/:exam_id/pdf", ExamPDF())
	return r
}

// createExamPDFFixture creates a super admin (bypasses ownership checks) plus
// one active exam row, and returns the user ID and exam ID.
func createExamPDFFixture(t *testing.T, pool *pgxpool.Pool, username string) (int, int) {
	t.Helper()
	userID := createSystemAppCleanupSuper(t, pool, username)

	var examID int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by, auto_approve)
		 VALUES ('Ujian PDF Guard', 'ujian-pdf-guard.pdf', 2048, 'PDFGUARD', 'PDFGUARD', 'active', 'medium', $1, false)
		 RETURNING id`, userID).Scan(&examID)
	if err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return userID, examID
}

// getAdminPDF performs the admin login + GET /admin/api/exams/:id/pdf dance over
// a shared cookie jar (like the upload tests) and returns the response. Redirects
// are not followed so the 302 Location stays observable.
func getAdminPDF(t *testing.T, router *gin.Engine, userID, examID int) *http.Response {
	t.Helper()
	srv := httptest.NewServer(router)
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(userID), "application/json", nil)
	if err != nil || login.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", login, err)
	}
	login.Body.Close()

	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Get(srv.URL + "/admin/api/exams/" + strconv.Itoa(examID) + "/pdf")
	if err != nil {
		t.Fatalf("GET exam pdf: %v", err)
	}
	return resp
}

// TestAdminExamPDFDisabledR2Rejected locks in that a DISABLED backend returns
// 500 before any SignedURL call (the guard must reject, not silently skip).
func TestAdminExamPDFDisabledR2Rejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	userID, examID := createExamPDFFixture(t, pool, "pdfguard-disabled")

	stub := &stubR2{enabled: false}
	router := newExamPDFTestRouter(pool, func(c *gin.Context) { c.Set("r2", stub) })

	resp := getAdminPDF(t, router, userID, examID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("disabled R2: status=%d, want 500", resp.StatusCode)
	}
	if len(stub.signed) != 0 {
		t.Fatalf("disabled R2: SignedURL called %d times (%v), want 0", len(stub.signed), stub.signed)
	}
}

// TestAdminExamPDFNilR2KeyDoesNotPanic locks in that a nil-interface "r2" key
// yields a clean 500 instead of a panic.
func TestAdminExamPDFNilR2KeyDoesNotPanic(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	userID, examID := createExamPDFFixture(t, pool, "pdfguard-nil")

	router := newExamPDFTestRouter(pool, func(c *gin.Context) { c.Set("r2", nil) })

	resp := getAdminPDF(t, router, userID, examID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("nil r2 key: status=%d, want 500", resp.StatusCode)
	}
}

// TestAdminExamPDFEnabledSignedURLRedirect locks in the happy path: an enabled
// backend signs the pdfs/<file_path> key and the handler 302-redirects to it.
func TestAdminExamPDFEnabledSignedURLRedirect(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	userID, examID := createExamPDFFixture(t, pool, "pdfguard-enabled")

	stub := newStubR2()
	router := newExamPDFTestRouter(pool, func(c *gin.Context) { c.Set("r2", stub) })

	resp := getAdminPDF(t, router, userID, examID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("enabled R2: status=%d, want 302", resp.StatusCode)
	}
	want := "https://storage.example.test/pdfs/ujian-pdf-guard.pdf"
	if loc := resp.Header.Get("Location"); loc != want {
		t.Fatalf("Location=%q, want %q", loc, want)
	}
	if len(stub.signed) != 1 || stub.signed[0] != "pdfs/ujian-pdf-guard.pdf" {
		t.Fatalf("SignedURL keys=%v, want [pdfs/ujian-pdf-guard.pdf]", stub.signed)
	}
}
