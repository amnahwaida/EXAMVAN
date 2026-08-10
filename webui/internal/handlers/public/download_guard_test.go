package public

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"

	r2client "github.com/examvan/webui/internal/handlers/r2"
)

// ---------------------------------------------------------------------------
// R2 guard consistency on the download paths (mirrors the upload/delete guard
// tests): DownloadAPK and DownloadSystemApp must reject a missing OR DISABLED
// backend with a clear message BEFORE calling SignedURL; an "r2" key holding a
// non-Client value (nil interface) must not panic; an enabled backend must
// redirect (302) to the signed URL for the exact stored object key.
// ---------------------------------------------------------------------------

// stubR2 implements r2client.Client for the public package tests (a separate
// stub from the admin package's, since test helpers are per-package). Only
// SignedURL is exercised by the download handlers.
type stubR2 struct {
	enabled  bool
	signed   []string
	failWith error
}

var _ r2client.Client = (*stubR2)(nil)

func (s *stubR2) Enabled() bool { return s.enabled }

func (s *stubR2) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	s.signed = append(s.signed, key)
	if s.failWith != nil {
		return "", s.failWith
	}
	return "https://storage.example.test/" + key, nil
}

func (s *stubR2) Upload(ctx context.Context, key string, reader io.Reader) error {
	return nil
}

func (s *stubR2) UploadWithContentType(ctx context.Context, key string, reader io.Reader, contentType string) error {
	return nil
}

func (s *stubR2) UploadBytes(ctx context.Context, key string, data []byte) error {
	return nil
}

func (s *stubR2) Delete(ctx context.Context, key string) error {
	return nil
}

// newDownloadGuardTestRouter wires the two download endpoints with a custom
// "r2" context value (a stub, a disabled stub, or nil — the nil-interface
// scenario). The routes are public, so no session/auth middleware is needed.
func newDownloadGuardTestRouter(pool *pgxpool.Pool, r2Val interface{}) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("r2", r2Val)
	})
	r.GET("/download/apk", DownloadAPK())
	r.GET("/download/app/:id", DownloadSystemApp())
	return r
}

// insertDownloadSystemApp inserts an app row directly (as if a previous upload
// had succeeded) and returns its id.
func insertDownloadSystemApp(t *testing.T, pool *pgxpool.Pool, name, platform, version, filePath string) int {
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

// downloadGet performs a GET without following redirects (so a 302's Location
// header is observable) and returns status, body, Location.
func downloadGet(t *testing.T, base, path string) (int, string, string) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(b), resp.Header.Get("Location")
}

// --- DownloadAPK ------------------------------------------------------------

func TestDownloadAPKDisabledR2Rejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	insertDownloadSystemApp(t, pool, "EXAMVAN", "android", "9.9", "apps/android/9.9/app.apk-1")
	stub := &stubR2{enabled: false}

	srv := httptest.NewServer(newDownloadGuardTestRouter(pool, stub))
	defer srv.Close()

	code, body, _ := downloadGet(t, srv.URL, "/download/apk")
	if code != http.StatusInternalServerError {
		t.Fatalf("GET /download/apk status=%d, want 500 (disabled backend must be rejected)", code)
	}
	if !strings.Contains(body, "Cloudflare R2") {
		t.Errorf("rejection body = %q, want a clear Cloudflare R2 message", body)
	}
	// The guard must reject BEFORE signing — a disabled backend must never
	// serve a signed URL.
	if len(stub.signed) != 0 {
		t.Errorf("SignedURL called on a DISABLED backend: %v — must be rejected before signing", stub.signed)
	}
}

func TestDownloadAPKNilR2KeyDoesNotPanic(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	insertDownloadSystemApp(t, pool, "EXAMVAN", "android", "9.9", "apps/android/9.9/app.apk-1")

	srv := httptest.NewServer(newDownloadGuardTestRouter(pool, nil))
	defer srv.Close()

	code, body, _ := downloadGet(t, srv.URL, "/download/apk")
	if code != http.StatusInternalServerError {
		t.Fatalf("GET /download/apk status=%d, want 500 (nil r2 key must not panic)", code)
	}
	if !strings.Contains(body, "Cloudflare R2") {
		t.Errorf("rejection body = %q, want a clear Cloudflare R2 message", body)
	}
}

