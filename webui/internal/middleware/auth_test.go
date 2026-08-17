package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// Unit tests for FeatureLockRequired: the billing-only gate that sits after
// AuthRequired and blocks every non-billing page/API for a feature-locked
// (expired, not suspended, not superadmin) account. Pure HTTP — no database —
// because the lock flag is set by AuthRequired from the session and the DB
// expiry row, which the integration tests cover; here we drive the flag
// directly and assert the two branches (403 JSON for API, redirect + flash to
// /admin/billing for HTML) plus the pass-through.
// ---------------------------------------------------------------------------

// newLockTestRouter builds a minimal router with a real session store and the
// FeatureLockRequired middleware, plus a billing page that consumes and echoes
// flashes so the redirect branch can be asserted end-to-end.
func newLockTestRouter(setLocked bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("middleware-test-secret-0123456789abcdef"))
	r.Use(sessions.Sessions("examvan_session", store))

	// Seed the lock flag before the middleware runs, exactly where AuthRequired
	// would have set it.
	r.Use(func(c *gin.Context) {
		if setLocked {
			c.Set(ContextKeyLocked, true)
		}
	})

	locked := r.Group("/locked")
	locked.Use(FeatureLockRequired())
	locked.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// Settings mirror: renders (and consumes) pending flashes so tests can
	// assert the lock notice is shown after the redirect to /admin/settings#billing.
	r.GET("/admin/settings", func(c *gin.Context) {
		flashes := sessions.Default(c).Flashes()
		if flashes == nil {
			flashes = []interface{}{}
		}
		c.JSON(http.StatusOK, gin.H{"flashes": flashes})
	})
	return r
}

// call performs a request against the test router, optionally following
// redirects (matching the http.Client used by the integration tests), and
// returns the final status, the response body, and the Location header of the
// first hop when a redirect was NOT followed.
func call(r *gin.Engine, method, path string, followRedirects bool) (int, string, string) {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if followRedirects && rec.Code == http.StatusFound {
		loc := rec.Header().Get("Location")
		// Serialize the session cookie so the flash (and any other state)
		// follows the redirect, exactly as a browser would.
		var cookieHeader []string
		for _, c := range rec.Result().Cookies() {
			cookieHeader = append(cookieHeader, c.String())
		}
		// A fragment (#...) is never sent to the server in a real browser —
		// strip it before the follow-up request, or httptest.NewRequest treats
		// the hash as part of the path and the route 404s. (The settings-hub
		// redirect target is /admin/settings#billing.)
		target := loc
		if i := strings.IndexByte(target, '#'); i >= 0 {
			target = target[:i]
		}
		req2 := httptest.NewRequest(method, target, nil)
		req2.Header.Set("Cookie", strings.Join(cookieHeader, "; "))
		rec2 := httptest.NewRecorder()
		r.ServeHTTP(rec2, req2)
		return rec2.Code, rec2.Body.String(), loc
	}
	return rec.Code, rec.Body.String(), rec.Header().Get("Location")
}

// TestSafeRedirectPathRejectsBackslash locks in the backslash guard: browsers
// normalize "\" to "/" when resolving a Location header, so a raw or
// URL-encoded backslash in the "next" target must never be accepted — it
// would turn a relative path into an open redirect off-site ("/\evil.com"
// resolves as "//evil.com"). Legitimate in-app paths never contain one.
func TestSafeRedirectPathRejectsBackslash(t *testing.T) {
	cases := []string{
		`/\evil.com`,
		`/\\evil.com`,
		`\\evil.com`,
		`/%5Cevil.com`,
		`/next%5cstep`,
	}
	for _, raw := range cases {
		if got := SafeRedirectPath(raw); got != "" {
			t.Errorf("SafeRedirectPath(%q) = %q, want empty (backslash must terminate the redirect)", raw, got)
		}
	}

	// Plain relative paths are untouched by the guard.
	if got := SafeRedirectPath("/admin/users"); got != "/admin/users" {
		t.Errorf("SafeRedirectPath(/admin/users) = %q, want unchanged", got)
	}
	if got := SafeRedirectPath("/admin/users?page=2"); got != "/admin/users?page=2" {
		t.Errorf("SafeRedirectPath(/admin/users?page=2) = %q, want unchanged", got)
	}
}

