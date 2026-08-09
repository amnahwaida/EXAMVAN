package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Shared integration-test infrastructure (see setupVoucherITDB for how the
// database is selected: TEST_DATABASE_URL, skipped when unset).
// ---------------------------------------------------------------------------

// setupVoucherITDB returns a pool for this package's DB-backed tests, scoped
// to the package's own PostgreSQL schema ("it_admin" — derived from the
// package name by database.NewPackageTestPool), so `go test ./...` can run
// this package and internal/models in parallel: each package TRUNCATEs only
// the tables in its own schema, so no AccessExclusiveLock is ever shared.
// Skips (not fails) when TEST_DATABASE_URL is unset.
func setupVoucherITDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return database.NewPackageTestPool(t, "admin")
}

// createTestVoucher helpers build the voucher rows the tests share: a sekolah
// package (grants the operator role, small 2-account sub-account quota so the
// max_users enforcement is exercised without creating ten accounts) and a
// plain guru package (grants no role).
func createSchoolVoucher(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH")
}

// createSchoolVoucherCode creates a sekolah package voucher (grants the
// operator role, 2-account sub-account quota) under a caller-chosen code, so a
// test can give a sub-account its OWN single-use school voucher distinct from
// the operator's.
func createSchoolVoucherCode(t *testing.T, pool *pgxpool.Pool, code string) {
	t.Helper()
	if _, err := models.CreateVoucher(context.Background(), pool, &models.Voucher{
		Code: code, Package: "sekolah-test", DurationType: "bulanan",
		MaxUsage: 1, IsActive: true,
		IsCustom: true, CustomLabel: "sekolah-test",
		CustomMaxExams: 3, CustomMaxPDFSize: 50 * 1024 * 1024,
		CustomMaxConcurrentExams: 3, CustomMaxStorageSize: 500 * 1024 * 1024,
		CustomMaxUsers: 2, CustomRole: models.SerializeRoles([]string{models.RoleOperator}),
	}); err != nil {
		t.Fatalf("create school voucher %s: %v", code, err)
	}
}

func createGuruVoucher(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	createGuruVoucherCode(t, pool, "IT-GURU")
}

// createGuruVoucherCode creates a plain guru voucher (grants no role) under a
// caller-chosen code, so a test can give a sub-account its OWN single-use
// voucher distinct from the operator's.
func createGuruVoucherCode(t *testing.T, pool *pgxpool.Pool, code string) {
	t.Helper()
	if _, err := models.CreateVoucher(context.Background(), pool, &models.Voucher{
		Code: code, Package: "guru", DurationType: "bulanan",
		MaxUsage: 1, IsActive: true,
	}); err != nil {
		t.Fatalf("create guru voucher %s: %v", code, err)
	}
}

// createOperatorUser creates a plain-guru account with a school instansi.
func createOperatorUser(t *testing.T, pool *pgxpool.Pool, username, instansi, password string) models.AdminUser {
	t.Helper()
	op, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: username, Name: username,
		PasswordHash: password, Status: models.UserStatusActive,
		Instansi: instansi,
		Role:     models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create operator %s: %v", username, err)
	}
	return *op
}

// expireActivePackage simulates the ACTIVE package's lifetime running out: the
// active redemption is zeroed with its clock already elapsed, and the account
// expiry is pushed into the past (the authoritative login clock would read the
// same once the package period ended).
func expireActivePackage(t *testing.T, pool *pgxpool.Pool, userID int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE voucher_redemptions
		SET remaining_seconds = 0, activated_at = now() - interval '2 seconds'
		WHERE user_id = $1 AND is_active`, userID); err != nil {
		t.Fatalf("expire active redemption: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, userID); err != nil {
		t.Fatalf("expire account: %v", err)
	}
}

// newVoucherTestRouter builds the Gin router used by the voucher integration
// tests, mirroring production wiring (sessions → db → AuthRequired →
// AdminManagementRequired) plus a /test/login/:id seam that opens a real
// session for a user, mirroring the login handler (same session keys
// AuthRequired reads).
func newVoucherTestRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("db", pool) })

	// /login mirror of the real login page: renders (and consumes) pending
	// flash messages as JSON so tests can assert the forced-logout reason is
	// shown after AuthRequired redirects an HTML request.
	r.GET("/login", func(c *gin.Context) {
		flashes := sessions.Default(c).Flashes()
		if flashes == nil {
			flashes = []interface{}{}
		}
		c.JSON(http.StatusOK, gin.H{"flashes": flashes})
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

	// /admin/billing mirror of the real billing page: renders (and consumes)
	// pending flash messages as JSON so tests can assert the feature-lock
	// notice is shown after FeatureLockRequired redirects an HTML request.
	r.GET("/admin/billing", func(c *gin.Context) {
		flashes := sessions.Default(c).Flashes()
		if flashes == nil {
			flashes = []interface{}{}
		}
		c.JSON(http.StatusOK, gin.H{"flashes": flashes})
	})

	// Billing-exempt endpoints: a feature-locked (expired) account may still
	// reach these — redeem/activate keep working so the owner can renew.
	billingAPI := r.Group("/api")
	billingAPI.Use(middleware.AuthRequired())
	billingAPI.POST("/vouchers/redeem", RedeemVoucherHandler())
	billingAPI.POST("/vouchers/activate", ActivateVoucherHandler())
	// Billing probe: answers 200 for any valid session (even a feature-locked
	// one) — the analogue of GET /admin/api/vouchers/mine in production.
	billingAPI.GET("/billing-ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	// The real billing display endpoint: the active package's "Sisa Masa
	// Aktif" badge is rendered from this response, so the freeze-clock
	// display test drives it directly (not just the billing-ping probe).
	billingAPI.GET("/vouchers/mine", ListMyRedemptionsHandler())

	// Feature-gated endpoints: mirror production wiring (AuthRequired →
	// FeatureLockRequired → AdminManagementRequired).
	api := r.Group("/api")
	api.Use(middleware.AuthRequired())
	api.Use(middleware.FeatureLockRequired())
	api.POST("/users", middleware.AdminManagementRequired(), CreateUser())
	api.POST("/users/:user_id/toggle-status", middleware.AdminManagementRequired(), ToggleUserStatus())
	api.POST("/users/:user_id/edit", middleware.AdminManagementRequired(), EditUser())
	api.POST("/users/:user_id/verify", middleware.AdminManagementRequired(), VerifyUser())
	// GET /users mirror of production GET /admin/api/users (AuthRequired →
	// FeatureLockRequired → AdminManagementRequired): the Kelola Users page
	// list endpoint. Tests assert the JSON carries operator_created so the
	// page can render the "Dibuat oleh Operator" badge on sub-accounts.
	api.GET("/users", middleware.AdminManagementRequired(), ListUsers())
	// AuthRequired/FeatureLockRequired probe: a protected GET that answers 200
	// only when a valid, non-locked session passes both middlewares — used to
	// observe per-request status and feature-lock enforcement without depending
	// on a handler's own logic.
	api.GET("/auth-ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	return r
}

// voucherTestClient is an HTTP client bound to a test router, carrying a
// session cookie jar, with small wrappers for the endpoints under test.
type voucherTestClient struct {
	srv    *httptest.Server
	client *http.Client
}

func newVoucherTestClient(t *testing.T, pool *pgxpool.Pool) *voucherTestClient {
	t.Helper()
	srv := httptest.NewServer(newVoucherTestRouter(pool))
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &voucherTestClient{srv: srv, client: &http.Client{Jar: jar}}
}

func (tc *voucherTestClient) login(t *testing.T, userID int) {
	t.Helper()
	status, resp := postForm(t, tc.client, tc.srv, "/test/login/"+strconv.Itoa(userID), nil)
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("test login as user %d: status=%d resp=%+v", userID, status, resp)
	}
}

func (tc *voucherTestClient) redeem(t *testing.T, code string) {
	t.Helper()
	status, resp := postForm(t, tc.client, tc.srv, "/api/vouchers/redeem", url.Values{"code": {code}})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("redeem %s: status=%d resp=%+v", code, status, resp)
	}
}

func (tc *voucherTestClient) activate(t *testing.T, redemptionID int) {
	t.Helper()
	status, resp := postForm(t, tc.client, tc.srv, "/api/vouchers/activate",
		url.Values{"redemption_id": {strconv.Itoa(redemptionID)}})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("activate redemption %d: status=%d resp=%+v", redemptionID, status, resp)
	}
}

// createUser posts a create-user request as the currently-logged-in user and
// returns the HTTP status and parsed response.
func (tc *voucherTestClient) createUser(t *testing.T, username string) (int, apiResp) {
	t.Helper()
	return postJSON(t, tc.client, tc.srv, "/api/users", map[string]interface{}{
		"username": username,
		"password": "pass-" + username,
		"name":     username,
		"roles":    []string{models.RoleGuru},
	})
}

type apiResp struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Body    string `json:"-"` // raw response body, kept for failure diagnostics
}

func postForm(t *testing.T, client *http.Client, srv *httptest.Server, path string, form url.Values) (int, apiResp) {
	t.Helper()
	resp, err := client.PostForm(srv.URL+path, form)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out apiResp
	_ = json.Unmarshal(body, &out)
	out.Body = string(body)
	return resp.StatusCode, out
}

func postJSON(t *testing.T, client *http.Client, srv *httptest.Server, path string, payload map[string]interface{}) (int, apiResp) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %v: %v", payload, err)
	}
	resp, err := client.Post(srv.URL+path, "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out apiResp
	_ = json.Unmarshal(body, &out)
	out.Body = string(body)
	return resp.StatusCode, out
}

// mustGetUser fetches an admin user by username, failing the test on error.
func mustGetUser(t *testing.T, pool *pgxpool.Pool, username string) models.AdminUser {
	t.Helper()
	u, err := models.GetUserByUsername(context.Background(), pool, username)
	if err != nil {
		t.Fatalf("get user %s: %v", username, err)
	}
	return u
}

// subFlags loads the status / suspended_by_cascade / suspended_at columns of
// an admin_users row (fields not part of the AdminUser model).
func subFlags(t *testing.T, pool *pgxpool.Pool, id int) (status string, cascade bool, suspendedAt *time.Time) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT status, COALESCE(suspended_by_cascade, FALSE), suspended_at FROM admin_users WHERE id=$1`,
		id).Scan(&status, &cascade, &suspendedAt); err != nil {
		t.Fatalf("load sub flags for user %d: %v", id, err)
	}
	return status, cascade, suspendedAt
}

