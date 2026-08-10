package admin

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"

	r2client "github.com/examvan/webui/internal/handlers/r2"
)

// ---------------------------------------------------------------------------
// R2/DB consistency for the system-app (APK) upload & delete paths:
//
//   - UploadSystemApp uploads to R2 BEFORE the row is created, so every
//     post-upload failure (insert error, unique race) must delete the
//     just-uploaded object — otherwise each rejected upload leaks an orphan
//     object in the bucket that no app row references (same contract as
//     UploadExam's cleanupR2Orphan).
//
//   - DeleteSystemApp must delete the DB row FIRST and only then the R2
//     object. Deleting R2 before the row removal commits leaves a broken
//     reference when the DB delete fails: the app stays listed but its
//     download link points at a deleted object. This is the same ordering
//     contract as DeleteExam / BulkDelete / DeleteUser.
//
// Both are driven through the real handlers with the r2.Client stub — no
// network, no mock S3 server.
// ---------------------------------------------------------------------------

// newSystemAppsCleanupTestRouter mirrors production wiring for the system-app
// endpoints (sessions, AuthRequired, /test/login/:id seam, r2 stub in context).
func newSystemAppsCleanupTestRouter(pool *pgxpool.Pool, r2c r2client.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
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
	api.POST("/system-apps", UploadSystemApp())
	api.POST("/system-apps/:id/delete", DeleteSystemApp())
	return r
}

// createSystemAppCleanupSuper creates a superadmin (the production routes are
// SuperAdmin-only; AuthRequired is enough for these handler tests).
func createSystemAppCleanupSuper(t *testing.T, pool *pgxpool.Pool, username string) int {
	t.Helper()
	u, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: username, Name: "SysApp Cleanup", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	return u.ID
}

// systemAppUploadDo posts a multipart APK upload (android magic PK\x03\x04)
// and returns the status code.
func systemAppUploadDo(t *testing.T, client *http.Client, base, name, platform, version, fileName string) int {
	t.Helper()
	// Android APK/zip magic bytes pass the handler's format check.
	data := append([]byte{0x50, 0x4B, 0x03, 0x04}, []byte("fake apk payload")...)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("name", name); err != nil {
		t.Fatalf("write name field: %v", err)
	}
	if err := mw.WriteField("platform", platform); err != nil {
		t.Fatalf("write platform field: %v", err)
	}
	if err := mw.WriteField("version", version); err != nil {
		t.Fatalf("write version field: %v", err)
	}
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, base+"/admin/api/system-apps", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// insertSystemAppRow inserts an app row directly (as if a previous upload had
// succeeded) and returns its id.
func insertSystemAppRow(t *testing.T, pool *pgxpool.Pool, name, platform, version, filePath string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO system_apps (name, platform, version, file_path, size_bytes)
		VALUES ($1, $2, $3, $4, 1024)
		RETURNING id`, name, platform, version, filePath).Scan(&id); err != nil {
		t.Fatalf("insert system_app row: %v", err)
	}
	return id
}

func countSystemApps(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var cnt int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM system_apps`).Scan(&cnt); err != nil {
		t.Fatalf("count system_apps: %v", err)
	}
	return cnt
}

func systemAppLogin(t *testing.T, srvURL string, userID int) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srvURL+"/test/login/"+strconv.Itoa(userID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}
	return client
}

