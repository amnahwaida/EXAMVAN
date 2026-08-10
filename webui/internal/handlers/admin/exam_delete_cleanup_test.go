package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
// Handler tests: exam deletion must clean the PDF file from the storage
// directory — on the single-delete path (DeleteExam), the bulk path
// (BulkDelete), and when deleting a user who owns exams (DeleteUser) — so the
// FreeDiskSpace indicator and the storage quota checks see the space returned
// and no orphan PDF lingers. R2 cleanup is covered by the same paths.
// DeleteExam treats R2 as MANDATORY for exams with a stored file (it refuses
// with R2_NOT_CONFIGURED when the backend is missing/disabled, so tests on
// the delete path register an enabled stub); BulkDelete/DeleteUser stay
// tolerant and gate R2 cleanup on client != nil && client.Enabled().
// ---------------------------------------------------------------------------

// newExamDeleteCleanupTestRouter mirrors production wiring for the exam/user
// delete endpoints: sessions, AuthRequired (+ AdminManagementRequired for
// DeleteUser), a /test/login/:id session seam, and the storage path pointed at
// a writable temp dir so SafeStoragePath resolves files there. When r2c is
// non-nil it is registered in the context (an R2-configured server).
func newExamDeleteCleanupTestRouter(pool *pgxpool.Pool, storageDir string, r2c r2client.Client) *gin.Engine {
	return newExamDeleteCleanupTestRouterWithR2(pool, storageDir, func(c *gin.Context) {
		if r2c != nil {
			c.Set("r2", r2c)
		}
	})
}

// newExamDeleteCleanupTestRouterR2KeyAlways is the same router, but the "r2"
// key is ALWAYS present in the context holding a value that is NOT an
// r2.Client (a nil interface). This reproduces the regression scenario for
// the `client != nil && client.Enabled()` guard in BulkDelete/DeleteUser:
// r2client.FromContext returns a nil interface for such a value, and calling
// .Enabled() on a nil interface panics.
func newExamDeleteCleanupTestRouterR2KeyAlways(pool *pgxpool.Pool, storageDir string) *gin.Engine {
	return newExamDeleteCleanupTestRouterWithR2(pool, storageDir, func(c *gin.Context) {
		c.Set("r2", nil)
	})
}

// newExamDeleteCleanupTestRouterWithR2 builds the delete-cleanup test router,
// delegating the "r2" key injection to setR2 so callers can simulate an
// R2-configured backend, an absent backend, or a malformed/disabled one.
func newExamDeleteCleanupTestRouterWithR2(pool *pgxpool.Pool, storageDir string, setR2 func(*gin.Context)) *gin.Engine {
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
	api.POST("/exams/:exam_id/delete", DeleteExam())
	api.POST("/exams/bulk-delete", BulkDelete())
	api.POST("/users/:user_id/delete", middleware.AdminManagementRequired(), DeleteUser())
	return r
}

// createExamDeleteCleanupFixture creates a superadmin plus an exam whose PDF
// file physically exists under storageDir, and returns the exam id + file path.
func createExamDeleteCleanupFixture(t *testing.T, pool *pgxpool.Pool, storageDir, username, examName string) (superID, examID int, rel string) {
	t.Helper()
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: username, Name: "Del Cleanup", PasswordHash: "x",
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

	// The PDF physically exists on the storage dir, like a real upload.
	filePath := filepath.Join(storageDir, examName+".pdf")
	if err := os.WriteFile(filePath, []byte("%PDF-1.4 test"), 0644); err != nil {
		t.Fatalf("write fake pdf: %v", err)
	}
	return su.ID, examID, examName + ".pdf"
}

// fileExists reports whether the storage file still exists.
func fileExists(t *testing.T, storageDir, rel string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(storageDir, rel))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", rel, err)
	return false
}