// pinSuspendedState deterministically suspends an account with the given clock
// (Go clock, stored verbatim — the freeze arithmetic now.Sub(suspended_at)
// then runs on a single clock, immune to host/container skew): status
// 'suspended', optionally the cascade marker (a real cascade suspension sets
// suspended_by_cascade=TRUE, which the verify tests deliberately simulate),
// suspended_at and expires_at as passed. The expiry is preserved as-is, so a
// caller passes a future value for a FreezeClock test or a past one for a
// RenewExpiry test.
func pinSuspendedState(t *testing.T, pool *pgxpool.Pool, id int, cascade bool, suspendedAt, expiresAt time.Time) {
	t.Helper()
	cascadeSQL := "FALSE"
	if cascade {
		cascadeSQL = "TRUE"
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE admin_users
		SET status = 'suspended', suspended_by_cascade = `+cascadeSQL+`,
		    suspended_at = $2, expires_at = $3
		WHERE id = $1`, id, suspendedAt, expiresAt); err != nil {
		t.Fatalf("pin suspended state for user %d: %v", id, err)
	}
}

// pinSuspendedClock pins only the suspension clock of an ALREADY-suspended
// account (sub-account of a cascade: status and suspended_by_cascade stay as
// the cascade left them, only the stamps are made deterministic). Same Go-clock
// semantics as pinSuspendedState.
func pinSuspendedClock(t *testing.T, pool *pgxpool.Pool, id int, suspendedAt, expiresAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE admin_users
		SET suspended_at = $2,
		    expires_at = $3
		WHERE id = $1`, id, suspendedAt, expiresAt); err != nil {
		t.Fatalf("pin suspension clock for user %d: %v", id, err)
	}
}

func approxEqual(a, b time.Time, tol time.Duration) bool {
	d := a.Sub(b)
	return d >= -tol && d <= tol
}

// assertFrozenExpiryDelta verifies the freeze-clock expiry arithmetic shared
// by every freeze test (with or without a redemption): expires_at must move
// forward by ~the pinned suspension duration (2h). A symmetric tolerance
// absorbs the sub-second gap between the deterministic pin and the
// reactivation (both on the Go clock).
func assertFrozenExpiryDelta(t *testing.T, label string, expiresBefore, expiresAfter time.Time) {
	t.Helper()
	const tol = 30 * time.Second
	if delta := expiresAfter.Sub(expiresBefore); delta < 2*time.Hour-tol || delta > 2*time.Hour+tol {
		t.Errorf("%s: expires_at extended by %v, want ~%v (the suspension duration)", label, delta, 2*time.Hour)
	}
}

// assertFrozenRedemptionSync verifies the freeze-clock redemption realignment
// shared by every reactivation path (toggle single-user, toggle cascade
// restore, EditUser form, voucher-switch restore): the account expiry moved
// forward by the pinned suspension duration (~2h, see assertFrozenExpiryDelta),
// and the ACTIVE package's clock followed it — the stored remaining_seconds
// equals (new expires_at − activated_at) written by the shared sync statement
// (a DB-internal exact invariant: Postgres computes EXTRACT(EPOCH) as float8
// and the float8→bigint cast rounds to nearest, so [floor, floor+1] is
// allowed), the stored remaining gained back the suspension period, and
// activated_at restarted.
func assertFrozenRedemptionSync(t *testing.T, label string, expiresBefore time.Time, remainingBefore int64, activatedBefore time.Time, expiresAfter *time.Time, remainingAfter int64, activatedAfter time.Time) {
	t.Helper()
	if expiresAfter == nil {
		t.Fatalf("%s: expires_at = nil after freeze reactivation", label)
	}
	assertFrozenExpiryDelta(t, label, expiresBefore, *expiresAfter)
	want := int64(expiresAfter.Sub(activatedAfter) / time.Second)
	if remainingAfter < want || remainingAfter > want+1 {
		t.Errorf("%s: remaining_seconds after freeze = %d, want %d or %d (= new expires_at − activated_at: the frozen remaining)",
			label, remainingAfter, want, want+1)
	}
	if remainingAfter <= remainingBefore {
		t.Errorf("%s: remaining_seconds %d -> %d after freeze reactivation, want increased (suspension duration added back)",
			label, remainingBefore, remainingAfter)
	}
	if !activatedAfter.After(activatedBefore) {
		t.Errorf("%s: activated_at = %v, want reset after the pre-reactivation %v (clock restarted)",
			label, activatedAfter, activatedBefore)
	}
}

// billingDisplayItem is one redemption item of the /admin/api/vouchers/mine
// payload. The billing display tests decode the response directly into this
// type (it carries the JSON tags), so the shared display helper asserts on the
// exact same type the tests parse.
type billingDisplayItem struct {
	ID               int    `json:"id"`
	Package          string `json:"package"`
	RemainingSeconds int64  `json:"remaining_seconds"`
	IsActive         bool   `json:"is_active"`
	IsExpired        bool   `json:"is_expired"`
	IsUnlimited      bool   `json:"is_unlimited"`
}

// assertBillingDisplay locks in the shared billing-display core used by every
// freeze/renewal display test: given the payload's ACTIVE package item, the
// account's (authoritative) expires_at the display is derived from, the
// realigned stored remaining_seconds, and the redemption's expected identity
// (wantID/wantPackage — read straight from the DB by the caller), it asserts
// the item is rendered as an active, finite package (not expired, not
// unlimited) that IS the expected redemption (id/package match — billing.html
// keys on id, labels on package), and that the displayed remaining_seconds (a)
// matches expires_at − now on the Go clock (±60s — only the sub-second
// request-to-read gap) and (b) agrees with the realigned stored clock (±120s —
// the stored value was written by the DB's now() while the display uses the Go
// clock, so any host/container skew shifts the cross-check by that amount).
// The failure difference that matters — a missing freeze, a skipped sync, a
// stale display, a wrong redemption — is far beyond both tolerances. Returns
// the item so the caller adds its context-specific sanity bound (frozen > 30d,
// renewed ≈ 1d) on top.
func assertBillingDisplay(t *testing.T, label, why string, got time.Time, storedRemaining int64, wantID int, wantPackage string, item billingDisplayItem) billingDisplayItem {
	t.Helper()
	if item.IsExpired {
		t.Errorf("%s: active package rendered as expired, want active (%s)", label, why)
	}
	if item.IsUnlimited {
		t.Errorf("%s: active package rendered as unlimited, want finite remaining", label)
	}

	// The display must show the post-reactivation remaining (computed from the
	// authoritative expires_at at request time): the sub-second request-to-read
	// gap is absorbed by the tolerance — the difference that matters, a
	// missing freeze/renewal or a stale display, is far beyond it.
	now := time.Now().UTC()
	want := int64(got.Sub(now) / time.Second)
	const tolGo = 60 // both sides on the Go clock: only the request-to-read gap
	if d := item.RemainingSeconds - want; d < -tolGo || d > tolGo {
		t.Errorf("%s: display remaining_seconds=%d, want ≈ %d (expires_at − now); gap=%d",
			label, item.RemainingSeconds, want, d)
	}

	// The displayed value must agree with the realigned stored clock: both
	// derive from the same account expiry, so a display that contradicts the
	// stored remaining_seconds (or a sync that was skipped, leaving the stored
	// clock at the stale pre-reactivation value) fails by the whole delta. The
	// tolerance is looser than the Go-only check because the stored value was
	// written by the DB's now() while the display uses the Go clock (the same
	// ±120s absorption as the sibling cascade test).
	const tolDB = 120
	if d := item.RemainingSeconds - storedRemaining; d < -tolDB || d > tolDB {
		t.Errorf("%s: display remaining_seconds=%d vs realigned stored=%d, want within ±%ds (same account clock)",
			label, item.RemainingSeconds, storedRemaining, tolDB)
	}

	// The rendered item must be the SAME redemption the display derives from:
	// its identity fields (billing.html keys on id, labels on package) must
	// match the expected redemption read from the DB — not just the remaining
	// lifetime.
	if item.ID != wantID {
		t.Errorf("%s: display redemption id=%d, want %d (the active redemption)", label, item.ID, wantID)
	}
	if item.Package != wantPackage {
		t.Errorf("%s: display package=%q, want %q (the active redemption's package key)", label, item.Package, wantPackage)
	}
	return item
}

// billingDisplayPayload is the decoded response of GET /admin/api/vouchers/mine
// (the endpoint that backs billing.html's "Paket Aktif" / "Sisa Masa Aktif").
type billingDisplayPayload struct {
	Success     bool                 `json:"success"`
	Redemptions []billingDisplayItem `json:"redemptions"`
}

