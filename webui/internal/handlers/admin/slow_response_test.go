package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRespondSlowOperationFastPathKeepsStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/slow", func(c *gin.Context) {
		respondSlowOperation(c, time.Second, func(context.Context) (int, gin.H) {
			return http.StatusInternalServerError, gin.H{"success": false, "message": "fast failure"}
		})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/slow", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestRespondSlowOperationCancelledStreamEndsWithJSONError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	r := gin.New()
	r.POST("/slow", func(c *gin.Context) {
		respondSlowOperation(c, 5*time.Millisecond, func(context.Context) (int, gin.H) {
			<-release
			return http.StatusOK, gin.H{"success": true}
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()

	req := httptest.NewRequest(http.MethodPost, "/slow", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("streamed status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(w.Body.String())), &body); err != nil {
		t.Fatalf("body is not valid JSON after whitespace: %v (%q)", err, w.Body.String())
	}
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
}

func TestRespondSlowOperationStreamsAfterThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/slow", func(c *gin.Context) {
		respondSlowOperation(c, 5*time.Millisecond, func(ctx context.Context) (int, gin.H) {
			time.Sleep(25 * time.Millisecond)
			return http.StatusInternalServerError, gin.H{"success": false, "error_code": "UPLOAD_FAILED", "message": "slow failure"}
		})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/slow", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("streamed status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(w.Body.String())), &body); err != nil {
		t.Fatalf("body is not valid JSON after whitespace: %v (%q)", err, w.Body.String())
	}
	if body["error_code"] != "UPLOAD_FAILED" {
		t.Fatalf("error_code = %v, want UPLOAD_FAILED", body["error_code"])
	}
}