func deleteCleanupDo(t *testing.T, client *http.Client, base, method, path string, body interface{}) int {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// ---------------------------------------------------------------------------
// R2 delete branch: every deletion path must call R2 Delete with the exact
// pdfs/<file_path> key of each deleted exam — verified through the r2.Client
// interface stub (no network, no mock S3 server).
// ---------------------------------------------------------------------------

// DeleteExam must remove the exam's PDF from the storage directory (not just
// the DB row and R2) — otherwise the freed space never shows up in the
// FreeDiskSpace indicator and storage quotas.
func TestDeleteExamRemovesStorageFile(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-del-file")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, examID, rel := createExamDeleteCleanupFixture(t, pool, storageDir, "del_file_super", "DelFileExam")
	if !fileExists(t, storageDir, rel) {
		t.Fatalf("fixture pdf missing before delete")
	}

	// DeleteExam treats R2 as mandatory for exams with a stored PDF: the
	// delete must proceed, so register an enabled backend (the
	// R2_NOT_CONFIGURED refusal is covered by TestDeleteExamR2NotConfiguredRefused).
	r2c := newStubR2()
	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		fmt.Sprintf("/admin/api/exams/%d/delete", examID), nil); code != http.StatusOK {
		t.Fatalf("delete exam status=%d, want 200", code)
	}
	if fileExists(t, storageDir, rel) {
		t.Errorf("exam pdf still on disk after DeleteExam — FreeDiskSpace never recovers")
	}

	// Deleting the exam must leave an append-only audit row: written BEFORE
	// the row is removed, so it survives with the name snapshotted in detail
	// (exam_id is NULLed by the FK's ON DELETE SET NULL).
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE action = $1 AND username = 'del_file_super'`,
		models.ActionExamDeleted).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("exam_deleted audit rows = %d, want 1", auditCount)
	}
	var auditDetail string
	if err := pool.QueryRow(context.Background(),
		`SELECT detail FROM admin_audit_logs WHERE action = $1 AND username = 'del_file_super'`,
		models.ActionExamDeleted).Scan(&auditDetail); err != nil {
		t.Fatalf("load audit detail: %v", err)
	}
	if !strings.Contains(auditDetail, "DelFileExam") {
		t.Errorf("audit detail = %q, want it to mention the deleted exam name", auditDetail)
	}
}

// DeleteExam refuses to delete an exam with a stored PDF when the R2 backend
// is missing/disabled: it must return 500 with the R2_NOT_CONFIGURED
// error_code (so the frontend shows the setup warning instead of a generic
// failure), keep the DB row and the storage file intact, and record no R2
// delete — a proceeding delete would silently orphan the pdfs/<file_path>
// object. Absent key, nil key and disabled backend all refuse identically.
func TestDeleteExamR2NotConfiguredRefused(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-r2cfg-del")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, examID, rel := createExamDeleteCleanupFixture(t, pool, storageDir, "r2cfg_super", "R2CfgDelExam")

	disR2 := &stubR2{enabled: false}
	cases := []struct {
		name   string
		router *gin.Engine
		r2     *stubR2 // the backend the router registered, when there is one
	}{
		{"absent key", newExamDeleteCleanupTestRouter(pool, storageDir, nil), nil},
		{"nil key", newExamDeleteCleanupTestRouterR2KeyAlways(pool, storageDir), nil},
		{"disabled backend", newExamDeleteCleanupTestRouter(pool, storageDir, disR2), disR2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.router)
			defer srv.Close()
			jar, _ := cookiejar.New(nil)
			client := &http.Client{Jar: jar}
			if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
				t.Fatalf("test login: status=%v err=%v", resp, err)
			}

			resp, err := client.Post(srv.URL+fmt.Sprintf("/admin/api/exams/%d/delete", examID), "application/json", nil)
			if err != nil {
				t.Fatalf("delete exam: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("delete exam status=%d, want 500", resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), r2client.ErrMsgNotConfigured) {
				t.Errorf("body=%q, want canonical message %q", body, r2client.ErrMsgNotConfigured)
			}
			if !strings.Contains(string(body), r2client.ErrCodeNotConfigured) {
				t.Errorf("body=%q, want error_code %q", body, r2client.ErrCodeNotConfigured)
			}

			// The row and its file must survive the refusal — the delete was
			// rejected BEFORE the DB commit, so there is no partial state.
			var cnt int
			if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM exams WHERE id = $1`, examID).Scan(&cnt); err != nil {
				t.Fatalf("count exam rows: %v", err)
			}
			if cnt != 1 {
				t.Errorf("exam rows after refused delete = %d, want 1", cnt)
			}
			if !fileExists(t, storageDir, rel) {
				t.Errorf("exam pdf was removed despite the refusal")
			}

			// A refused delete must never reach the backend's Delete.
			if tc.r2 != nil && len(tc.r2.deletes) != 0 {
				t.Errorf("R2 deletes = %v, want none (refused before any Delete call)", tc.r2.deletes)
			}
		})
	}
}