// fetchBillingDisplay drives the real billing display endpoint as the
// currently-logged-in user and decodes the response exactly as billing.html
// would render it. Returns the payload and the index of the ACTIVE package
// item (failing the test when there is none). The caller then asserts on the
// item (e.g. via assertBillingDisplay) plus its context-specific sanity bound.
func fetchBillingDisplay(t *testing.T, tc *voucherTestClient) (billingDisplayPayload, int) {
	t.Helper()
	resp, err := tc.client.Get(tc.srv.URL + "/api/vouchers/mine")
	if err != nil {
		t.Fatalf("GET /api/vouchers/mine: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/vouchers/mine: status=%d", resp.StatusCode)
	}
	var payload billingDisplayPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /api/vouchers/mine: %v", err)
	}
	if !payload.Success {
		t.Fatal("payload success=false")
	}
	activeIdx := -1
	for i := range payload.Redemptions {
		if payload.Redemptions[i].IsActive {
			activeIdx = i
			break
		}
	}
	if activeIdx == -1 {
		t.Fatal("no active package in the billing display")
	}
	return payload, activeIdx
}

// ---------------------------------------------------------------------------
// Test 1: manual sekolah → guru → sekolah switch
// ---------------------------------------------------------------------------

// TestVoucherLifecycleOperatorAccountsSuspendRestore is an end-to-end
// integration test (requires a real PostgreSQL) covering the school-operator
// lifecycle that the voucher system is built around:
//
//	operator redeems a sekolah voucher → becomes operator and creates guru
//	accounts in the school → switches to a guru voucher → the accounts are
//	suspended (login blocked, management access revoked) → reactivates the
//	sekolah package → the accounts are restored, their clocks frozen for the
//	suspension period.
//
// The test drives the real HTTP handlers (CreateUser, RedeemVoucherHandler,
// ActivateVoucherHandler) through the real auth middleware and session store,
// so it exercises the whole wiring — role-follows-package, the instansi
// cascade, the max_users quota, and session role sync — not just the SQL.
//
// It skips (rather than fails) when TEST_DATABASE_URL is unset, so the plain
// `go test ./...` in CI (no Postgres) keeps passing. Point TEST_DATABASE_URL
// at an empty disposable database; the schema is applied and the data tables
// wiped at startup:
//
//	TEST_DATABASE_URL=postgresql://user:pass@localhost:5432/examvan_test \
//	  go test ./internal/handlers/admin/ -run TestVoucherLifecycle -v
func TestVoucherLifecycleOperatorAccountsSuspendRestore(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucher(t, pool)
	op := createOperatorUser(t, pool, "op1", "SMAN 1 Test", "pass-op1")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)

	// --- Redeem the sekolah voucher → operator --------------------------------
	tc.redeem(t, "IT-SEKOLAH")
	opAfter := mustGetUser(t, pool, "op1")
	if !models.HasRole(opAfter.Role, models.RoleOperator) {
		t.Fatalf("after sekolah redeem: op role=%s, want operator", opAfter.Role)
	}
	if opAfter.Package != "sekolah-test" {
		t.Errorf("after sekolah redeem: op package=%s, want sekolah-test", opAfter.Package)
	}
	// Role follows the active package: the operator grant is tracked in
	// package_role only, base_role stays [guru].
	var pkgRole, baseRole string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(package_role,''), COALESCE(base_role,'') FROM admin_users WHERE id=$1`, opAfter.ID).Scan(&pkgRole, &baseRole); err != nil {
		t.Fatalf("load op role columns: %v", err)
	}
	if want := models.SerializeRoles([]string{models.RoleOperator}); pkgRole != want {
		t.Errorf("after sekolah redeem: package_role=%s, want %s", pkgRole, want)
	}
	if want := models.SerializeRoles([]string{models.RoleGuru}); baseRole != want {
		t.Errorf("after sekolah redeem: base_role=%s, want %s", baseRole, want)
	}
	// The quota snapshot is captured at redeem time.
	var maxUsers int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(max_users,0) FROM voucher_redemptions WHERE user_id=$1 AND is_active`, opAfter.ID).Scan(&maxUsers); err != nil {
		t.Fatalf("load redemption max_users: %v", err)
	}
	if maxUsers != 2 {
		t.Errorf("school redemption max_users=%d, want 2", maxUsers)
	}
	if gotMax, gotUsed := loadOperatorAccountQuota(ctx, pool, opAfter.ID, true, opAfter.Instansi); gotMax != 2 || gotUsed != 0 {
		t.Errorf("loadOperatorAccountQuota after redeem = (%d,%d), want (2,0)", gotMax, gotUsed)
	}

	// --- Operator creates guru accounts in the school ---------------------------
	for _, name := range []string{"guru1", "guru2"} {
		if status, resp := tc.createUser(t, name); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", name, status, resp)
		}
	}
	// Accounts inherit the operator's instansi and expiry.
	for _, name := range []string{"guru1", "guru2"} {
		sub := mustGetUser(t, pool, name)
		if sub.Status != models.UserStatusActive {
			t.Errorf("%s: status=%s, want active", name, sub.Status)
		}
		if sub.Instansi != opAfter.Instansi {
			t.Errorf("%s: instansi=%q, want %q", name, sub.Instansi, opAfter.Instansi)
		}
		if sub.ExpiresAt == nil || !approxEqual(*sub.ExpiresAt, *opAfter.ExpiresAt, time.Minute) {
			t.Errorf("%s: expires_at=%v, want ≈ operator's %v", name, sub.ExpiresAt, opAfter.ExpiresAt)
		}
	}
	// Quota display (billing page) now reports 2/2.
	if gotMax, gotUsed := loadOperatorAccountQuota(ctx, pool, opAfter.ID, true, opAfter.Instansi); gotMax != 2 || gotUsed != 2 {
		t.Errorf("loadOperatorAccountQuota after 2 accounts = (%d,%d), want (2,2)", gotMax, gotUsed)
	}
	// A third account is blocked by the max_users quota.
	if status, resp := tc.createUser(t, "guru3"); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Kuota akun") {
		t.Errorf("create guru3: status=%d resp=%+v, want 400 quota message", status, resp)
	}
	subsBefore := []models.AdminUser{
		mustGetUser(t, pool, "guru1"),
		mustGetUser(t, pool, "guru2"),
	}

	// --- Switch to the guru voucher → accounts suspended ------------------------
	tc.redeem(t, "IT-GURU")
	opGuru := mustGetUser(t, pool, "op1")
	if models.HasRole(opGuru.Role, models.RoleOperator) {
		t.Errorf("after guru switch: op role=%s, operator must be gone", opGuru.Role)
	}
	if opGuru.Status != models.UserStatusActive {
		t.Errorf("after guru switch: operator status=%s, must stay active", opGuru.Status)
	}
	for i, sub := range subsBefore {
		now := mustGetUser(t, pool, sub.Username)
		status, cascade, _ := subFlags(t, pool, now.ID)
		if status != models.UserStatusSuspended {
			t.Errorf("%s: status=%s, want suspended", sub.Username, status)
		}
		if !cascade {
			t.Errorf("%s: suspended_by_cascade=false, want true", sub.Username)
		}
		subsBefore[i] = now // remember the frozen expiry for the restore check
	}
	// Suspended accounts can no longer authenticate.
	if _, msg := models.AuthenticateUser(ctx, pool, "guru1", "pass-guru1"); !strings.Contains(msg, "dinonaktifkan") {
		t.Errorf("login after suspend: msg=%q, want suspension message", msg)
	}
	// Management access (AdminManagementRequired) is revoked together with the
	// operator role: user creation must now be forbidden.
	if status, _ := tc.createUser(t, "guru3"); status != http.StatusForbidden {
		t.Errorf("create guru3 after switch: status=%d, want 403", status)
	}

	// --- Back to the sekolah package → accounts restored ------------------------
	var schoolRedemptionID int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test' ORDER BY id DESC LIMIT 1`,
		opGuru.ID).Scan(&schoolRedemptionID); err != nil {
		t.Fatalf("find school redemption: %v", err)
	}
	tc.activate(t, schoolRedemptionID)
	opRestored := mustGetUser(t, pool, "op1")
	if !models.HasRole(opRestored.Role, models.RoleOperator) {
		t.Errorf("after sekolah reactivate: op role=%s, want operator back", opRestored.Role)
	}
	for _, sub := range subsBefore {
		now := mustGetUser(t, pool, sub.Username)
		status, cascade, suspendedAt := subFlags(t, pool, now.ID)
		if status != models.UserStatusActive {
			t.Errorf("%s: status=%s, want active again", sub.Username, status)
		}
		if cascade {
			t.Errorf("%s: suspended_by_cascade=true, want false after restore", sub.Username)
		}
		if suspendedAt != nil {
			t.Errorf("%s: suspended_at=%v, want NULL after restore", sub.Username, suspendedAt)
		}
		// Clock freeze: the suspension period is added back to the expiry —
		// the account's lifetime must not burn while locked out (the freeze is
		// >= 0 by construction, so assert "not before" rather than "strictly
		// after" to match the semantics exactly).
		if now.ExpiresAt == nil || sub.ExpiresAt == nil || now.ExpiresAt.Before(*sub.ExpiresAt) {
			t.Errorf("%s: restored expiry=%v must not be before suspended expiry=%v", sub.Username, now.ExpiresAt, sub.ExpiresAt)
		}
		// Restored accounts are realigned to the operator's school expiry.
		if now.ExpiresAt == nil || !approxEqual(*now.ExpiresAt, *opRestored.ExpiresAt, time.Minute) {
			t.Errorf("%s: restored expiry=%v, want ≈ operator's %v", sub.Username, now.ExpiresAt, opRestored.ExpiresAt)
		}
	}
	// Login works again for the restored accounts.
	if _, msg := models.AuthenticateUser(ctx, pool, "guru1", "pass-guru1"); msg != "" {
		t.Errorf("login after restore: msg=%q, want success", msg)
	}
	// Management access is back (operator again): the create attempt now
	// reaches the quota check (400) instead of the plain-guru 403.
	if status, resp := tc.createUser(t, "guru3"); status != http.StatusBadRequest || !strings.Contains(resp.Message, "Kuota akun") {
		t.Errorf("create guru3 after restore: status=%d resp=%+v, want 400 quota (management access restored)", status, resp)
	}
}

