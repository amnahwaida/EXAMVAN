package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/database"

	r2client "github.com/examvan/webui/internal/handlers/r2"
)

func newSystemAppUploadIntegrationRouter(pool *pgxpool.Pool, r2c r2client.Client, storageDir string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("r2", r2c)
		c.Set("cfg", &config.Config{StoragePath: storageDir})
	})
	r.POST("/admin/api/system-app-upload/init", InitSystemAppUpload())
	r.POST("/admin/api/system-app-upload/chunk", UploadSystemAppChunk())
	r.POST("/admin/api/system-app-upload/complete", CompleteSystemAppUpload())
	r.POST("/admin/api/system-app-upload/cancel", CancelSystemAppUpload())
	return r
}

func TestChunkedSystemAppUploadSuccess(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	storageDir := t.TempDir()
	r2c := newStubR2()
	srv := httptest.NewServer(newSystemAppUploadIntegrationRouter(pool, r2c, storageDir))
	defer srv.Close()

	initBody := `{"name":"ChunkedApp","platform":"android","version":"1.2.3","filename":"chunked.apk","size":4}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/admin/api/system-app-upload/init", strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("init request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("init status = %d", resp.StatusCode)
	}
	var initRes struct {
		Success   bool   `json:"success"`
		UploadID  string `json:"upload_id"`
		ChunkSize int64  `json:"chunk_size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&initRes); err != nil {
		t.Fatalf("decode init: %v", err)
	}
	if !initRes.Success || initRes.UploadID == "" {
		t.Fatalf("unexpected init response: %+v", initRes)
	}

	payload := []byte("PK\x03\x04")
	req, _ = http.NewRequest(http.MethodPost,
		srv.URL+"/admin/api/system-app-upload/chunk?id="+initRes.UploadID+"&index=0",
		strings.NewReader(string(payload)))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("chunk request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("chunk status = %d", resp.StatusCode)
	}

	completeBody := `{"upload_id":"` + initRes.UploadID + `"}`
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/admin/api/system-app-upload/complete", strings.NewReader(completeBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("complete request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete status = %d", resp.StatusCode)
	}
	var completeRes struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completeRes); err != nil {
		t.Fatalf("decode complete: %v", err)
	}
	if !completeRes.Success {
		t.Fatal("complete response success=false")
	}

	if len(r2c.uploads) != 1 {
		t.Fatalf("R2 uploads = %d, want 1", len(r2c.uploads))
	}
	if cnt := countSystemApps(t, pool); cnt != 1 {
		t.Fatalf("system_apps rows = %d, want 1", cnt)
	}
	uploadDir := systemAppUploadRootForTest(storageDir)
	if entries, err := os.ReadDir(uploadDir); err == nil && len(entries) != 0 {
		t.Fatalf("upload temp dir still has %d entries", len(entries))
	}
}