// A legacy exam row without a stored file_path has no R2 artifact to clean,
// so DeleteExam must succeed even when the R2 backend is absent — the
// mandatory pre-check only applies to exams that could orphan an object.
func TestDeleteExamLegacyNoFilePathSucceedsWithoutR2(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-legacy-del")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, _, _ := createExamDeleteCleanupFixture(t, pool, storageDir, "legacy_super", "LegacyCfg")
	var examID int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('LegacyNoPath', '', 0, $1, $1, 'active', 'medium', $2)
		RETURNING id`, "LGCY00001", superID).Scan(&examID); err != nil {
		t.Fatalf("insert legacy exam: %v", err)
	}

	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, nil)) // no R2 at all
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		fmt.Sprintf("/admin/api/exams/%d/delete", examID), nil); code != http.StatusOK {
		t.Fatalf("delete legacy exam status=%d, want 200", code)
	}
	var cnt int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM exams WHERE id = $1`, examID).Scan(&cnt); err != nil {
		t.Fatalf("count exam rows: %v", err)
	}
	if cnt != 0 {
		t.Errorf("legacy exam rows after delete = %d, want 0", cnt)
	}
}

// An ENABLED backend whose Delete call fails must not fail the delete itself:
// the row is already gone at that point, so reporting an error would be
// misleading (a retry would 404) and the orphaned object is swept by the
// admin cleanup job. This locks in the contract that DeleteExam only reports
// R2 problems via the pre-check (R2_NOT_CONFIGURED), never after the DB
// commit.
func TestDeleteExamR2DeleteFailureStillSucceeds(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-r2fail-del")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, examID, rel := createExamDeleteCleanupFixture(t, pool, storageDir, "r2fail_super", "R2FailDelExam")
	r2c := &stubR2{enabled: true, deleteFailWith: errors.New("s3 boom")}

	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		fmt.Sprintf("/admin/api/exams/%d/delete", examID), nil); code != http.StatusOK {
		t.Fatalf("delete exam status=%d, want 200 (R2 delete failure is best-effort)", code)
	}
	// The attempt DID reach the enabled backend.
	if len(r2c.deletes) != 1 {
		t.Fatalf("R2 deletes = %d, want 1 (the attempt reached the enabled backend)", len(r2c.deletes))
	}
	if fileExists(t, storageDir, rel) {
		t.Errorf("exam pdf still on disk after delete")
	}
}