// ---------------------------------------------------------------------------
// Test 2: expiry job auto-fallback
// ---------------------------------------------------------------------------

// TestVoucherExpiryAutoFallbackSuspendsSubAccounts covers the background
// package-expiry job's auto-fallback path: a school operator whose ACTIVE
// sekolah package runs out of lifetime while holding a claimed-but-paused guru
// voucher must never be locked out. The job (runPackageExpiryPass →
// handleExpiredPackage) pauses the exhausted package, auto-activates the guru
// fallback via applyRedemptionEntitlement — which, like a manual switch,
// reconciles the instansi: the guru package grants no operator role, so the
// sub-accounts are cascade-suspended.
func TestVoucherExpiryAutoFallbackSuspendsSubAccounts(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucher(t, pool)
	op := createOperatorUser(t, pool, "op2", "SMP 2 Test", "pass-op2")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)

	// Claim both vouchers: redeem guru first (active), then sekolah (pauses
	// guru, grants the operator role). The guru redemption is now the
	// claimed-but-paused fallback the expiry job will pick up.
	tc.redeem(t, "IT-GURU")
	tc.redeem(t, "IT-SEKOLAH")

	opSchool := mustGetUser(t, pool, "op2")
	if !models.HasRole(opSchool.Role, models.RoleOperator) {
		t.Fatalf("after sekolah redeem: op role=%s, want operator", opSchool.Role)
	}
	for _, name := range []string{"guru1", "guru2"} {
		if status, resp := tc.createUser(t, name); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", name, status, resp)
		}
	}
	for _, name := range []string{"guru1", "guru2"} {
		status, cascade, _ := subFlags(t, pool, mustGetUser(t, pool, name).ID)
		if status != models.UserStatusActive || cascade {
			t.Fatalf("%s pre-expiry: status=%s cascade=%v, want active without cascade", name, status, cascade)
		}
	}

	// Simulate the sekolah package's lifetime running out: zero the ACTIVE
	// redemption (clock already elapsed) and push the account expiry into the
	// past as the authoritative login clock would read. The guru redemption
	// stays paused with ~30 days left — the fallback.
	expireActivePackage(t, pool, op.ID)

	// Run the background reconciliation pass exactly like the ticker would.
	runPackageExpiryPass(ctx, pool)

	// 1. The exhausted sekolah package is paused and its lifetime zeroed.
	var schoolRemaining int64
	var schoolActive bool
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds, is_active FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test'`,
		op.ID).Scan(&schoolRemaining, &schoolActive); err != nil {
		t.Fatalf("load school redemption: %v", err)
	}
	if schoolActive || schoolRemaining != 0 {
		t.Errorf("school redemption after expiry: is_active=%v remaining=%d, want paused (false, 0)", schoolActive, schoolRemaining)
	}

	// 2. The guru voucher was auto-activated (fallback) with its lifetime kept.
	var guruActive bool
	var guruRemaining int64
	if err := pool.QueryRow(ctx,
		`SELECT is_active, remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND package='guru'`,
		op.ID).Scan(&guruActive, &guruRemaining); err != nil {
		t.Fatalf("load guru redemption: %v", err)
	}
	if !guruActive || guruRemaining < 20*86400 {
		t.Errorf("guru redemption after fallback: is_active=%v remaining=%d, want active with ~30 days left", guruActive, guruRemaining)
	}

	// 3. Operator: the operator role is gone (role follows the active package),
	// the package label is the fallback's, and the account expiry was re-opened
	// (now + guru lifetime) so login works again.
	opAfter := mustGetUser(t, pool, "op2")
	if models.HasRole(opAfter.Role, models.RoleOperator) {
		t.Errorf("after expiry fallback: op role=%s, operator must be gone", opAfter.Role)
	}
	if opAfter.Package != "guru" {
		t.Errorf("after expiry fallback: op package=%s, want guru", opAfter.Package)
	}
	if opAfter.ExpiresAt == nil || !opAfter.ExpiresAt.After(time.Now().UTC().Add(20*24*time.Hour)) {
		t.Errorf("after expiry fallback: op expires_at=%v, want ~30 days in the future (login re-opened)", opAfter.ExpiresAt)
	}
	if _, msg := models.AuthenticateUser(ctx, pool, "op2", "pass-op2"); msg != "" {
		t.Errorf("op login after fallback: msg=%q, want success", msg)
	}

	// 4. Sub-accounts were cascade-suspended by the fallback (the guru package
	// grants no operator role) and can no longer log in.
	for _, name := range []string{"guru1", "guru2"} {
		sub := mustGetUser(t, pool, name)
		status, cascade, _ := subFlags(t, pool, sub.ID)
		if status != models.UserStatusSuspended {
			t.Errorf("%s after fallback: status=%s, want suspended", name, status)
		}
		if !cascade {
			t.Errorf("%s after fallback: suspended_by_cascade=false, want true", name)
		}
		if _, msg := models.AuthenticateUser(ctx, pool, name, "pass-"+name); !strings.Contains(msg, "dinonaktifkan") {
			t.Errorf("login %s after fallback: msg=%q, want suspension message", name, msg)
		}
	}

	// 5. The pass is idempotent: re-running it leaves the state untouched (the
	// guru package is active and far from exhaustion, so nothing is selected).
	runPackageExpiryPass(ctx, pool)
	opAgain := mustGetUser(t, pool, "op2")
	if opAgain.Role != opAfter.Role || opAgain.Package != opAfter.Package {
		t.Errorf("second pass changed operator state: role %s -> %s, package %s -> %s",
			opAfter.Role, opAgain.Role, opAfter.Package, opAgain.Package)
	}
	for _, name := range []string{"guru1", "guru2"} {
		status, cascade, _ := subFlags(t, pool, mustGetUser(t, pool, name).ID)
		if status != models.UserStatusSuspended || !cascade {
			t.Errorf("%s after second pass: status=%s cascade=%v, want still suspended", name, status, cascade)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 3: expiry job without a fallback
// ---------------------------------------------------------------------------

// TestVoucherExpiryNoFallbackStaysExpired guards the other expiry-job branch:
// when the exhausted package is the user's only claim, the job pauses it but
// there is nothing to fall back to — the account keeps its past expiry. Login
// is no longer expiry-gated, so the account is admitted as feature-locked
// (its owner can still reach the billing page and redeem a new voucher);
// only a redemption with remaining lifetime re-opens full access.
func TestVoucherExpiryNoFallbackStaysExpired(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op3", "SMA 3 Test", "pass-op3")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")

	if !models.HasRole(mustGetUser(t, pool, "op3").Role, models.RoleOperator) {
		t.Fatalf("after sekolah redeem: op3 must be operator")
	}

	expireActivePackage(t, pool, op.ID)
	runPackageExpiryPass(ctx, pool)

	// The exhausted package is paused and zeroed; no other redemption exists
	// to fall back to.
	var remaining int64
	var active bool
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds, is_active FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test'`,
		op.ID).Scan(&remaining, &active); err != nil {
		t.Fatalf("load school redemption: %v", err)
	}
	if active || remaining != 0 {
		t.Errorf("school redemption after expiry: is_active=%v remaining=%d, want paused (false, 0)", active, remaining)
	}

	// The account keeps its past expiry (nothing re-opened it). Login is no
	// longer expiry-gated: the account is admitted but feature-locked, so its
	// owner can still reach the billing page to redeem a new voucher. (The
	// operator role is not reconciled here — no entitlement is applied without
	// a fallback — which is harmless because feature access is gated by the
	// lock, not by role.)
	opAfter := mustGetUser(t, pool, "op3")
	if opAfter.ExpiresAt == nil || opAfter.ExpiresAt.After(time.Now().UTC()) {
		t.Errorf("op3 expires_at=%v, want still in the past", opAfter.ExpiresAt)
	}
	if got, msg := models.AuthenticateUser(ctx, pool, "op3", "pass-op3"); msg != "" || got == nil {
		t.Errorf("op3 login without fallback must succeed (feature-locked), got msg=%q user=%+v", msg, got)
	} else if !got.IsFeatureLocked() {
		t.Errorf("op3 login without fallback: want IsFeatureLocked()=true, got false")
	}

	// The pass is idempotent: re-running it must NOT re-select this user (the
	// exhausted package is now inactive, so the busy-loop is gone) — the state
	// stays exactly as after the first pass.
	runPackageExpiryPass(ctx, pool)
	var againRemaining int64
	var againActive bool
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds, is_active FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test'`,
		op.ID).Scan(&againRemaining, &againActive); err != nil {
		t.Fatalf("load school redemption after second pass: %v", err)
	}
	if againActive || againRemaining != 0 {
		t.Errorf("school redemption after second pass: is_active=%v remaining=%d, want still paused (false, 0)", againActive, againRemaining)
	}
}

// ---------------------------------------------------------------------------
// Test 3b: unlimited account (admin cleared expiry) with an active package
// ---------------------------------------------------------------------------

