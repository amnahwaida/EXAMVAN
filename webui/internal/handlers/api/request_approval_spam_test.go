package api

// ---------------------------------------------------------------------------
// Anti-spam test suite for POST /api/exams/request-approval.
//
// The endpoint is the gate that (with auto-approve on) grants a device submit
// access, so it is also the spam target: without hardening, anyone who knows
// an exam_id could mint unlimited approved devices (and their submissions
// rows) or flood the pending queue. These tests lock in the four mitigations:
//
//  1. token required (401 without a valid exam token),
//  2. per-exam approved-device cap (max_approvals_per_exam, default 500),
//  3. global per-exam Redis flood brake (checkRateLimit),
//  4. pagination of the pending queue (covered in the admin package tests).
//
// "Ditolak" here means NOT auto-approved: an over-cap device falls back to the
// pending queue (a pengawas can still admit it manually) — never an approved
// row, and never a submissions row.
// ---------------------------------------------------------------------------

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

// setApprovalCap overrides the per-exam auto-approve device cap for the test.
// The setting is scoped to the package's disposable schema, so it never leaks
// into other tests.
func setApprovalCap(t *testing.T, pool *pgxpool.Pool, cap int) {
	t.Helper()
	if err := models.SetSaasSetting(context.Background(), pool,
		models.SettingMaxApprovalsPerExam, strconv.Itoa(cap)); err != nil {
		t.Fatalf("set approval cap: %v", err)
	}
}

// spamApprovalPayload builds a request-approval body the way an automated
// flood would: real exam id + identity, attacker-chosen MAC, reset=true.
func spamApprovalPayload(examID int, mac string) map[string]interface{} {
	return map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Spammer", "exam_number": "99", "student_class": "X Z",
		"identity_data": map[string]interface{}{}, "reset": true,
	}
}