// TestFeatureLockRequiredAPIBranch asserts the API/AJAX path: a feature-locked
// account gets a 403 JSON with the lock message — never a redirect — so the
// admin frontend's AJAX calls surface a clean error.
func TestFeatureLockRequiredAPIBranch(t *testing.T) {
	r := newLockTestRouter(true)

	req := httptest.NewRequest(http.MethodGet, "/locked/ping", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var out struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode JSON response: %v (body=%s)", err, rec.Body.String())
	}
	if out.Success {
		t.Error("success = true, want false")
	}
	if !strings.Contains(out.Message, "Masa aktif akun Anda telah habis") {
		t.Errorf("message = %q, want the feature-lock notice", out.Message)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Location = %q, want empty (API must not redirect)", loc)
	}
}

// TestFeatureLockRequiredHTMLRedirectsToBilling covers the browser branch: a
// locked account navigating the admin UI is redirected to
// /admin/settings#billing — the settings hub that replaced the old
// /admin/billing renewal page — and the settings mirror renders the lock
// flash, so the lock is not silent and the owner lands where they can renew.
func TestFeatureLockRequiredHTMLRedirectsToBilling(t *testing.T) {
	r := newLockTestRouter(true)

	// Follow the redirect to the settings hub and read the flash it consumes.
	status, body, loc := call(r, http.MethodGet, "/locked/ping", true)
	if loc != "/admin/settings#billing" {
		t.Errorf("redirect Location = %q, want /admin/settings#billing", loc)
	}
	if status != http.StatusOK {
		t.Fatalf("settings page after redirect: status = %d, want 200", status)
	}
	if !strings.Contains(body, "Masa aktif akun Anda telah habis") {
		t.Errorf("settings page missing lock flash, body=%s", body)
	}
}

// TestFeatureLockRequiredAllowsUnlocked covers the pass-through: an account
// that is not locked (flag absent) is untouched by the middleware.
func TestFeatureLockRequiredAllowsUnlocked(t *testing.T) {
	r := newLockTestRouter(false)
	status, body, loc := call(r, http.MethodGet, "/locked/ping", false)
	if status != http.StatusOK {
		t.Fatalf("unlocked status = %d, want 200", status)
	}
	if !strings.Contains(body, `"success":true`) {
		t.Errorf("unlocked body = %s, want success JSON", body)
	}
	if loc != "" {
		t.Errorf("unlocked Location = %q, want empty", loc)
	}
}

// TestFeatureLockRequiredFlashIsConsumedOnce guards against the flash being
// re-shown on every page view: the settings hub consumes the notice on the
// first load, so a second navigation does not repeat it.
func TestFeatureLockRequiredFlashIsConsumedOnce(t *testing.T) {
	r := newLockTestRouter(true)

	// First navigation: redirect, flash rendered and consumed.
	status, body, _ := call(r, http.MethodGet, "/locked/ping", true)
	if status != http.StatusOK || !strings.Contains(body, "Masa aktif akun Anda telah habis") {
		t.Fatalf("first navigation: status=%d body=%s, want flash on settings", status, body)
	}
	// Fresh navigation to the settings hub directly: no new lock flash.
	status2, body2, _ := call(r, http.MethodGet, "/admin/settings", false)
	if status2 != http.StatusOK {
		t.Fatalf("settings reload: status = %d, want 200", status2)
	}
	if strings.Contains(body2, "Masa aktif akun Anda telah habis") {
		t.Errorf("settings reload still shows lock flash: body=%s (flash must be consumed)", body2)
	}
}
