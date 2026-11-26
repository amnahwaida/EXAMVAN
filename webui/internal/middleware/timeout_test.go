package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func timeoutRemaining(t *testing.T, rules map[string]time.Duration, path string) (time.Duration, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TimeoutMiddlewareWithOverrides(30*time.Second, rules))

	var remaining time.Duration
	var hasDeadline bool
	r.Any("/*any", func(c *gin.Context) {
		d, ok := c.Request.Context().Deadline()
		if ok {
			remaining, hasDeadline = time.Until(d), true
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, path, nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	return remaining, hasDeadline
}

func TestTimeoutMiddlewareOverridesLargeUploadRoutes(t *testing.T) {
	rules := map[string]time.Duration{
		"/admin/api/system-apps":  10 * time.Minute,
		"/admin/api/upload":       10 * time.Minute,
		"/admin/api/exams/*/edit": 10 * time.Minute,
	}

	for _, path := range []string{
		"/admin/api/system-apps",
		"/admin/api/upload",
		"/admin/api/exams/123/edit",
	} {
		remaining, ok := timeoutRemaining(t, rules, path)
		if !ok {
			t.Fatalf("%s: no deadline set", path)
		}
		if remaining < 9*time.Minute {
			t.Errorf("%s: remaining timeout %s, want ~10m", path, remaining)
		}
	}

	remaining, ok := timeoutRemaining(t, rules, "/admin/api/stats")
	if !ok {
		t.Fatal("/admin/api/stats: no deadline set")
	}
	if remaining > 31*time.Second {
		t.Errorf("/admin/api/stats: remaining timeout %s, want ~30s", remaining)
	}
}

func TestTimeoutMiddlewareSkipsHealthChecks(t *testing.T) {
	if _, ok := timeoutRemaining(t, nil, "/api/health"); ok {
		t.Fatal("/api/health should not get a deadline")
	}
}
