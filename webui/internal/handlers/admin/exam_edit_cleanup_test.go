package admin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"

	r2client "github.com/examvan/webui/internal/handlers/r2"
)

// ---------------------------------------------------------------------------
// Handler tests: replacing an exam's PDF (EditExam) must clean up the OLD
// local storage file — otherwise the replaced PDF's ghost keeps occupying
// disk, FreeDiskSpace never recovers and storage quota checks keep counting
// it. A failed replacement (e.g. R2 upload error) must NOT delete the old
// file: the exam still points at it until the update commits.
// ---------------------------------------------------------------------------

// newExamEditCleanupTestRouter mirrors production wiring for the exam edit
// endpoint: sessions, AuthRequired, a /test/login/:id session seam, and the
// storage path pointed at a writable temp dir so SafeStoragePath resolves
// files there. When r2c is non-nil it is registered in the context, matching
// a server with R2 configured (EditExam requires R2 for the new upload).
func newExamEditCleanupTestRouter(pool *pgxpool.Pool, storageDir string, r2c r2client.Client) *gin.Engine {
	return newExamEditCleanupTestRouterWithR2(pool, storageDir, func(c *gin.Context) {
		if r2c != nil {
			c.Set("r2", r2c)
		}
	})
}

// newExamEditCleanupTestRouterR2Nil is the same router, but the "r2" key is
// ALWAYS present holding a value that is NOT an r2.Client (a nil interface) —
// the handler must reject the replacement with a clear message instead of
// panicking.
func newExamEditCleanupTestRouterR2Nil(pool *pgxpool.Pool, storageDir string) *gin.Engine {
	return newExamEditCleanupTestRouterWithR2(pool, storageDir, func(c *gin.Context) {
		c.Set("r2", nil)
	})
}

// newExamEditCleanupTestRouterWithR2 builds the edit-cleanup test router,
// delegating the "r2" key injection to setR2 so callers can simulate an
// enabled backend, a disabled one, or a malformed (nil) key.
func newExamEditCleanupTestRouterWithR2(pool *pgxpool.Pool, storageDir string, setR2 func(*gin.Context)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{StoragePath: storageDir})
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
	api.POST("/exams/:exam_id/edit", EditExam())
	return r
}