// BulkDelete must remove every deleted exam's PDF from the storage directory
// while leaving the kept exam's file alone.
func TestBulkDeleteRemovesStorageFiles(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-bulk-file")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, del1, rel1 := createExamDeleteCleanupFixture(t, pool, storageDir, "bulk_del_super", "BulkDel1")
	_, del2, rel2 := createExamDeleteCleanupFixture(t, pool, storageDir, "bulk_del_super2", "BulkDel2")
	_, _, rel3 := createExamDeleteCleanupFixture(t, pool, storageDir, "bulk_del_super3", "BulkKeep")

	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, nil))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	code := deleteCleanupDo(t, client, srv.URL, http.MethodPost, "/admin/api/exams/bulk-delete",
		map[string]interface{}{"ids": []int{del1, del2}})
	if code != http.StatusOK {
		t.Fatalf("bulk delete status=%d, want 200", code)
	}
	if fileExists(t, storageDir, rel1) || fileExists(t, storageDir, rel2) {
		t.Errorf("bulk-deleted exam pdfs still on disk")
	}
	if !fileExists(t, storageDir, rel3) {
		t.Errorf("kept exam pdf was removed — bulk delete must only touch the selected exams")
	}

	// The bulk delete must audit one row per deleted exam (name snapshots),
	// and nothing for the kept exam.
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE action = $1 AND username = 'bulk_del_super'`,
		models.ActionExamDeleted).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("exam_deleted audit rows = %d, want 2 (one per deleted exam)", auditCount)
	}
	detailRows, err := pool.Query(context.Background(),
		`SELECT detail FROM admin_audit_logs WHERE action = $1 AND username = 'bulk_del_super'`,
		models.ActionExamDeleted)
	if err != nil {
		t.Fatalf("query audit details: %v", err)
	}
	defer detailRows.Close()
	var details strings.Builder
	for detailRows.Next() {
		var d string
		if err := detailRows.Scan(&d); err != nil {
			t.Fatalf("scan audit detail: %v", err)
		}
		details.WriteString(d)
		details.WriteString("|")
	}
	for _, want := range []string{"BulkDel1", "BulkDel2"} {
		if !strings.Contains(details.String(), want) {
			t.Errorf("audit trail missing deleted exam %q: %q", want, details.String())
		}
	}
	if strings.Contains(details.String(), "BulkKeep") {
		t.Errorf("audit trail mentions the KEPT exam: %q", details.String())
	}
}

// DeleteUser must remove the PDFs of the exams owned by the deleted user — the
// user's exams are gone, so their files must not linger on disk either.
func TestDeleteUserRemovesExamStorageFiles(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-user-file")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	// Creator (guru) owns the exam; a separate superadmin performs the delete.
	guru, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: "del_user_guru", Name: "Del Guru", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create guru: %v", err)
	}
	su, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: "del_user_super", Name: "Del Super", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	token := fmt.Sprintf("CLN%05d", time.Now().UnixNano()%100000)
	var examID int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('UserDelExam', $1, 1024, $2, $2, 'active', 'medium', $3)
		RETURNING id`, "UserDelExam.pdf", token, guru.ID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storageDir, "UserDelExam.pdf"), []byte("%PDF-1.4 test"), 0644); err != nil {
		t.Fatalf("write fake pdf: %v", err)
	}

	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, nil))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		fmt.Sprintf("/admin/api/users/%d/delete", guru.ID), nil); code != http.StatusOK {
		t.Fatalf("delete user status=%d, want 200", code)
	}
	if fileExists(t, storageDir, "UserDelExam.pdf") {
		t.Errorf("deleted user's exam pdf still on disk")
	}
}

// DeleteExam must call R2 Delete with the exact pdfs/<file_path> key of the
// deleted exam (verified via the r2.Client stub — no network).
func TestDeleteExamCallsR2Delete(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-r2del-file")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, examID, rel := createExamDeleteCleanupFixture(t, pool, storageDir, "r2del_super", "R2DelExam")
	r2c := newStubR2()

	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		fmt.Sprintf("/admin/api/exams/%d/delete", examID), nil); code != http.StatusOK {
		t.Fatalf("delete exam status=%d, want 200", code)
	}
	if len(r2c.deletes) != 1 {
		t.Fatalf("R2 deletes = %d, want 1", len(r2c.deletes))
	}
	if r2c.deletes[0] != "pdfs/"+rel {
		t.Errorf("R2 delete key = %q, want pdfs/%s", r2c.deletes[0], rel)
	}
}