// TestUnlimitedAccountExpiredPackageDoesNotRelimit covers the admin clearing a
// user's expiry via the edit form (expires_at = "" → NULL = unlimited) while
// an ACTIVE package is still running. There is nothing to realign the package
// clock to (no expiry), so the clock simply must not re-limit the account
// later: when the active package's lifetime runs out, the expiry job pauses it
// WITHOUT the auto-fallback — otherwise applyRedemptionEntitlement would
// rewrite expires_at to a concrete value (silently undoing the admin's
// "unlimited" intent) and, since a guru fallback grants no operator role,
// cascade-suspend the school's sub-accounts.
func TestUnlimitedAccountExpiredPackageDoesNotRelimit(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucher(t, pool) // the claimed-but-paused fallback that must NOT auto-activate
	op := createOperatorUser(t, pool, "op-unl", "SMK Unlimited", "pass-op-unl")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-GURU")    // claimed first → becomes the paused fallback
	tc.redeem(t, "IT-SEKOLAH") // active, grants the operator role
	op = mustGetUser(t, pool, "op-unl")
	if !models.HasRole(op.Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}
	// One sub-account: if the fallback were auto-activated (guru grants no
	// operator role), syncInstansiWithOperatorRole would cascade-suspend it.
	if status, resp := tc.createUser(t, "guru1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create guru1: status=%d resp=%+v", status, resp)
	}

	// A superadmin clears the operator's expiry via the edit form — the exact
	// EditUser path under investigation (expires_at = "" → NULL).
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin-unl", Name: "Root Unl",
		PasswordHash: "pass-root-unl", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc.login(t, root.ID)
	if status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(op.ID)+"/edit",
		map[string]interface{}{"expires_at": ""}); status != http.StatusOK || !resp.Success {
		t.Fatalf("clear op expiry: status=%d resp=%+v", status, resp)
	}
	op = mustGetUser(t, pool, "op-unl")
	if op.ExpiresAt != nil {
		t.Fatalf("fixture: op expires_at = %v, want NULL (unlimited)", op.ExpiresAt)
	}
	// The clear must NOT have realigned the active redemption (there is
	// nothing to align to) — its stored clock is untouched.
	var sekolahRemaining int64
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND is_active`, op.ID).Scan(&sekolahRemaining); err != nil {
		t.Fatalf("load active sekolah redemption: %v", err)
	}
	if sekolahRemaining < 20*86400 {
		t.Fatalf("sekolah redemption remaining=%d after clear, want ~30 days untouched (no realign on NULL)", sekolahRemaining)
	}

	// The active package's lifetime runs out (only the redemption clock is
	// zeroed; the account expiry stays NULL — the job selects on the package
	// clock, not on expires_at).
	if _, err := pool.Exec(ctx,
		`UPDATE voucher_redemptions SET remaining_seconds = 0, activated_at = now() - interval '2 seconds'
		 WHERE user_id = $1 AND is_active`, op.ID); err != nil {
		t.Fatalf("exhaust sekolah redemption: %v", err)
	}
	runPackageExpiryPass(ctx, pool)

	// The exhausted package is paused and zeroed...
	var sekolahActive bool
	var sekolahRemainingAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT is_active, remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test'`,
		op.ID).Scan(&sekolahActive, &sekolahRemainingAfter); err != nil {
		t.Fatalf("load sekolah redemption after pass: %v", err)
	}
	if sekolahActive || sekolahRemainingAfter != 0 {
		t.Errorf("sekolah redemption after pass: is_active=%v remaining=%d, want paused (false, 0)", sekolahActive, sekolahRemainingAfter)
	}
	// ...but the fallback is NOT auto-activated and the account stays
	// unlimited.
	var guruActive bool
	if err := pool.QueryRow(ctx,
		`SELECT is_active FROM voucher_redemptions WHERE user_id=$1 AND package='guru'`,
		op.ID).Scan(&guruActive); err != nil {
		t.Fatalf("load guru redemption after pass: %v", err)
	}
	if guruActive {
		t.Error("guru fallback must NOT be auto-activated for an unlimited account")
	}
	op = mustGetUser(t, pool, "op-unl")
	if op.ExpiresAt != nil {
		t.Errorf("op expires_at = %v after pass, want still NULL (unlimited must not be re-limited)", op.ExpiresAt)
	}
	if op.IsFeatureLocked() {
		t.Error("op must not be feature-locked (NULL expiry = unlimited)")
	}
	// The operator role survives (no fallback entitlement was applied) and the
	// school's sub-account is not cascade-suspended.
	if !models.HasRole(op.Role, models.RoleOperator) {
		t.Errorf("op role = %s after pass, want operator retained (no fallback entitlement applied)", op.Role)
	}
	guru1 := mustGetUser(t, pool, "guru1")
	if status, cascade, _ := subFlags(t, pool, guru1.ID); status != models.UserStatusActive || cascade {
		t.Errorf("guru1 after pass: status=%s cascade=%v, want active without cascade (no fallback applied)", status, cascade)
	}

	// Idempotent: a second pass leaves the state untouched (nothing is active
	// anymore).
	runPackageExpiryPass(ctx, pool)
	op = mustGetUser(t, pool, "op-unl")
	if op.ExpiresAt != nil {
		t.Errorf("op expires_at = %v after second pass, want still NULL", op.ExpiresAt)
	}
	if err := pool.QueryRow(ctx,
		`SELECT is_active FROM voucher_redemptions WHERE user_id=$1 AND package='guru'`,
		op.ID).Scan(&guruActive); err != nil {
		t.Fatalf("load guru redemption after second pass: %v", err)
	}
	if guruActive {
		t.Error("guru fallback activated on the second pass")
	}
}

// ---------------------------------------------------------------------------
// Test 4: sub-account that bought its own voucher
// ---------------------------------------------------------------------------

// TestVoucherOperatorExitSuspendsSubAccountWithOwnVoucher locks in the answer
// to "does an account that holds its own voucher still get suspended when the
// operator leaves the school package?": YES. Note that since the sub-account
// voucher policy (operator-created accounts may not redeem) the own-package
// state here is planted as a pre-policy claim via claimSubOwnVoucher — new
// sub-accounts can no longer acquire their own voucher. The cascade suspend branch in
// syncInstansiWithOperatorRole matches EVERY active non-operator account in
// the instansi, regardless of whether it runs its own active package. The
// sub-account's own package is NOT paused by the suspension — its lifetime
// simply freezes — and both the account and its package are restored, clock
// intact, when the operator returns to a school package. (Accounts that hold
// an operator role themselves are the one documented exception and are never
// cascade-suspended.)
func TestVoucherOperatorExitSuspendsSubAccountWithOwnVoucher(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucher(t, pool)                    // the operator's switch target
	createGuruVoucherCode(t, pool, "IT-GURU-SUB") // the sub's own single-use voucher
	op := createOperatorUser(t, pool, "op4", "SMK 4 Test", "pass-op4")

	tc := newVoucherTestClient(t, pool)

	// Operator redeems sekolah → operator, then creates two sub-accounts.
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	for _, name := range []string{"guru1", "guru2"} {
		if status, resp := tc.createUser(t, name); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", name, status, resp)
		}
	}

	// guru2's own package is planted as a PRE-POLICY claim: the sub-account
	// voucher policy (operator-created accounts may not redeem vouchers)
	// blocks the real RedeemVoucherHandler path, so claimSubOwnVoucher
	// inserts the redemption + expiry directly — mirroring exactly what a
	// claim produced before the policy.
	guru2 := mustGetUser(t, pool, "guru2")
	claimSubOwnVoucher(t, pool, guru2.ID)
	var subRedemptionID int
	var subRemaining int64
	if err := pool.QueryRow(ctx,
		`SELECT id, remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND package='guru'`,
		guru2.ID).Scan(&subRedemptionID, &subRemaining); err != nil {
		t.Fatalf("load guru2 redemption: %v", err)
	}
	if subRemaining < 20*86400 {
		t.Fatalf("guru2 own redemption remaining=%d, want ~30 days", subRemaining)
	}

	// Operator switches to the guru voucher → leaves the school package.
	tc.login(t, op.ID)
	tc.redeem(t, "IT-GURU")
	if models.HasRole(mustGetUser(t, pool, "op4").Role, models.RoleOperator) {
		t.Fatalf("op must have lost the operator role")
	}

	// BOTH sub-accounts are cascade-suspended — including guru2, who bought
	// their own voucher. That is the behavior under test.
	for _, name := range []string{"guru1", "guru2"} {
		sub := mustGetUser(t, pool, name)
		status, cascade, _ := subFlags(t, pool, sub.ID)
		if status != models.UserStatusSuspended {
			t.Errorf("%s after operator exit: status=%s, want suspended", name, status)
		}
		if !cascade {
			t.Errorf("%s after operator exit: suspended_by_cascade=false, want true", name)
		}
		if _, msg := models.AuthenticateUser(ctx, pool, name, "pass-"+name); !strings.Contains(msg, "dinonaktifkan") {
			t.Errorf("login %s after operator exit: msg=%q, want suspension message", name, msg)
		}
	}
	// The suspension does NOT pause guru2's own package: its redemption stays
	// active with its stored lifetime untouched (the clock simply freezes).
	// The stored remaining_seconds only changes on pause/sync, and no expiry
	// pass runs in-test, so the exact-equality check below is deterministic.
	var stillActive bool
	var stillRemaining int64
	if err := pool.QueryRow(ctx, `SELECT is_active, remaining_seconds FROM voucher_redemptions WHERE id=$1`, subRedemptionID).Scan(&stillActive, &stillRemaining); err != nil {
		t.Fatalf("load guru2 redemption after suspend: %v", err)
	}
	if !stillActive {
		t.Errorf("guru2 own redemption must stay active while suspended")
	}
	if stillRemaining != subRemaining {
		t.Errorf("guru2 own redemption remaining changed while suspended: %d -> %d", subRemaining, stillRemaining)
	}

	// Operator returns to the sekolah package → everything is restored.
	var schoolRedemptionID int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test' ORDER BY id DESC LIMIT 1`,
		op.ID).Scan(&schoolRedemptionID); err != nil {
		t.Fatalf("find school redemption: %v", err)
	}
	tc.activate(t, schoolRedemptionID)

	opRestored := mustGetUser(t, pool, "op4")
	if !models.HasRole(opRestored.Role, models.RoleOperator) {
		t.Errorf("after sekolah reactivate: op role=%s, want operator back", opRestored.Role)
	}
	for _, name := range []string{"guru1", "guru2"} {
		sub := mustGetUser(t, pool, name)
		status, cascade, suspendedAt := subFlags(t, pool, sub.ID)
		if status != models.UserStatusActive {
			t.Errorf("%s after restore: status=%s, want active", name, status)
		}
		if cascade || suspendedAt != nil {
			t.Errorf("%s after restore: cascade=%v suspended_at=%v, want clean", name, cascade, suspendedAt)
		}
		if _, msg := models.AuthenticateUser(ctx, pool, name, "pass-"+name); msg != "" {
			t.Errorf("login %s after restore: msg=%q, want success", name, msg)
		}
	}
	// guru2's own package survived the whole round trip: still active, its
	// frozen lifetime restored (realigned to the frozen expiry on restore).
	var restoredActive bool
	var restoredRemaining int64
	if err := pool.QueryRow(ctx, `SELECT is_active, remaining_seconds FROM voucher_redemptions WHERE id=$1`, subRedemptionID).Scan(&restoredActive, &restoredRemaining); err != nil {
		t.Fatalf("load guru2 redemption after restore: %v", err)
	}
	if !restoredActive {
		t.Errorf("guru2 own redemption must be active again after restore")
	}
	if restoredRemaining < 20*86400 {
		t.Errorf("guru2 own redemption remaining=%d after restore, want ~30 days preserved", restoredRemaining)
	}
	// guru1 (no own package) follows the operator's school expiry again; guru2
	// is deliberately NOT realigned — the expiry-alignment UPDATE skips
	// accounts with an active redemption (NOT EXISTS guard), so guru2 keeps
	// its own package clock (asserted via the preserved remaining_seconds
	// above, not via expiry inequality, which would be fragile since both
	// land ~30 days out).
	guru1 := mustGetUser(t, pool, "guru1")
	if guru1.ExpiresAt == nil || !approxEqual(*guru1.ExpiresAt, *opRestored.ExpiresAt, time.Minute) {
		t.Errorf("guru1 expiry=%v, want ≈ operator's %v", guru1.ExpiresAt, opRestored.ExpiresAt)
	}
}

