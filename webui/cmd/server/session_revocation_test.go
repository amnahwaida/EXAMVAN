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
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// M2 — password-change session revocation
//
// The session store is a client-side signed cookie with MaxAge 24h and no
// server-side revocation: after a password RESET the victim's existing cookie
// (e.g. stolen by an attacker) stayed valid until its MaxAge ran out —
// AuthRequired revalidated status/role but NOTHING changed on the row the
// check consults. The intended contract:
//
//   - a successful password reset (POST /reset-password) stamps
//     password_changed_at on the admin_users row;
//   - AuthRequired ends every session issued BEFORE that stamp (legacy
//     sessions without issued_at count as issued at time zero);
//   - a session issued AFTER the stamp keeps working (the legitimate owner
//     re-logging in is not trapped in a logout loop);
//   - no stamp (password_changed_at NULL) revokes nothing.
//
// The reset stamp itself is asserted at the DB level (the OTP/Turnstile
// guards of POST /reset-password are exercised by their own tests); the
// login and admin-probe legs drive the REAL production handlers.
// ---------------------------------------------------------------------------

func setupRevocationDB(t *testing.T) (*pgxpool.Pool, *models.AdminUser, string) {
	t.Helper()
	pool := database.NewPackageTestPool(t, "server")
	ctx := context.Background()

	username := "m2-revocation"
	password := "Password123!"
	// CreateUser hashes the PasswordHash field itself — pass the PLAIN
	// password (a pre-hashed value would be double-hashed and the login
	// below would fail).
	if _, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: username, Name: "M2 Revocation", PasswordHash: password,
		Status: models.UserStatusActive, Instansi: "SMK M2",
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	u, err := models.GetUserByUsername(ctx, pool, username)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return pool, &u, password
}

// newRevocationRouter wires the production login handler (the exact session
// the AuthRequired check consumes), a legacy seam (a login that mimics the
// pre-feature session WITHOUT issued_at), and the admin probe behind the
// real AuthRequired middleware.
func newRevocationRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("db", pool); c.Next() })

	cfg := &config.Config{Version: "test", AdminUser: "superadmin"}
	r.POST("/login", loginHandler(cfg))
	r.POST("/test/legacy-login", func(c *gin.Context) {
		// Pre-feature login: every session key the old build set, EXCEPT
		// issued_at.
		u, err := models.GetUserByUsername(c.Request.Context(), pool, c.PostForm("username"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false})
			return
		}
		s := sessions.Default(c)
		s.Clear()
		s.Set(middleware.SessionKeyAdminID, u.ID)
		s.Set(middleware.SessionKeyUsername, u.Username)
		s.Set(middleware.SessionKeyName, u.Name)
		s.Set(middleware.SessionKeyRole, models.NormalizeSessionRole(u.Role))
		s.Set(middleware.SessionKeyIsSuper, false)
		s.Set(middleware.SessionKeyInstansi, u.Instansi)
		_ = s.Save()
		c.Status(http.StatusOK)
	})
	api := r.Group("/admin/api", middleware.AuthRequired())
	api.GET("/queue/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	return r
}

// revocationClient is one logged-in browser.
type revocationClient struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
}

