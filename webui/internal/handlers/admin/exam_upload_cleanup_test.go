package admin

import (
	"context"
	"fmt"
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

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"

	r2client "github.com/examvan/webui/internal/handlers/r2"
)

// ---------------------------------------------------------------------------
// Handler tests: UploadExam uploads the PDF to R2 BEFORE the exam row is
// created. Every post-upload failure (quota reject, insert failure, ...) must
// delete the just-uploaded R2 object — otherwise each rejected upload leaks an
// orphan object in the bucket that no exam references. These tests drive the
// real handler with the r2.Client stub and assert the orphan key is deleted.
// ---------------------------------------------------------------------------

// newExamUploadCleanupTestRouter mirrors production wiring for the upload
// endpoint: sessions, AuthRequired, a /test/login/:id session seam, and an R2
// client stub (UploadExam requires R2 — the else branch rejects without it).
func newExamUploadCleanupTestRouter(pool *pgxpool.Pool, r2c r2client.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{StoragePath: ""})
		c.Set("r2", r2c)
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
	api.POST("/upload", UploadExam())
	return r
}

// createUploadCleanupUser creates a non-super, non-operator user (the quota
// path) so the upload handler runs the atomic max_exams check+insert.
func createUploadCleanupUser(t *testing.T, pool *pgxpool.Pool, username, examName string, maxExams int) int {
	t.Helper()
	ctx := context.Background()
	u, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: username, Name: "Upload Cleanup", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: maxExams, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if examName != "" {
		token := fmt.Sprintf("CLN%05d", time.Now().UnixNano()%100000)
		var examID int
		if err := pool.QueryRow(ctx, `
			INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
			VALUES ($1, $2, 1024, $3, $3, 'active', 'medium', $4)
			RETURNING id`, examName, examName+".pdf", token, u.ID).Scan(&examID); err != nil {
			t.Fatalf("insert pre-existing exam: %v", err)
		}
	}
	return u.ID
}

// uploadExamDo performs a multipart upload request and returns the status code.
func uploadExamDo(t *testing.T, client *http.Client, base string, name, pdfName string) int {
	t.Helper()
	body, contentType := editExamMultipart(t, name, []byte("%PDF-1.4 upload test\n%%EOF\n"), pdfName)
	req, err := http.NewRequest(http.MethodPost, base+"/admin/api/upload", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func countExamsForUser(t *testing.T, pool *pgxpool.Pool, userID int) int {
	t.Helper()
	var cnt int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM exams WHERE created_by = $1`, userID).Scan(&cnt); err != nil {
		t.Fatalf("count exams: %v", err)
	}
	return cnt
}

// The quota-reject branch (max_exams reached) must delete the R2 object that
// was just uploaded — the exact "gagal di tengah setelah upload R2" scenario.
func TestUploadExamQuotaRejectCleansUpR2(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	// maxExams=1 with one pre-existing exam → the next upload is rejected
	// AFTER the R2 upload succeeded.
	userID := createUploadCleanupUser(t, pool, "upload_quota_user", "QuotaExisting", 1)
	r2c := newStubR2()

	srv := httptest.NewServer(newExamUploadCleanupTestRouter(pool, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(userID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := uploadExamDo(t, client, srv.URL, "RejectedExam", "rejected.pdf"); code != http.StatusForbidden {
		t.Fatalf("upload status=%d, want 403 (quota reject)", code)
	}

	// The PDF reached R2 (upload recorded) and the orphan was cleaned up.
	if len(r2c.uploads) != 1 {
		t.Fatalf("R2 uploads = %d, want 1", len(r2c.uploads))
	}
	if len(r2c.deletes) != 1 {
		t.Fatalf("R2 deletes = %d (%v), want 1 orphan delete", len(r2c.deletes), r2c.deletes)
	}
	if !strings.HasPrefix(r2c.deletes[0], "pdfs/") || !strings.HasSuffix(r2c.deletes[0], "_rejected.pdf") {
		t.Errorf("orphan delete key = %q, want pdfs/<ts>_rejected.pdf", r2c.deletes[0])
	}
	// The deleted key is the exact object that was just uploaded — the orphan
	// cleanup removed the same object, not something else.
	if len(r2c.uploads) == 1 && r2c.uploads[0] != r2c.deletes[0] {
		t.Errorf("orphan delete key %q != uploaded key %q", r2c.deletes[0], r2c.uploads[0])
	}
	// No new exam row: the quota reject must not have created one.
	if cnt := countExamsForUser(t, pool, userID); cnt != 1 {
		t.Errorf("exams after quota reject = %d, want 1 (the pre-existing one)", cnt)
	}

	// No forged creation row either: a rejected upload never reaches the
	// audit write, so the trail stays trustworthy.
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE action = $1`,
		models.ActionExamCreated).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("exam_created audit rows after quota reject = %d, want 0", auditCount)
	}
}

