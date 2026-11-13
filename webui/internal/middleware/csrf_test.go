package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func newCSRFRouter(t *testing.T) (*gin.Engine, func() *http.Request, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("csrf-test-secret-0123456789abcdef-0123456789"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true})
	r.Use(sessions.Sessions("csrf_test", store))
	// Simulate GenerateCSRFToken running during a page render, then expose it.
	var token string
	r.GET("/setup", func(c *gin.Context) {
		token = GenerateCSRFToken(c)
		c.String(http.StatusOK, "ok")
	})
	r.POST("/action", CSRFRequired(), func(c *gin.Context) {
		c.String(http.StatusOK, "done")
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if w.Code != http.StatusOK || token == "" {
		t.Fatalf("setup failed: code=%d token=%q", w.Code, token)
	}
	// Cookie sesi dari render halaman wajib dibawa ke setiap request POST —
	// tanpa itu token tersimpan kosong dan semua kasus 403 karena alasan yang
	// salah (sesi kosong, bukan token salah).
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("setup did not set a session cookie")
	}
	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/action", nil)
		for _, ck := range cookies {
			req.AddCookie(ck)
		}
		return req
	}
	return r, newReq, token
}

// TestCSRFRequiredAcceptsValidToken pins the three delivery channels the
// frontend uses: X-CSRF-Token header (admin SPA), csrf_token form field
// (public HTML forms) and the legacy _csrf_token alias.
func TestCSRFRequiredAcceptsValidToken(t *testing.T) {
	r, newReq, token := newCSRFRouter(t)

	// Header path.
	w := httptest.NewRecorder()
	req := newReq()
	req.Header.Set("X-CSRF-Token", token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("header token rejected: code=%d body=%s", w.Code, w.Body.String())
	}

	// Form field path (urlencoded via PostForm).
	w = httptest.NewRecorder()
	req = newReq()
	req.Body = io.NopCloser(strings.NewReader("csrf_token=" + token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("form csrf_token rejected: code=%d body=%s", w.Code, w.Body.String())
	}

	// Legacy alias path.
	w = httptest.NewRecorder()
	req = newReq()
	req.Body = io.NopCloser(strings.NewReader("_csrf_token=" + token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("legacy _csrf_token rejected: code=%d body=%s", w.Code, w.Body.String())
	}
}

// TestCSRFRequiredRejectsInvalidToken covers the negative space, including a
// token whose LENGTH differs from the stored one — the constant-time compare
// must reject it (ConstantTimeCompare returns 0 on length mismatch) without
// panicking or short-circuiting into acceptance.
func TestCSRFRequiredRejectsInvalidToken(t *testing.T) {
	r, newReq, token := newCSRFRouter(t)

	cases := []struct {
		name   string
		supply func() *http.Request
	}{
		{"wrong value, same length", func() *http.Request {
			req := newReq()
			req.Header.Set("X-CSRF-Token", strings.Repeat("a", len(token)))
			return req
		}},
		{"shorter token", func() *http.Request {
			req := newReq()
			req.Header.Set("X-CSRF-Token", token[:len(token)-1])
			return req
		}},
		{"longer token", func() *http.Request {
			req := newReq()
			req.Header.Set("X-CSRF-Token", token+"x")
			return req
		}},
		{"empty token", func() *http.Request {
			req := newReq()
			req.Header.Set("X-CSRF-Token", "")
			return req
		}},
		{"no token at all", newReq},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, tc.supply())
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: code=%d want 403", tc.name, w.Code)
		}
	}
}

// TestCSRFRequiredRejectsWhenSessionHasNoToken: without GenerateCSRFToken ever
// having run, the stored token is empty — every POST must be rejected (never
// accepted because provided is empty too; comparing two empties must not pass).
func TestCSRFRequiredRejectsWhenSessionHasNoToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("csrf-test-secret-0123456789abcdef-0123456789"))
	r.Use(sessions.Sessions("csrf_test", store))
	r.POST("/action", CSRFRequired(), func(c *gin.Context) {
		c.String(http.StatusOK, "done")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/action", nil))
	if w.Code != http.StatusForbidden {
		t.Errorf("no stored token: code=%d want 403", w.Code)
	}

	// An attacker sending an empty token against an empty stored token must
	// still be rejected.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/action", nil)
	req.Header.Set("X-CSRF-Token", "")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("empty vs empty: code=%d want 403", w.Code)
	}
}

// TestCSRFRequiredSkipsSafeMethods: GET/HEAD/OPTIONS pass through untouched —
// the session-bearing page render flow depends on it.
func TestCSRFRequiredSkipsSafeMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("csrf-test-secret-0123456789abcdef-0123456789"))
	r.Use(sessions.Sessions("csrf_test", store))
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		called := false
		switch m {
		case http.MethodGet:
			r.GET("/safe", CSRFRequired(), func(c *gin.Context) { called = true; c.Status(http.StatusOK) })
		case http.MethodHead:
			r.HEAD("/safe", CSRFRequired(), func(c *gin.Context) { called = true; c.Status(http.StatusOK) })
		case http.MethodOptions:
			r.OPTIONS("/safe", CSRFRequired(), func(c *gin.Context) { called = true; c.Status(http.StatusOK) })
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(m, "/safe", nil))
		if w.Code != http.StatusOK || !called {
			t.Errorf("%s: code=%d called=%v — safe method harus lolos tanpa token", m, w.Code, called)
		}
	}
}