// BulkDelete must call R2 Delete for EVERY deleted exam's key, and none for
// the kept exam.
func TestBulkDeleteCallsR2Delete(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-r2bulk-file")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, del1, rel1 := createExamDeleteCleanupFixture(t, pool, storageDir, "r2bulk_super", "R2BulkDel1")
	_, del2, rel2 := createExamDeleteCleanupFixture(t, pool, storageDir, "r2bulk_super2", "R2BulkDel2")
	_, _, rel3 := createExamDeleteCleanupFixture(t, pool, storageDir, "r2bulk_super3", "R2BulkKeep")
	r2c := newStubR2()

	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	code := deleteCleanupDo(t, client, srv.URL, http.MethodPost, "/admin/api/exams/bulk-delete",
		map[string]interface{}{"ids": []int{del1, del2}})
	if code != http.StatusOK {
		t.Fatalf("bulk delete status=%d, want 200", code)
	}
	wantDeleted := map[string]bool{"pdfs/" + rel1: true, "pdfs/" + rel2: true}
	if len(r2c.deletes) != 2 {
		t.Fatalf("R2 deletes = %d (%v), want 2", len(r2c.deletes), r2c.deletes)
	}
	for k := range wantDeleted {
		if !containsString(r2c.deletes, k) {
			t.Errorf("R2 delete key %q missing", k)
		}
	}
	if len(r2c.deletes) == 2 && r2c.deletes[0] == r2c.deletes[1] {
		t.Errorf("R2 delete keys are duplicated: %v", r2c.deletes)
	}
	if containsString(r2c.deletes, "pdfs/"+rel3) {
		t.Errorf("kept exam key pdfs/%s was deleted", rel3)
	}
}

