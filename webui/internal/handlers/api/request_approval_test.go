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
// returns the exam ID and its token. active/started control whether the exam
// is joinable; autoApprove sets the server-side flag under test.
func createRequestApprovalFixture(t *testing.T, pool *pgxpool.Pool, active, started, autoApprove bool) (int, string) {
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

	token := fmt.Sprintf("T%07d", time.Now().UnixNano()%10000000)

	var id int
	insertSQL := fmt.Sprintf(`
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by, exam_started_at, auto_approve)
		VALUES ('Ujian AutoApprove', 'ujian.pdf', 2048, $1, $1, $2, 'medium', $3, %s, $4)
		RETURNING id`, startedSQL)
	if err := pool.QueryRow(ctx, insertSQL,
		token, status, owner.ID, autoApprove).Scan(&id); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return id, token
}

// postRequestApproval posts a request-approval payload and decodes the JSON
// response, returning the HTTP status and body. The exam token is injected
// into the payload (the server requires it — anti-spam); pass an empty or
// wrong value to exercise the rejection paths.
func postRequestApproval(t *testing.T, srv *httptest.Server, payload map[string]interface{}, token string) (int, map[string]interface{}) {
	t.Helper()
	payload["token"] = token
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
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, false)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:01",
		"student_name": "Budi", "exam_number": "01", "student_class": "XII A",
		"identity_data": map[string]interface{}{"student_name": "Budi"}, "reset": true,
	}, examToken)
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
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, false)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	payload := map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:10",
		"student_name": "Hana", "exam_number": "10", "student_class": "XI D",
		"identity_data": map[string]interface{}{}, "reset": true,
	}

	// Initial request queues the device.
	code, out := postRequestApproval(t, srv, payload, examToken)
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("initial status=%d out=%v, want 200 + pending", code, out)
	}

	// Repeated polls must neither approve nor reject — the row stays pending.
	for i := 0; i < 2; i++ {
		payload["reset"] = false
		code, out := postRequestApproval(t, srv, payload, examToken)
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
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:02",
		"student_name": "Siti", "exam_number": "02", "student_class": "XI B",
		"identity_data": map[string]interface{}{"student_name": "Siti"}, "reset": true,
	}, examToken)
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
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	payload := map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:03",
		"student_name": "Andi", "exam_number": "03", "student_class": "X A",
		"identity_data": map[string]interface{}{"student_name": "Andi"}, "reset": true,
	}
	for i := 0; i < 3; i++ {
		code, out := postRequestApproval(t, srv, payload, examToken)
		if code != http.StatusOK || out["status"] != "approved" {
			t.Fatalf("poll %d: status=%d out=%v, want 200 + approved", i, code, out)
		}
		payload["reset"] = false
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:03"); got != 1 {
		t.Errorf("submissions = %d, want exactly 1 after repeated polls", got)
	}
}

// Only live exams accept approval requests: a device requesting access to an
// inactive (stopped) exam is rejected outright — the old behaviour of dropping
// the request into the pending queue just left dead rows nobody could act on.
// Auto-approve would never fire either (it requires the exam to be live).
func TestRequestApprovalRejectsInactiveExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, false, false, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:04",
		"student_name": "Dewi", "exam_number": "04", "student_class": "XII C",
		"identity_data": map[string]interface{}{}, "reset": true,
	}, examToken)
	if code != http.StatusForbidden {
		t.Fatalf("status=%d out=%v, want 403 for inactive exam", code, out)
	}
	if msg, _ := out["message"].(string); msg == "" {
		t.Errorf("message = %q, want a non-empty explanation", msg)
	}
	// No approval row may be created for an inactive exam.
	var rows int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
		examID, "AA:BB:CC:DD:EE:04").Scan(&rows); err != nil {
		t.Fatalf("count approvals: %v", err)
	}
	if rows != 0 {
		t.Errorf("approvals = %d, want 0 — no row may be created for an inactive exam", rows)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:04"); got != 0 {
		t.Errorf("submissions = %d, want 0 for inactive exam", got)
	}
}

