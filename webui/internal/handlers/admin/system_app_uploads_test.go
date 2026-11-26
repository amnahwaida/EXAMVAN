package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/config"
)

func newSystemAppUploadTestRouter(storageDir string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("cfg", &config.Config{StoragePath: storageDir})
	})
	r.POST("/admin/api/system-app-upload/chunk", UploadSystemAppChunk())
	r.POST("/admin/api/system-app-upload/cancel", CancelSystemAppUpload())
	return r
}

func writeTestSystemAppUpload(t *testing.T, storageDir string, meta systemAppUploadMeta) string {
	t.Helper()
	dir := filepath.Join(systemAppUploadRootForTest(storageDir), meta.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	if err := writeSystemAppUploadMeta(dir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	return dir
}

func systemAppUploadRootForTest(storageDir string) string {
	return filepath.Join(storageDir, "tmp", "system-app-uploads")
}

func testSystemAppUploadMeta(id string) systemAppUploadMeta {
	return systemAppUploadMeta{
		ID:          id,
		Name:        "EXAMVAN Client",
		Platform:    "windows",
		Version:     "2.7.20",
		Filename:    "EXAMVAN-Setup.exe",
		ContentType: "application/x-msdownload",
		Key:         "apps/windows/2.7.20/EXAMVAN-Setup.exe-1",
		Size:        5,
		ChunkSize:   3,
		TotalChunks: 2,
	}
}

func TestUploadSystemAppChunkWritesExactPart(t *testing.T) {
	storageDir := t.TempDir()
	id := strings.Repeat("a", 32)
	dir := writeTestSystemAppUpload(t, storageDir, testSystemAppUploadMeta(id))
	r := newSystemAppUploadTestRouter(storageDir)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/system-app-upload/chunk?id="+id+"&index=0", strings.NewReader("abc"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("chunk 0 status = %d, body=%s", w.Code, w.Body.String())
	}
	if got, err := os.ReadFile(filepath.Join(dir, "part-000000")); err != nil || string(got) != "abc" {
		t.Fatalf("part 0 = %q, err=%v", got, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/api/system-app-upload/chunk?id="+id+"&index=1", strings.NewReader("de"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("chunk 1 status = %d, body=%s", w.Code, w.Body.String())
	}
	if got, err := os.ReadFile(filepath.Join(dir, "part-000001")); err != nil || string(got) != "de" {
		t.Fatalf("part 1 = %q, err=%v", got, err)
	}
}

func TestUploadSystemAppChunkRejectsWrongSize(t *testing.T) {
	storageDir := t.TempDir()
	id := strings.Repeat("b", 32)
	writeTestSystemAppUpload(t, storageDir, testSystemAppUploadMeta(id))
	r := newSystemAppUploadTestRouter(storageDir)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/system-app-upload/chunk?id="+id+"&index=0", strings.NewReader("ab"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestUploadSystemAppChunkRejectsUnknownSession(t *testing.T) {
	storageDir := t.TempDir()
	r := newSystemAppUploadTestRouter(storageDir)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/system-app-upload/chunk?id="+strings.Repeat("c", 32)+"&index=0", strings.NewReader("abc"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestCancelSystemAppUploadRemovesSession(t *testing.T) {
	storageDir := t.TempDir()
	id := strings.Repeat("d", 32)
	dir := writeTestSystemAppUpload(t, storageDir, testSystemAppUploadMeta(id))
	r := newSystemAppUploadTestRouter(storageDir)

	body := `{"upload_id":"` + id + `"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/api/system-app-upload/cancel", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("upload dir still exists: %v", err)
	}
}

func TestValidateSystemAppUploadParts(t *testing.T) {
	storageDir := t.TempDir()
	meta := testSystemAppUploadMeta(strings.Repeat("e", 32))
	dir := writeTestSystemAppUpload(t, storageDir, meta)

	if err := os.WriteFile(filepath.Join(dir, "part-000000"), []byte("MZx"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "part-000001"), []byte("yz"), 0644); err != nil {
		t.Fatal(err)
	}
	total, err := validateSystemAppUploadParts(dir, meta)
	if err != nil {
		t.Fatalf("validate valid parts: %v", err)
	}
	if total != meta.Size {
		t.Fatalf("total = %d, want %d", total, meta.Size)
	}

	if err := os.WriteFile(filepath.Join(dir, "part-000001"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := validateSystemAppUploadParts(dir, meta); err == nil {
		t.Fatal("expected size mismatch error")
	}
}

func TestSystemAppMagicValid(t *testing.T) {
	cases := []struct {
		platform string
		header   string
		want     bool
	}{
		{"android", "PK\x03\x04", true},
		{"android", "MZxx", false},
		{"windows", "MZ", true},
		{"windows", "PK\x03\x04", false},
		{"linux", "\x7fELF", true},
		{"linux", "MZxx", false},
		{"invalid", "MZxx", false},
	}
	for _, tc := range cases {
		if got := systemAppMagicValid(tc.platform, []byte(tc.header)); got != tc.want {
			t.Errorf("systemAppMagicValid(%q, %q) = %v, want %v", tc.platform, tc.header, got, tc.want)
		}
	}
}

func TestSystemAppUploadFilenameSanitizes(t *testing.T) {
	cases := map[string]string{
		"../evil.exe":       "evil.exe",
		"folder\\Setup.exe": "Setup.exe",
		"":                  "app",
		"..":                "app",
		"EXAMVAN-Setup.exe": "EXAMVAN-Setup.exe",
	}
	for in, want := range cases {
		if got := systemAppUploadFilename(in); got != want {
			t.Errorf("systemAppUploadFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInitSystemAppUploadRequestJSON(t *testing.T) {
	var req initSystemAppUploadRequest
	if err := json.Unmarshal([]byte(`{"name":"EXAMVAN","platform":"windows","version":"2.7.20","filename":"EXAMVAN-Setup.exe","size":101590926}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.Size != 101590926 || req.Platform != "windows" {
		t.Fatalf("unexpected request: %+v", req)
	}
}