// editExamMultipart builds a multipart form body for EditExam (name + a PDF
// file) and returns the body + content type.
func editExamMultipart(t *testing.T, newName string, pdf []byte, pdfName string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("name", newName); err != nil {
		t.Fatalf("write name field: %v", err)
	}
	fw, err := mw.CreateFormFile("pdf_file", pdfName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(pdf); err != nil {
		t.Fatalf("write pdf part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

// editExamDoFull posts an EditExam request (multipart: name + new PDF) and
// returns the status code + response body so tests can assert the rejection
// message wording.
func editExamDoFull(t *testing.T, client *http.Client, base string, examID int, newName string, pdf []byte, pdfName string) (int, string) {
	t.Helper()
	body, contentType := editExamMultipart(t, newName, pdf, pdfName)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/admin/api/exams/%d/edit", base, examID), body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("edit request: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp.StatusCode, string(b)
}

// createExamEditFixture creates a superadmin plus an exam whose PDF file
// physically exists under storageDir (a legacy pre-R2 file that must be
// cleaned up when the PDF is replaced), and returns the superadmin id, exam
// id and the exam's file_path.
func createExamEditFixture(t *testing.T, pool *pgxpool.Pool, storageDir, username, examName string) (superID, examID int, rel string) {
	t.Helper()
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: username, Name: "Edit Cleanup", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	token := fmt.Sprintf("CLN%05d", time.Now().UnixNano()%100000)
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ($1, $2, 1024, $3, $3, 'active', 'medium', $4)
		RETURNING id`, examName, examName+".pdf", token, su.ID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	// The old PDF physically exists on the storage dir, like a pre-R2 file.
	filePath := filepath.Join(storageDir, examName+".pdf")
	if err := os.WriteFile(filePath, []byte("%PDF-1.4 old"), 0644); err != nil {
		t.Fatalf("write fake old pdf: %v", err)
	}
	return su.ID, examID, examName + ".pdf"
}

// EditExam must remove the OLD local PDF when the PDF is replaced: the new
// PDF is uploaded (R2), then the old local file is deleted so FreeDiskSpace
// recovers and no ghost file lingers.
func TestEditExamReplacesPDFRemovesOldStorageFile(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-edit-file")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, examID, rel := createExamEditFixture(t, pool, storageDir, "edit_ok_super", "EditOkExam")
	if !fileExists(t, storageDir, rel) {
		t.Fatalf("fixture old pdf missing before edit")
	}

	r2c := newStubR2()
	srv := httptest.NewServer(newExamEditCleanupTestRouter(pool, storageDir, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	newPDF := []byte("%PDF-1.4 new content\n%%EOF\n")
	body, contentType := editExamMultipart(t, "Edited Name", newPDF, "newfile.pdf")
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/admin/api/exams/%d/edit", srv.URL, examID), body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("edit request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit exam status=%d, want 200", resp.StatusCode)
	}

	// The new PDF was uploaded to R2 and the OLD R2 object was deleted.
	if len(r2c.uploads) != 1 || len(r2c.deletes) != 1 {
		t.Errorf("R2 calls = uploads:%d deletes:%d, want 1 upload + 1 delete", len(r2c.uploads), len(r2c.deletes))
	}
	if len(r2c.deletes) == 1 && r2c.deletes[0] != "pdfs/"+rel {
		t.Errorf("R2 delete key = %q, want pdfs/%s (the old file)", r2c.deletes[0], rel)
	}

	// The old local PDF must be gone — the replacement cleaned it up.
	if fileExists(t, storageDir, rel) {
		t.Errorf("old exam pdf still on disk after PDF replacement — FreeDiskSpace never recovers")
	}
	// The exam now points at the new file (timestamp-prefixed) and the new
	// name is saved.
	var newPath, name string
	if err := pool.QueryRow(context.Background(),
		`SELECT file_path, name FROM exams WHERE id = $1`, examID).Scan(&newPath, &name); err != nil {
		t.Fatalf("load exam after edit: %v", err)
	}
	if newPath == rel {
		t.Errorf("exam file_path unchanged after edit: %q", newPath)
	}
	if !strings.HasSuffix(newPath, "_newfile.pdf") {
		t.Errorf("exam file_path = %q, want suffix _newfile.pdf", newPath)
	}
	if name != "Edited Name" {
		t.Errorf("exam name = %q, want %q", name, "Edited Name")
	}

	// Replacing the PDF must leave an append-only audit trail row attributing
	// the action: who replaced it, when, and with the old → new file names.
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE exam_id = $1`, examID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit rows after PDF replacement = %d, want 1", auditCount)
	}
	var auditUsername, auditAction, auditDetail string
	if err := pool.QueryRow(context.Background(),
		`SELECT username, action, detail FROM admin_audit_logs WHERE exam_id = $1`, examID).Scan(&auditUsername, &auditAction, &auditDetail); err != nil {
		t.Fatalf("load audit log: %v", err)
	}
	if auditUsername != "edit_ok_super" {
		t.Errorf("audit username = %q, want %q", auditUsername, "edit_ok_super")
	}
	if auditAction != models.ActionExamPDFReplaced {
		t.Errorf("audit action = %q, want %q", auditAction, models.ActionExamPDFReplaced)
	}
	if !strings.Contains(auditDetail, rel) || !strings.Contains(auditDetail, newPath) {
		t.Errorf("audit detail = %q, want it to mention old file %q and new file %q", auditDetail, rel, newPath)
	}
}

// A FAILED EditExam (R2 client not configured → upload rejected) must NOT
// remove the old local PDF: the exam still points at it, so deleting it would
// break the exam's PDF. The old R2 object is likewise untouched (deleted only
// after the new upload succeeds).
func TestEditExamFailureKeepsOldStorageFile(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-edit-fail")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, examID, rel := createExamEditFixture(t, pool, storageDir, "edit_fail_super", "EditFailExam")

	// Router WITHOUT an R2 client: EditExam's upload step fails with
	// r2client.ErrMsgNotConfigured before any old-file deletion.
	srv := httptest.NewServer(newExamEditCleanupTestRouter(pool, storageDir, nil))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	newPDF := []byte("%PDF-1.4 new content\n%%EOF\n")
	body, contentType := editExamMultipart(t, "Should Not Apply", newPDF, "newfile.pdf")
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/admin/api/exams/%d/edit", srv.URL, examID), body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("edit request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("edit without R2 unexpectedly succeeded")
	}

	// The old local PDF must survive the failed edit.
	if !fileExists(t, storageDir, rel) {
		t.Errorf("old exam pdf removed on FAILED edit — exam still points at it")
	}
	// The exam row is untouched (name unchanged, file_path unchanged).
	var name, path string
	if err := pool.QueryRow(context.Background(),
		`SELECT name, file_path FROM exams WHERE id = $1`, examID).Scan(&name, &path); err != nil {
		t.Fatalf("load exam after failed edit: %v", err)
	}
	if name != "EditFailExam" {
		t.Errorf("exam name after failed edit = %q, want %q", name, "EditFailExam")
	}
	if path != rel {
		t.Errorf("exam file_path after failed edit = %q, want %q", path, rel)
	}

	// A FAILED edit must leave NO audit trail: no row, no forged replacement
	// entry for a change that never happened.
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE exam_id = $1`, examID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("audit rows after FAILED edit = %d, want 0", auditCount)
	}
}

// ---------------------------------------------------------------------------
// R2 guard consistency on EditExam (mirrors the UploadExam guard tests): a
// missing OR DISABLED backend must reject a PDF replacement with a clear
// message BEFORE any upload — the exam row, the old R2 object and the old
// local file all stay untouched, and no audit row is forged. A nil-interface
// "r2" key must not panic either.
// ---------------------------------------------------------------------------

// A disabled backend (Enabled() == false) must reject the replacement with a
// clear message and leave everything untouched.
func TestEditExamR2DisabledRejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-edit-disr2")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, examID, rel := createExamEditFixture(t, pool, storageDir, "edit_disr2_super", "EditDisR2Exam")
	r2c := &stubR2{enabled: false}

	srv := httptest.NewServer(newExamEditCleanupTestRouter(pool, storageDir, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	code, body := editExamDoFull(t, client, srv.URL, examID, "Should Not Apply",
		[]byte("%PDF-1.4 new content\n%%EOF\n"), "newfile.pdf")
	if code != http.StatusInternalServerError {
		t.Fatalf("edit status=%d, want 500 (disabled backend must be rejected)", code)
	}
	if !strings.Contains(body, "Cloudflare R2") {
		t.Errorf("rejection body = %q, want a clear Cloudflare R2 message", body)
	}
	if len(r2c.uploads) != 0 {
		t.Errorf("R2 uploads = %v, want none (disabled backend never touched)", r2c.uploads)
	}

	// The exam row, the old local file and the audit trail are all untouched.
	var name, path string
	if err := pool.QueryRow(context.Background(),
		`SELECT name, file_path FROM exams WHERE id = $1`, examID).Scan(&name, &path); err != nil {
		t.Fatalf("load exam after rejected edit: %v", err)
	}
	if name != "EditDisR2Exam" || path != rel {
		t.Errorf("exam after rejected edit = name:%q path:%q, want name:%q path:%q", name, path, "EditDisR2Exam", rel)
	}
	if !fileExists(t, storageDir, rel) {
		t.Errorf("old exam pdf removed on REJECTED edit — exam still points at it")
	}
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE exam_id = $1`, examID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("audit rows after rejected edit = %d, want 0", auditCount)
	}
}

// An "r2" key present but holding a non-Client value (nil interface) must not
// panic — the replacement is rejected cleanly and the exam stays untouched.
func TestEditExamR2NilKeyDoesNotPanic(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-edit-nilr2")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, examID, rel := createExamEditFixture(t, pool, storageDir, "edit_nilr2_super", "EditNilR2Exam")

	srv := httptest.NewServer(newExamEditCleanupTestRouterR2Nil(pool, storageDir))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	code, body := editExamDoFull(t, client, srv.URL, examID, "Should Not Apply",
		[]byte("%PDF-1.4 new content\n%%EOF\n"), "newfile.pdf")
	if code != http.StatusInternalServerError {
		t.Fatalf("edit status=%d, want 500 (nil r2 key must not panic)", code)
	}
	if !strings.Contains(body, "Cloudflare R2") {
		t.Errorf("rejection body = %q, want a clear Cloudflare R2 message", body)
	}

	var name, path string
	if err := pool.QueryRow(context.Background(),
		`SELECT name, file_path FROM exams WHERE id = $1`, examID).Scan(&name, &path); err != nil {
		t.Fatalf("load exam after rejected edit: %v", err)
	}
	if name != "EditNilR2Exam" || path != rel {
		t.Errorf("exam after rejected edit = name:%q path:%q, want unchanged", name, path)
	}
	if !fileExists(t, storageDir, rel) {
		t.Errorf("old exam pdf removed on REJECTED edit — exam still points at it")
	}
}