// ---------------------------------------------------------------------------
// Test 4b: transactional restore — sub redemption realigned on voucher switch
// ---------------------------------------------------------------------------

// TestVoucherSwitchRestoreFreezeClockSyncsSubRedemption locks in the OTHER
// call site of RestoreCascadeSuspendedInstansi — the TRANSACTIONAL one inside
// syncInstansiWithOperatorRole (entitlement.go), reached when the operator
// returns to the school package via a voucher switch. A sub that runs its OWN
// active package must have its remaining_seconds realigned to the frozen
// account expiry, exactly like the toggle-status cascade restore
// (TestToggleOperatorRestoreFreezeClockSyncsSubRedemption) — the same exact
// DB-internal invariant, now inside a pgx.Tx.
func TestVoucherSwitchRestoreFreezeClockSyncsSubRedemption(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucher(t, pool)                    // the operator's switch target
	createGuruVoucherCode(t, pool, "IT-GURU-SUB") // the sub's own active package
	op := createOperatorUser(t, pool, "op-vsr", "SMK Voucher Sync", "pass-op-vsr")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	op = mustGetUser(t, pool, "op-vsr")
	if !models.HasRole(op.Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}
	if status, resp := tc.createUser(t, "guru2"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create guru2: status=%d resp=%+v", status, resp)
	}
	guru2 := mustGetUser(t, pool, "guru2")
	// Pre-policy claim simulation (see claimSubOwnVoucher): the sub-account
	// policy forbids operator-created accounts from redeeming vouchers.
	claimSubOwnVoucher(t, pool, guru2.ID)

	// Operator leaves the school package → the sub is cascade-suspended.
	tc.login(t, op.ID)
	tc.redeem(t, "IT-GURU")
	guru2 = mustGetUser(t, pool, "guru2")
	if status, cascade, _ := subFlags(t, pool, guru2.ID); status != models.UserStatusSuspended || !cascade {
		t.Fatalf("guru2 must be cascade-suspended, got status=%s cascade=%v", status, cascade)
	}

	// Pin the sub's clock deterministically (Go clock, verbatim): suspended 2
	// hours ago with a future expiry at now + 30d. The cascade marker stays
	// set, so the restore branch genuinely matches.
	goNow := time.Now().UTC()
	pinSuspendedClock(t, pool, guru2.ID, goNow.Add(-2*time.Hour), goNow.Add(30*24*time.Hour))
	var expiresBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT expires_at FROM admin_users WHERE id = $1`, guru2.ID).Scan(&expiresBefore); err != nil {
		t.Fatalf("read sub expires_at: %v", err)
	}
	var redemptionID int
	var remainingBefore int64
	var activatedBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT id, remaining_seconds, activated_at FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		guru2.ID).Scan(&redemptionID, &remainingBefore, &activatedBefore); err != nil {
		t.Fatalf("load sub redemption: %v", err)
	}

	// Operator returns to the school package → the activation transaction runs
	// syncInstansiWithOperatorRole → RestoreCascadeSuspendedInstansi (pgx.Tx).
	var schoolRedemptionID int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test' ORDER BY id DESC LIMIT 1`,
		op.ID).Scan(&schoolRedemptionID); err != nil {
		t.Fatalf("find school redemption: %v", err)
	}
	tc.activate(t, schoolRedemptionID)

	got := mustGetUser(t, pool, "guru2")
	status, cascade, suspendedAt := subFlags(t, pool, guru2.ID)
	if status != models.UserStatusActive {
		t.Errorf("guru2 after restore: status=%s, want active", status)
	}
	if cascade {
		t.Errorf("guru2 after restore: suspended_by_cascade=true, want false")
	}
	if suspendedAt != nil {
		t.Errorf("guru2 after restore: suspended_at=%v, want NULL", suspendedAt)
	}
	if _, msg := models.AuthenticateUser(ctx, pool, "guru2", "pass-guru2"); msg != "" {
		t.Errorf("login guru2 after restore: msg=%q, want success", msg)
	}
	var remainingAfter int64
	var activatedAfter time.Time
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds, activated_at FROM voucher_redemptions WHERE id=$1`,
		redemptionID).Scan(&remainingAfter, &activatedAfter); err != nil {
		t.Fatalf("load sub redemption after restore: %v", err)
	}
	assertFrozenRedemptionSync(t, "guru2 after voucher-switch restore", expiresBefore, remainingBefore, activatedBefore, got.ExpiresAt, remainingAfter, activatedAfter)
}

// ---------------------------------------------------------------------------
// Test 5: contrast — sub-account with its own SCHOOL (operator) voucher
// ---------------------------------------------------------------------------

// TestVoucherOperatorExitSkipsSubWithOwnOperatorRole is the contrast case to
// TestVoucherOperatorExitSuspendsSubAccountWithOwnVoucher: a sub-account that
// redeemed its own SEKOLAH voucher holds the operator role itself, and the
// cascade suspend branch explicitly excludes operator-role accounts
// (`NOT (role ILIKE '%"operator"%')`) — so THIS sub is NOT suspended when the
// main operator leaves the school package, while the plain sub in the same
// instansi still is. Both remain usable after the operator returns.
func TestVoucherOperatorExitSkipsSubWithOwnOperatorRole(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)                       // the operator's school voucher
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-SUB") // the sub's own single-use school voucher
	createGuruVoucher(t, pool)                         // the operator's switch target
	op := createOperatorUser(t, pool, "op5", "SMA 5 Test", "pass-op5")

	tc := newVoucherTestClient(t, pool)

	// Operator redeems sekolah → operator, then creates two sub-accounts.
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	for _, name := range []string{"guru1", "guru2"} {
		if status, resp := tc.createUser(t, name); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", name, status, resp)
		}
	}

	// guru2 runs its own school package → holds the operator role itself
	// (pre-policy claim — see claimSubOwnSchoolVoucher).
	guru2 := mustGetUser(t, pool, "guru2")
	// Pre-policy school-package claim (see claimSubOwnSchoolVoucher): the
	// sub-account voucher policy forbids operator-created accounts from
	// redeeming vouchers.
	claimSubOwnSchoolVoucher(t, pool, guru2.ID)
	guru2 = mustGetUser(t, pool, "guru2")
	if !models.HasRole(guru2.Role, models.RoleOperator) {
		t.Fatalf("guru2 must hold the operator role after redeeming its own school voucher")
	}

	// Operator switches to the guru voucher → leaves the school package.
	tc.login(t, op.ID)
	tc.redeem(t, "IT-GURU")

	// The plain sub is cascade-suspended; the operator-role sub is NOT.
	guru1 := mustGetUser(t, pool, "guru1")
	status, cascade, _ := subFlags(t, pool, guru1.ID)
	if status != models.UserStatusSuspended || !cascade {
		t.Errorf("guru1 (plain) after operator exit: status=%s cascade=%v, want suspended+cascade", status, cascade)
	}
	guru2 = mustGetUser(t, pool, "guru2")
	status, cascade, _ = subFlags(t, pool, guru2.ID)
	if status != models.UserStatusActive || cascade {
		t.Errorf("guru2 (own operator role) after operator exit: status=%s cascade=%v, want active without cascade", status, cascade)
	}
	if !models.HasRole(guru2.Role, models.RoleOperator) {
		t.Errorf("guru2 must keep its operator role after the operator's exit")
	}
	if _, msg := models.AuthenticateUser(ctx, pool, "guru1", "pass-guru1"); !strings.Contains(msg, "dinonaktifkan") {
		t.Errorf("login guru1 after operator exit: msg=%q, want suspension message", msg)
	}
	if _, msg := models.AuthenticateUser(ctx, pool, "guru2", "pass-guru2"); msg != "" {
		t.Errorf("login guru2 after operator exit: msg=%q, want success", msg)
	}

	// Operator returns to sekolah → the plain sub is restored; the
	// operator-role sub was never touched.
	var schoolRedemptionID int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test' ORDER BY id DESC LIMIT 1`,
		op.ID).Scan(&schoolRedemptionID); err != nil {
		t.Fatalf("find school redemption: %v", err)
	}
	tc.activate(t, schoolRedemptionID)

	guru1 = mustGetUser(t, pool, "guru1")
	status, cascade, _ = subFlags(t, pool, guru1.ID)
	if status != models.UserStatusActive || cascade {
		t.Errorf("guru1 after operator return: status=%s cascade=%v, want active without cascade", status, cascade)
	}
	guru2 = mustGetUser(t, pool, "guru2")
	status, cascade, _ = subFlags(t, pool, guru2.ID)
	if status != models.UserStatusActive || cascade {
		t.Errorf("guru2 after operator return: status=%s cascade=%v, want active without cascade", status, cascade)
	}
	if !models.HasRole(guru2.Role, models.RoleOperator) {
		t.Errorf("guru2 must keep its operator role after the operator's return")
	}
	if _, msg := models.AuthenticateUser(ctx, pool, "guru1", "pass-guru1"); msg != "" {
		t.Errorf("login guru1 after operator return: msg=%q, want success", msg)
	}
	if _, msg := models.AuthenticateUser(ctx, pool, "guru2", "pass-guru2"); msg != "" {
		t.Errorf("login guru2 after operator return: msg=%q, want success", msg)
	}
}