func countApprovalsByStatus(t *testing.T, pool *pgxpool.Pool, examID int, status string) int {
	t.Helper()
	var cnt int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id = $1 AND status = $2`,
		examID, status).Scan(&cnt); err != nil {
		t.Fatalf("count approvals %q: %v", status, err)
	}
	return cnt
}

// A mass-device spam wave (distinct MACs, valid token): only the cap's worth
// of devices may be auto-approved; every device beyond it lands in the pending
// queue and must NOT mint a submissions row.
func TestRequestApprovalSpamMassDevicesBeyondCap(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	setApprovalCap(t, pool, 2)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	approved, pending := 0, 0
	for i := 0; i < 10; i++ {
		mac := fmt.Sprintf("AA:BB:CC:DD:EE:%02d", i)
		code, out := postRequestApproval(t, srv, spamApprovalPayload(examID, mac), examToken)
		if code != http.StatusOK {
			t.Fatalf("device %s: status=%d, want 200", mac, code)
		}
		switch out["status"] {
		case "approved":
			approved++
		case "pending":
			pending++
		default:
			t.Fatalf("device %s: unexpected status %v", mac, out["status"])
		}
	}
	if approved != 2 {
		t.Errorf("approved = %d, want exactly 2 (the cap)", approved)
	}
	if pending != 8 {
		t.Errorf("pending = %d, want 8", pending)
	}
	// Over-cap spam must not mint submissions rows (they exist only for
	// approved devices) — otherwise a flood still bloats the DB.
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:09"); got != 0 {
		t.Errorf("submissions for over-cap device = %d, want 0", got)
	}
}

// Retrying with reset=true ("Minta Izin Lagi") must not bypass the cap: an
// over-cap device stays pending no matter how often it re-asks.
func TestRequestApprovalSpamResetDoesNotBypassCap(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	setApprovalCap(t, pool, 1)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	// The first device fills the only cap slot.
	code, out := postRequestApproval(t, srv, spamApprovalPayload(examID, "AA:BB:CC:DD:EE:C0"), examToken)
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("first device status=%d out=%v, want approved", code, out)
	}

	// The spammer hammers reset=true from a second MAC — the cap holds.
	mac := "AA:BB:CC:DD:EE:C1"
	for i := 0; i < 3; i++ {
		code, out := postRequestApproval(t, srv, spamApprovalPayload(examID, mac), examToken)
		if code != http.StatusOK || out["status"] != "pending" {
			t.Fatalf("retry %d status=%d out=%v, want pending (cap blocks reset)", i, code, out)
		}
	}
	if got := countSubmissions(t, pool, examID, mac); got != 0 {
		t.Errorf("submissions = %d, want 0 — reset spam cannot mint approved rows", got)
	}
}

// A cap of 0 disables the brake entirely (operator-visible escape hatch for
// exams larger than the default).
func TestRequestApprovalCapZeroDisablesBrake(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	setApprovalCap(t, pool, 0) // 0 = unlimited
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	for i := 0; i < 5; i++ {
		mac := fmt.Sprintf("AA:BB:CC:DD:EE:D%d", i)
		code, out := postRequestApproval(t, srv, spamApprovalPayload(examID, mac), examToken)
		if code != http.StatusOK || out["status"] != "approved" {
			t.Fatalf("device %s: status=%d out=%v, want approved with cap=0", mac, code, out)
		}
	}
}

// The cap counts APPROVED rows only. When a pengawas rejects an approved
// device (e.g. it was let in by a leaked token), the slot is freed and a
// waiting device may be auto-approved on its next retry — spam from one MAC
// does not permanently block a school's legitimate latecomers.
func TestRequestApprovalRejectedDeviceFreesCapSlot(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	setApprovalCap(t, pool, 1)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	// Device A takes the only slot.
	code, out := postRequestApproval(t, srv, spamApprovalPayload(examID, "AA:BB:CC:DD:EE:E1"), examToken)
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("device A status=%d out=%v, want approved", code, out)
	}
	// Device B is over the cap → pending.
	code, out = postRequestApproval(t, srv, spamApprovalPayload(examID, "AA:BB:CC:DD:EE:E2"), examToken)
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("device B status=%d out=%v, want pending (cap full)", code, out)
	}

	// The pengawas rejects device A — its approved row no longer counts.
	if _, err := pool.Exec(context.Background(),
		`UPDATE exam_approvals SET status = 'rejected' WHERE exam_id = $1 AND mac_address = $2`,
		examID, "AA:BB:CC:DD:EE:E1"); err != nil {
		t.Fatalf("reject device A: %v", err)
	}

	// Device B retries and now fits under the cap.
	code, out = postRequestApproval(t, srv, spamApprovalPayload(examID, "AA:BB:CC:DD:EE:E2"), examToken)
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("device B retry status=%d out=%v, want approved after slot freed", code, out)
	}
}

// A token-less spam wave (attacker guessing exam ids) must be rejected before
// any row exists: no pending queue pollution, no approved devices.
func TestRequestApprovalSpamWithoutTokenCreatesNoRows(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, _ := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	for i := 0; i < 5; i++ {
		mac := fmt.Sprintf("AA:BB:CC:DD:EE:F%d", i)
		code, _ := postRequestApproval(t, srv, spamApprovalPayload(examID, mac), "")
		if code != http.StatusUnauthorized {
			t.Fatalf("device %s without token: status=%d, want 401", mac, code)
		}
	}
	if got := countApprovalsByStatus(t, pool, examID, "pending"); got != 0 {
		t.Errorf("pending rows = %d, want 0 — token-less spam must not queue", got)
	}
	if got := countApprovalsByStatus(t, pool, examID, "approved"); got != 0 {
		t.Errorf("approved rows = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Unit tests for the Redis flood brake (checkRateLimit) — the per-exam bucket
// behind the request-approval rate limit. Uses miniredis (in-process, no
// external Redis needed).
// ---------------------------------------------------------------------------

func TestCheckRateLimitFloodBrake(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	const max int64 = 5
	const window = time.Minute

	// The first max requests are allowed; the next one trips the brake.
	for i := 0; i < int(max); i++ {
		if !checkRateLimit(rdb, "exam:1", max, window) {
			t.Fatalf("call %d rejected, want allowed", i+1)
		}
	}
	if checkRateLimit(rdb, "exam:1", max, window) {
		t.Fatal("request over the cap was allowed, want blocked")
	}

	// Buckets are per-key: an attacker spoofing one key cannot drain another
	// exam's bucket.
	if !checkRateLimit(rdb, "exam:2", max, window) {
		t.Fatal("fresh exam bucket was blocked — keys must be independent")
	}

	// The bucket resets once the window elapses.
	mr.FastForward(window + time.Second)
	if !checkRateLimit(rdb, "exam:1", max, window) {
		t.Fatal("bucket did not reset after the window elapsed")
	}

	// Nil Redis = open access (fail-open; the per-IP middleware still applies).
	if !checkRateLimit(nil, "exam:3", max, window) {
		t.Fatal("nil redis must not block")
	}
}

// ---------------------------------------------------------------------------
// Handler wiring with Redis present: the per-exam flood brake must not break
// the happy path, and it must populate its bucket key.
//
// NOTE: the 429 branch itself (bucket exceeded) is not exercised here — the
// production bucket is 12000 req/min, impractical to trip over HTTP. The
// boundary semantics are covered directly by TestCheckRateLimitFloodBrake and
// the per-IP middleware path by middleware/ratelimit_test.go.
// ---------------------------------------------------------------------------

// newRequestApprovalRedisRouter mirrors production wiring with a real (but
// in-process) Redis client injected, so the per-exam rate-limit branch runs.
func newRequestApprovalRedisRouter(pool *pgxpool.Pool, rdb *redis.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("redis", rdb)
	})
	r.POST("/api/exams/request-approval", RequestApproval())
	return r
}

func TestRequestApprovalWorksWithRedisPresent(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	srv := httptest.NewServer(newRequestApprovalRedisRouter(pool, rdb))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, spamApprovalPayload(examID, "AA:BB:CC:DD:EE:R1"), examToken)
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("status=%d out=%v, want 200 + approved with Redis present", code, out)
	}

	// The per-exam flood-brake bucket was populated by the request.
	if !mr.Exists("ratelimit:reqapp-exam:" + strconv.Itoa(examID)) {
		t.Error("per-exam rate-limit key was not created in Redis")
	}
}
