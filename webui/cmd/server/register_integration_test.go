package main

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

// newRegisterTestRouter builds a Gin router exposing the real public
// registration endpoint (POST /register → registerPostHandler) against the
// test pool, mirroring production wiring (session store + DB injection).
// CSRF and rate-limit middleware are deliberately omitted: they are orthogonal
// to the expiry grant under test and are exercised by their own middleware
// tests. Only POST is wired — the success path never renders a template (it
// redirects to /login), so no template loading is needed here, and the GET
// page (which renders public/register.html) is out of scope.
func newRegisterTestRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("db", pool); c.Next() })

	cfg := &config.Config{Version: "test", AdminUser: "superadmin"}
	r.POST("/register", registerPostHandler(cfg))
	return r
}

// registerViaHTTP drives the real POST /register endpoint and returns the
// HTTP status and Location header without following the redirect, so the test
// can assert the browser-visible outcome (302 → /login) exactly.
func registerViaHTTP(t *testing.T, srv *httptest.Server, username string) (int, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	form := url.Values{
		"username": {username},
		"email":    {username + "@example.com"},
		"password": {"Password123!"},
	}
	resp, err := client.PostForm(srv.URL+"/register", form)
	if err != nil {
		t.Fatalf("POST /register (%s): %v", username, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, resp.Header.Get("Location")
}

// mustGetUserByUsername fetches an admin user by username, failing the test on
// error.
func mustGetUserByUsername(t *testing.T, pool *pgxpool.Pool, username string) models.AdminUser {
	t.Helper()
	u, err := models.GetUserByUsername(context.Background(), pool, username)
	if err != nil {
		t.Fatalf("get user %s: %v", username, err)
	}
	return u
}

// assertExpiryIn locks in that expires_at is now + wantDays (the trial
// period), within a generous ±10 minute window that absorbs the sub-second gap
// between the handler's time.Now() and this assertion while remaining far
// below any real regression (1 day vs 14, or no expiry at all).
func assertExpiryIn(t *testing.T, label string, expiresAt time.Time, wantDays int) {
	t.Helper()
	now := time.Now().UTC()
	lo := now.AddDate(0, 0, wantDays).Add(-10 * time.Minute)
	hi := now.AddDate(0, 0, wantDays).Add(10 * time.Minute)
	if expiresAt.Before(lo) || expiresAt.After(hi) {
		t.Errorf("%s: expires_at=%v, want ≈ now+%dd (within ±10m); actual delta from now = %v",
			label, expiresAt, wantDays, expiresAt.Sub(now))
	}
}

// TestRegisterEndpointGrantsDefaultActiveDays locks in the trial policy at the
// HTTP layer: POST /register creates an account whose expires_at is
// now + default_active_days — first with the default (14 days), then with a
// custom value stored in saas_settings (5 days) — proving the endpoint reads
// the setting rather than a hardcoded constant, and that a registered account
// is 'active' (no email-verification gate when the feature is off).
//
// It drives the real registerPostHandler through the real session middleware
// and a real PostgreSQL (skips when TEST_DATABASE_URL is unset):
//
//	TEST_DATABASE_URL=postgresql://user:pass@localhost:5432/examvan_test \
//	  go test ./cmd/server/ -run TestRegisterEndpointGrantsDefaultActiveDays -v
func TestRegisterEndpointGrantsDefaultActiveDays(t *testing.T) {
	pool := database.NewPackageTestPool(t, "server")
	ctx := context.Background()

	// Deterministic SaaS state: the test schema keeps seeded settings rows
	// across runs (TruncateDataTables preserves saas_settings), so pin every
	// setting the register flow consults — email verification off, Turnstile
	// off, a per-IP cap high enough for the two registrations below, and the
	// expiry default back to 14 before each case.
	for _, kv := range [][2]string{
		{models.SettingEmailVerificationEnabled, "0"},
		{models.SettingTurnstileEnabled, "0"},
		{models.SettingMaxAccountsPerIP, "3"},
		{models.SettingDefaultActiveDays, "14"},
	} {
		if err := models.SetSaasSetting(ctx, pool, kv[0], kv[1]); err != nil {
			t.Fatalf("set %s=%s: %v", kv[0], kv[1], err)
		}
	}

	srv := httptest.NewServer(newRegisterTestRouter(pool))
	t.Cleanup(srv.Close)

	t.Run("default grants 14 days and activates the account", func(t *testing.T) {
		status, location := registerViaHTTP(t, srv, "regit1")
		if status != http.StatusFound {
			t.Fatalf("register regit1: status=%d, want 302 (redirect to login)", status)
		}
		if location != "/login" {
			t.Errorf("register regit1: Location=%q, want \"/login\"", location)
		}
		u := mustGetUserByUsername(t, pool, "regit1")
		if u.Status != models.UserStatusActive {
			t.Errorf("regit1 status=%s, want active (email verification disabled)", u.Status)
		}
		if u.ExpiresAt == nil {
			t.Fatal("regit1 expires_at = nil, want now + 14 days")
		}
		assertExpiryIn(t, "regit1", *u.ExpiresAt, 14)
	})

	t.Run("stored setting override: 5 days", func(t *testing.T) {
		// The endpoint must read default_active_days from the DB — a stored
		// value must win over the code default (14).
		if err := models.SetSaasSetting(ctx, pool, models.SettingDefaultActiveDays, "5"); err != nil {
			t.Fatalf("set default_active_days=5: %v", err)
		}
		status, location := registerViaHTTP(t, srv, "regit2")
		if status != http.StatusFound {
			t.Fatalf("register regit2: status=%d, want 302 (redirect to login)", status)
		}
		if location != "/login" {
			t.Errorf("register regit2: Location=%q, want \"/login\"", location)
		}
		u := mustGetUserByUsername(t, pool, "regit2")
		if u.ExpiresAt == nil {
			t.Fatal("regit2 expires_at = nil, want now + 5 days")
		}
		assertExpiryIn(t, "regit2", *u.ExpiresAt, 5)
	})
}
