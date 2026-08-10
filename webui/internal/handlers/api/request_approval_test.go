package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Integration tests: POST /api/exams/request-approval — server-side auto-approve
// ---------------------------------------------------------------------------
//
// The exam.auto_approve flag lives in the DB, so RequestApproval must approve
// a requesting device immediately (and create its monitoring row) while the
// flag is on — even with no pengawas monitoring page open — and keep the old
// pending-queue behaviour when it is off.

// newRequestApprovalRouter mirrors the production wiring for the public
// request-approval endpoint: sessions + "db" injected, then the handler.
func newRequestApprovalRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("db", pool) })
	r.POST("/api/exams/request-approval", RequestApproval())
	return r
}

// createRequestApprovalFixture creates an owner account plus one exam and
// returns the exam ID. active/started control whether the exam is joinable;
// autoApprove sets the server-side flag under test.
func createRequestApprovalFixture(t *testing.T, pool *pgxpool.Pool, active, started, autoApprove bool) int {
	t.Helper()
	ctx := context.Background()

	owner, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "reqapp-guru", Name: "Guru RequestApproval",
		PasswordHash: "pass", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	status := "inactive"
	if active {
		status = "active"
	}
	startedSQL := "NULL"
	if started {
		startedSQL = "CURRENT_TIMESTAMP"
	}

	var id int
	insertSQL := fmt.Sprintf(`
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by, exam_started_at, auto_approve)
		VALUES ('Ujian AutoApprove', 'ujian.pdf', 2048, $1, $1, $2, 'medium', $3, %s, $4)
		RETURNING id`, startedSQL)
	if err := pool.QueryRow(ctx, insertSQL,
		fmt.Sprintf("T%07d", time.Now().UnixNano()%10000000), status, owner.ID, autoApprove).Scan(&id); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return id
}

// postRequestApproval posts a request-approval payload and decodes the JSON
// response, returning the HTTP status and body.
func postRequestApproval(t *testing.T, srv *httptest.Server, payload map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	resp, err := http.Post(srv.URL+"/api/exams/request-approval", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST request-approval: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, out
}

func countSubmissions(t *testing.T, pool *pgxpool.Pool, examID int, macAddress string) int {
	t.Helper()
	var cnt int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND mac_address = $2`,
		examID, macAddress).Scan(&cnt); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	return cnt
}

// Flag off: the device must land in the pending queue and no monitoring row
// may be created — that is the pengawas' manual decision.
func TestRequestApprovalAutoApproveDisabled(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID := createRequestApprovalFixture(t, pool, true, true, false)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:01",
		"student_name": "Budi", "exam_number": "01", "student_class": "XII A",
		"identity_data": map[string]interface{}{"student_name": "Budi"}, "reset": true,
	})
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("status=%d out=%v, want 200 + pending", code, out)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:01"); got != 0 {
		t.Errorf("submissions = %d, want 0 while device is only pending", got)
	}
}

// While the flag is OFF, a poll (reset=false) against an existing pending row
// must keep it pending — polling never decides anything by itself, only a
// pengawas (or the auto-approve flag) may move the row out of pending.
func TestRequestApprovalPollKeepsPendingWhenFlagOff(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID := createRequestApprovalFixture(t, pool, true, true, false)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	payload := map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:10",
		"student_name": "Hana", "exam_number": "10", "student_class": "XI D",
		"identity_data": map[string]interface{}{}, "reset": true,
	}

	// Initial request queues the device.
	code, out := postRequestApproval(t, srv, payload)
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("initial status=%d out=%v, want 200 + pending", code, out)
	}

	// Repeated polls must neither approve nor reject — the row stays pending.
	for i := 0; i < 2; i++ {
		payload["reset"] = false
		code, out := postRequestApproval(t, srv, payload)
		if code != http.StatusOK || out["status"] != "pending" {
			t.Fatalf("poll %d status=%d out=%v, want 200 + pending preserved", i, code, out)
		}
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:10"); got != 0 {
		t.Errorf("submissions = %d, want 0 — polling alone must not create monitoring rows", got)
	}
}

// Flag on: a live exam approves the device instantly and the device appears in
// the monitoring table via its fresh submission row.
func TestRequestApprovalAutoApproveEnabled(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:02",
		"student_name": "Siti", "exam_number": "02", "student_class": "XI B",
		"identity_data": map[string]interface{}{"student_name": "Siti"}, "reset": true,
	})
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("status=%d out=%v, want 200 + approved", code, out)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:02"); got != 1 {
		t.Errorf("submissions = %d, want 1 after auto-approve", got)
	}
}

// The Android client polls request-approval every 5s while waiting; repeated
// polls must never duplicate the monitoring row (EnsureFreshSubmissionOnApproval
// is idempotent).
func TestRequestApprovalAutoApprovePollIdempotent(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	payload := map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:03",
		"student_name": "Andi", "exam_number": "03", "student_class": "X A",
		"identity_data": map[string]interface{}{"student_name": "Andi"}, "reset": true,
	}
	for i := 0; i < 3; i++ {
		code, out := postRequestApproval(t, srv, payload)
		if code != http.StatusOK || out["status"] != "approved" {
			t.Fatalf("poll %d: status=%d out=%v, want 200 + approved", i, code, out)
		}
		payload["reset"] = false
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:03"); got != 1 {
		t.Errorf("submissions = %d, want exactly 1 after repeated polls", got)
	}
}

// Auto-approve must only apply while the exam is live. A request against a
// dormant (inactive) exam stays in the pending queue.
func TestRequestApprovalAutoApproveInactiveExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID := createRequestApprovalFixture(t, pool, false, false, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:04",
		"student_name": "Dewi", "exam_number": "04", "student_class": "XII C",
		"identity_data": map[string]interface{}{}, "reset": true,
	})
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("status=%d out=%v, want 200 + pending for inactive exam", code, out)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:04"); got != 0 {
		t.Errorf("submissions = %d, want 0 for dormant exam", got)
	}
}

// Auto-approve must respect an explicit pengawas rejection: while the flag is
// on, a rejected device stays rejected on polls (reset=false); it only gets in
// by actively retrying (reset=true — the client's "Minta Izin Lagi").
func TestRequestApprovalAutoApproveRespectsRejection(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	payload := func(reset bool) map[string]interface{} {
		return map[string]interface{}{
			"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:06",
			"student_name": "Rina", "exam_number": "06", "student_class": "X B",
			"identity_data": map[string]interface{}{}, "reset": reset,
		}
	}

	// Initial request is auto-approved.
	code, out := postRequestApproval(t, srv, payload(true))
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("initial request status=%d out=%v, want approved", code, out)
	}

	// The pengawas rejects the device.
	if _, err := pool.Exec(context.Background(),
		`UPDATE exam_approvals SET status = 'rejected' WHERE exam_id = $1 AND mac_address = $2`,
		examID, "AA:BB:CC:DD:EE:06"); err != nil {
		t.Fatalf("reject device: %v", err)
	}

	// Polls must NOT flip the rejection back to approved.
	for i := 0; i < 2; i++ {
		code, out := postRequestApproval(t, srv, payload(false))
		if code != http.StatusOK || out["status"] != "rejected" {
			t.Fatalf("poll %d status=%d out=%v, want rejected preserved", i, code, out)
		}
	}

	// An active retry (reset=true) is auto-approved again.
	code, out = postRequestApproval(t, srv, payload(true))
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("retry status=%d out=%v, want approved", code, out)
	}
}

// Auto-approve must also unstuck devices that queued BEFORE the flag was
// turned on: a plain poll (reset=false) against a leftover pending row flips
// it to approved — pending was never a manual decision, so flipping it is safe
// (unlike an explicit rejection).
func TestRequestApprovalAutoApproveUnsticksLeftoverPending(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID := createRequestApprovalFixture(t, pool, true, true, false) // flag off initially
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	payload := map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:07",
		"student_name": "Eko", "exam_number": "07", "student_class": "X C",
		"identity_data": map[string]interface{}{}, "reset": true,
	}

	// First request while the flag is OFF → lands in the pending queue.
	code, out := postRequestApproval(t, srv, payload)
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("initial status=%d out=%v, want pending (flag off)", code, out)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:07"); got != 0 {
		t.Fatalf("submissions = %d, want 0 while pending", got)
	}

	// The pengawas turns auto-approve ON while the device is still waiting.
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET auto_approve = TRUE WHERE id = $1`, examID); err != nil {
		t.Fatalf("enable auto-approve: %v", err)
	}

	// A plain poll (reset=false) must now approve the leftover pending row.
	payload["reset"] = false
	code, out = postRequestApproval(t, srv, payload)
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("poll status=%d out=%v, want approved after flag enabled", code, out)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:07"); got != 1 {
		t.Errorf("submissions = %d, want 1 after unstick", got)
	}
}