// A DB INSERT failure after the R2 upload must delete the orphan object — the
// same "gagal di tengah setelah upload R2" contract as UploadExam.
func TestUploadSystemAppDBFailureCleansUpR2(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	// Guard trigger: refuse the INSERT the upload is about to do.
	if _, err := pool.Exec(ctx, `DROP FUNCTION IF EXISTS fn_block_sysapp_insert_cleanup_test() CASCADE`); err != nil {
		t.Fatalf("drop stale function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fn_block_sysapp_insert_cleanup_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.name = 'BlockedSystemApp' THEN
				RAISE EXCEPTION 'blocked by test trigger';
			END IF;
			RETURN NEW;
		END; $$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create trigger function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TRIGGER trg_block_sysapp_insert_cleanup_test
		BEFORE INSERT ON system_apps FOR EACH ROW
		EXECUTE FUNCTION fn_block_sysapp_insert_cleanup_test()`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DROP TRIGGER IF EXISTS trg_block_sysapp_insert_cleanup_test ON system_apps`)
		_, _ = pool.Exec(ctx, `DROP FUNCTION IF EXISTS fn_block_sysapp_insert_cleanup_test() CASCADE`)
	})

	superID := createSystemAppCleanupSuper(t, pool, "sysapp_insertfail")
	r2c := newStubR2()
	srv := httptest.NewServer(newSystemAppsCleanupTestRouter(pool, r2c))
	defer srv.Close()
	client := systemAppLogin(t, srv.URL, superID)

	if code := systemAppUploadDo(t, client, srv.URL, "BlockedSystemApp", "android", "1.0", "blocked.apk"); code != http.StatusInternalServerError {
		t.Fatalf("upload status=%d, want 500 (insert failed)", code)
	}

	if len(r2c.uploads) != 1 {
		t.Fatalf("R2 uploads = %d, want 1", len(r2c.uploads))
	}
	if len(r2c.deletes) != 1 {
		t.Fatalf("R2 deletes = %d (%v), want 1 orphan delete", len(r2c.deletes), r2c.deletes)
	}
	// The deleted key is the exact object that was just uploaded.
	if r2c.uploads[0] != r2c.deletes[0] {
		t.Errorf("orphan delete key %q != uploaded key %q", r2c.deletes[0], r2c.uploads[0])
	}
	if cnt := countSystemApps(t, pool); cnt != 0 {
		t.Errorf("system_apps rows after failed insert = %d, want 0", cnt)
	}
}

// An R2 upload failure must fail the upload without any cleanup delete (nothing
// reached the bucket — the stub records the attempt, and no row may be created).
func TestUploadSystemAppR2FailureNoOrphanDelete(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	superID := createSystemAppCleanupSuper(t, pool, "sysapp_upfail")
	r2c := newStubR2()
	r2c.failWith = fmt.Errorf("r2 upload failed")
	srv := httptest.NewServer(newSystemAppsCleanupTestRouter(pool, r2c))
	defer srv.Close()
	client := systemAppLogin(t, srv.URL, superID)

	if code := systemAppUploadDo(t, client, srv.URL, "UpFailSystemApp", "android", "1.0", "upfail.apk"); code != http.StatusInternalServerError {
		t.Fatalf("upload status=%d, want 500 (r2 upload failed)", code)
	}

	if len(r2c.deletes) != 0 {
		t.Errorf("R2 deletes after failed upload = %v, want none (nothing reached the bucket)", r2c.deletes)
	}
	if cnt := countSystemApps(t, pool); cnt != 0 {
		t.Errorf("system_apps rows after failed upload = %d, want 0", cnt)
	}
}

// A successful upload must NOT delete anything and must store the exact R2
// object path in the row (the object stays referenced by the new row).
func TestUploadSystemAppSuccessNoOrphanCleanup(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	superID := createSystemAppCleanupSuper(t, pool, "sysapp_ok")
	r2c := newStubR2()
	srv := httptest.NewServer(newSystemAppsCleanupTestRouter(pool, r2c))
	defer srv.Close()
	client := systemAppLogin(t, srv.URL, superID)

	if code := systemAppUploadDo(t, client, srv.URL, "OkSystemApp", "android", "1.0", "ok.apk"); code != http.StatusOK {
		t.Fatalf("upload status=%d, want 200", code)
	}

	if len(r2c.uploads) != 1 {
		t.Fatalf("R2 uploads = %d, want 1", len(r2c.uploads))
	}
	if len(r2c.deletes) != 0 {
		t.Errorf("R2 deletes on SUCCESS = %v, want none (the object is referenced)", r2c.deletes)
	}
	if cnt := countSystemApps(t, pool); cnt != 1 {
		t.Fatalf("system_apps rows = %d, want 1", cnt)
	}
	// The row points at the exact uploaded object path (apps/<platform>/<ver>/...).
	var fp string
	if err := pool.QueryRow(context.Background(),
		`SELECT file_path FROM system_apps WHERE name = 'OkSystemApp'`).Scan(&fp); err != nil {
		t.Fatalf("load created app: %v", err)
	}
	if fp != r2c.uploads[0] {
		t.Errorf("row file_path = %q, want the uploaded key %q", fp, r2c.uploads[0])
	}
}

