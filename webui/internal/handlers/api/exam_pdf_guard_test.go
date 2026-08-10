package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	r2client "github.com/examvan/webui/internal/handlers/r2"
)

// ---------------------------------------------------------------------------
// API ExamPDF R2 guard tests
// ---------------------------------------------------------------------------
//
// ExamPDF (student side) serves the exam PDF via a Cloudflare R2 signed URL
// after the active-token gate. Only an ENABLED backend may sign: a disabled
// backend or a malformed (nil-interface) "r2" key must be rejected with 500
// and a clear message — never a panic, never a silent non-signing pass.

// stubR2 is a local in-memory r2.Client for the api package tests (test
// helpers are per-package in Go). It records the keys passed to SignedURL and
// can be switched to a disabled/failing backend. No network involved.
type stubR2 struct {
	enabled      bool
	signed       []string
	signFailWith error // fails SignedURL (signed-URL generation failure path)
}

var _ r2client.Client = (*stubR2)(nil)

func (s *stubR2) Enabled() bool { return s.enabled }

func (s *stubR2) Upload(ctx context.Context, key string, reader io.Reader) error {
	return nil
}

func (s *stubR2) UploadWithContentType(ctx context.Context, key string, reader io.Reader, contentType string) error {
	return nil
}

func (s *stubR2) UploadBytes(ctx context.Context, key string, data []byte) error {
	return nil
}

func (s *stubR2) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	s.signed = append(s.signed, key)
	if s.signFailWith != nil {
		return "", s.signFailWith
	}
	return "https://storage.example.test/" + key, nil
}

func (s *stubR2) Delete(ctx context.Context, key string) error { return nil }

// newExamPDFGuardRouter wires the api ExamPDF endpoint with sessions + "db" +
// a pluggable "r2" key (enabled stub / disabled stub / nil interface).
func newExamPDFGuardRouter(pool *pgxpool.Pool, setR2 func(*gin.Context)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		setR2(c)
	})
	r.GET("/api/exams/:exam_id/pdf", ExamPDF())
	return r
}

// getStudentPDF requests the student PDF endpoint with the exam token header.
func getStudentPDF(t *testing.T, router *gin.Engine, examID int, token string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/exams/%d/pdf", examID), nil)
	req.Header.Set("X-Exam-Token", token)
	router.ServeHTTP(rec, req)
	return rec
}

// TestAPIExamPDFDisabledR2Rejected locks in that a DISABLED backend returns
// 500 with a clear message and SignedURL is never called.
func TestAPIExamPDFDisabledR2Rejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, false)

	stub := &stubR2{enabled: false}
	router := newExamPDFGuardRouter(pool, func(c *gin.Context) { c.Set("r2", stub) })

	rec := getStudentPDF(t, router, examID, token)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("disabled R2: status=%d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, r2client.ErrMsgNotConfigured) {
		t.Fatalf("disabled R2: body=%q, want canonical message %q", body, r2client.ErrMsgNotConfigured)
	}
	if !strings.Contains(body, r2client.ErrCodeNotConfigured) {
		t.Fatalf("disabled R2: body=%q, want error_code %q", body, r2client.ErrCodeNotConfigured)
	}
	if len(stub.signed) != 0 {
		t.Fatalf("disabled R2: SignedURL called %d times, want 0", len(stub.signed))
	}
}

// TestAPIExamPDFNilR2KeyDoesNotPanic locks in that a nil-interface "r2" key
// yields a clean 500 instead of a panic.
func TestAPIExamPDFNilR2KeyDoesNotPanic(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, false)

	router := newExamPDFGuardRouter(pool, func(c *gin.Context) { c.Set("r2", nil) })

	rec := getStudentPDF(t, router, examID, token)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("nil r2 key: status=%d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, r2client.ErrMsgNotConfigured) {
		t.Fatalf("nil r2 key: body=%q, want canonical message %q", body, r2client.ErrMsgNotConfigured)
	}
	if !strings.Contains(body, r2client.ErrCodeNotConfigured) {
		t.Fatalf("nil r2 key: body=%q, want error_code %q", body, r2client.ErrCodeNotConfigured)
	}
}

// TestAPIExamPDFEnabledSignedURLRedirect locks in the happy path: an enabled
// backend signs pdfs/<file_path> and the handler 302-redirects to it.
func TestAPIExamPDFEnabledSignedURLRedirect(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, false)

	stub := &stubR2{enabled: true}
	router := newExamPDFGuardRouter(pool, func(c *gin.Context) { c.Set("r2", stub) })

	rec := getStudentPDF(t, router, examID, token)
	if rec.Code != http.StatusFound {
		t.Fatalf("enabled R2: status=%d, want 302", rec.Code)
	}
	want := "https://storage.example.test/pdfs/ujian.pdf"
	if loc := rec.Header().Get("Location"); loc != want {
		t.Fatalf("Location=%q, want %q", loc, want)
	}
	if len(stub.signed) != 1 || stub.signed[0] != "pdfs/ujian.pdf" {
		t.Fatalf("SignedURL keys=%v, want [pdfs/ujian.pdf]", stub.signed)
	}
}

// TestAPIExamPDFSignedURLFailureHasCode locks in that a signed-URL generation
// failure on an ENABLED backend returns 500 with the SIGNED_URL_FAILED code
// (not the not-configured code) so clients can branch on the exact cause.
func TestAPIExamPDFSignedURLFailureHasCode(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, false)

	stub := &stubR2{enabled: true, signFailWith: fmt.Errorf("presign boom")}
	router := newExamPDFGuardRouter(pool, func(c *gin.Context) { c.Set("r2", stub) })

	rec := getStudentPDF(t, router, examID, token)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("signed-URL failure: status=%d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, r2client.ErrMsgSignURLFailed) {
		t.Fatalf("signed-URL failure: body=%q, want canonical message %q", body, r2client.ErrMsgSignURLFailed)
	}
	if !strings.Contains(body, r2client.ErrCodeSignURLFailed) {
		t.Fatalf("signed-URL failure: body=%q, want error_code %q", body, r2client.ErrCodeSignURLFailed)
	}
	// The sign attempt DID reach the backend (it is enabled and was called).
	if len(stub.signed) != 1 || stub.signed[0] != "pdfs/ujian.pdf" {
		t.Fatalf("SignedURL keys=%v, want [pdfs/ujian.pdf]", stub.signed)
	}
}