// Auto-approve must not grant access once the exam's end_time has passed —
// the schedule guard applies even when the flag is on.
func TestRequestApprovalAutoApproveExamEnded(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID := createRequestApprovalFixture(t, pool, true, true, true)
	// Force the schedule into the past.
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET end_time = $1 WHERE id = $2`,
		time.Now().UTC().Add(-2*time.Hour), examID); err != nil {
		t.Fatalf("set end_time: %v", err)
	}
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:08",
		"student_name": "Fajar", "exam_number": "08", "student_class": "X D",
		"identity_data": map[string]interface{}{}, "reset": true,
	})
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("status=%d out=%v, want pending for ended exam", code, out)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:08"); got != 0 {
		t.Errorf("submissions = %d, want 0 for ended exam", got)
	}
}

// Concurrent approval bookkeeping must never create duplicate submission rows:
// the per-device advisory lock serialises racing requests, so exactly one open
// row survives even when many requests land at the same instant.
func TestRequestApprovalAutoApproveConcurrentNoDuplicate(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	mac := "AA:BB:CC:DD:EE:09"
	payload := map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Gita", "exam_number": "09", "student_class": "X E",
		"identity_data": map[string]interface{}{}, "reset": true,
	}

	const workers = 8
	client := &http.Client{Timeout: 10 * time.Second}
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := json.Marshal(payload)
			if err != nil {
				errs <- err
				return
			}
			resp, err := client.Post(srv.URL+"/api/exams/request-approval", "application/json", bytes.NewReader(body))
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			var out map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				errs <- err
				return
			}
			if out["status"] != "approved" {
				errs <- fmt.Errorf("status = %v, want approved", out["status"])
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent request: %v", err)
	}

	if got := countSubmissions(t, pool, examID, mac); got != 1 {
		t.Errorf("submissions = %d, want exactly 1 (no duplicates from race)", got)
	}
}

// Requesting approval for an unknown exam must 404 (previously it surfaced a
// 500 via the FK constraint — the explicit exam lookup makes it clean).
func TestRequestApprovalUnknownExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, _ := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": 999999, "mac_address": "AA:BB:CC:DD:EE:05",
		"student_name": "X", "exam_number": "05", "student_class": "X A",
		"identity_data": map[string]interface{}{}, "reset": true,
	})
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown exam", code)
	}
}