func newRevocationClient(t *testing.T, srv *httptest.Server) *revocationClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &revocationClient{t: t, srv: srv, client: &http.Client{
		Jar: jar,
		// Do NOT follow redirects: the login 302 points at /admin/dashboard,
		// which the test router does not register — the test asserts the
		// status the HANDLER produced, not the followed redirect's 404.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (rc *revocationClient) postForm(path string, form url.Values) int {
	rc.t.Helper()
	resp, err := rc.client.PostForm(rc.srv.URL+path, form)
	if err != nil {
		rc.t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// login establishes a real session via the production login handler.
func (rc *revocationClient) login(username, password string) {
	rc.t.Helper()
	status := rc.postForm("/login", url.Values{"username": {username}, "password": {password}})
	if status != http.StatusFound {
		rc.t.Fatalf("login: status=%d, want 302", status)
	}
}

// legacyLogin establishes a pre-feature session (no issued_at).
func (rc *revocationClient) legacyLogin(username string) {
	rc.t.Helper()
	status := rc.postForm("/test/legacy-login", url.Values{"username": {username}})
	if status != http.StatusOK {
		rc.t.Fatalf("legacy login: status=%d, want 200", status)
	}
}

// probeAdminAPI returns the status the admin probe yields for the current
// cookie — 200 (session alive) vs 401 (session dead; JSON path of
// AuthRequired).
func (rc *revocationClient) probeAdminAPI() int {
	rc.t.Helper()
	req, err := http.NewRequest(http.MethodGet, rc.srv.URL+"/admin/api/queue/status", nil)
	if err != nil {
		rc.t.Fatalf("build probe: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := rc.client.Do(req)
	if err != nil {
		rc.t.Fatalf("probe: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// stampPasswordChanged writes the revocation anchor exactly the way the reset
// (auth_recovery.go) and change-password (admin/settings.go) handlers do
// after persisting the new hash.
func stampPasswordChanged(t *testing.T, pool *pgxpool.Pool, userID int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE admin_users SET password_changed_at = $1 WHERE id = $2`,
		time.Now().UTC(), userID); err != nil {
		t.Fatalf("stamp password_changed_at: %v", err)
	}
}

// TestResetPasswordRevokesPreResetSessions is the M2 regression: a session
// established BEFORE the password change must die at AuthRequired once the
// change is stamped — the stolen-cookie window collapses from 24h to one
// request.
func TestResetPasswordRevokesPreResetSessions(t *testing.T) {
	pool, user, password := setupRevocationDB(t)

	r := newRevocationRouter(pool)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	victim := newRevocationClient(t, srv)
	victim.login(user.Username, password)
	if got := victim.probeAdminAPI(); got != http.StatusOK {
		t.Fatalf("pre-reset probe: status=%d, want 200 (session alive)", got)
	}

	stampPasswordChanged(t, pool, user.ID)

	if got := victim.probeAdminAPI(); got != http.StatusUnauthorized {
		t.Fatalf("post-reset probe: status=%d, want 401 (M2: pre-reset session must be revoked)", got)
	}
}

// TestSessionAfterPasswordChangeSurvives pins that the legitimate owner is
// NOT trapped in a logout loop: a session established AFTER the stamp keeps
// working.
func TestSessionAfterPasswordChangeSurvives(t *testing.T) {
	pool, user, password := setupRevocationDB(t)

	r := newRevocationRouter(pool)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	stampPasswordChanged(t, pool, user.ID)

	fresh := newRevocationClient(t, srv)
	fresh.login(user.Username, password)
	if got := fresh.probeAdminAPI(); got != http.StatusOK {
		t.Fatalf("post-change login probe: status=%d, want 200 (fresh session must survive)", got)
	}
}

// TestLegacySessionWithoutIssuedAtIsRevoked pins the fail-closed half: a
// cookie issued before this feature (no issued_at in the session) counts as
// issued at time zero, so a recorded password change revokes it too.
func TestLegacySessionWithoutIssuedAtIsRevoked(t *testing.T) {
	pool, user, _ := setupRevocationDB(t)

	r := newRevocationRouter(pool)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	legacy := newRevocationClient(t, srv)
	legacy.legacyLogin(user.Username)
	if got := legacy.probeAdminAPI(); got != http.StatusOK {
		t.Fatalf("legacy pre-stamp probe: status=%d, want 200", got)
	}

	stampPasswordChanged(t, pool, user.ID)
	if got := legacy.probeAdminAPI(); got != http.StatusUnauthorized {
		t.Fatalf("legacy probe: status=%d, want 401 (fail-closed revocation)", got)
	}
}

// TestNoStampRevokesNothing guards the NULL-stamp behaviour: without any
// password change on record, existing sessions keep working.
func TestNoStampRevokesNothing(t *testing.T) {
	pool, user, password := setupRevocationDB(t)

	r := newRevocationRouter(pool)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	c1 := newRevocationClient(t, srv)
	c1.login(user.Username, password)
	if got := c1.probeAdminAPI(); got != http.StatusOK {
		t.Fatalf("probe: status=%d, want 200", got)
	}
}