// DeleteUser must call R2 Delete for every exam owned by the deleted user
// (and its cascaded instansi users).
func TestDeleteUserCallsR2Delete(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-r2user-file")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	// Creator (guru) owns two exams; a superadmin performs the delete.
	guru, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: "r2del_guru", Name: "R2 Del Guru", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create guru: %v", err)
	}
	su, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: "r2del_super", Name: "R2 Del Super", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	for _, name := range []string{"R2UserDel1", "R2UserDel2"} {
		token := fmt.Sprintf("CLN%05d", time.Now().UnixNano()%100000)
		var examID int
		if err := pool.QueryRow(context.Background(), `
			INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
			VALUES ($1, $2, 1024, $3, $3, 'active', 'medium', $4)
			RETURNING id`, name, fmt.Sprintf("%s.pdf", name), token, guru.ID).Scan(&examID); err != nil {
			t.Fatalf("insert exam %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(storageDir, name+".pdf"), []byte("%PDF-1.4 test"), 0644); err != nil {
			t.Fatalf("write fake pdf %s: %v", name, err)
		}
	}
	r2c := newStubR2()

	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		fmt.Sprintf("/admin/api/users/%d/delete", guru.ID), nil); code != http.StatusOK {
		t.Fatalf("delete user status=%d, want 200", code)
	}
	wantDeleted := map[string]bool{"pdfs/R2UserDel1.pdf": true, "pdfs/R2UserDel2.pdf": true}
	if len(r2c.deletes) != 2 {
		t.Fatalf("R2 deletes = %d (%v), want 2", len(r2c.deletes), r2c.deletes)
	}
	for k := range wantDeleted {
		if !containsString(r2c.deletes, k) {
			t.Errorf("R2 delete key %q missing", k)
		}
	}

	// The cascade must audit one exam_deleted row per deleted exam, attributed
	// to the acting superadmin (the exam names are snapshotted in detail).
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE action = $1 AND username = 'r2del_super'`,
		models.ActionExamDeleted).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("exam_deleted audit rows = %d, want 2 (one per deleted exam)", auditCount)
	}
	auditRows, err := pool.Query(context.Background(),
		`SELECT detail FROM admin_audit_logs WHERE action = $1 AND username = 'r2del_super'`,
		models.ActionExamDeleted)
	if err != nil {
		t.Fatalf("query audit details: %v", err)
	}
	defer auditRows.Close()
	var auditDetails strings.Builder
	for auditRows.Next() {
		var d string
		if err := auditRows.Scan(&d); err != nil {
			t.Fatalf("scan audit detail: %v", err)
		}
		auditDetails.WriteString(d)
		auditDetails.WriteString("|")
	}
	for _, want := range []string{"R2UserDel1", "R2UserDel2"} {
		if !strings.Contains(auditDetails.String(), want) {
			t.Errorf("audit trail missing deleted exam %q: %q", want, auditDetails.String())
		}
	}
}

// DeleteUser on an OPERATOR must audit an exam_deleted row for every exam the
// cascade removes — the operator's own exams AND those of its instansi
// sub-accounts (the exact set models.DeleteUser deletes).
func TestDeleteUserCascadeAuditsEveryDeletedExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	mkUser := func(username, role string, instansi string) int {
		t.Helper()
		u, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: username, Name: "Cascade Audit", Instansi: instansi,
			PasswordHash: "x", Status: models.UserStatusActive,
			Role:     models.SerializeRoles([]string{role}),
			MaxExams: 5, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		})
		if err != nil {
			t.Fatalf("create user %s: %v", username, err)
		}
		return u.ID
	}

	opID := mkUser("cascade_op", models.RoleOperator, "SMA Audit Cascade")
	sub1ID := mkUser("cascade_sub1", models.RoleGuru, "SMA Audit Cascade")
	sub2ID := mkUser("cascade_sub2", models.RoleGuru, "SMA Audit Cascade")

	mkExam := func(owner int, name string) {
		t.Helper()
		token := fmt.Sprintf("CLN%05d", time.Now().UnixNano()%100000)
		var examID int
		if err := pool.QueryRow(ctx, `
			INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
			VALUES ($1, $2, 1024, $3, $3, 'active', 'medium', $4)
			RETURNING id`, name, name+".pdf", token, owner).Scan(&examID); err != nil {
			t.Fatalf("insert exam %s: %v", name, err)
		}
	}
	mkExam(opID, "CascadeOpExam")
	mkExam(sub1ID, "CascadeSub1Exam")
	mkExam(sub2ID, "CascadeSub2Exam")

	suID := mkUser("cascade_audit_super", models.RoleSuperAdmin, "")
	storageDir, err := os.MkdirTemp("", "examvan-cascade-file")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)
	r2c := newStubR2()
	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(suID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	if code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		fmt.Sprintf("/admin/api/users/%d/delete", opID), nil); code != http.StatusOK {
		t.Fatalf("delete operator status=%d, want 200", code)
	}

	// All three exams (operator's own + both sub-accounts') must be audited,
	// attributed to the acting superadmin.
	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_audit_logs WHERE action = $1 AND username = 'cascade_audit_super'`,
		models.ActionExamDeleted).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 3 {
		t.Fatalf("exam_deleted audit rows = %d, want 3 (operator + 2 sub-accounts)", auditCount)
	}
	auditRows, err := pool.Query(context.Background(),
		`SELECT detail FROM admin_audit_logs WHERE action = $1 AND username = 'cascade_audit_super'`,
		models.ActionExamDeleted)
	if err != nil {
		t.Fatalf("query audit details: %v", err)
	}
	defer auditRows.Close()
	var auditDetails strings.Builder
	for auditRows.Next() {
		var d string
		if err := auditRows.Scan(&d); err != nil {
			t.Fatalf("scan audit detail: %v", err)
		}
		auditDetails.WriteString(d)
		auditDetails.WriteString("|")
	}
	for _, want := range []string{"CascadeOpExam", "CascadeSub1Exam", "CascadeSub2Exam"} {
		if !strings.Contains(auditDetails.String(), want) {
			t.Errorf("audit trail missing deleted exam %q: %q", want, auditDetails.String())
		}
	}
	// No exam rows survive the cascade.
	var cnt int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM exams WHERE created_by = ANY($1)`, []int{opID, sub1ID, sub2ID}).Scan(&cnt); err != nil {
		t.Fatalf("count remaining exams: %v", err)
	}
	if cnt != 0 {
		t.Errorf("exams after operator cascade = %d, want 0", cnt)
	}
}

// ---------------------------------------------------------------------------
// Nil / disabled R2 guard: BulkDelete & DeleteUser gate R2 cleanup on
// `client != nil && client.Enabled()`. A context whose "r2" key holds a
// value that is NOT an r2.Client (a nil interface) makes r2client.FromContext
// return a nil interface — calling .Enabled() on that panics. These tests
// lock in the guard so a regression cannot reintroduce the panic.
// ---------------------------------------------------------------------------

// BulkDelete with an "r2" key present but holding a non-Client value (nil
// interface) must NOT panic, must still delete the rows + local files, and
// must leave the kept exam's file alone.
func TestBulkDeleteNilR2KeyDoesNotPanic(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-nilr2-bulk")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, del1, rel1 := createExamDeleteCleanupFixture(t, pool, storageDir, "nilr2_bulk_super", "NilR2BulkDel")
	_, _, rel2 := createExamDeleteCleanupFixture(t, pool, storageDir, "nilr2_bulk_super2", "NilR2BulkKeep")

	srv := httptest.NewServer(newExamDeleteCleanupTestRouterR2KeyAlways(pool, storageDir))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	// The handler reads the r2 key, FromContext yields a nil interface, and
	// the guard skips R2 cleanup instead of calling .Enabled() on it.
	code := deleteCleanupDo(t, client, srv.URL, http.MethodPost, "/admin/api/exams/bulk-delete",
		map[string]interface{}{"ids": []int{del1}})
	if code != http.StatusOK {
		t.Fatalf("bulk delete status=%d, want 200 (nil r2 key must not panic)", code)
	}
	if fileExists(t, storageDir, rel1) {
		t.Errorf("bulk-deleted exam pdf still on disk with nil r2 key")
	}
	if !fileExists(t, storageDir, rel2) {
		t.Errorf("kept exam pdf was removed with nil r2 key")
	}
}

// BulkDelete with a typed-but-disabled stub (Enabled() == false) must skip R2
// cleanup entirely — no Delete call recorded — while still deleting rows +
// local files.
func TestBulkDeleteDisabledR2SkipsDelete(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-disr2-bulk")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	superID, del1, rel1 := createExamDeleteCleanupFixture(t, pool, storageDir, "disr2_bulk_super", "DisR2BulkDel")
	r2c := &stubR2{enabled: false} // configured backend but disabled

	srv := httptest.NewServer(newExamDeleteCleanupTestRouter(pool, storageDir, r2c))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	code := deleteCleanupDo(t, client, srv.URL, http.MethodPost, "/admin/api/exams/bulk-delete",
		map[string]interface{}{"ids": []int{del1}})
	if code != http.StatusOK {
		t.Fatalf("bulk delete status=%d, want 200", code)
	}
	if len(r2c.deletes) != 0 {
		t.Errorf("R2 deletes = %v, want none (disabled backend)", r2c.deletes)
	}
	if fileExists(t, storageDir, rel1) {
		t.Errorf("bulk-deleted exam pdf still on disk")
	}
}

// DeleteUser with an "r2" key present but holding a non-Client value (nil
// interface) must NOT panic and must still remove the deleted user's exam
// PDFs from disk.
func TestDeleteUserNilR2KeyDoesNotPanic(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir, err := os.MkdirTemp("", "examvan-nilr2-user")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	guru, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: "nilr2_user_guru", Name: "NilR2 Guru", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create guru: %v", err)
	}
	su, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: "nilr2_user_super", Name: "NilR2 Super", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	token := fmt.Sprintf("CLN%05d", time.Now().UnixNano()%100000)
	var examID int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('NilR2UserExam', $1, 1024, $2, $2, 'active', 'medium', $3)
		RETURNING id`, "NilR2UserExam.pdf", token, guru.ID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storageDir, "NilR2UserExam.pdf"), []byte("%PDF-1.4 test"), 0644); err != nil {
		t.Fatalf("write fake pdf: %v", err)
	}

	srv := httptest.NewServer(newExamDeleteCleanupTestRouterR2KeyAlways(pool, storageDir))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		fmt.Sprintf("/admin/api/users/%d/delete", guru.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("delete user status=%d, want 200 (nil r2 key must not panic)", code)
	}
	if fileExists(t, storageDir, "NilR2UserExam.pdf") {
		t.Errorf("deleted user's exam pdf still on disk with nil r2 key")
	}
}