// ---------------------------------------------------------------------------
// Test 6: manual reactivation of a cascade-suspended sub-account
// ---------------------------------------------------------------------------

// TestManualReactivationOfCascadeSuspendedSub covers the escape hatch when the
// operator does NOT come back: a superadmin can reactivate a cascade-suspended
// sub-account manually via the toggle-status endpoint. The account comes back
// active with its clock frozen for the suspension period (expiry extended, not
// burned) and the cascade marker dropped, so a later operator return does not
// touch it again.
func TestManualReactivationOfCascadeSuspendedSub(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucher(t, pool)
	op := createOperatorUser(t, pool, "op6", "SMK 6 Test", "pass-op6")

	tc := newVoucherTestClient(t, pool)

	// Operator redeems sekolah, creates one sub, then leaves the school
	// package → the sub is cascade-suspended.
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if status, resp := tc.createUser(t, "guru1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create guru1: status=%d resp=%+v", status, resp)
	}
	tc.redeem(t, "IT-GURU")
	guru1 := mustGetUser(t, pool, "guru1")
	status, cascade, _ := subFlags(t, pool, guru1.ID)
	if status != models.UserStatusSuspended || !cascade {
		t.Fatalf("guru1 must be cascade-suspended, got status=%s cascade=%v", status, cascade)
	}
	if guru1.ExpiresAt == nil {
		t.Fatalf("guru1 must have an expiry to freeze")
	}
	suspendedExpiry := *guru1.ExpiresAt

	// A superadmin (not the operator — the operator lost their role) manually
	// reactivates the account via toggle-status.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin", Name: "Root Admin",
		PasswordHash: "pass-root", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc.login(t, root.ID)
	toggleStatus, resp := postForm(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(guru1.ID)+"/toggle-status", nil)
	if toggleStatus != http.StatusOK || !resp.Success {
		t.Fatalf("toggle-status guru1: status=%d resp=%+v", toggleStatus, resp)
	}

	// The account is active again, the cascade marker and suspension clock are
	// gone, the expiry was extended by the suspension period (clock freeze),
	// and login works.
	guru1 = mustGetUser(t, pool, "guru1")
	status, cascade, suspendedAt := subFlags(t, pool, guru1.ID)
	if status != models.UserStatusActive {
		t.Errorf("guru1 after manual reactivation: status=%s, want active", status)
	}
	if cascade {
		t.Errorf("guru1 after manual reactivation: suspended_by_cascade=true, want false")
	}
	if suspendedAt != nil {
		t.Errorf("guru1 after manual reactivation: suspended_at=%v, want NULL", suspendedAt)
	}
	if guru1.ExpiresAt == nil || guru1.ExpiresAt.Before(suspendedExpiry) {
		t.Errorf("guru1 expiry=%v must not be before suspended expiry=%v (clock freeze)", guru1.ExpiresAt, suspendedExpiry)
	}
	if _, msg := models.AuthenticateUser(ctx, pool, "guru1", "pass-guru1"); msg != "" {
		t.Errorf("login guru1 after manual reactivation: msg=%q, want success", msg)
	}

	// The operator returning to the school package must NOT disturb the
	// manually-reactivated account (its cascade marker is gone).
	var schoolRedemptionID int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test' ORDER BY id DESC LIMIT 1`,
		op.ID).Scan(&schoolRedemptionID); err != nil {
		t.Fatalf("find school redemption: %v", err)
	}
	tc.login(t, op.ID)
	tc.activate(t, schoolRedemptionID)

	guru1 = mustGetUser(t, pool, "guru1")
	status, cascade, _ = subFlags(t, pool, guru1.ID)
	if status != models.UserStatusActive || cascade {
		t.Errorf("guru1 after operator return: status=%s cascade=%v, want untouched (active, no cascade)", status, cascade)
	}
	if _, msg := models.AuthenticateUser(ctx, pool, "guru1", "pass-guru1"); msg != "" {
		t.Errorf("login guru1 after operator return: msg=%q, want success", msg)
	}
}

// ---------------------------------------------------------------------------
// Test 7: unstarted exams are tombstoned when the operator leaves
// ---------------------------------------------------------------------------

// insertTestExam inserts an exams row with a unique token and returns its ID.
// startedAt nil = exam never started; non-nil = exam currently running.
func insertTestExam(t *testing.T, pool *pgxpool.Pool, createdBy int, name string, startedAt *time.Time) int {
	t.Helper()
	// "T" + 7 digits = the app's 8-char token format (tokenRegex ^[A-Z0-9]{8}$).
	token := fmt.Sprintf("T%07d", time.Now().UnixNano()%10000000)
	var id int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at)
		VALUES ($1, $1 || '.pdf', 1024, $2, $2, 'active', 'medium', $3, $4)
		RETURNING id`, name, token, createdBy, startedAt).Scan(&id); err != nil {
		t.Fatalf("insert test exam %s: %v", name, err)
	}
	return id
}

// mustGetExam fetches an exam by ID, failing the test on error.
func mustGetExam(t *testing.T, pool *pgxpool.Pool, id int) models.Exam {
	t.Helper()
	e, err := models.GetExamByID(context.Background(), pool, id)
	if err != nil {
		t.Fatalf("get exam %d: %v", id, err)
	}
	return e
}

// TestVoucherOperatorExitTombstonesUnstartedExams locks in policy B for the
// operator exit: exams created by accounts that no longer hold the operator
// role (the operator who just lost it and the cascade-suspended subs) go
// inactive when the operator leaves the school package IF they are active but
// have never been started (exam_started_at IS NULL). Exams that are already
// running are deliberately left untouched so students working on them can
// finish, and the tombstone is NOT auto-reversed when the operator returns —
// the owner re-activates manually, so an admin's explicit inactivation is
// never clobbered.
func TestVoucherOperatorExitTombstonesUnstartedExams(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucher(t, pool)
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-SUB") // guru2's own school voucher
	op := createOperatorUser(t, pool, "op7", "SMP 7 Test", "pass-op7")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	for _, name := range []string{"guru1", "guru2"} {
		if status, resp := tc.createUser(t, name); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", name, status, resp)
		}
	}
	sub := mustGetUser(t, pool, "guru1")

	// guru2 runs its own school package → holds the operator role itself, so
	// its exams must be spared by the tombstone's operator-role filter
	// (pre-policy claim — see claimSubOwnSchoolVoucher).
	guru2 := mustGetUser(t, pool, "guru2")
	// Pre-policy school-package claim (see claimSubOwnSchoolVoucher): the
	// sub-account voucher policy forbids operator-created accounts from
	// redeeming vouchers.
	claimSubOwnSchoolVoucher(t, pool, guru2.ID)
	guru2 = mustGetUser(t, pool, "guru2")
	if !models.HasRole(guru2.Role, models.RoleOperator) {
		t.Fatalf("guru2 must hold the operator role after redeeming its own school voucher")
	}
	tc.login(t, op.ID)

	// Seed four exams: unstarted ones by the operator and by the plain sub, an
	// unstarted one by the operator-role sub (must be spared), and a running
	// one by the plain sub (must survive so students can finish).
	opUnstarted := insertTestExam(t, pool, op.ID, "op-unstarted", nil)
	subUnstarted := insertTestExam(t, pool, sub.ID, "sub-unstarted", nil)
	opRoleSubUnstarted := insertTestExam(t, pool, guru2.ID, "op-role-sub-unstarted", nil)
	started := time.Now().UTC().Add(-5 * time.Minute)
	subRunning := insertTestExam(t, pool, sub.ID, "sub-running", &started)

	// Operator leaves the school package → the unstarted exams of accounts
	// without the operator role go dormant; the running one and the
	// operator-role sub's exam survive.
	tc.redeem(t, "IT-GURU")
	for _, e := range []struct {
		id   int
		name string
		want string
	}{{opUnstarted, "op-unstarted", "inactive"}, {subUnstarted, "sub-unstarted", "inactive"}, {opRoleSubUnstarted, "op-role-sub-unstarted", "active"}, {subRunning, "sub-running", "active"}} {
		if got := mustGetExam(t, pool, e.id).Status; got != e.want {
			t.Errorf("%s after operator exit: status=%s, want %s", e.name, got, e.want)
		}
	}
	// The running exam is not just active — it is still marked as started.
	if got := mustGetExam(t, pool, subRunning).ExamStartedAt; got == nil {
		t.Errorf("sub-running after operator exit: exam_started_at cleared, want still set")
	}
	// The tombstone marker (tombstoned_at) is set exactly on the exams that
	// were auto-inactivated — and on no others — so the admin UI can tell
	// them apart from manual inactivations.
	if got := mustGetExam(t, pool, opUnstarted).TombstonedAt; got == nil {
		t.Errorf("op-unstarted after operator exit: tombstoned_at must be set")
	}
	if got := mustGetExam(t, pool, subUnstarted).TombstonedAt; got == nil {
		t.Errorf("sub-unstarted after operator exit: tombstoned_at must be set")
	}
	if got := mustGetExam(t, pool, opRoleSubUnstarted).TombstonedAt; got != nil {
		t.Errorf("op-role-sub-unstarted after operator exit: tombstoned_at=%v, want nil (spared)", got)
	}
	if got := mustGetExam(t, pool, subRunning).TombstonedAt; got != nil {
		t.Errorf("sub-running after operator exit: tombstoned_at=%v, want nil (running)", got)
	}

	// Operator returns to the sekolah package → the running exam stays active
	// and the tombstoned exams stay inactive (no auto-restore).
	var schoolRedemptionID int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM voucher_redemptions WHERE user_id=$1 AND package='sekolah-test' ORDER BY id DESC LIMIT 1`,
		op.ID).Scan(&schoolRedemptionID); err != nil {
		t.Fatalf("find school redemption: %v", err)
	}
	tc.activate(t, schoolRedemptionID)
	for _, e := range []struct {
		id   int
		name string
		want string
	}{{opUnstarted, "op-unstarted", "inactive"}, {subUnstarted, "sub-unstarted", "inactive"}, {opRoleSubUnstarted, "op-role-sub-unstarted", "active"}, {subRunning, "sub-running", "active"}} {
		if got := mustGetExam(t, pool, e.id).Status; got != e.want {
			t.Errorf("%s after operator return: status=%s, want %s", e.name, got, e.want)
		}
	}
	// Re-activating a tombstoned exam (the owner clicking the badge) clears
	// the tombstone marker — it becomes a normal active exam again.
	if _, err := models.ToggleExamStatus(ctx, pool, opUnstarted); err != nil {
		t.Fatalf("re-activate op-unstarted: %v", err)
	}
	if re := mustGetExam(t, pool, opUnstarted); re.Status != "active" || re.TombstonedAt != nil {
		t.Errorf("op-unstarted after re-activate: status=%s tombstoned_at=%v, want active with nil marker", re.Status, re.TombstonedAt)
	}
	// Regression guard: an exam that was tombstoned, re-activated (marker
	// cleared), and then manually deactivated by the owner must NOT show the
	// "Ditombstone" badge again — the marker stays NULL for a plain manual
	// deactivation.
	if _, err := models.ToggleExamStatus(ctx, pool, opUnstarted); err != nil {
		t.Fatalf("deactivate op-unstarted after re-activate: %v", err)
	}
	if re := mustGetExam(t, pool, opUnstarted); re.Status != "inactive" || re.TombstonedAt != nil {
		t.Errorf("op-unstarted after manual deactivation: status=%s tombstoned_at=%v, want inactive with nil marker (not Ditombstone)", re.Status, re.TombstonedAt)
	}
}