// Auto-approve must respect an explicit pengawas rejection: while the flag is
// on, a rejected device stays rejected on polls (reset=false); it only gets in
// by actively retrying (reset=true — the client's "Minta Izin Lagi").
func TestRequestApprovalAutoApproveRespectsRejection(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
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
	code, out := postRequestApproval(t, srv, payload(true), examToken)
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
		code, out := postRequestApproval(t, srv, payload(false), examToken)
		if code != http.StatusOK || out["status"] != "rejected" {
			t.Fatalf("poll %d status=%d out=%v, want rejected preserved", i, code, out)
		}
	}

	// An active retry (reset=true) is auto-approved again.
	code, out = postRequestApproval(t, srv, payload(true), examToken)
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
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, false) // flag off initially
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	payload := map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:07",
		"student_name": "Eko", "exam_number": "07", "student_class": "X C",
		"identity_data": map[string]interface{}{}, "reset": true,
	}

	// First request while the flag is OFF → lands in the pending queue.
	code, out := postRequestApproval(t, srv, payload, examToken)
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
	code, out = postRequestApproval(t, srv, payload, examToken)
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("poll status=%d out=%v, want approved after flag enabled", code, out)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:07"); got != 1 {
		t.Errorf("submissions = %d, want 1 after unstick", got)
	}
}

// An exam whose schedule has ended (end_time + 60s grace) is rejected like an
// inactive one: a device requesting access after the deadline must not land in
// the pending queue — nobody can act on it, and auto-approve would never fire.
func TestRequestApprovalRejectsEndedExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	// Force the schedule into the past (beyond the grace window).
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
	}, examToken)
	if code != http.StatusForbidden {
		t.Fatalf("status=%d out=%v, want 403 for ended exam", code, out)
	}
	var rows int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
		examID, "AA:BB:CC:DD:EE:08").Scan(&rows); err != nil {
		t.Fatalf("count approvals: %v", err)
	}
	if rows != 0 {
		t.Errorf("approvals = %d, want 0 — no row may be created for an ended exam", rows)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:08"); got != 0 {
		t.Errorf("submissions = %d, want 0 for ended exam", got)
	}
}

// The grace window still applies: a request within 60s after end_time is a
// legitimate borderline join (same tolerance as SubmitExam), so it is accepted
// into the pending queue instead of being rejected.
func TestRequestApprovalGraceWindowStillAccepted(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, false)
	// end_time 30s in the past — inside the 60s grace window.
	if _, err := pool.Exec(context.Background(),
		`UPDATE exams SET end_time = $1 WHERE id = $2`,
		time.Now().UTC().Add(-30*time.Second), examID); err != nil {
		t.Fatalf("set end_time: %v", err)
	}
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:09",
		"student_name": "Grace", "exam_number": "09", "student_class": "X E",
		"identity_data": map[string]interface{}{}, "reset": true,
	}, examToken)
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("status=%d out=%v, want 200 + pending within grace window", code, out)
	}
}

// Concurrent approval bookkeeping must never create duplicate submission rows:
// the per-device advisory lock serialises racing requests, so exactly one open
// row survives even when many requests land at the same instant.
func TestRequestApprovalAutoApproveConcurrentNoDuplicate(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	mac := "AA:BB:CC:DD:EE:09"
	payload := map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Gita", "exam_number": "09", "student_class": "X E",
		"identity_data": map[string]interface{}{}, "reset": true,
		"token":     examToken,
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
	}, "")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown exam", code)
	}
}