// DeleteSystemApp must delete the DB row FIRST and only then the R2 object.
// When the row deletion fails, the R2 object must remain untouched — the app
// is still listed, so its download must keep working. (The old order deleted
// R2 first and left a dead download link on a surviving row.)
func TestDeleteSystemAppDBFailureKeepsR2(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	// Guard trigger: refuse every DELETE on system_apps.
	if _, err := pool.Exec(ctx, `DROP FUNCTION IF EXISTS fn_block_sysapp_delete_cleanup_test() CASCADE`); err != nil {
		t.Fatalf("drop stale function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fn_block_sysapp_delete_cleanup_test() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'blocked by test trigger';
		END; $$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create trigger function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TRIGGER trg_block_sysapp_delete_cleanup_test
		BEFORE DELETE ON system_apps FOR EACH ROW
		EXECUTE FUNCTION fn_block_sysapp_delete_cleanup_test()`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DROP TRIGGER IF EXISTS trg_block_sysapp_delete_cleanup_test ON system_apps`)
		_, _ = pool.Exec(ctx, `DROP FUNCTION IF EXISTS fn_block_sysapp_delete_cleanup_test() CASCADE`)
	})

	superID := createSystemAppCleanupSuper(t, pool, "sysapp_delfail")
	appID := insertSystemAppRow(t, pool, "KeepApp", "android", "1.0", "apps/android/1.0/keep.apk-1")
	r2c := newStubR2()
	srv := httptest.NewServer(newSystemAppsCleanupTestRouter(pool, r2c))
	defer srv.Close()
	client := systemAppLogin(t, srv.URL, superID)

	if code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		"/admin/api/system-apps/"+strconv.Itoa(appID)+"/delete", nil); code != http.StatusInternalServerError {
		t.Fatalf("delete app status=%d, want 500 (db delete failed)", code)
	}

	if len(r2c.deletes) != 0 {
		t.Errorf("R2 delete recorded despite failed DB delete: %v — R2 must only be touched AFTER the row is gone", r2c.deletes)
	}
	if cnt := countSystemApps(t, pool); cnt != 1 {
		t.Errorf("system_apps rows after failed delete = %d, want 1 (row survived, so its file must survive too)", cnt)
	}
}

// Successful delete: the row is gone and the R2 object is deleted with the
// exact stored path — but only after the DB delete committed.
func TestDeleteSystemAppSuccessRemovesR2AfterDB(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	superID := createSystemAppCleanupSuper(t, pool, "sysapp_delok")
	appID := insertSystemAppRow(t, pool, "DelOkApp", "android", "1.0", "apps/android/1.0/ok.apk-1")
	r2c := newStubR2()
	srv := httptest.NewServer(newSystemAppsCleanupTestRouter(pool, r2c))
	defer srv.Close()
	client := systemAppLogin(t, srv.URL, superID)

	if code := deleteCleanupDo(t, client, srv.URL, http.MethodPost,
		"/admin/api/system-apps/"+strconv.Itoa(appID)+"/delete", nil); code != http.StatusOK {
		t.Fatalf("delete app status=%d, want 200", code)
	}

	if cnt := countSystemApps(t, pool); cnt != 0 {
		t.Errorf("system_apps rows after delete = %d, want 0", cnt)
	}
	if len(r2c.deletes) != 1 {
		t.Fatalf("R2 deletes = %d (%v), want 1", len(r2c.deletes), r2c.deletes)
	}
	if r2c.deletes[0] != "apps/android/1.0/ok.apk-1" {
		t.Errorf("R2 delete key = %q, want apps/android/1.0/ok.apk-1", r2c.deletes[0])
	}
}