// ---------------------------------------------------------------------------
// Test 8: manual operator suspension tombstones unstarted exams
// ---------------------------------------------------------------------------

// TestToggleOperatorStatusTombstonesUnstartedExams locks in policy B for the
// manual-suspension path (ToggleUserStatus): when a SuperAdmin suspends the
// school's operator account, the whole school is frozen — every
// active-but-unstarted exam in the instansi goes inactive (spareOperatorRole-
// Creators=false, since even operator-role subs are suspended by this
// cascade), the cascade-suspended subs stay suspended, and the running exam
// survives so students can finish. When the operator is reactivated the subs
// are restored (clock freeze) but the tombstoned exams are NOT auto-reversed.
func TestToggleOperatorStatusTombstonesUnstartedExams(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createSchoolVoucherCode(t, pool, "IT-SEKOLAH-SUB") // guru2's own school voucher
	op := createOperatorUser(t, pool, "op8", "SMP 8 Test", "pass-op8")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	for _, name := range []string{"guru1", "guru2"} {
		if status, resp := tc.createUser(t, name); status != http.StatusOK || !resp.Success {
			t.Fatalf("create %s: status=%d resp=%+v", name, status, resp)
		}
	}
	sub := mustGetUser(t, pool, "guru1")

	// guru2 runs its own school package → holds the operator role itself. The
	// manual-suspend tombstone freezes the WHOLE school
	// (spareOperatorRoleCreators=false), so its exam must be tombstoned too —
	// unlike the voucher switch (Test 7), where it is spared.
	guru2 := mustGetUser(t, pool, "guru2")
	// Pre-policy school-package claim (see claimSubOwnSchoolVoucher): the
	// sub-account voucher policy forbids operator-created accounts from
	// redeeming vouchers.
	claimSubOwnSchoolVoucher(t, pool, guru2.ID)
	if !models.HasRole(mustGetUser(t, pool, "guru2").Role, models.RoleOperator) {
		t.Fatalf("guru2 must hold the operator role after redeeming its own school voucher")
	}

	opUnstarted := insertTestExam(t, pool, op.ID, "op-unstarted", nil)
	subUnstarted := insertTestExam(t, pool, sub.ID, "sub-unstarted", nil)
	opRoleSubUnstarted := insertTestExam(t, pool, guru2.ID, "op-role-sub-unstarted", nil)
	started := time.Now().UTC().Add(-5 * time.Minute)
	subRunning := insertTestExam(t, pool, sub.ID, "sub-running", &started)

	// A SuperAdmin suspends the operator manually via toggle-status.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin8", Name: "Root Admin 8",
		PasswordHash: "pass-root", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc.login(t, root.ID)
	if status, resp := postForm(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(op.ID)+"/toggle-status", nil); status != http.StatusOK || !resp.Success {
		t.Fatalf("toggle-status suspend op: status=%d resp=%+v", status, resp)
	}

	// Operator and sub suspended; unstarted exams inactive; running one alive.
	opNow := mustGetUser(t, pool, "op8")
	if opNow.Status != models.UserStatusSuspended {
		t.Errorf("op after suspend: status=%s, want suspended", opNow.Status)
	}
	if status, cascade, _ := subFlags(t, pool, sub.ID); status != models.UserStatusSuspended || !cascade {
		t.Errorf("guru1 after suspend: status=%s cascade=%v, want suspended+cascade", status, cascade)
	}
	for _, e := range []struct {
		id   int
		name string
		want string
	}{{opUnstarted, "op-unstarted", "inactive"}, {subUnstarted, "sub-unstarted", "inactive"}, {opRoleSubUnstarted, "op-role-sub-unstarted", "inactive"}, {subRunning, "sub-running", "active"}} {
		if got := mustGetExam(t, pool, e.id).Status; got != e.want {
			t.Errorf("%s after suspend: status=%s, want %s", e.name, got, e.want)
		}
	}
	if got := mustGetExam(t, pool, subRunning).ExamStartedAt; got == nil {
		t.Errorf("sub-running after suspend: exam_started_at cleared, want still set")
	}
	// The tombstone marker is set on every auto-inactivated exam — including
	// the operator-role sub's (spareOperatorRoleCreators=false) — and nowhere
	// else.
	for _, e := range []struct {
		id     int
		name   string
		spared bool
	}{{opUnstarted, "op-unstarted", false}, {subUnstarted, "sub-unstarted", false}, {opRoleSubUnstarted, "op-role-sub-unstarted", false}, {subRunning, "sub-running", true}} {
		got := mustGetExam(t, pool, e.id).TombstonedAt
		if e.spared && got != nil {
			t.Errorf("%s after suspend: tombstoned_at=%v, want nil (running)", e.name, got)
		}
		if !e.spared && got == nil {
			t.Errorf("%s after suspend: tombstoned_at must be set", e.name)
		}
	}

	// Reactivate the operator → subs restored (clock freeze), exams unchanged.
	if status, resp := postForm(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(op.ID)+"/toggle-status", nil); status != http.StatusOK || !resp.Success {
		t.Fatalf("toggle-status reactivate op: status=%d resp=%+v", status, resp)
	}
	opNow = mustGetUser(t, pool, "op8")
	if opNow.Status != models.UserStatusActive {
		t.Errorf("op after reactivate: status=%s, want active", opNow.Status)
	}
	if status, cascade, _ := subFlags(t, pool, sub.ID); status != models.UserStatusActive || cascade {
		t.Errorf("guru1 after reactivate: status=%s cascade=%v, want active without cascade", status, cascade)
	}
	for _, e := range []struct {
		id   int
		name string
		want string
	}{{opUnstarted, "op-unstarted", "inactive"}, {subUnstarted, "sub-unstarted", "inactive"}, {opRoleSubUnstarted, "op-role-sub-unstarted", "inactive"}, {subRunning, "sub-running", "active"}} {
		if got := mustGetExam(t, pool, e.id).Status; got != e.want {
			t.Errorf("%s after reactivate: status=%s, want %s", e.name, got, e.want)
		}
	}
	// Reactivating the operator does NOT clear the exam tombstone markers —
	// only re-activating the exam itself does.
	if got := mustGetExam(t, pool, opUnstarted).TombstonedAt; got == nil {
		t.Errorf("op-unstarted after operator reactivate: tombstoned_at must still be set (no auto-restore)")
	}
}