// Anti-spam: request-approval is the gate that (with auto-approve on) grants
// submit access, so it must require the exam token. A caller without it must
// be able to neither queue nor self-approve.
func TestRequestApprovalRequiresToken(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, _ := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:F1",
		"student_name": "Tanpa Token", "exam_number": "F1", "student_class": "X F",
		"identity_data": map[string]interface{}{}, "reset": true,
	}, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("status=%d out=%v, want 401 without token", code, out)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:F1"); got != 0 {
		t.Errorf("submissions = %d, want 0 — no row may be created without a token", got)
	}
	var rows int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
		examID, "AA:BB:CC:DD:EE:F1").Scan(&rows); err != nil {
		t.Fatalf("count approvals: %v", err)
	}
	if rows != 0 {
		t.Errorf("approvals = %d, want 0 — no approval row may be created without a token", rows)
	}
}

// A wrong token is rejected the same way as a missing one.
func TestRequestApprovalRejectsWrongToken(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:F2",
		"student_name": "Token Salah", "exam_number": "F2", "student_class": "X F",
		"identity_data": map[string]interface{}{}, "reset": true,
	}, "WRONG"+examToken)
	if code != http.StatusUnauthorized {
		t.Fatalf("status=%d out=%v, want 401 for wrong token", code, out)
	}
}

// Cap: when max_approvals_per_exam is reached, further devices fall back to
// the pending queue instead of being auto-approved; already-approved devices
// are never demoted by the cap.
func TestRequestApprovalCapLimitsAutoApprovedDevices(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	if err := models.SetSaasSetting(context.Background(), pool, models.SettingMaxApprovalsPerExam, "2"); err != nil {
		t.Fatalf("set cap: %v", err)
	}
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	// Two devices fit under the cap.
	for _, mac := range []string{"AA:BB:CC:DD:EE:F3", "AA:BB:CC:DD:EE:F4"} {
		code, out := postRequestApproval(t, srv, map[string]interface{}{
			"exam_id": examID, "mac_address": mac,
			"student_name": "Siswa Cap", "exam_number": "F3", "student_class": "X F",
			"identity_data": map[string]interface{}{}, "reset": true,
		}, examToken)
		if code != http.StatusOK || out["status"] != "approved" {
			t.Fatalf("device %s: status=%d out=%v, want approved", mac, code, out)
		}
	}

	// The third device must land in the pending queue, not be auto-approved.
	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:F5",
		"student_name": "Siswa Cap", "exam_number": "F3", "student_class": "X F",
		"identity_data": map[string]interface{}{}, "reset": true,
	}, examToken)
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("over-cap status=%d out=%v, want pending", code, out)
	}
	if got := countSubmissions(t, pool, examID, "AA:BB:CC:DD:EE:F5"); got != 0 {
		t.Errorf("submissions = %d, want 0 for over-cap pending device", got)
	}

	// An already-approved device retrying (reset=true) is NOT demoted by the cap.
	code, out = postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:F3",
		"student_name": "Siswa Cap", "exam_number": "F3", "student_class": "X F",
		"identity_data": map[string]interface{}{}, "reset": true,
	}, examToken)
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("already-approved retry status=%d out=%v, want approved preserved", code, out)
	}
}

// Stale-token tolerance: a device that already has an approval row may keep
// polling with an outdated token (dynamic-token rotation mid-wait) — the same
// tolerance SubmitExam/AccessLog apply to known devices.
func TestRequestApprovalKnownDeviceToleratesStaleToken(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, examToken := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	// Device joins with the current token → auto-approved.
	code, out := postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:F6",
		"student_name": "Stale", "exam_number": "F6", "student_class": "X F",
		"identity_data": map[string]interface{}{}, "reset": true,
	}, examToken)
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("join status=%d out=%v, want approved", code, out)
	}

	// The token rotates; the device's next poll carries the OLD token and must
	// still be tolerated (the row exists, so it is a known device).
	code, out = postRequestApproval(t, srv, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:DD:EE:F6",
		"student_name": "Stale", "exam_number": "F6", "student_class": "X F",
		"identity_data": map[string]interface{}{}, "reset": false,
	}, "OLD"+examToken)
	if code != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("stale-token poll status=%d out=%v, want approved preserved", code, out)
	}
}