// A DB insert failure (simulated with a guard trigger raising on a specific
// name) after the R2 upload must also delete the orphan object.
func TestUploadExamCreateFailureCleansUpR2(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	// Guard trigger: refuse the INSERT that the upload is about to do.
	if _, err := pool.Exec(ctx, `DROP FUNCTION IF EXISTS fn_block_upload_insert_cleanup_test() CASCADE`); err != nil {
		t.Fatalf("drop stale function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fn_block_upload_insert_cleanup_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.name = 'BlockedUploadExam' THEN
				RAISE EXCEPTION 'blocked by test trigger';
			END IF;
			RETURN NEW;
		END; $$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create trigger function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TRIGGER trg_block_upload_insert_cleanup_test
		BEFORE INSERT ON exams FOR EACH ROW
		EXECUTE FUNCTION fn_block_upload_insert_cleanup_test()`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DROP TRIGGER IF EXISTS trg_block_upload_insert_cleanup_test ON exams`)
		_, _ = pool.Exec(ctx, `DROP FUNCTION IF EXISTS fn_block_upload_insert_cleanup_test() CASCADE`)
	})

	userID := createUploadCleanupUser(t, pool, "upload_createfail_user", "", 3)
	r2c := newStubR2()
	srv := httptest.NewServer(newExamUploadCleanupTestRouter(pool, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(userID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := uploadExamDo(t, client, srv.URL, "BlockedUploadExam", "blocked.pdf"); code != http.StatusInternalServerError {
		t.Fatalf("upload status=%d, want 500 (insert failed)", code)
	}

	if len(r2c.uploads) != 1 {
		t.Fatalf("R2 uploads = %d, want 1", len(r2c.uploads))
	}
	if len(r2c.deletes) != 1 {
		t.Fatalf("R2 deletes = %d (%v), want 1 orphan delete", len(r2c.deletes), r2c.deletes)
	}
	if !strings.HasPrefix(r2c.deletes[0], "pdfs/") || !strings.HasSuffix(r2c.deletes[0], "_blocked.pdf") {
		t.Errorf("orphan delete key = %q, want pdfs/<ts>_blocked.pdf", r2c.deletes[0])
	}
	if cnt := countExamsForUser(t, pool, userID); cnt != 0 {
		t.Errorf("exams after failed insert = %d, want 0", cnt)
	}

	// No forged creation row: the insert failed, so no exam_created entry.
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE action = $1`,
		models.ActionExamCreated).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("exam_created audit rows after failed insert = %d, want 0", auditCount)
	}
}

// A successful upload must NOT delete anything (the object stays referenced by
// the new exam row).
func TestUploadExamSuccessNoOrphanCleanup(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	userID := createUploadCleanupUser(t, pool, "upload_ok_user", "", 3)
	r2c := newStubR2()

	srv := httptest.NewServer(newExamUploadCleanupTestRouter(pool, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(userID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := uploadExamDo(t, client, srv.URL, "OkUploadExam", "ok.pdf"); code != http.StatusOK {
		t.Fatalf("upload status=%d, want 200", code)
	}

	if len(r2c.uploads) != 1 {
		t.Fatalf("R2 uploads = %d, want 1", len(r2c.uploads))
	}
	if len(r2c.deletes) != 0 {
		t.Errorf("R2 deletes on SUCCESS = %v, want none (the object is referenced)", r2c.deletes)
	}
	if cnt := countExamsForUser(t, pool, userID); cnt != 1 {
		t.Errorf("exams after successful upload = %d, want 1", cnt)
	}
	// The exam row points at the exact uploaded object (timestamp-prefixed).
	var fp string
	if err := pool.QueryRow(context.Background(),
		`SELECT file_path FROM exams WHERE created_by = $1`, userID).Scan(&fp); err != nil {
		t.Fatalf("load created exam: %v", err)
	}
	if !strings.HasSuffix(fp, "_ok.pdf") {
		t.Errorf("exam file_path = %q, want suffix _ok.pdf", fp)
	}

	// Creating the exam must open the lifecycle audit trail (created → PDF
	// replaced → deleted): exactly one exam_created row attributing the actor.
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE action = $1 AND username = 'upload_ok_user'`,
		models.ActionExamCreated).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("exam_created audit rows = %d, want 1", auditCount)
	}
	var auditDetail string
	if err := pool.QueryRow(context.Background(),
		`SELECT detail FROM admin_audit_logs WHERE action = $1 AND username = 'upload_ok_user'`,
		models.ActionExamCreated).Scan(&auditDetail); err != nil {
		t.Fatalf("load audit detail: %v", err)
	}
	if !strings.Contains(auditDetail, "OkUploadExam") {
		t.Errorf("audit detail = %q, want it to mention the created exam name", auditDetail)
	}
}