func TestDownloadAPKEnabledSignedURLRedirect(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	insertDownloadSystemApp(t, pool, "EXAMVAN", "android", "9.9", "apps/android/9.9/app.apk-1")
	stub := &stubR2{enabled: true}

	srv := httptest.NewServer(newDownloadGuardTestRouter(pool, stub))
	defer srv.Close()

	code, _, loc := downloadGet(t, srv.URL, "/download/apk")
	if code != http.StatusFound {
		t.Fatalf("GET /download/apk status=%d, want 302", code)
	}
	if loc != "https://storage.example.test/apps/android/9.9/app.apk-1" {
		t.Errorf("Location = %q, want the signed URL for the stored object key", loc)
	}
	if len(stub.signed) != 1 || stub.signed[0] != "apps/android/9.9/app.apk-1" {
		t.Errorf("SignedURL called with %v, want exactly [apps/android/9.9/app.apk-1]", stub.signed)
	}
}

// --- DownloadSystemApp ------------------------------------------------------

func TestDownloadSystemAppDisabledR2Rejected(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	id := insertDownloadSystemApp(t, pool, "EXAMVAN Kiosk", "android", "2.5.0", "apps/android/2.5.0/kiosk.apk-1")
	stub := &stubR2{enabled: false}

	srv := httptest.NewServer(newDownloadGuardTestRouter(pool, stub))
	defer srv.Close()

	code, body, _ := downloadGet(t, srv.URL, "/download/app/"+strconv.Itoa(id))
	if code != http.StatusInternalServerError {
		t.Fatalf("GET /download/app/%d status=%d, want 500 (disabled backend must be rejected)", id, code)
	}
	if !strings.Contains(body, "Cloudflare R2") {
		t.Errorf("rejection body = %q, want a clear Cloudflare R2 message", body)
	}
	// The guard must reject BEFORE signing — a disabled backend must never
	// serve a signed URL.
	if len(stub.signed) != 0 {
		t.Errorf("SignedURL called on a DISABLED backend: %v — must be rejected before signing", stub.signed)
	}
}

func TestDownloadSystemAppNilR2KeyDoesNotPanic(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	id := insertDownloadSystemApp(t, pool, "EXAMVAN Kiosk", "android", "2.5.0", "apps/android/2.5.0/kiosk.apk-1")

	srv := httptest.NewServer(newDownloadGuardTestRouter(pool, nil))
	defer srv.Close()

	code, body, _ := downloadGet(t, srv.URL, "/download/app/"+strconv.Itoa(id))
	if code != http.StatusInternalServerError {
		t.Fatalf("GET /download/app/%d status=%d, want 500 (nil r2 key must not panic)", id, code)
	}
	if !strings.Contains(body, "Cloudflare R2") {
		t.Errorf("rejection body = %q, want a clear Cloudflare R2 message", body)
	}
}

func TestDownloadSystemAppEnabledSignedURLRedirect(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	id := insertDownloadSystemApp(t, pool, "EXAMVAN Kiosk", "android", "2.5.0", "apps/android/2.5.0/kiosk.apk-1")
	stub := &stubR2{enabled: true}

	srv := httptest.NewServer(newDownloadGuardTestRouter(pool, stub))
	defer srv.Close()

	code, _, loc := downloadGet(t, srv.URL, "/download/app/"+strconv.Itoa(id))
	if code != http.StatusFound {
		t.Fatalf("GET /download/app/%d status=%d, want 302", id, code)
	}
	if loc != "https://storage.example.test/apps/android/2.5.0/kiosk.apk-1" {
		t.Errorf("Location = %q, want the signed URL for the stored object key", loc)
	}
	if len(stub.signed) != 1 || stub.signed[0] != "apps/android/2.5.0/kiosk.apk-1" {
		t.Errorf("SignedURL called with %v, want exactly [apps/android/2.5.0/kiosk.apk-1]", stub.signed)
	}
}
